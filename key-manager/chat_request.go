package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const defaultMaxBody = 32 << 20

type rawObject = map[string]json.RawMessage

type chatRequest struct {
	fields                           rawObject
	stream, includeUsage, continuous bool
	n                                int64
	maxTokens                        *int64
}

func apiError(w http.ResponseWriter, status int, code, param, message string) {
	kind := "invalid_request_error"
	switch status {
	case 401:
		kind = "authentication_error"
	case 429:
		kind = "rate_limit_error"
	case 502, 503:
		kind = "server_error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"type": kind, "code": code, "param": param, "message": message}})
}

func decodeField(fields rawObject, name string, dest any) error {
	if raw := fields[name]; raw != nil {
		return json.Unmarshal(raw, dest)
	}
	return nil
}

func readChat(w http.ResponseWriter, r *http.Request, maxBody int64) (*chatRequest, bool) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		apiError(w, 413, "request_too_large", "body", fmt.Sprintf("Chat request exceeds the %d byte limit.", maxBody))
		return nil, false
	}
	c := &chatRequest{n: 1}
	if err != nil || json.Unmarshal(data, &c.fields) != nil || c.fields == nil {
		apiError(w, 400, "invalid_json", "body", "Expected one JSON object.")
		return nil, false
	}
	// Route, worker and server-side extension settings belong to the operator.
	for _, field := range []string{
		"bootstrap_host", "bootstrap_port", "bootstrap_room", "routed_dp_rank",
		"disagg_prefill_dp_rank", "data_parallel_rank", "lora_path", "custom_logit_processor",
		"custom_params", "session_id", "session_params", "extra_key", "rid", "priority",
	} {
		if _, present := c.fields[field]; present {
			apiError(w, 400, "unsupported_parameter", field, "Server-side setting: "+field)
			return nil, false
		}
	}
	for _, field := range []string{"max_tokens", "max_completion_tokens"} {
		var v *int64
		if decodeField(c.fields, field, &v) != nil || (v != nil && *v < 0) {
			apiError(w, 400, "invalid_parameter", field, "Expected a non-negative token limit.")
			return nil, false
		}
		// SGLang prefers a positive max_completion_tokens over max_tokens.
		if v != nil && (field == "max_tokens" || *v > 0) {
			c.maxTokens = v
		}
	}
	if decodeField(c.fields, "n", &c.n) != nil || c.n < 1 {
		apiError(w, 400, "invalid_parameter", "n", "Expected a positive number of choices.")
		return nil, false
	}
	if decodeField(c.fields, "stream", &c.stream) != nil {
		apiError(w, 400, "invalid_parameter", "stream", "Expected a boolean.")
		return nil, false
	}
	if c.stream {
		opts := rawObject{}
		if decodeField(c.fields, "stream_options", &opts) != nil || decodeField(opts, "include_usage", &c.includeUsage) != nil || decodeField(opts, "continuous_usage_stats", &c.continuous) != nil {
			apiError(w, 400, "invalid_parameter", "stream_options", "Invalid stream options.")
			return nil, false
		}
		if opts == nil {
			opts = rawObject{}
		}
		opts["include_usage"] = json.RawMessage("true")
		opts["continuous_usage_stats"] = json.RawMessage("true")
		c.fields["stream_options"], _ = json.Marshal(opts)
	}
	return c, true
}

func (g *gateway) call(r *http.Request, path string, fields rawObject) (*http.Response, error) {
	method := http.MethodGet
	var body []byte
	if fields != nil {
		method = http.MethodPost
		var err error
		body, err = json.Marshal(fields)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(r.Context(), method, g.upstream+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func copyResponse(w http.ResponseWriter, resp *http.Response) {
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	if delay := resp.Header.Get("Retry-After"); delay != "" {
		w.Header().Set("Retry-After", delay)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// Use the same chat template and options as generation. The tokenize endpoint
// also accepts raw-prompt controls which have no meaning on the chat endpoint.
func (g *gateway) promptTokens(w http.ResponseWriter, r *http.Request, c *chatRequest) (int64, bool) {
	fields := make(rawObject, len(c.fields))
	for k, v := range c.fields {
		switch k {
		case "stream", "stream_options", "prompt", "add_generation_prompt", "add_special_tokens":
			continue
		}
		fields[k] = v
	}
	resp, err := g.call(r, "/v1/tokenize", fields)
	if err != nil {
		apiError(w, 502, "upstream_unavailable", "", "Model tokenizer unavailable.")
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		copyResponse(w, resp)
		return 0, false
	}
	var result struct{ Count *int64 }
	if json.NewDecoder(resp.Body).Decode(&result) != nil || result.Count == nil || *result.Count < 0 {
		apiError(w, 502, "invalid_upstream_response", "", "Model tokenizer returned no token count.")
		return 0, false
	}
	return *result.Count, true
}

// /v1/models reports the actual serving context. The tokenizer may advertise
// a different model_max_length, based on its tokenizer configuration.
func (g *gateway) modelContextLength(w http.ResponseWriter, r *http.Request, c *chatRequest) (int64, bool) {
	resp, err := g.call(r, "/v1/models", nil)
	if err != nil {
		apiError(w, 502, "upstream_unavailable", "", "Model service unavailable.")
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		copyResponse(w, resp)
		return 0, false
	}
	var models struct {
		Data []struct {
			ID     string
			Length int64 `json:"max_model_len"`
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&models); err == nil && len(models.Data) > 0 {
		var name string
		_ = decodeField(c.fields, "model", &name)
		if name == "" || name == "default" || len(models.Data) == 1 {
			name = models.Data[0].ID
		}
		for _, m := range models.Data {
			if m.ID == name && m.Length > 0 {
				return m.Length, true
			}
		}
	}
	apiError(w, 502, "invalid_upstream_response", "", "Missing serving context length for the requested model.")
	return 0, false
}

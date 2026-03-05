package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type GeminiClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewGeminiClient(baseURL, apiKey string, timeout time.Duration) *GeminiClient {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &GeminiClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *GeminiClient) Chat(ctx context.Context, req ChatRequest) (ChatResult, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = "gemini-2.0-flash"
	}
	model = strings.TrimPrefix(model, "models/")

	systemPrompt := make([]string, 0, 2)
	type geminiPart struct {
		Text string `json:"text"`
	}
	type geminiContent struct {
		Role  string       `json:"role"`
		Parts []geminiPart `json:"parts"`
	}
	contents := make([]geminiContent, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		text := strings.TrimSpace(m.Content)
		if role == "system" {
			if text != "" {
				systemPrompt = append(systemPrompt, text)
			}
			continue
		}
		targetRole := "user"
		if role == "assistant" {
			targetRole = "model"
		}
		contents = append(contents, geminiContent{
			Role:  targetRole,
			Parts: []geminiPart{{Text: text}},
		})
	}
	if len(contents) == 0 {
		return ChatResult{}, ErrUpstreamResponse
	}

	payload := map[string]any{
		"contents": contents,
	}
	if len(systemPrompt) > 0 {
		payload["system_instruction"] = map[string]any{
			"parts": []map[string]string{{"text": strings.Join(systemPrompt, "\n\n")}},
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ChatResult{}, err
	}

	route := "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
	if strings.HasSuffix(c.baseURL, "/v1beta") || strings.HasSuffix(c.baseURL, "/v1") {
		route = "/models/" + url.PathEscape(model) + ":generateContent"
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+route, bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ChatResult{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResult{}, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return ChatResult{}, fmt.Errorf("gemini upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed struct {
		ModelVersion string `json:"modelVersion"`
		Candidates   []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ChatResult{}, err
	}
	if len(parsed.Candidates) == 0 {
		return ChatResult{}, ErrUpstreamResponse
	}
	parts := parsed.Candidates[0].Content.Parts
	if len(parts) == 0 {
		return ChatResult{}, ErrUpstreamResponse
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p.Text) != "" {
			out = append(out, p.Text)
		}
	}
	if len(out) == 0 {
		return ChatResult{}, ErrUpstreamResponse
	}
	resModel := strings.TrimSpace(parsed.ModelVersion)
	if resModel == "" {
		resModel = model
	}
	return ChatResult{
		Model:            "gemini/" + resModel,
		Content:          strings.Join(out, "\n"),
		PromptTokens:     parsed.UsageMetadata.PromptTokenCount,
		CompletionTokens: parsed.UsageMetadata.CandidatesTokenCount,
	}, nil
}

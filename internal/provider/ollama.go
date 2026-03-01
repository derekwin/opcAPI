package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OllamaClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewOllamaClient(baseURL string, timeout time.Duration) *OllamaClient {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &OllamaClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *OllamaClient) Chat(ctx context.Context, req ChatRequest) (ChatResult, error) {
	model := req.Model
	if strings.TrimSpace(model) == "" {
		model = "llama3.1"
	}

	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	messages := make([]msg, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, msg{Role: m.Role, Content: m.Content})
	}

	body, err := json.Marshal(map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	})
	if err != nil {
		return ChatResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

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
		return ChatResult{}, fmt.Errorf("ollama upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed struct {
		Model   string `json:"model"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount int `json:"prompt_eval_count"`
		EvalCount       int `json:"eval_count"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ChatResult{}, err
	}
	if strings.TrimSpace(parsed.Message.Content) == "" {
		return ChatResult{}, ErrUpstreamResponse
	}

	resModel := parsed.Model
	if resModel == "" {
		resModel = model
	}

	return ChatResult{
		Model:            "ollama/" + resModel,
		Content:          parsed.Message.Content,
		PromptTokens:     parsed.PromptEvalCount,
		CompletionTokens: parsed.EvalCount,
	}, nil
}

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

type OpenAIClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewOpenAIClient(baseURL, apiKey string, timeout time.Duration) *OpenAIClient {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &OpenAIClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *OpenAIClient) Chat(ctx context.Context, req ChatRequest) (ChatResult, error) {
	model := req.Model
	if strings.TrimSpace(model) == "" {
		model = "gpt-4o-mini"
	}

	messages := make([]map[string]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, map[string]string{
			"role":    m.Role,
			"content": m.Content,
		})
	}

	body, err := json.Marshal(map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	})
	if err != nil {
		return ChatResult{}, err
	}

	route := "/v1/chat/completions"
	if strings.HasSuffix(c.baseURL, "/v1") {
		route = "/chat/completions"
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+route, bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
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
		return ChatResult{}, fmt.Errorf("openai upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ChatResult{}, err
	}
	if len(parsed.Choices) == 0 {
		return ChatResult{}, ErrUpstreamResponse
	}

	resModel := parsed.Model
	if resModel == "" {
		resModel = model
	}

	return ChatResult{
		Model:            "openai/" + resModel,
		Content:          parsed.Choices[0].Message.Content,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
	}, nil
}

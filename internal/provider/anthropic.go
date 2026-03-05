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

type AnthropicClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewAnthropicClient(baseURL, apiKey string, timeout time.Duration) *AnthropicClient {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &AnthropicClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *AnthropicClient) Chat(ctx context.Context, req ChatRequest) (ChatResult, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = "claude-3-5-haiku-latest"
	}

	systemPrompt := make([]string, 0, 2)
	type anthropicMessage struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	messages := make([]anthropicMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		content := strings.TrimSpace(m.Content)
		if role == "system" {
			if content != "" {
				systemPrompt = append(systemPrompt, content)
			}
			continue
		}
		targetRole := "user"
		if role == "assistant" {
			targetRole = "assistant"
		}
		block := struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{
			Type: "text",
			Text: content,
		}
		messages = append(messages, anthropicMessage{
			Role: targetRole,
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{block},
		})
	}
	if len(messages) == 0 {
		return ChatResult{}, ErrUpstreamResponse
	}

	bodyPayload := map[string]any{
		"model":      model,
		"messages":   messages,
		"max_tokens": 1024,
	}
	if len(systemPrompt) > 0 {
		bodyPayload["system"] = strings.Join(systemPrompt, "\n\n")
	}
	body, err := json.Marshal(bodyPayload)
	if err != nil {
		return ChatResult{}, err
	}

	route := "/v1/messages"
	if strings.HasSuffix(c.baseURL, "/v1") {
		route = "/messages"
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+route, bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if c.apiKey != "" {
		httpReq.Header.Set("x-api-key", c.apiKey)
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
		return ChatResult{}, fmt.Errorf("anthropic upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed struct {
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ChatResult{}, err
	}
	if len(parsed.Content) == 0 {
		return ChatResult{}, ErrUpstreamResponse
	}
	parts := make([]string, 0, len(parsed.Content))
	for _, item := range parsed.Content {
		if strings.TrimSpace(item.Text) != "" {
			parts = append(parts, item.Text)
		}
	}
	if len(parts) == 0 {
		return ChatResult{}, ErrUpstreamResponse
	}
	resModel := strings.TrimSpace(parsed.Model)
	if resModel == "" {
		resModel = model
	}
	return ChatResult{
		Model:            "anthropic/" + resModel,
		Content:          strings.Join(parts, "\n"),
		PromptTokens:     parsed.Usage.InputTokens,
		CompletionTokens: parsed.Usage.OutputTokens,
	}, nil
}

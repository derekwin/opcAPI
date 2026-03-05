package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAnthropicClientChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "anth-key" {
			t.Fatalf("unexpected api key: %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Fatalf("missing anthropic-version header")
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["model"] != "claude-3-5-haiku-latest" {
			t.Fatalf("unexpected model: %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "model":"claude-3-5-haiku-latest",
  "content":[{"type":"text","text":"hello from anthropic"}],
  "usage":{"input_tokens":9,"output_tokens":5}
}`))
	}))
	defer server.Close()

	client := NewAnthropicClient(server.URL, "anth-key", 3*time.Second)
	res, err := client.Chat(context.Background(), ChatRequest{
		Model: "claude-3-5-haiku-latest",
		Messages: []Message{
			{Role: "system", Content: "you are assistant"},
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if res.Model != "anthropic/claude-3-5-haiku-latest" {
		t.Fatalf("unexpected model: %s", res.Model)
	}
	if res.Content != "hello from anthropic" {
		t.Fatalf("unexpected content: %s", res.Content)
	}
	if res.PromptTokens != 9 || res.CompletionTokens != 5 {
		t.Fatalf("unexpected usage: %+v", res)
	}
}

func TestAnthropicClientChatUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "anthropic failed", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewAnthropicClient(server.URL, "anth-key", 3*time.Second)
	_, err := client.Chat(context.Background(), ChatRequest{
		Model: "claude-3-5-haiku-latest",
		Messages: []Message{{
			Role:    "user",
			Content: "hello",
		}},
	})
	if err == nil {
		t.Fatalf("expected upstream error")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("unexpected error: %v", err)
	}
}

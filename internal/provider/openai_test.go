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

func TestOpenAIClientChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["model"] != "gpt-4o-mini" {
			t.Fatalf("unexpected model: %v", body["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "model":"gpt-4o-mini",
  "choices":[{"message":{"content":"hello from openai"}}],
  "usage":{"prompt_tokens":11,"completion_tokens":7}
}`))
	}))
	defer server.Close()

	client := NewOpenAIClient(server.URL, "test-key", 3*time.Second)
	res, err := client.Chat(context.Background(), ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []Message{{
			Role:    "user",
			Content: "hello",
		}},
	})
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}

	if res.Model != "openai/gpt-4o-mini" {
		t.Fatalf("unexpected model: %s", res.Model)
	}
	if res.Content != "hello from openai" {
		t.Fatalf("unexpected content: %s", res.Content)
	}
	if res.PromptTokens != 11 || res.CompletionTokens != 7 {
		t.Fatalf("unexpected token usage: %+v", res)
	}
}

func TestOpenAIClientChatBaseURLWithV1Suffix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "model":"gpt-4o-mini",
  "choices":[{"message":{"content":"ok"}}],
  "usage":{"prompt_tokens":1,"completion_tokens":1}
}`))
	}))
	defer server.Close()

	client := NewOpenAIClient(server.URL+"/v1", "test-key", 3*time.Second)
	_, err := client.Chat(context.Background(), ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []Message{{
			Role:    "user",
			Content: "hello",
		}},
	})
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
}

func TestOpenAIClientChatUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream failed", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewOpenAIClient(server.URL, "test-key", 3*time.Second)
	_, err := client.Chat(context.Background(), ChatRequest{
		Model: "gpt-4o-mini",
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

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

func TestOllamaClientChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["model"] != "qwen2.5:7b" {
			t.Fatalf("unexpected model: %v", body["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "model":"qwen2.5:7b",
  "message":{"role":"assistant","content":"hello from ollama"},
  "prompt_eval_count":13,
  "eval_count":8
}`))
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, 3*time.Second)
	res, err := client.Chat(context.Background(), ChatRequest{
		Model: "qwen2.5:7b",
		Messages: []Message{{
			Role:    "user",
			Content: "hello",
		}},
	})
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}

	if res.Model != "ollama/qwen2.5:7b" {
		t.Fatalf("unexpected model: %s", res.Model)
	}
	if res.Content != "hello from ollama" {
		t.Fatalf("unexpected content: %s", res.Content)
	}
	if res.PromptTokens != 13 || res.CompletionTokens != 8 {
		t.Fatalf("unexpected token usage: %+v", res)
	}
}

func TestOllamaClientChatUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "ollama failed", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, 3*time.Second)
	_, err := client.Chat(context.Background(), ChatRequest{
		Model: "qwen2.5:7b",
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

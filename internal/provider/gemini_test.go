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

func TestGeminiClientChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-2.0-flash:generateContent" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "gem-key" {
			t.Fatalf("unexpected api key header: %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		contents, ok := body["contents"].([]any)
		if !ok || len(contents) == 0 {
			t.Fatalf("missing contents: %#v", body["contents"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "modelVersion":"gemini-2.0-flash",
  "candidates":[{"content":{"parts":[{"text":"hello from gemini"}]}}],
  "usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":4}
}`))
	}))
	defer server.Close()

	client := NewGeminiClient(server.URL, "gem-key", 3*time.Second)
	res, err := client.Chat(context.Background(), ChatRequest{
		Model: "gemini-2.0-flash",
		Messages: []Message{
			{Role: "system", Content: "be concise"},
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if res.Model != "gemini/gemini-2.0-flash" {
		t.Fatalf("unexpected model: %s", res.Model)
	}
	if res.Content != "hello from gemini" {
		t.Fatalf("unexpected content: %s", res.Content)
	}
	if res.PromptTokens != 7 || res.CompletionTokens != 4 {
		t.Fatalf("unexpected usage: %+v", res)
	}
}

func TestGeminiClientChatUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gemini failed", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewGeminiClient(server.URL, "gem-key", 3*time.Second)
	_, err := client.Chat(context.Background(), ChatRequest{
		Model: "gemini-2.0-flash",
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

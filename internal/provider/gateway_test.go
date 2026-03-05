package provider

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/derekwin/opcAPI/internal/config"
)

func TestParseModel(t *testing.T) {
	tests := []struct {
		name            string
		model           string
		defaultProvider string
		wantProvider    string
		wantModel       string
		wantExplicit    bool
	}{
		{
			name:            "explicit openai",
			model:           "openai/gpt-4o-mini",
			defaultProvider: "stub",
			wantProvider:    "openai",
			wantModel:       "gpt-4o-mini",
			wantExplicit:    true,
		},
		{
			name:            "explicit ollama",
			model:           "ollama/qwen2.5:7b",
			defaultProvider: "openai",
			wantProvider:    "ollama",
			wantModel:       "qwen2.5:7b",
			wantExplicit:    true,
		},
		{
			name:            "explicit anthropic",
			model:           "anthropic/claude-3-5-haiku-latest",
			defaultProvider: "openai",
			wantProvider:    "anthropic",
			wantModel:       "claude-3-5-haiku-latest",
			wantExplicit:    true,
		},
		{
			name:            "explicit gemini",
			model:           "gemini/gemini-2.0-flash",
			defaultProvider: "openai",
			wantProvider:    "gemini",
			wantModel:       "gemini-2.0-flash",
			wantExplicit:    true,
		},
		{
			name:            "no prefix",
			model:           "gpt-4o-mini",
			defaultProvider: "openai",
			wantProvider:    "openai",
			wantModel:       "gpt-4o-mini",
			wantExplicit:    false,
		},
		{
			name:            "unknown prefix treated as model",
			model:           "kimi/moonshot-v1",
			defaultProvider: "stub",
			wantProvider:    "stub",
			wantModel:       "kimi/moonshot-v1",
			wantExplicit:    false,
		},
		{
			name:            "empty model",
			model:           "",
			defaultProvider: "ollama",
			wantProvider:    "ollama",
			wantModel:       "",
			wantExplicit:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			providerName, cleanModel, explicit := parseModel(tc.model, tc.defaultProvider)
			if providerName != tc.wantProvider {
				t.Fatalf("provider mismatch: got %s want %s", providerName, tc.wantProvider)
			}
			if cleanModel != tc.wantModel {
				t.Fatalf("model mismatch: got %s want %s", cleanModel, tc.wantModel)
			}
			if explicit != tc.wantExplicit {
				t.Fatalf("explicit mismatch: got %v want %v", explicit, tc.wantExplicit)
			}
		})
	}
}

func TestGatewayFallbackToStub(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "openai",
		ProviderTimeout: 3 * time.Second,
	}
	g := NewGateway(cfg, testLogger())

	res, err := g.Chat(context.Background(), ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []Message{{
			Role:    "user",
			Content: "hello",
		}},
	})
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if res.Content == "" {
		t.Fatalf("expected fallback content")
	}
}

func TestGatewayExplicitProviderNoFallback(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "stub",
		ProviderTimeout: 3 * time.Second,
	}
	g := NewGateway(cfg, testLogger())

	_, err := g.Chat(context.Background(), ChatRequest{
		Model: "openai/gpt-4o-mini",
		Messages: []Message{{
			Role:    "user",
			Content: "hello",
		}},
	})
	if err == nil {
		t.Fatalf("expected explicit provider request to fail without fallback")
	}
}

func TestGatewayModels(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "stub",
		OpenAIBaseURL:   "https://example.com",
		OllamaBaseURL:   "http://127.0.0.1:11434",
		ProviderTimeout: 3 * time.Second,
	}
	g := NewGateway(cfg, testLogger())

	models := g.Models(context.Background())
	if len(models) != 1 || models[0].ID != "stub/opcapi-mvp" {
		t.Fatalf("expected stub fallback model only, got %+v", models)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

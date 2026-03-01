package provider

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/derekwin/opcAPI/internal/config"
)

type multiGateway struct {
	defaultProvider string
	openai          *OpenAIClient
	ollama          *OllamaClient
	stub            *StubGateway
	logger          *slog.Logger
}

func NewGateway(cfg config.Config, logger *slog.Logger) Gateway {
	g := &multiGateway{
		defaultProvider: strings.ToLower(strings.TrimSpace(cfg.DefaultProvider)),
		stub:            NewStubGateway(),
		logger:          logger,
	}

	if g.defaultProvider == "" {
		g.defaultProvider = "stub"
	}

	if strings.TrimSpace(cfg.OpenAIBaseURL) != "" {
		g.openai = NewOpenAIClient(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.ProviderTimeout)
	}
	if strings.TrimSpace(cfg.OllamaBaseURL) != "" {
		g.ollama = NewOllamaClient(cfg.OllamaBaseURL, cfg.ProviderTimeout)
	}

	logger.Info("provider gateway initialized",
		"default_provider", g.defaultProvider,
		"openai_enabled", g.openai != nil,
		"ollama_enabled", g.ollama != nil,
	)

	return g
}

func (g *multiGateway) Chat(ctx context.Context, req ChatRequest) (ChatResult, error) {
	providerName, model, explicit := parseModel(req.Model, g.defaultProvider)
	req.Model = model

	result, err := g.chatByProvider(ctx, providerName, req)
	if err == nil {
		return result, nil
	}
	if explicit {
		return ChatResult{}, err
	}

	fallback := []string{"openai", "ollama", "stub"}
	for _, name := range fallback {
		if name == providerName {
			continue
		}
		res, fallbackErr := g.chatByProvider(ctx, name, req)
		if fallbackErr == nil {
			if g.logger != nil {
				g.logger.Warn("provider fallback succeeded", "from", providerName, "to", name)
			}
			return res, nil
		}
	}

	return ChatResult{}, err
}

func (g *multiGateway) Models(_ context.Context) []ModelInfo {
	models := make([]ModelInfo, 0, 4)
	if g.openai != nil {
		models = append(models, ModelInfo{ID: "openai/gpt-4o-mini", OwnedBy: "openai"})
	}
	if g.ollama != nil {
		models = append(models, ModelInfo{ID: "ollama/llama3.1", OwnedBy: "ollama"})
	}
	models = append(models, g.stub.Models(context.Background())...)
	return models
}

func (g *multiGateway) chatByProvider(ctx context.Context, providerName string, req ChatRequest) (ChatResult, error) {
	switch providerName {
	case "openai":
		if g.openai == nil {
			return ChatResult{}, fmt.Errorf("openai: %w", ErrProviderUnavailable)
		}
		return g.openai.Chat(ctx, req)
	case "ollama":
		if g.ollama == nil {
			return ChatResult{}, fmt.Errorf("ollama: %w", ErrProviderUnavailable)
		}
		return g.ollama.Chat(ctx, req)
	case "stub":
		return g.stub.Chat(ctx, req)
	default:
		return ChatResult{}, fmt.Errorf("%s: %w", providerName, ErrProviderUnavailable)
	}
}

func parseModel(model, defaultProvider string) (providerName string, cleanModel string, explicit bool) {
	v := strings.TrimSpace(model)
	if v == "" {
		return normalizeProvider(defaultProvider), "", false
	}

	parts := strings.SplitN(v, "/", 2)
	if len(parts) == 2 {
		prefix := strings.ToLower(strings.TrimSpace(parts[0]))
		if prefix == "openai" || prefix == "ollama" || prefix == "stub" {
			return prefix, strings.TrimSpace(parts[1]), true
		}
	}

	return normalizeProvider(defaultProvider), v, false
}

func normalizeProvider(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "openai", "ollama", "stub":
		return strings.ToLower(strings.TrimSpace(name))
	default:
		return "stub"
	}
}

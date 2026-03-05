package provider

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/derekwin/opcAPI/internal/config"
	"github.com/derekwin/opcAPI/internal/upstream"
)

type multiGateway struct {
	defaultProvider string
	providerTimeout time.Duration
	openai          *OpenAIClient
	anthropic       *AnthropicClient
	gemini          *GeminiClient
	ollama          *OllamaClient
	stub            *StubGateway
	upstream        *upstream.Service
	logger          *slog.Logger
}

func NewGateway(cfg config.Config, logger *slog.Logger) Gateway {
	return NewGatewayWithUpstream(cfg, logger, nil)
}

func NewGatewayWithUpstream(cfg config.Config, logger *slog.Logger, upstreamService *upstream.Service) Gateway {
	g := &multiGateway{
		defaultProvider: strings.ToLower(strings.TrimSpace(cfg.DefaultProvider)),
		providerTimeout: cfg.ProviderTimeout,
		stub:            NewStubGateway(),
		upstream:        upstreamService,
		logger:          logger,
	}

	if g.defaultProvider == "" {
		g.defaultProvider = "stub"
	}

	if strings.TrimSpace(cfg.OpenAIBaseURL) != "" {
		g.openai = NewOpenAIClient(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.ProviderTimeout)
	}
	if strings.TrimSpace(cfg.AnthropicBaseURL) != "" {
		g.anthropic = NewAnthropicClient(cfg.AnthropicBaseURL, cfg.AnthropicAPIKey, cfg.ProviderTimeout)
	}
	if strings.TrimSpace(cfg.GeminiBaseURL) != "" {
		g.gemini = NewGeminiClient(cfg.GeminiBaseURL, cfg.GeminiAPIKey, cfg.ProviderTimeout)
	}
	if strings.TrimSpace(cfg.OllamaBaseURL) != "" {
		g.ollama = NewOllamaClient(cfg.OllamaBaseURL, cfg.ProviderTimeout)
	}

	logger.Info("provider gateway initialized",
		"default_provider", g.defaultProvider,
		"openai_enabled", g.openai != nil,
		"anthropic_enabled", g.anthropic != nil,
		"gemini_enabled", g.gemini != nil,
		"ollama_enabled", g.ollama != nil,
		"upstream_router_enabled", g.upstream != nil,
	)

	return g
}

func (g *multiGateway) Chat(ctx context.Context, req ChatRequest) (ChatResult, error) {
	if g == nil {
		return ChatResult{}, ErrProviderUnavailable
	}

	providerName, model, explicit := parseModel(req.Model, g.defaultProvider)
	req.Model = model

	if !explicit && g.upstream != nil {
		targets, matched, err := g.upstream.Resolve(req.Model)
		if err != nil && matched {
			return ChatResult{}, err
		}
		if matched {
			var firstErr error
			for _, target := range targets {
				routedReq := req
				routedReq.Model = target.Model
				start := time.Now()
				res, routeErr := g.chatByResolvedTarget(ctx, target, routedReq)
				if g.upstream != nil {
					g.upstream.RecordRuntime(req.Model, target.UpstreamID, routeErr == nil, time.Since(start), routeErr)
				}
				if routeErr == nil {
					return res, nil
				}
				if firstErr == nil {
					firstErr = routeErr
				}
			}
			if firstErr != nil {
				return ChatResult{}, firstErr
			}
		}
	}

	result, err := g.chatByProvider(ctx, providerName, req)
	if err == nil {
		return result, nil
	}
	if explicit {
		return ChatResult{}, err
	}

	fallback := []string{"openai", "anthropic", "gemini", "ollama", "stub"}
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
	models := make([]ModelInfo, 0, 16)
	seen := map[string]struct{}{}

	appendModel := func(id, owner string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		models = append(models, ModelInfo{ID: id, OwnedBy: owner})
	}

	if g.upstream != nil {
		if upstreams, err := g.upstream.ListUpstreams(); err == nil {
			for _, up := range upstreams {
				if !up.Enabled {
					continue
				}
				for _, model := range up.Models {
					appendModel(model, "upstream")
				}
			}
		}
		if routes, err := g.upstream.ListRoutes(); err == nil {
			for _, route := range routes {
				if !route.Enabled {
					continue
				}
				appendModel(route.Alias, "route")
			}
		}
	}
	if len(models) == 0 {
		for _, model := range g.stub.Models(context.Background()) {
			appendModel(model.ID, model.OwnedBy)
		}
	}
	return models
}

func (g *multiGateway) chatByProvider(ctx context.Context, providerName string, req ChatRequest) (ChatResult, error) {
	switch providerName {
	case "openai":
		if g.openai == nil {
			return ChatResult{}, fmt.Errorf("openai: %w", ErrProviderUnavailable)
		}
		return g.openai.Chat(ctx, req)
	case "anthropic":
		if g.anthropic == nil {
			return ChatResult{}, fmt.Errorf("anthropic: %w", ErrProviderUnavailable)
		}
		return g.anthropic.Chat(ctx, req)
	case "gemini":
		if g.gemini == nil {
			return ChatResult{}, fmt.Errorf("gemini: %w", ErrProviderUnavailable)
		}
		return g.gemini.Chat(ctx, req)
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

func (g *multiGateway) chatByResolvedTarget(ctx context.Context, target upstream.ResolvedTarget, req ChatRequest) (ChatResult, error) {
	switch target.ProviderType {
	case upstream.ProviderOpenAICompat:
		client := NewOpenAIClient(target.BaseURL, target.APIKey, g.providerTimeout)
		return client.Chat(ctx, req)
	case upstream.ProviderAnthropic:
		client := NewAnthropicClient(target.BaseURL, target.APIKey, g.providerTimeout)
		return client.Chat(ctx, req)
	case upstream.ProviderGemini:
		client := NewGeminiClient(target.BaseURL, target.APIKey, g.providerTimeout)
		return client.Chat(ctx, req)
	case upstream.ProviderOllama:
		client := NewOllamaClient(target.BaseURL, g.providerTimeout)
		return client.Chat(ctx, req)
	default:
		return ChatResult{}, fmt.Errorf("%s: %w", target.ProviderType, ErrProviderUnavailable)
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
		if prefix == "openai" || prefix == "anthropic" || prefix == "gemini" || prefix == "ollama" || prefix == "stub" {
			return prefix, strings.TrimSpace(parts[1]), true
		}
	}

	return normalizeProvider(defaultProvider), v, false
}

func normalizeProvider(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "openai", "anthropic", "gemini", "ollama", "stub":
		return strings.ToLower(strings.TrimSpace(name))
	default:
		return "stub"
	}
}

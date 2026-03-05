package upstream

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpstreamAndRouteCRUD(t *testing.T) {
	svc := newTestService(t)

	up1, err := svc.UpsertUpstream(UpsertUpstreamRequest{
		Name:    "openai-main",
		Type:    ProviderOpenAICompat,
		BaseURL: "https://api.openai.com",
		APIKey:  "sk-test",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert upstream failed: %v", err)
	}
	if up1.ID == "" {
		t.Fatalf("expected upstream id")
	}

	if _, err := svc.UpsertRoute(UpsertRouteRequest{
		Alias:    "gpt-4o-mini",
		Strategy: StrategyFallback,
		Enabled:  true,
		Targets: []RouteTarget{
			{UpstreamID: up1.ID, Model: "gpt-4o-mini", Weight: 100, Priority: 1, Enabled: true},
		},
	}); err != nil {
		t.Fatalf("upsert route failed: %v", err)
	}

	targets, matched, err := svc.Resolve("gpt-4o-mini")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if !matched || len(targets) != 1 {
		t.Fatalf("unexpected resolve result: matched=%v targets=%d", matched, len(targets))
	}
	if targets[0].ProviderType != ProviderOpenAICompat {
		t.Fatalf("unexpected provider type: %s", targets[0].ProviderType)
	}
}

func TestDeleteUpstreamPrunesRouteTargets(t *testing.T) {
	svc := newTestService(t)

	up1, err := svc.UpsertUpstream(UpsertUpstreamRequest{Name: "primary", Type: ProviderOpenAICompat, BaseURL: "https://api.openai.com", Enabled: true})
	if err != nil {
		t.Fatalf("upsert upstream1 failed: %v", err)
	}
	up2, err := svc.UpsertUpstream(UpsertUpstreamRequest{Name: "backup", Type: ProviderOllama, BaseURL: "http://localhost:11434", Enabled: true})
	if err != nil {
		t.Fatalf("upsert upstream2 failed: %v", err)
	}

	_, err = svc.UpsertRoute(UpsertRouteRequest{
		Alias:    "chat-main",
		Strategy: StrategyFallback,
		Enabled:  true,
		Targets: []RouteTarget{
			{UpstreamID: up1.ID, Model: "gpt-4o-mini", Weight: 100, Priority: 1, Enabled: true},
			{UpstreamID: up2.ID, Model: "llama3.1", Weight: 50, Priority: 2, Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("upsert route failed: %v", err)
	}

	if err := svc.DeleteUpstream(up1.ID); err != nil {
		t.Fatalf("delete upstream failed: %v", err)
	}

	targets, matched, err := svc.Resolve("chat-main")
	if err != nil {
		t.Fatalf("resolve after delete failed: %v", err)
	}
	if !matched || len(targets) != 1 {
		t.Fatalf("unexpected resolve targets after prune: matched=%v targets=%d", matched, len(targets))
	}
	if targets[0].UpstreamID != up2.ID {
		t.Fatalf("expected fallback upstream id %s, got %s", up2.ID, targets[0].UpstreamID)
	}
}

func TestDefaultRouteAlias(t *testing.T) {
	svc := newTestService(t)

	up1, err := svc.UpsertUpstream(UpsertUpstreamRequest{
		Name:    "primary",
		Type:    ProviderOpenAICompat,
		BaseURL: "https://api.openai.com",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert upstream failed: %v", err)
	}

	if _, err := svc.UpsertRoute(UpsertRouteRequest{
		Alias:    "z-route",
		Strategy: StrategyFallback,
		Enabled:  true,
		Targets: []RouteTarget{
			{UpstreamID: up1.ID, Model: "gpt-4o-mini", Priority: 1, Weight: 100, Enabled: true},
		},
	}); err != nil {
		t.Fatalf("upsert z-route failed: %v", err)
	}
	if _, err := svc.UpsertRoute(UpsertRouteRequest{
		Alias:    "default",
		Strategy: StrategyFallback,
		Enabled:  true,
		Targets: []RouteTarget{
			{UpstreamID: up1.ID, Model: "gpt-4o-mini", Priority: 1, Weight: 100, Enabled: true},
		},
	}); err != nil {
		t.Fatalf("upsert default route failed: %v", err)
	}

	alias, ok, err := svc.DefaultRouteAlias()
	if err != nil {
		t.Fatalf("default alias failed: %v", err)
	}
	if !ok || alias != "default" {
		t.Fatalf("expected default alias, got ok=%v alias=%s", ok, alias)
	}
}

func TestResolveHealthURLForMainstreamProviders(t *testing.T) {
	cases := []struct {
		name string
		up   Upstream
		want string
	}{
		{
			name: "openai compat",
			up:   Upstream{Type: ProviderOpenAICompat, BaseURL: "https://api.openai.com"},
			want: "https://api.openai.com/v1/models",
		},
		{
			name: "anthropic",
			up:   Upstream{Type: ProviderAnthropic, BaseURL: "https://api.anthropic.com"},
			want: "https://api.anthropic.com/v1/models",
		},
		{
			name: "gemini",
			up:   Upstream{Type: ProviderGemini, BaseURL: "https://generativelanguage.googleapis.com"},
			want: "https://generativelanguage.googleapis.com/v1beta/models",
		},
		{
			name: "ollama",
			up:   Upstream{Type: ProviderOllama, BaseURL: "http://localhost:11434"},
			want: "http://localhost:11434/api/tags",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveHealthURL(tc.up)
			if err != nil {
				t.Fatalf("resolveHealthURL failed: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected health url: got %s want %s", got, tc.want)
			}
		})
	}
}

func TestCheckHealthIncludesHTTPDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"qwen-plus"}]}`))
	}))
	defer server.Close()

	svc := newTestService(t)
	up, err := svc.UpsertUpstream(UpsertUpstreamRequest{
		Name:    "health-detail-up",
		Type:    ProviderOpenAICompat,
		BaseURL: server.URL,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert upstream failed: %v", err)
	}

	results, err := svc.CheckHealth(context.Background(), up.ID)
	if err != nil {
		t.Fatalf("check health failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if results[0].HTTPCode != http.StatusOK {
		t.Fatalf("expected 200 http code, got %d", results[0].HTTPCode)
	}
	if !strings.HasSuffix(results[0].Endpoint, "/v1/models") {
		t.Fatalf("unexpected endpoint: %s", results[0].Endpoint)
	}
	if !strings.Contains(results[0].Response, `"id":"qwen-plus"`) {
		t.Fatalf("expected response content, got %s", results[0].Response)
	}

	items, err := svc.ListUpstreams()
	if err != nil {
		t.Fatalf("list upstreams failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one upstream, got %d", len(items))
	}
	if items[0].LastHealthHTTPCode != http.StatusOK {
		t.Fatalf("expected saved http code 200, got %d", items[0].LastHealthHTTPCode)
	}
	if !strings.HasSuffix(items[0].LastHealthEndpoint, "/v1/models") {
		t.Fatalf("unexpected saved endpoint: %s", items[0].LastHealthEndpoint)
	}
	if !strings.Contains(items[0].LastHealthResponse, `"id":"qwen-plus"`) {
		t.Fatalf("expected saved response content, got %s", items[0].LastHealthResponse)
	}
}

func TestCheckHealthTreatsReachable4xxAsHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	svc := newTestService(t)
	up, err := svc.UpsertUpstream(UpsertUpstreamRequest{
		Name:    "reachability-up",
		Type:    ProviderOpenAICompat,
		BaseURL: server.URL,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert upstream failed: %v", err)
	}

	results, err := svc.CheckHealth(context.Background(), up.ID)
	if err != nil {
		t.Fatalf("check health failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if !results[0].Healthy {
		t.Fatalf("expected reachable upstream to be healthy, got %+v", results[0])
	}
	if results[0].HTTPCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 http code, got %d", results[0].HTTPCode)
	}
	if !strings.Contains(results[0].Response, "unauthorized") {
		t.Fatalf("expected error body preserved, got %s", results[0].Response)
	}

	items, err := svc.ListUpstreams()
	if err != nil {
		t.Fatalf("list upstreams failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one upstream, got %d", len(items))
	}
	if items[0].LastHealthStatus != "healthy" {
		t.Fatalf("expected saved health status healthy, got %s", items[0].LastHealthStatus)
	}
	if items[0].LastHealthHTTPCode != http.StatusUnauthorized {
		t.Fatalf("expected saved http code 401, got %d", items[0].LastHealthHTTPCode)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	store, err := NewFileStore(filepath.Join(t.TempDir(), "upstreams.json"))
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	return NewService(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

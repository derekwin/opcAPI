package upstream

import "time"

const (
	ProviderOpenAICompat = "openai_compat"
	ProviderAnthropic    = "anthropic"
	ProviderGemini       = "gemini"
	ProviderOllama       = "ollama"

	StrategyFallback = "fallback"
	StrategyWeighted = "weighted"
)

type Upstream struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Type                string    `json:"type"`
	BaseURL             string    `json:"base_url"`
	APIKey              string    `json:"api_key,omitempty"`
	Models              []string  `json:"models,omitempty"`
	Enabled             bool      `json:"enabled"`
	LastHealthStatus    string    `json:"last_health_status,omitempty"`
	LastHealthLatency   int64     `json:"last_health_latency_ms,omitempty"`
	LastHealthError     string    `json:"last_health_error,omitempty"`
	LastHealthHTTPCode  int       `json:"last_health_http_code,omitempty"`
	LastHealthEndpoint  string    `json:"last_health_endpoint,omitempty"`
	LastHealthResponse  string    `json:"last_health_response,omitempty"`
	LastHealthCheckedAt time.Time `json:"last_health_checked_at,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type RouteTarget struct {
	UpstreamID string `json:"upstream_id"`
	Model      string `json:"model"`
	Weight     int    `json:"weight"`
	Priority   int    `json:"priority"`
	Enabled    bool   `json:"enabled"`
}

type ModelRoute struct {
	Alias     string        `json:"alias"`
	Strategy  string        `json:"strategy"`
	Enabled   bool          `json:"enabled"`
	Targets   []RouteTarget `json:"targets"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type UpsertUpstreamRequest struct {
	ID      string
	Name    string
	Type    string
	BaseURL string
	APIKey  string
	Models  []string
	Enabled bool
}

type UpsertRouteRequest struct {
	Alias    string
	Strategy string
	Enabled  bool
	Targets  []RouteTarget
}

type ResolvedTarget struct {
	UpstreamID   string
	UpstreamName string
	ProviderType string
	BaseURL      string
	APIKey       string
	Model        string
}

type HealthCheckResult struct {
	UpstreamID string        `json:"upstream_id"`
	Healthy    bool          `json:"healthy"`
	LatencyMS  int64         `json:"latency_ms"`
	Error      string        `json:"error,omitempty"`
	HTTPCode   int           `json:"http_code,omitempty"`
	Endpoint   string        `json:"endpoint,omitempty"`
	Response   string        `json:"response,omitempty"`
	CheckedAt  time.Time     `json:"checked_at"`
	Duration   time.Duration `json:"-"`
}

type RuntimeStat struct {
	ModelAlias    string    `json:"model_alias"`
	UpstreamID    string    `json:"upstream_id"`
	SuccessCount  int64     `json:"success_count"`
	FailureCount  int64     `json:"failure_count"`
	LastError     string    `json:"last_error,omitempty"`
	LastLatencyMS int64     `json:"last_latency_ms"`
	LastSeenAt    time.Time `json:"last_seen_at"`
}

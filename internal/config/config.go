package config

import (
	"os"
	"strconv"
	"time"
)

// Config contains runtime settings for the API service.
type Config struct {
	ServiceName     string
	HTTPAddr        string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	LogLevel        string

	RequireAPIKey   bool
	APIKeys         string
	RequestRPMLimit int
	DailyTokenLimit int
	PolicyStorePath string
	PolicyRefresh   time.Duration
	PolicyBackend   string

	RedisAddr         string
	RedisPassword     string
	RedisDB           int
	RedisPolicyKey    string
	RedisQuotaPrefix  string
	RedisDialTimeout  time.Duration
	RedisReadTimeout  time.Duration
	RedisWriteTimeout time.Duration

	UserStorePath      string
	BillingStorePath   string
	PromptAPIStorePath string
	AuditStorePath     string
	UpstreamStorePath  string
	SessionSecret      string
	SessionTTL         time.Duration

	ProviderTimeout  time.Duration
	DefaultProvider  string
	OpenAIBaseURL    string
	OpenAIAPIKey     string
	AnthropicBaseURL string
	AnthropicAPIKey  string
	GeminiBaseURL    string
	GeminiAPIKey     string
	OllamaBaseURL    string
}

// Load reads configuration from environment variables with safe defaults.
func Load() Config {
	return Config{
		ServiceName:     getEnv("SERVICE_NAME", "opcapi"),
		HTTPAddr:        getEnv("HTTP_ADDR", ":8080"),
		ReadTimeout:     getEnvDuration("HTTP_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:    getEnvDuration("HTTP_WRITE_TIMEOUT", 30*time.Second),
		ShutdownTimeout: getEnvDuration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second),
		LogLevel:        getEnv("LOG_LEVEL", "info"),

		RequireAPIKey:   getEnvBool("REQUIRE_API_KEY", true),
		APIKeys:         getEnv("API_KEYS", "sk-opcapi-dev"),
		RequestRPMLimit: getEnvInt("REQUEST_RPM_LIMIT", 120),
		DailyTokenLimit: getEnvInt("DAILY_TOKEN_LIMIT", 1000000),
		PolicyStorePath: getEnv("POLICY_STORE_PATH", "data/policies.json"),
		PolicyRefresh:   getEnvDuration("POLICY_REFRESH_INTERVAL", 30*time.Second),
		PolicyBackend:   getEnv("POLICY_BACKEND", "file"),

		RedisAddr:         getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:     getEnv("REDIS_PASSWORD", ""),
		RedisDB:           getEnvInt("REDIS_DB", 0),
		RedisPolicyKey:    getEnv("REDIS_POLICY_KEY", "opcapi:policies:v1"),
		RedisQuotaPrefix:  getEnv("REDIS_QUOTA_PREFIX", "opcapi:quota"),
		RedisDialTimeout:  getEnvDuration("REDIS_DIAL_TIMEOUT", 2*time.Second),
		RedisReadTimeout:  getEnvDuration("REDIS_READ_TIMEOUT", 2*time.Second),
		RedisWriteTimeout: getEnvDuration("REDIS_WRITE_TIMEOUT", 2*time.Second),

		UserStorePath:      getEnv("USER_STORE_PATH", "data/users.json"),
		BillingStorePath:   getEnv("BILLING_STORE_PATH", "data/billing.json"),
		PromptAPIStorePath: getEnv("PROMPT_API_STORE_PATH", "data/prompt_apis.json"),
		AuditStorePath:     getEnv("AUDIT_STORE_PATH", "data/audit_logs.json"),
		UpstreamStorePath:  getEnv("UPSTREAM_STORE_PATH", "data/upstreams.json"),
		SessionSecret:      getEnv("SESSION_SECRET", "opcapi-dev-session-secret"),
		SessionTTL:         getEnvDuration("SESSION_TTL", 168*time.Hour),

		ProviderTimeout:  getEnvDuration("PROVIDER_TIMEOUT", 60*time.Second),
		DefaultProvider:  getEnv("DEFAULT_PROVIDER", "stub"),
		OpenAIBaseURL:    getEnv("OPENAI_BASE_URL", "https://api.openai.com"),
		OpenAIAPIKey:     getEnv("OPENAI_API_KEY", ""),
		AnthropicBaseURL: getEnv("ANTHROPIC_BASE_URL", "https://api.anthropic.com"),
		AnthropicAPIKey:  getEnv("ANTHROPIC_API_KEY", ""),
		GeminiBaseURL:    getEnv("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com"),
		GeminiAPIKey:     getEnv("GEMINI_API_KEY", ""),
		OllamaBaseURL:    getEnv("OLLAMA_BASE_URL", "http://localhost:11434"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
		if seconds, err := strconv.Atoi(v); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		switch v {
		case "1", "true", "TRUE", "True", "yes", "YES", "on", "ON":
			return true
		case "0", "false", "FALSE", "False", "no", "NO", "off", "OFF":
			return false
		}
	}
	return fallback
}

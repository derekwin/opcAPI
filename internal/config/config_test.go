package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("SERVICE_NAME", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("HTTP_READ_TIMEOUT", "")
	t.Setenv("HTTP_WRITE_TIMEOUT", "")
	t.Setenv("HTTP_SHUTDOWN_TIMEOUT", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("REQUIRE_API_KEY", "")
	t.Setenv("REQUIRE_TENANT_BOUND_KEY", "")
	t.Setenv("API_KEYS", "")
	t.Setenv("REQUEST_RPM_LIMIT", "")
	t.Setenv("DAILY_TOKEN_LIMIT", "")
	t.Setenv("POLICY_STORE_PATH", "")
	t.Setenv("POLICY_REFRESH_INTERVAL", "")
	t.Setenv("POLICY_BACKEND", "")
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "")
	t.Setenv("REDIS_POLICY_KEY", "")
	t.Setenv("REDIS_QUOTA_PREFIX", "")
	t.Setenv("REDIS_DIAL_TIMEOUT", "")
	t.Setenv("REDIS_READ_TIMEOUT", "")
	t.Setenv("REDIS_WRITE_TIMEOUT", "")
	t.Setenv("USER_STORE_PATH", "")
	t.Setenv("BILLING_STORE_PATH", "")
	t.Setenv("PROMPT_API_STORE_PATH", "")
	t.Setenv("AUDIT_STORE_PATH", "")
	t.Setenv("SESSION_SECRET", "")
	t.Setenv("SESSION_TTL", "")
	t.Setenv("PHONE_TEST_CODE", "")
	t.Setenv("PROVIDER_TIMEOUT", "")
	t.Setenv("DEFAULT_PROVIDER", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OLLAMA_BASE_URL", "")

	cfg := Load()

	if cfg.ServiceName != "opcapi" {
		t.Fatalf("unexpected service name: %s", cfg.ServiceName)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("unexpected http addr: %s", cfg.HTTPAddr)
	}
	if cfg.ReadTimeout != 15*time.Second {
		t.Fatalf("unexpected read timeout: %s", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout != 30*time.Second {
		t.Fatalf("unexpected write timeout: %s", cfg.WriteTimeout)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected shutdown timeout: %s", cfg.ShutdownTimeout)
	}
	if cfg.ProviderTimeout != 60*time.Second {
		t.Fatalf("unexpected provider timeout: %s", cfg.ProviderTimeout)
	}
	if !cfg.RequireAPIKey {
		t.Fatalf("expected RequireAPIKey=true by default")
	}
	if !cfg.RequireTenantBoundKey {
		t.Fatalf("expected RequireTenantBoundKey=true by default")
	}
	if cfg.APIKeys != "sk-opcapi-dev" {
		t.Fatalf("unexpected API keys: %s", cfg.APIKeys)
	}
	if cfg.RequestRPMLimit != 120 {
		t.Fatalf("unexpected request rpm limit: %d", cfg.RequestRPMLimit)
	}
	if cfg.DailyTokenLimit != 1000000 {
		t.Fatalf("unexpected daily token limit: %d", cfg.DailyTokenLimit)
	}
	if cfg.PolicyStorePath != "data/policies.json" {
		t.Fatalf("unexpected policy store path: %s", cfg.PolicyStorePath)
	}
	if cfg.PolicyRefresh != 30*time.Second {
		t.Fatalf("unexpected policy refresh: %s", cfg.PolicyRefresh)
	}
	if cfg.PolicyBackend != "file" {
		t.Fatalf("unexpected policy backend: %s", cfg.PolicyBackend)
	}
	if cfg.RedisAddr != "127.0.0.1:6379" {
		t.Fatalf("unexpected redis addr: %s", cfg.RedisAddr)
	}
	if cfg.RedisDB != 0 {
		t.Fatalf("unexpected redis db: %d", cfg.RedisDB)
	}
	if cfg.RedisPolicyKey != "opcapi:policies:v1" {
		t.Fatalf("unexpected redis policy key: %s", cfg.RedisPolicyKey)
	}
	if cfg.RedisQuotaPrefix != "opcapi:quota" {
		t.Fatalf("unexpected redis quota prefix: %s", cfg.RedisQuotaPrefix)
	}
	if cfg.RedisDialTimeout != 2*time.Second {
		t.Fatalf("unexpected redis dial timeout: %s", cfg.RedisDialTimeout)
	}
	if cfg.RedisReadTimeout != 2*time.Second {
		t.Fatalf("unexpected redis read timeout: %s", cfg.RedisReadTimeout)
	}
	if cfg.RedisWriteTimeout != 2*time.Second {
		t.Fatalf("unexpected redis write timeout: %s", cfg.RedisWriteTimeout)
	}
	if cfg.UserStorePath != "data/users.json" {
		t.Fatalf("unexpected user store path: %s", cfg.UserStorePath)
	}
	if cfg.BillingStorePath != "data/billing.json" {
		t.Fatalf("unexpected billing store path: %s", cfg.BillingStorePath)
	}
	if cfg.PromptAPIStorePath != "data/prompt_apis.json" {
		t.Fatalf("unexpected prompt api store path: %s", cfg.PromptAPIStorePath)
	}
	if cfg.AuditStorePath != "data/audit_logs.json" {
		t.Fatalf("unexpected audit store path: %s", cfg.AuditStorePath)
	}
	if cfg.SessionSecret != "opcapi-dev-session-secret" {
		t.Fatalf("unexpected session secret: %s", cfg.SessionSecret)
	}
	if cfg.SessionTTL != 168*time.Hour {
		t.Fatalf("unexpected session ttl: %s", cfg.SessionTTL)
	}
	if cfg.PhoneTestCode != "000000" {
		t.Fatalf("unexpected phone test code: %s", cfg.PhoneTestCode)
	}
	if cfg.DefaultProvider != "stub" {
		t.Fatalf("unexpected default provider: %s", cfg.DefaultProvider)
	}
	if cfg.OpenAIBaseURL != "https://api.openai.com" {
		t.Fatalf("unexpected openai base url: %s", cfg.OpenAIBaseURL)
	}
	if cfg.OllamaBaseURL != "http://localhost:11434" {
		t.Fatalf("unexpected ollama base url: %s", cfg.OllamaBaseURL)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("SERVICE_NAME", "opcapi-test")
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("HTTP_READ_TIMEOUT", "25")
	t.Setenv("HTTP_WRITE_TIMEOUT", "45s")
	t.Setenv("HTTP_SHUTDOWN_TIMEOUT", "5")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("REQUIRE_API_KEY", "false")
	t.Setenv("REQUIRE_TENANT_BOUND_KEY", "false")
	t.Setenv("API_KEYS", "sk-1,sk-2")
	t.Setenv("REQUEST_RPM_LIMIT", "240")
	t.Setenv("DAILY_TOKEN_LIMIT", "3000000")
	t.Setenv("POLICY_STORE_PATH", "data/test-policies.json")
	t.Setenv("POLICY_REFRESH_INTERVAL", "45s")
	t.Setenv("POLICY_BACKEND", "redis")
	t.Setenv("REDIS_ADDR", "10.0.0.8:6379")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("REDIS_DB", "2")
	t.Setenv("REDIS_POLICY_KEY", "opcapi:policies:test")
	t.Setenv("REDIS_QUOTA_PREFIX", "opcapi:quota:test")
	t.Setenv("REDIS_DIAL_TIMEOUT", "3s")
	t.Setenv("REDIS_READ_TIMEOUT", "4s")
	t.Setenv("REDIS_WRITE_TIMEOUT", "5s")
	t.Setenv("USER_STORE_PATH", "data/test-users.json")
	t.Setenv("BILLING_STORE_PATH", "data/test-billing.json")
	t.Setenv("PROMPT_API_STORE_PATH", "data/test-prompt-apis.json")
	t.Setenv("AUDIT_STORE_PATH", "data/test-audit.json")
	t.Setenv("SESSION_SECRET", "test-session-secret")
	t.Setenv("SESSION_TTL", "72h")
	t.Setenv("PHONE_TEST_CODE", "123456")
	t.Setenv("PROVIDER_TIMEOUT", "90")
	t.Setenv("DEFAULT_PROVIDER", "openai")
	t.Setenv("OPENAI_BASE_URL", "https://example.com/v1")
	t.Setenv("OPENAI_API_KEY", "abc")
	t.Setenv("OLLAMA_BASE_URL", "http://127.0.0.1:11434")

	cfg := Load()

	if cfg.ServiceName != "opcapi-test" {
		t.Fatalf("unexpected service name: %s", cfg.ServiceName)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("unexpected http addr: %s", cfg.HTTPAddr)
	}
	if cfg.ReadTimeout != 25*time.Second {
		t.Fatalf("unexpected read timeout: %s", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout != 45*time.Second {
		t.Fatalf("unexpected write timeout: %s", cfg.WriteTimeout)
	}
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Fatalf("unexpected shutdown timeout: %s", cfg.ShutdownTimeout)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("unexpected log level: %s", cfg.LogLevel)
	}
	if cfg.ProviderTimeout != 90*time.Second {
		t.Fatalf("unexpected provider timeout: %s", cfg.ProviderTimeout)
	}
	if cfg.RequireAPIKey {
		t.Fatalf("expected RequireAPIKey=false")
	}
	if cfg.RequireTenantBoundKey {
		t.Fatalf("expected RequireTenantBoundKey=false")
	}
	if cfg.APIKeys != "sk-1,sk-2" {
		t.Fatalf("unexpected API keys: %s", cfg.APIKeys)
	}
	if cfg.RequestRPMLimit != 240 {
		t.Fatalf("unexpected request rpm limit: %d", cfg.RequestRPMLimit)
	}
	if cfg.DailyTokenLimit != 3000000 {
		t.Fatalf("unexpected daily token limit: %d", cfg.DailyTokenLimit)
	}
	if cfg.PolicyStorePath != "data/test-policies.json" {
		t.Fatalf("unexpected policy store path: %s", cfg.PolicyStorePath)
	}
	if cfg.PolicyRefresh != 45*time.Second {
		t.Fatalf("unexpected policy refresh: %s", cfg.PolicyRefresh)
	}
	if cfg.PolicyBackend != "redis" {
		t.Fatalf("unexpected policy backend: %s", cfg.PolicyBackend)
	}
	if cfg.RedisAddr != "10.0.0.8:6379" {
		t.Fatalf("unexpected redis addr: %s", cfg.RedisAddr)
	}
	if cfg.RedisPassword != "secret" {
		t.Fatalf("unexpected redis password: %s", cfg.RedisPassword)
	}
	if cfg.RedisDB != 2 {
		t.Fatalf("unexpected redis db: %d", cfg.RedisDB)
	}
	if cfg.RedisPolicyKey != "opcapi:policies:test" {
		t.Fatalf("unexpected redis policy key: %s", cfg.RedisPolicyKey)
	}
	if cfg.RedisQuotaPrefix != "opcapi:quota:test" {
		t.Fatalf("unexpected redis quota prefix: %s", cfg.RedisQuotaPrefix)
	}
	if cfg.RedisDialTimeout != 3*time.Second {
		t.Fatalf("unexpected redis dial timeout: %s", cfg.RedisDialTimeout)
	}
	if cfg.RedisReadTimeout != 4*time.Second {
		t.Fatalf("unexpected redis read timeout: %s", cfg.RedisReadTimeout)
	}
	if cfg.RedisWriteTimeout != 5*time.Second {
		t.Fatalf("unexpected redis write timeout: %s", cfg.RedisWriteTimeout)
	}
	if cfg.UserStorePath != "data/test-users.json" {
		t.Fatalf("unexpected user store path: %s", cfg.UserStorePath)
	}
	if cfg.BillingStorePath != "data/test-billing.json" {
		t.Fatalf("unexpected billing store path: %s", cfg.BillingStorePath)
	}
	if cfg.PromptAPIStorePath != "data/test-prompt-apis.json" {
		t.Fatalf("unexpected prompt api store path: %s", cfg.PromptAPIStorePath)
	}
	if cfg.AuditStorePath != "data/test-audit.json" {
		t.Fatalf("unexpected audit store path: %s", cfg.AuditStorePath)
	}
	if cfg.SessionSecret != "test-session-secret" {
		t.Fatalf("unexpected session secret: %s", cfg.SessionSecret)
	}
	if cfg.SessionTTL != 72*time.Hour {
		t.Fatalf("unexpected session ttl: %s", cfg.SessionTTL)
	}
	if cfg.PhoneTestCode != "123456" {
		t.Fatalf("unexpected phone test code: %s", cfg.PhoneTestCode)
	}
	if cfg.DefaultProvider != "openai" {
		t.Fatalf("unexpected default provider: %s", cfg.DefaultProvider)
	}
	if cfg.OpenAIBaseURL != "https://example.com/v1" {
		t.Fatalf("unexpected openai base url: %s", cfg.OpenAIBaseURL)
	}
	if cfg.OpenAIAPIKey != "abc" {
		t.Fatalf("unexpected openai api key: %s", cfg.OpenAIAPIKey)
	}
	if cfg.OllamaBaseURL != "http://127.0.0.1:11434" {
		t.Fatalf("unexpected ollama base url: %s", cfg.OllamaBaseURL)
	}
}

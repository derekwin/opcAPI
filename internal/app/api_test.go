package app

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/derekwin/opcAPI/internal/config"
)

func TestNewInitializesServer(t *testing.T) {
	userStorePath := filepath.Join(t.TempDir(), "users.json")
	cfg := config.Config{
		HTTPAddr:           ":18080",
		ReadTimeout:        2 * time.Second,
		WriteTimeout:       3 * time.Second,
		ShutdownTimeout:    1 * time.Second,
		DefaultProvider:    "stub",
		UserStorePath:      userStorePath,
		BillingStorePath:   filepath.Join(t.TempDir(), "billing.json"),
		PromptAPIStorePath: filepath.Join(t.TempDir(), "prompt_apis.json"),
		AuditStorePath:     filepath.Join(t.TempDir(), "audit.json"),
		UpstreamStorePath:  filepath.Join(t.TempDir(), "upstreams.json"),
		SessionSecret:      "test-secret",
		SessionTTL:         24 * time.Hour,
		PolicyBackend:      "file",
		PolicyStorePath:    filepath.Join(t.TempDir(), "policies.json"),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	a := New(cfg, logger)
	if a == nil {
		t.Fatalf("expected api instance")
	}
	if a.server == nil {
		t.Fatalf("expected http server")
	}
	if a.server.Addr != cfg.HTTPAddr {
		t.Fatalf("unexpected server addr: %s", a.server.Addr)
	}
	if a.initErr != nil {
		t.Fatalf("unexpected init err: %v", a.initErr)
	}
}

func TestRunReturnsListenError(t *testing.T) {
	userStorePath := filepath.Join(t.TempDir(), "users.json")
	cfg := config.Config{
		HTTPAddr:           "bad-address",
		ReadTimeout:        2 * time.Second,
		WriteTimeout:       3 * time.Second,
		ShutdownTimeout:    1 * time.Second,
		DefaultProvider:    "stub",
		UserStorePath:      userStorePath,
		BillingStorePath:   filepath.Join(t.TempDir(), "billing.json"),
		PromptAPIStorePath: filepath.Join(t.TempDir(), "prompt_apis.json"),
		AuditStorePath:     filepath.Join(t.TempDir(), "audit.json"),
		UpstreamStorePath:  filepath.Join(t.TempDir(), "upstreams.json"),
		SessionSecret:      "test-secret",
		SessionTTL:         24 * time.Hour,
		PolicyBackend:      "file",
		PolicyStorePath:    filepath.Join(t.TempDir(), "policies.json"),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := New(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := a.Run(ctx); err == nil {
		t.Fatalf("expected listen error")
	}
}

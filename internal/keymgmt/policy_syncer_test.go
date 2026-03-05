package keymgmt

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/derekwin/opcAPI/internal/auth"
	"github.com/derekwin/opcAPI/internal/config"
	"github.com/derekwin/opcAPI/internal/quota"
)

func TestInitializeCreatesPolicyFile(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "policies.json")

	cfg := config.Config{
		PolicyStorePath: path,
		PolicyRefresh:   5 * time.Second,
		APIKeys:         "sk-a,sk-b",
		RequestRPMLimit: 12,
		DailyTokenLimit: 300,
	}
	s := NewPolicySyncer(cfg, testLogger(), auth.NewStaticManager(true, nil), quota.NewLimiter(0, 0), nil)
	if s == nil {
		t.Fatalf("expected syncer")
	}

	if err := s.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("policy file was not created: %v", err)
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read policy file failed: %v", err)
	}
	if bytes.Contains(buf, []byte(`"api_key"`)) {
		t.Fatalf("default policy file should not persist api_key in plaintext")
	}
	if !bytes.Contains(buf, []byte(auth.HashAPIKey("sk-a"))) || !bytes.Contains(buf, []byte(auth.HashAPIKey("sk-b"))) {
		t.Fatalf("default policy file should include hashed default keys")
	}
}

func TestLoadOnceAppliesDynamicPolicies(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "policies.json")

	payload := `{
  "version": 1,
  "keys": [
    {
      "api_key": "sk-dyn",
      "enabled": true,
      "rpm_limit": 1,
      "daily_token_limit": 10
    },
    {
      "api_key": "sk-disabled",
      "enabled": false,
      "rpm_limit": 1,
      "daily_token_limit": 10
    }
  ]
}`
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatalf("write policy file: %v", err)
	}

	m := auth.NewStaticManager(true, nil)
	l := quota.NewLimiter(100, 1000)

	cfg := config.Config{
		PolicyStorePath: path,
		PolicyRefresh:   5 * time.Second,
		RequestRPMLimit: 100,
		DailyTokenLimit: 1000,
	}
	s := NewPolicySyncer(cfg, testLogger(), m, l, nil)
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	if _, err := m.Authenticate("Bearer sk-dyn"); err != nil {
		t.Fatalf("expected dynamic key authenticate, got %v", err)
	}
	if _, err := m.Authenticate("Bearer sk-disabled"); err != auth.ErrInvalidAPIKey {
		t.Fatalf("expected disabled key to fail, got %v", err)
	}

	if !l.AllowRequest(auth.HashAPIKey("sk-dyn")).Allowed {
		t.Fatalf("first request should pass")
	}
	if l.AllowRequest(auth.HashAPIKey("sk-dyn")).Allowed {
		t.Fatalf("second request should be rate limited by dynamic policy")
	}
}

func TestInitializeRedisBackendRequiresClient(t *testing.T) {
	cfg := config.Config{
		PolicyBackend:   "redis",
		RedisPolicyKey:  "opcapi:policies:test",
		PolicyRefresh:   5 * time.Second,
		RequestRPMLimit: 100,
		DailyTokenLimit: 1000,
	}
	s := NewPolicySyncer(cfg, testLogger(), auth.NewStaticManager(true, nil), quota.NewLimiter(0, 0), nil)
	if s == nil {
		t.Fatalf("expected syncer")
	}
	if err := s.Initialize(context.Background()); err == nil {
		t.Fatalf("expected initialize to fail without redis client")
	}
}

func TestPolicyCRUDWithUserAndPlanTier(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "policies.json")

	m := auth.NewStaticManager(true, nil)
	l := quota.NewLimiter(100, 1000)
	cfg := config.Config{
		PolicyBackend:   "file",
		PolicyStorePath: path,
		PolicyRefresh:   5 * time.Second,
		RequestRPMLimit: 100,
		DailyTokenLimit: 1000,
	}
	s := NewPolicySyncer(cfg, testLogger(), m, l, nil)
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	enabled := true
	userID := "usr_test"
	paid := true
	plan := "pro"
	record, err := s.Upsert(context.Background(), UpsertPolicyInput{
		APIKey:      "sk-crud",
		UserID:      &userID,
		Enabled:     &enabled,
		PaidEnabled: &paid,
		PlanTier:    &plan,
	})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if record.UserID != userID {
		t.Fatalf("expected user_id=%s, got %s", userID, record.UserID)
	}
	if !record.PaidEnabled || record.PlanTier != "pro" {
		t.Fatalf("unexpected paid fields: %+v", record)
	}
	if record.RPMLimit < 1000 || record.DailyTokenLimit < 20_000_000 {
		t.Fatalf("expected upgraded limits for pro tier, got rpm=%d daily=%d", record.RPMLimit, record.DailyTokenLimit)
	}

	if _, err := m.Authenticate("Bearer sk-crud"); err != nil {
		t.Fatalf("dynamic key authenticate failed: %v", err)
	}
	if got := s.ResolveUserByKeyHash(record.KeyHash); got != userID {
		t.Fatalf("unexpected resolved user: %s", got)
	}

	list, total, err := s.List(context.Background(), ListQuery{UserID: userID, Limit: 10})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if total < 1 || len(list) == 0 {
		t.Fatalf("expected list to contain record")
	}

	if err := s.DeleteForUser(context.Background(), record.KeyHash, "usr_other"); err != ErrPolicyNotFound {
		t.Fatalf("expected scoped delete to fail, got %v", err)
	}
	if err := s.DeleteForUser(context.Background(), record.KeyHash, userID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := m.Authenticate("Bearer sk-crud"); err != auth.ErrInvalidAPIKey {
		t.Fatalf("expected removed key to fail auth, got %v", err)
	}
}

func TestDefaultPayloadSupportsSha256PrefixedKeys(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "policies.json")
	hash := auth.HashAPIKey("sk-prefixed")

	cfg := config.Config{
		PolicyStorePath: path,
		PolicyRefresh:   5 * time.Second,
		APIKeys:         "sha256:" + hash,
		RequestRPMLimit: 12,
		DailyTokenLimit: 300,
	}
	s := NewPolicySyncer(cfg, testLogger(), auth.NewStaticManager(true, nil), quota.NewLimiter(0, 0), nil)
	if s == nil {
		t.Fatalf("expected syncer")
	}

	if err := s.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read policy file failed: %v", err)
	}
	if !strings.Contains(string(buf), hash) {
		t.Fatalf("expected prefixed hash to persist as key_hash")
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

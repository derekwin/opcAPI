package keymgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/derekwin/opcAPI/internal/auth"
	"github.com/derekwin/opcAPI/internal/config"
	"github.com/derekwin/opcAPI/internal/quota"
	redisstore "github.com/derekwin/opcAPI/internal/store/redis"
)

var (
	ErrPolicyNotFound = errors.New("policy not found")
	ErrInvalidPolicy  = errors.New("invalid policy")
)

type persistedPolicies struct {
	Version int               `json:"version"`
	Keys    []persistedPolicy `json:"keys"`
}

type persistedPolicy struct {
	APIKey          string `json:"api_key,omitempty"`
	KeyHash         string `json:"key_hash,omitempty"`
	TenantID        string `json:"tenant_id,omitempty"`
	Enabled         bool   `json:"enabled"`
	RPMLimit        int    `json:"rpm_limit"`
	DailyTokenLimit int    `json:"daily_token_limit"`
}

type PolicyRecord struct {
	KeyHash         string `json:"key_hash"`
	TenantID        string `json:"tenant_id,omitempty"`
	Enabled         bool   `json:"enabled"`
	RPMLimit        int    `json:"rpm_limit"`
	DailyTokenLimit int    `json:"daily_token_limit"`
}

type UpsertPolicyInput struct {
	APIKey          string
	KeyHash         string
	TenantID        *string
	Enabled         *bool
	RPMLimit        *int
	DailyTokenLimit *int
}

type ListQuery struct {
	TenantID string
	Enabled  *bool
	Limit    int
	Offset   int
}

type PolicySyncer struct {
	backend  string
	path     string
	interval time.Duration
	logger   *slog.Logger

	redisClient *redisstore.Client
	redisKey    string

	authManager *auth.Manager
	limiter     *quota.Limiter

	defaultRPMLimit int
	defaultDaily    int
	defaultKeys     []string

	once    sync.Once
	stateMu sync.RWMutex
	tenants map[string]string
}

func NewPolicySyncer(cfg config.Config, logger *slog.Logger, authManager *auth.Manager, limiter *quota.Limiter, redisClient *redisstore.Client) *PolicySyncer {
	backend := strings.ToLower(strings.TrimSpace(cfg.PolicyBackend))
	if backend == "" {
		backend = "file"
	}

	path := strings.TrimSpace(cfg.PolicyStorePath)
	if backend == "file" && path == "" {
		return nil
	}

	interval := cfg.PolicyRefresh
	if interval <= 0 {
		interval = 30 * time.Second
	}

	redisKey := strings.TrimSpace(cfg.RedisPolicyKey)
	if redisKey == "" {
		redisKey = "opcapi:policies:v1"
	}

	return &PolicySyncer{
		backend:         backend,
		path:            path,
		interval:        interval,
		logger:          logger,
		redisClient:     redisClient,
		redisKey:        redisKey,
		authManager:     authManager,
		limiter:         limiter,
		defaultRPMLimit: cfg.RequestRPMLimit,
		defaultDaily:    cfg.DailyTokenLimit,
		defaultKeys:     parseDefaultKeys(cfg.APIKeys),
		tenants:         make(map[string]string),
	}
}

func (s *PolicySyncer) Initialize(ctx context.Context) error {
	if s == nil {
		return nil
	}

	if err := s.ensureStore(ctx); err != nil {
		return err
	}

	if err := s.LoadOnce(ctx); err != nil {
		return err
	}

	return nil
}

func (s *PolicySyncer) Start(ctx context.Context) {
	if s == nil {
		return
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.LoadOnce(ctx); err != nil && s.logger != nil {
				s.logger.Error("policy sync failed", "err", err)
			}
		}
	}
}

func (s *PolicySyncer) LoadOnce(ctx context.Context) error {
	if s == nil {
		return nil
	}

	data, err := s.readPayload(ctx)
	if err != nil {
		return err
	}

	var payload persistedPolicies
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}

	hashes := make([]string, 0, len(payload.Keys))
	policies := make(map[string]quota.KeyPolicy, len(payload.Keys))
	tenants := make(map[string]string, len(payload.Keys))

	for _, item := range payload.Keys {
		h := resolvePolicyHash(item)
		if h == "" {
			continue
		}

		rpm := item.RPMLimit
		if rpm <= 0 {
			rpm = s.defaultRPMLimit
		}
		daily := item.DailyTokenLimit
		if daily <= 0 {
			daily = s.defaultDaily
		}

		if item.Enabled {
			hashes = append(hashes, h)
			policies[h] = quota.KeyPolicy{RPMLimit: rpm, DailyTokenLimit: daily}
			if tenantID := strings.TrimSpace(item.TenantID); tenantID != "" {
				tenants[h] = tenantID
			}
		}
	}

	if s.authManager != nil {
		s.authManager.SetDynamicKeyHashes(hashes)
	}
	if s.limiter != nil {
		s.limiter.SetKeyPolicies(policies)
	}
	s.stateMu.Lock()
	s.tenants = tenants
	s.stateMu.Unlock()

	if s.logger != nil {
		s.logger.Info("policy sync updated", "backend", s.backend, "key_count", len(hashes))
	}
	return nil
}

func (s *PolicySyncer) ResolveTenantByKeyHash(keyHash string) string {
	if s == nil {
		return ""
	}
	keyHash = normalizeHash(keyHash)
	if keyHash == "" {
		return ""
	}

	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.tenants[keyHash]
}

func (s *PolicySyncer) List(ctx context.Context, query ListQuery) ([]PolicyRecord, int, error) {
	if s == nil {
		return nil, 0, nil
	}

	if err := s.ensureStore(ctx); err != nil {
		return nil, 0, err
	}

	data, err := s.readPayload(ctx)
	if err != nil {
		return nil, 0, err
	}

	var payload persistedPolicies
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, 0, err
	}

	tenantID := strings.TrimSpace(query.TenantID)
	out := make([]PolicyRecord, 0, len(payload.Keys))
	for _, item := range payload.Keys {
		keyHash := resolvePolicyHash(item)
		if keyHash == "" {
			continue
		}
		rpm := item.RPMLimit
		if rpm <= 0 {
			rpm = s.defaultRPMLimit
		}
		daily := item.DailyTokenLimit
		if daily <= 0 {
			daily = s.defaultDaily
		}
		record := PolicyRecord{
			KeyHash:         keyHash,
			TenantID:        strings.TrimSpace(item.TenantID),
			Enabled:         item.Enabled,
			RPMLimit:        rpm,
			DailyTokenLimit: daily,
		}
		if tenantID != "" && record.TenantID != tenantID {
			continue
		}
		if query.Enabled != nil && record.Enabled != *query.Enabled {
			continue
		}
		out = append(out, record)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].KeyHash < out[j].KeyHash
	})
	total := len(out)
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []PolicyRecord{}, total, nil
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return out[offset:end], total, nil
}

func (s *PolicySyncer) Upsert(ctx context.Context, input UpsertPolicyInput) (PolicyRecord, error) {
	if s == nil {
		return PolicyRecord{}, ErrInvalidPolicy
	}

	if err := s.ensureStore(ctx); err != nil {
		return PolicyRecord{}, err
	}

	keyHash := normalizeHash(input.KeyHash)
	if keyHash == "" && strings.TrimSpace(input.APIKey) != "" {
		keyHash = auth.HashAPIKey(input.APIKey)
	}
	if keyHash == "" {
		return PolicyRecord{}, ErrInvalidPolicy
	}

	payload, err := s.loadPayload(ctx)
	if err != nil {
		return PolicyRecord{}, err
	}

	idx := -1
	for i, p := range payload.Keys {
		if resolvePolicyHash(p) == keyHash {
			idx = i
			break
		}
	}

	base := persistedPolicy{
		KeyHash:         keyHash,
		Enabled:         true,
		RPMLimit:        s.defaultRPMLimit,
		DailyTokenLimit: s.defaultDaily,
	}
	if idx >= 0 {
		existing := payload.Keys[idx]
		base.Enabled = existing.Enabled
		if existing.RPMLimit > 0 {
			base.RPMLimit = existing.RPMLimit
		}
		if existing.DailyTokenLimit > 0 {
			base.DailyTokenLimit = existing.DailyTokenLimit
		}
		base.TenantID = strings.TrimSpace(existing.TenantID)
	}

	if input.TenantID != nil {
		tenant := strings.TrimSpace(*input.TenantID)
		if len(tenant) > 128 {
			return PolicyRecord{}, ErrInvalidPolicy
		}
		base.TenantID = tenant
	}
	if input.Enabled != nil {
		base.Enabled = *input.Enabled
	}
	if input.RPMLimit != nil && *input.RPMLimit > 0 {
		base.RPMLimit = *input.RPMLimit
	}
	if input.DailyTokenLimit != nil && *input.DailyTokenLimit > 0 {
		base.DailyTokenLimit = *input.DailyTokenLimit
	}

	if idx >= 0 {
		payload.Keys[idx] = base
	} else {
		payload.Keys = append(payload.Keys, base)
	}

	if err := s.writePayload(ctx, payload); err != nil {
		return PolicyRecord{}, err
	}
	if err := s.LoadOnce(ctx); err != nil {
		return PolicyRecord{}, err
	}

	return PolicyRecord{
		KeyHash:         keyHash,
		TenantID:        base.TenantID,
		Enabled:         base.Enabled,
		RPMLimit:        base.RPMLimit,
		DailyTokenLimit: base.DailyTokenLimit,
	}, nil
}

func (s *PolicySyncer) Delete(ctx context.Context, keyHash string) error {
	return s.DeleteForTenant(ctx, keyHash, "")
}

func (s *PolicySyncer) DeleteForTenant(ctx context.Context, keyHash, tenantID string) error {
	if s == nil {
		return ErrInvalidPolicy
	}

	if err := s.ensureStore(ctx); err != nil {
		return err
	}

	keyHash = normalizeHash(keyHash)
	if keyHash == "" {
		return ErrInvalidPolicy
	}
	tenantID = strings.TrimSpace(tenantID)

	payload, err := s.loadPayload(ctx)
	if err != nil {
		return err
	}

	idx := -1
	for i, item := range payload.Keys {
		if resolvePolicyHash(item) != keyHash {
			continue
		}
		if tenantID != "" && strings.TrimSpace(item.TenantID) != tenantID {
			return ErrPolicyNotFound
		}
		idx = i
		break
	}
	if idx < 0 {
		return ErrPolicyNotFound
	}

	payload.Keys = append(payload.Keys[:idx], payload.Keys[idx+1:]...)
	if err := s.writePayload(ctx, payload); err != nil {
		return err
	}
	return s.LoadOnce(ctx)
}

func (s *PolicySyncer) ensureStore(ctx context.Context) error {
	if s.backend == "redis" {
		return s.ensureRedisStore(ctx)
	}
	return s.ensureFileStore()
}

func (s *PolicySyncer) ensureFileStore() error {
	var ensureErr error
	s.once.Do(func() {
		dir := filepath.Dir(s.path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			ensureErr = err
			return
		}

		if _, err := os.Stat(s.path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			ensureErr = err
			return
		}

		buf, err := s.defaultPayloadBytes()
		if err != nil {
			ensureErr = err
			return
		}
		if err := os.WriteFile(s.path, buf, 0o644); err != nil {
			ensureErr = err
			return
		}
	})

	if ensureErr != nil {
		return fmt.Errorf("ensure file policy store: %w", ensureErr)
	}
	return nil
}

func (s *PolicySyncer) ensureRedisStore(ctx context.Context) error {
	if s.redisClient == nil {
		return errors.New("redis policy backend requires redis client")
	}

	if err := s.redisClient.Ping(ctx); err != nil {
		return err
	}

	_, exists, err := s.redisClient.Get(ctx, s.redisKey)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	buf, err := s.defaultPayloadBytes()
	if err != nil {
		return err
	}
	return s.redisClient.Set(ctx, s.redisKey, string(buf), 0)
}

func (s *PolicySyncer) readPayload(ctx context.Context) ([]byte, error) {
	if s.backend == "redis" {
		if s.redisClient == nil {
			return nil, errors.New("redis policy backend requires redis client")
		}
		v, ok, err := s.redisClient.Get(ctx, s.redisKey)
		if err != nil {
			return nil, err
		}
		if !ok || strings.TrimSpace(v) == "" {
			return nil, errors.New("redis policy payload is empty")
		}
		return []byte(v), nil
	}
	return os.ReadFile(s.path)
}

func (s *PolicySyncer) loadPayload(ctx context.Context) (persistedPolicies, error) {
	data, err := s.readPayload(ctx)
	if err != nil {
		return persistedPolicies{}, err
	}
	var payload persistedPolicies
	if err := json.Unmarshal(data, &payload); err != nil {
		return persistedPolicies{}, err
	}
	if payload.Version == 0 {
		payload.Version = 1
	}
	if payload.Keys == nil {
		payload.Keys = []persistedPolicy{}
	}
	return payload, nil
}

func (s *PolicySyncer) writePayload(ctx context.Context, payload persistedPolicies) error {
	if payload.Version == 0 {
		payload.Version = 1
	}
	buf, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')

	if s.backend == "redis" {
		if s.redisClient == nil {
			return errors.New("redis policy backend requires redis client")
		}
		return s.redisClient.Set(ctx, s.redisKey, string(buf), 0)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *PolicySyncer) defaultPayloadBytes() ([]byte, error) {
	payload := persistedPolicies{Version: 1, Keys: make([]persistedPolicy, 0, len(s.defaultKeys))}
	for _, key := range s.defaultKeys {
		keyHash := resolveDefaultKeyHash(key)
		if keyHash == "" {
			continue
		}
		payload.Keys = append(payload.Keys, persistedPolicy{
			KeyHash:         keyHash,
			Enabled:         true,
			RPMLimit:        s.defaultRPMLimit,
			DailyTokenLimit: s.defaultDaily,
		})
	}

	buf, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	buf = append(buf, '\n')
	return buf, nil
}

func resolvePolicyHash(p persistedPolicy) string {
	if h := normalizeHash(p.KeyHash); h != "" {
		return h
	}
	if strings.TrimSpace(p.APIKey) != "" {
		return auth.HashAPIKey(p.APIKey)
	}
	return ""
}

func parseDefaultKeys(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func resolveDefaultKeyHash(key string) string {
	v := strings.TrimSpace(key)
	if v == "" {
		return ""
	}

	if strings.HasPrefix(strings.ToLower(v), "sha256:") {
		return normalizeHash(strings.TrimSpace(v[len("sha256:"):]))
	}
	if h := normalizeHash(v); h != "" {
		return h
	}
	return auth.HashAPIKey(v)
}

func normalizeHash(hash string) string {
	v := strings.ToLower(strings.TrimSpace(hash))
	if len(v) != 64 {
		return ""
	}
	for _, ch := range v {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return ""
		}
	}
	return v
}

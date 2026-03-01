package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/derekwin/opcAPI/internal/config"
)

var (
	ErrMissingAuthorization = errors.New("missing authorization header")
	ErrInvalidAuthorization = errors.New("invalid authorization header")
	ErrInvalidAPIKey        = errors.New("invalid api key")
)

type Manager struct {
	enabled bool

	mu              sync.RWMutex
	staticKeyHashes map[string]struct{}
	dynamicKeyHash  map[string]struct{}
}

func NewManager(cfg config.Config, logger *slog.Logger) *Manager {
	keys := parseKeys(cfg.APIKeys)
	m := NewStaticManager(cfg.RequireAPIKey, keys)

	if logger != nil {
		logger.Info("auth manager initialized",
			"enabled", m.enabled,
			"static_key_count", len(m.staticKeyHashes),
		)
	}

	return m
}

func NewStaticManager(enabled bool, keys []string) *Manager {
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if normalized := normalizeKeyInput(key); normalized != "" {
			set[normalized] = struct{}{}
		}
	}
	return &Manager{enabled: enabled, staticKeyHashes: set, dynamicKeyHash: make(map[string]struct{})}
}

func (m *Manager) Enabled() bool {
	return m != nil && m.enabled
}

func (m *Manager) Authenticate(authorizationHeader string) (string, error) {
	if m == nil || !m.enabled {
		return "anonymous", nil
	}

	if strings.TrimSpace(authorizationHeader) == "" {
		return "", ErrMissingAuthorization
	}

	parts := strings.Fields(authorizationHeader)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", ErrInvalidAuthorization
	}

	apiKey := strings.TrimSpace(parts[1])
	if apiKey == "" {
		return "", ErrInvalidAuthorization
	}

	hash := HashAPIKey(apiKey)

	m.mu.RLock()
	_, okStatic := m.staticKeyHashes[hash]
	_, okDynamic := m.dynamicKeyHash[hash]
	m.mu.RUnlock()
	if !okStatic && !okDynamic {
		return "", ErrInvalidAPIKey
	}

	return hash, nil
}

func (m *Manager) SetDynamicKeyHashes(hashes []string) {
	if m == nil {
		return
	}

	set := make(map[string]struct{}, len(hashes))
	for _, h := range hashes {
		if normalized := normalizeHash(h); normalized != "" {
			set[normalized] = struct{}{}
		}
	}

	m.mu.Lock()
	m.dynamicKeyHash = set
	m.mu.Unlock()
}

func parseKeys(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func HashAPIKey(apiKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(apiKey)))
	return hex.EncodeToString(sum[:])
}

func normalizeKeyInput(key string) string {
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

	return HashAPIKey(v)
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

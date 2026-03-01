package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/derekwin/opcAPI/internal/audit"
	"github.com/derekwin/opcAPI/internal/keymgmt"
)

type adminKeyUpsertRequest struct {
	APIKey          string  `json:"api_key,omitempty"`
	KeyHash         string  `json:"key_hash,omitempty"`
	TenantID        *string `json:"tenant_id,omitempty"`
	Enabled         *bool   `json:"enabled,omitempty"`
	RPMLimit        *int    `json:"rpm_limit,omitempty"`
	DailyTokenLimit *int    `json:"daily_token_limit,omitempty"`
}

type adminKeyDeleteRequest struct {
	KeyHash string `json:"key_hash"`
}

func (h *handler) adminKeysList(w http.ResponseWriter, r *http.Request) {
	if h.policySync == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "policy_sync_unavailable", "server_error", "policy sync unavailable", "service")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if claims.TenantID == "" {
		writeOpenAIError(w, r, http.StatusUnauthorized, "invalid_session", "authentication_error", "invalid session", "authorization")
		return
	}
	if tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantID != "" && tenantID != claims.TenantID {
		writeOpenAIError(w, r, http.StatusForbidden, "forbidden", "permission_error", "cannot query keys outside current tenant", "tenant_id")
		return
	}

	var enabled *bool
	if v := strings.TrimSpace(r.URL.Query().Get("enabled")); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			enabled = &b
		}
	}
	limit := 50
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			limit = i
		}
	}
	offset := 0
	if v := strings.TrimSpace(r.URL.Query().Get("offset")); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			offset = i
		}
	}

	items, total, err := h.policySync.List(r.Context(), keymgmt.ListQuery{
		TenantID: claims.TenantID,
		Enabled:  enabled,
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		writePolicyError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": items,
		"meta": map[string]any{
			"total":  total,
			"limit":  limit,
			"offset": offset,
			"count":  len(items),
		},
	})
}

func (h *handler) adminKeysUpsert(w http.ResponseWriter, r *http.Request) {
	if h.policySync == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "policy_sync_unavailable", "server_error", "policy sync unavailable", "service")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if claims.TenantID == "" {
		writeOpenAIError(w, r, http.StatusUnauthorized, "invalid_session", "authentication_error", "invalid session", "authorization")
		return
	}
	defer r.Body.Close()

	var req adminKeyUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "invalid JSON request body", "body")
		return
	}
	if req.TenantID != nil {
		tenantID := strings.TrimSpace(*req.TenantID)
		if tenantID != "" && tenantID != claims.TenantID {
			writeOpenAIError(w, r, http.StatusForbidden, "forbidden", "permission_error", "cannot manage keys outside current tenant", "tenant_id")
			return
		}
	}
	tenantID := claims.TenantID

	record, err := h.policySync.Upsert(r.Context(), keymgmt.UpsertPolicyInput{
		APIKey:          strings.TrimSpace(req.APIKey),
		KeyHash:         strings.TrimSpace(req.KeyHash),
		TenantID:        &tenantID,
		Enabled:         req.Enabled,
		RPMLimit:        req.RPMLimit,
		DailyTokenLimit: req.DailyTokenLimit,
	})
	if err != nil {
		writePolicyError(w, r, err)
		return
	}
	if h.audit != nil {
		_, _ = h.audit.Record(r.Context(), audit.RecordInput{
			Resource:       "api_key",
			Action:         "upsert",
			ActorUserID:    claims.UserID,
			ActorTenantID:  claims.TenantID,
			TargetID:       record.KeyHash,
			TargetTenantID: record.TenantID,
			Metadata: map[string]string{
				"enabled":           strconv.FormatBool(record.Enabled),
				"rpm_limit":         strconv.Itoa(record.RPMLimit),
				"daily_token_limit": strconv.Itoa(record.DailyTokenLimit),
			},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": record})
}

func (h *handler) adminKeysDelete(w http.ResponseWriter, r *http.Request) {
	if h.policySync == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "policy_sync_unavailable", "server_error", "policy sync unavailable", "service")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if claims.TenantID == "" {
		writeOpenAIError(w, r, http.StatusUnauthorized, "invalid_session", "authentication_error", "invalid session", "authorization")
		return
	}

	keyHash := strings.TrimSpace(r.URL.Query().Get("key_hash"))
	if keyHash == "" {
		defer r.Body.Close()
		var req adminKeyDeleteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			keyHash = strings.TrimSpace(req.KeyHash)
		}
	}

	if err := h.policySync.DeleteForTenant(r.Context(), keyHash, claims.TenantID); err != nil {
		writePolicyError(w, r, err)
		return
	}
	if h.audit != nil {
		_, _ = h.audit.Record(r.Context(), audit.RecordInput{
			Resource:      "api_key",
			Action:        "delete",
			ActorUserID:   claims.UserID,
			ActorTenantID: claims.TenantID,
			TargetID:      keyHash,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "key_hash": keyHash})
}

func writePolicyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, keymgmt.ErrInvalidPolicy):
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_policy", "invalid_request_error", err.Error(), "policy")
	case errors.Is(err, keymgmt.ErrPolicyNotFound):
		writeOpenAIError(w, r, http.StatusNotFound, "policy_not_found", "invalid_request_error", err.Error(), "policy")
	default:
		writeOpenAIError(w, r, http.StatusInternalServerError, "internal_error", "server_error", "internal server error", "")
	}
}

package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/derekwin/opcAPI/internal/audit"
)

func (h *handler) adminAuditLogs(w http.ResponseWriter, r *http.Request) {
	if h.audit == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "audit_unavailable", "server_error", "audit service unavailable", "service")
		return
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

	page, err := h.audit.List(r.Context(), audit.Query{
		Resource:       strings.TrimSpace(r.URL.Query().Get("resource")),
		Action:         strings.TrimSpace(r.URL.Query().Get("action")),
		ActorUserID:    strings.TrimSpace(r.URL.Query().Get("actor_user_id")),
		TargetTenantID: strings.TrimSpace(r.URL.Query().Get("target_tenant_id")),
		Limit:          limit,
		Offset:         offset,
	})
	if err != nil {
		writeAuditError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": page.Data,
		"meta": map[string]any{
			"total":  page.Total,
			"limit":  page.Limit,
			"offset": page.Offset,
			"count":  len(page.Data),
		},
	})
}

func writeAuditError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, audit.ErrStoreUnavailable):
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "audit_unavailable", "server_error", "audit service unavailable", "service")
	case errors.Is(err, audit.ErrInvalidEvent):
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_audit_event", "invalid_request_error", err.Error(), "audit")
	default:
		writeOpenAIError(w, r, http.StatusInternalServerError, "internal_error", "server_error", "internal server error", "")
	}
}

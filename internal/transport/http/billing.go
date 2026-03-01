package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/derekwin/opcAPI/internal/billing"
)

func (h *handler) billingBalance(w http.ResponseWriter, r *http.Request) {
	if h.billing == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "billing_unavailable", "server_error", "billing service unavailable", "service")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if claims.TenantID == "" {
		writeOpenAIError(w, r, http.StatusUnauthorized, "invalid_session", "authentication_error", "invalid session", "authorization")
		return
	}

	bal, err := h.billing.GetBalance(r.Context(), claims.TenantID)
	if err != nil {
		writeBillingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, bal)
}

func (h *handler) billingTopup(w http.ResponseWriter, r *http.Request) {
	if h.billing == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "billing_unavailable", "server_error", "billing service unavailable", "service")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if claims.TenantID == "" {
		writeOpenAIError(w, r, http.StatusUnauthorized, "invalid_session", "authentication_error", "invalid session", "authorization")
		return
	}
	defer r.Body.Close()

	var req billing.TopupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "invalid JSON request body", "body")
		return
	}

	bal, ord, err := h.billing.Topup(r.Context(), claims.TenantID, req)
	if err != nil {
		writeBillingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"balance": bal, "order": ord})
}

func (h *handler) billingSubscribe(w http.ResponseWriter, r *http.Request) {
	if h.billing == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "billing_unavailable", "server_error", "billing service unavailable", "service")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if claims.TenantID == "" {
		writeOpenAIError(w, r, http.StatusUnauthorized, "invalid_session", "authentication_error", "invalid session", "authorization")
		return
	}
	defer r.Body.Close()

	var req billing.SubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "invalid JSON request body", "body")
		return
	}

	sub, ord, err := h.billing.Subscribe(r.Context(), claims.TenantID, req)
	if err != nil {
		writeBillingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscription": sub, "order": ord})
}

func (h *handler) adminBillingOrders(w http.ResponseWriter, r *http.Request) {
	if h.billing == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "billing_unavailable", "server_error", "billing service unavailable", "service")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if claims.TenantID == "" {
		writeOpenAIError(w, r, http.StatusUnauthorized, "invalid_session", "authentication_error", "invalid session", "authorization")
		return
	}

	query := billing.OrderQuery{
		TenantID: claims.TenantID,
		Type:     r.URL.Query().Get("type"),
		Status:   r.URL.Query().Get("status"),
		Channel:  r.URL.Query().Get("channel"),
		Limit:    50,
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			query.Limit = i
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			query.Offset = i
		}
	}

	page, err := h.billing.ListOrdersAdmin(r.Context(), query)
	if err != nil {
		writeBillingError(w, r, err)
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

func writeBillingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, billing.ErrInvalidAmount), errors.Is(err, billing.ErrInvalidChannel), errors.Is(err, billing.ErrInvalidCurrency), errors.Is(err, billing.ErrInvalidPlan), errors.Is(err, billing.ErrInvalidSubscription):
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_billing_request", "invalid_request_error", err.Error(), "billing")
	case errors.Is(err, billing.ErrInsufficientBalance):
		writeOpenAIError(w, r, http.StatusPaymentRequired, "insufficient_balance", "billing_error", err.Error(), "balance")
	case errors.Is(err, billing.ErrStoreUnavailable):
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "billing_unavailable", "server_error", "billing service unavailable", "service")
	default:
		writeOpenAIError(w, r, http.StatusInternalServerError, "internal_error", "server_error", "internal server error", "")
	}
}

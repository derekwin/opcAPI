package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/derekwin/opcAPI/internal/promptapi"
)

type promptInvokeRequest struct {
	Input  map[string]any `json:"input"`
	Stream bool           `json:"stream,omitempty"`
}

func (h *handler) adminPromptAPIUpsert(w http.ResponseWriter, r *http.Request) {
	if h.promptAPI == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "prompt_api_unavailable", "server_error", "prompt api service unavailable", "service")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if claims.TenantID == "" {
		writeOpenAIError(w, r, http.StatusUnauthorized, "invalid_session", "authentication_error", "invalid session", "authorization")
		return
	}
	defer r.Body.Close()

	var req promptapi.UpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "invalid JSON request body", "body")
		return
	}

	def, err := h.promptAPI.Upsert(r.Context(), claims.TenantID, req)
	if err != nil {
		writePromptAPIError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": def})
}

func (h *handler) adminPromptAPIList(w http.ResponseWriter, r *http.Request) {
	if h.promptAPI == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "prompt_api_unavailable", "server_error", "prompt api service unavailable", "service")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if claims.TenantID == "" {
		writeOpenAIError(w, r, http.StatusUnauthorized, "invalid_session", "authentication_error", "invalid session", "authorization")
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			limit = i
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			offset = i
		}
	}

	page, err := h.promptAPI.List(r.Context(), promptapi.ListQuery{
		TenantID: claims.TenantID,
		Status:   r.URL.Query().Get("status"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		writePromptAPIError(w, r, err)
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

func (h *handler) promptAPIInvoke(w http.ResponseWriter, r *http.Request) {
	if h.promptAPI == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "prompt_api_unavailable", "server_error", "prompt api service unavailable", "service")
		return
	}
	if r.Method != http.MethodPost {
		writeOpenAIError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "invalid_request_error", "unsupported method", "method")
		return
	}

	slug, ok := extractPromptSlug(r.URL.Path)
	if !ok {
		writeOpenAIError(w, r, http.StatusNotFound, "not_found", "invalid_request_error", "not found", "path")
		return
	}

	defer r.Body.Close()
	var req promptInvokeRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "invalid JSON request body", "body")
		return
	}
	if req.Input == nil {
		writeOpenAIError(w, r, http.StatusBadRequest, "missing_input", "invalid_request_error", "input is required", "input")
		return
	}

	apiKey := APIKeyFromContext(r.Context())
	tenantID := ""
	if h.policySync != nil {
		tenantID = strings.TrimSpace(h.policySync.ResolveTenantByKeyHash(apiKey))
	}
	if tenantID == "" {
		writeOpenAIError(w, r, http.StatusForbidden, "tenant_binding_required", "permission_error", "api key is not bound to tenant", "tenant_id")
		return
	}

	if h.limiter != nil {
		tokenDecision := h.limiter.AllowTokenUsage(apiKey)
		if !tokenDecision.Allowed {
			writeOpenAIError(w, r, http.StatusTooManyRequests, tokenDecision.Reason, "rate_limit_error", "daily token limit exceeded", "token_limit")
			return
		}
		if tokenDecision.Limit > 0 {
			w.Header().Set("X-TokenLimit-Daily", fmt.Sprintf("%d", tokenDecision.Limit))
			w.Header().Set("X-TokenLimit-Remaining-Daily", fmt.Sprintf("%d", tokenDecision.Remaining))
		}
	}

	def, providerReq, err := h.promptAPI.BuildChatRequest(r.Context(), tenantID, slug, req.Input)
	if err != nil {
		writePromptAPIError(w, r, err)
		return
	}

	result, err := h.gateway.Chat(r.Context(), providerReq)
	if err != nil {
		h.logger.Error("prompt api upstream failed", "err", err, "slug", slug, "request_id", RequestIDFromContext(r.Context()))
		writeOpenAIError(w, r, http.StatusBadGateway, "upstream_error", "server_error", "upstream provider request failed", "provider")
		return
	}

	reply := strings.TrimSpace(result.Content)
	resModel := result.Model
	if strings.TrimSpace(resModel) == "" {
		resModel = providerReq.Model
	}

	id := "chatcmpl-" + shortID()
	created := time.Now().Unix()
	usageVal := usage{
		PromptTokens:     result.PromptTokens,
		CompletionTokens: result.CompletionTokens,
		TotalTokens:      result.PromptTokens + result.CompletionTokens,
	}
	if h.limiter != nil && usageVal.TotalTokens > 0 {
		h.limiter.AddTokenUsage(apiKey, usageVal.TotalTokens)
	}
	if err := h.applyUsageBilling(r, tenantID, usageVal); err != nil {
		writeInferenceBillingError(w, r, err)
		return
	}

	if req.Stream {
		h.streamChatCompletion(w, r, id, created, resModel, reply)
		return
	}

	resp := chatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   resModel,
		Choices: []chatCompletionChoice{{
			Index: 0,
			Message: chatCompletionMessage{
				Role:    "assistant",
				Content: reply,
			},
			FinishReason: "stop",
		}},
		Usage: usageVal,
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"definition": map[string]any{
			"slug":    def.Slug,
			"version": def.Version,
			"status":  def.Status,
		},
		"completion": resp,
	})
}

func extractPromptSlug(path string) (string, bool) {
	if !strings.HasPrefix(path, "/v1/prompt-apis/") {
		return "", false
	}
	rest := strings.TrimPrefix(path, "/v1/prompt-apis/")
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 {
		return "", false
	}
	if parts[1] != "chat" || parts[2] != "completions" {
		return "", false
	}
	slug := strings.TrimSpace(parts[0])
	if slug == "" {
		return "", false
	}
	return slug, true
}

func writePromptAPIError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, promptapi.ErrInvalidSlug), errors.Is(err, promptapi.ErrInvalidName), errors.Is(err, promptapi.ErrInvalidModel), errors.Is(err, promptapi.ErrInvalidTemplate), errors.Is(err, promptapi.ErrInvalidInput):
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_prompt_api_request", "invalid_request_error", err.Error(), "prompt_api")
	case errors.Is(err, promptapi.ErrNotFound):
		writeOpenAIError(w, r, http.StatusNotFound, "prompt_api_not_found", "invalid_request_error", err.Error(), "prompt_api")
	case errors.Is(err, promptapi.ErrNotPublished):
		writeOpenAIError(w, r, http.StatusConflict, "prompt_api_not_published", "invalid_request_error", err.Error(), "prompt_api")
	case errors.Is(err, promptapi.ErrStoreUnavailable):
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "prompt_api_unavailable", "server_error", "prompt api service unavailable", "service")
	default:
		writeOpenAIError(w, r, http.StatusInternalServerError, "internal_error", "server_error", "internal server error", "")
	}
}

package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/derekwin/opcAPI/internal/upstream"
)

type adminUpstreamUpsertRequest struct {
	ID      string   `json:"id,omitempty"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	BaseURL string   `json:"base_url"`
	APIKey  string   `json:"api_key,omitempty"`
	Models  []string `json:"models,omitempty"`
	Enabled bool     `json:"enabled"`
}

type adminUpstreamDeleteRequest struct {
	ID string `json:"id"`
}

type adminRouteUpsertRequest struct {
	Alias    string                 `json:"alias"`
	Strategy string                 `json:"strategy"`
	Enabled  bool                   `json:"enabled"`
	Targets  []upstream.RouteTarget `json:"targets"`
}

type adminRouteDeleteRequest struct {
	Alias string `json:"alias"`
}

type adminUpstreamDiagnoseRequest struct {
	ID           string `json:"id,omitempty"`
	Model        string `json:"model,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	UserPrompt   string `json:"user_prompt,omitempty"`
}

func (h *handler) adminUpstreamsList(w http.ResponseWriter, r *http.Request) {
	if h.upstream == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "upstream_service_unavailable", "server_error", "upstream service unavailable", "service")
		return
	}
	items, err := h.upstream.ListUpstreams()
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *handler) adminUpstreamsUpsert(w http.ResponseWriter, r *http.Request) {
	if h.upstream == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "upstream_service_unavailable", "server_error", "upstream service unavailable", "service")
		return
	}
	defer r.Body.Close()

	var req adminUpstreamUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "invalid JSON request body", "body")
		return
	}

	saved, err := h.upstream.UpsertUpstream(upstream.UpsertUpstreamRequest{
		ID:      req.ID,
		Name:    req.Name,
		Type:    req.Type,
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
		Models:  req.Models,
		Enabled: req.Enabled,
	})
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": saved})
}

func (h *handler) adminUpstreamsDelete(w http.ResponseWriter, r *http.Request) {
	if h.upstream == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "upstream_service_unavailable", "server_error", "upstream service unavailable", "service")
		return
	}

	upstreamID := strings.TrimSpace(r.URL.Query().Get("id"))
	if upstreamID == "" {
		defer r.Body.Close()
		var req adminUpstreamDeleteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			upstreamID = strings.TrimSpace(req.ID)
		}
	}

	if err := h.upstream.DeleteUpstream(upstreamID); err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": upstreamID})
}

func (h *handler) adminRoutesList(w http.ResponseWriter, r *http.Request) {
	if h.upstream == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "upstream_service_unavailable", "server_error", "upstream service unavailable", "service")
		return
	}
	items, err := h.upstream.ListRoutes()
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (h *handler) adminRoutesUpsert(w http.ResponseWriter, r *http.Request) {
	if h.upstream == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "upstream_service_unavailable", "server_error", "upstream service unavailable", "service")
		return
	}
	defer r.Body.Close()

	var req adminRouteUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "invalid JSON request body", "body")
		return
	}
	saved, err := h.upstream.UpsertRoute(upstream.UpsertRouteRequest{
		Alias:    req.Alias,
		Strategy: req.Strategy,
		Enabled:  req.Enabled,
		Targets:  req.Targets,
	})
	if err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": saved})
}

func (h *handler) adminRoutesDelete(w http.ResponseWriter, r *http.Request) {
	if h.upstream == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "upstream_service_unavailable", "server_error", "upstream service unavailable", "service")
		return
	}

	alias := strings.TrimSpace(r.URL.Query().Get("alias"))
	if alias == "" {
		defer r.Body.Close()
		var req adminRouteDeleteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			alias = strings.TrimSpace(req.Alias)
		}
	}

	if err := h.upstream.DeleteRoute(alias); err != nil {
		writeUpstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "alias": alias})
}

func (h *handler) adminUpstreamsDiagnose(w http.ResponseWriter, r *http.Request) {
	if h.upstream == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "upstream_service_unavailable", "server_error", "upstream service unavailable", "service")
		return
	}

	req := adminUpstreamDiagnoseRequest{
		ID:           strings.TrimSpace(r.URL.Query().Get("id")),
		Model:        strings.TrimSpace(r.URL.Query().Get("model")),
		SystemPrompt: strings.TrimSpace(r.URL.Query().Get("system_prompt")),
		UserPrompt:   strings.TrimSpace(r.URL.Query().Get("user_prompt")),
	}
	if req.ID == "" || req.Model == "" || req.SystemPrompt == "" || req.UserPrompt == "" {
		defer r.Body.Close()
		var bodyReq adminUpstreamDiagnoseRequest
		if err := json.NewDecoder(r.Body).Decode(&bodyReq); err == nil {
			if req.ID == "" {
				req.ID = strings.TrimSpace(bodyReq.ID)
			}
			if req.Model == "" {
				req.Model = strings.TrimSpace(bodyReq.Model)
			}
			if req.SystemPrompt == "" {
				req.SystemPrompt = strings.TrimSpace(bodyReq.SystemPrompt)
			}
			if req.UserPrompt == "" {
				req.UserPrompt = strings.TrimSpace(bodyReq.UserPrompt)
			}
		} else if !errors.Is(err, io.EOF) {
			writeOpenAIError(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "invalid JSON request body", "body")
			return
		}
	}

	payload, err := h.performUpstreamDiagnose(r.Context(), req.ID, req.Model, req.SystemPrompt, req.UserPrompt)
	if err != nil {
		switch {
		case errors.Is(err, errMissingUpstreamID):
			writeOpenAIError(w, r, http.StatusBadRequest, "invalid_upstream_request", "invalid_request_error", err.Error(), "upstream")
		case errors.Is(err, upstream.ErrUpstreamNotFound):
			writeUpstreamError(w, r, err)
		default:
			writeOpenAIError(w, r, http.StatusInternalServerError, "internal_error", "server_error", err.Error(), "")
		}
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *handler) adminRoutesRuntimeStats(w http.ResponseWriter, r *http.Request) {
	if h.upstream == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "upstream_service_unavailable", "server_error", "upstream service unavailable", "service")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": h.upstream.RuntimeStats()})
}

func writeUpstreamError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, upstream.ErrInvalidUpstream), errors.Is(err, upstream.ErrInvalidRoute):
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_upstream_request", "invalid_request_error", err.Error(), "upstream")
	case errors.Is(err, upstream.ErrUpstreamNotFound), errors.Is(err, upstream.ErrRouteNotFound):
		writeOpenAIError(w, r, http.StatusNotFound, "upstream_not_found", "invalid_request_error", err.Error(), "upstream")
	default:
		writeOpenAIError(w, r, http.StatusInternalServerError, "internal_error", "server_error", "internal server error", "")
	}
}

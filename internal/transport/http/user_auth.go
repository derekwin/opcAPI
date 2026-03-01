package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/derekwin/opcAPI/internal/auth"
	"github.com/derekwin/opcAPI/internal/keymgmt"
	"github.com/derekwin/opcAPI/internal/user"
)

type signupResponse struct {
	user.AuthResult
	APIKey string `json:"api_key,omitempty"`
}

func (h *handler) authSignup(w http.ResponseWriter, r *http.Request) {
	if h.userService == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "server_error", "user service unavailable", "service")
		return
	}
	defer r.Body.Close()

	var req user.SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "invalid JSON request body", "body")
		return
	}

	res, err := h.userService.Signup(r.Context(), req)
	if err != nil {
		writeUserError(w, r, err)
		return
	}

	apiKey := ""
	if h.policySync != nil {
		provisioned := auth.GenerateAPIKey("sk-opcapi-")
		tenantID := res.Tenant.ID
		enabled := true

		if _, err := h.policySync.Upsert(r.Context(), keymgmt.UpsertPolicyInput{
			APIKey:   provisioned,
			TenantID: &tenantID,
			Enabled:  &enabled,
		}); err != nil {
			if h.logger != nil {
				h.logger.Error("signup api key provision failed", "tenant_id", tenantID, "err", err, "request_id", RequestIDFromContext(r.Context()))
			}
			writeOpenAIError(w, r, http.StatusInternalServerError, "api_key_provision_failed", "server_error", "failed to provision tenant api key", "api_key")
			return
		}
		apiKey = provisioned
	}

	writeJSON(w, http.StatusOK, signupResponse{AuthResult: res, APIKey: apiKey})
}

func (h *handler) authSignin(w http.ResponseWriter, r *http.Request) {
	if h.userService == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "server_error", "user service unavailable", "service")
		return
	}
	defer r.Body.Close()

	var req user.SigninRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_json", "invalid_request_error", "invalid JSON request body", "body")
		return
	}

	res, err := h.userService.Signin(r.Context(), req)
	if err != nil {
		writeUserError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *handler) authMe(w http.ResponseWriter, r *http.Request) {
	if h.userService == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "server_error", "user service unavailable", "service")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if claims.UserID == "" {
		writeOpenAIError(w, r, http.StatusUnauthorized, "invalid_session", "authentication_error", "invalid session", "authorization")
		return
	}

	res, err := h.userService.GetMe(r.Context(), claims)
	if err != nil {
		writeUserError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *handler) adminUsers(w http.ResponseWriter, r *http.Request) {
	if h.userService == nil {
		writeOpenAIError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "server_error", "user service unavailable", "service")
		return
	}
	claims := SessionClaimsFromContext(r.Context())
	if claims.UserID == "" {
		writeOpenAIError(w, r, http.StatusUnauthorized, "invalid_session", "authentication_error", "invalid session", "authorization")
		return
	}

	users, err := h.userService.ListTenantUsers(r.Context(), claims)
	if err != nil {
		writeUserError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": users})
}

func writeUserError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, user.ErrInvalidChannel), errors.Is(err, user.ErrInvalidIdentifier), errors.Is(err, user.ErrInvalidVerifyCode), errors.Is(err, user.ErrInvalidRole), errors.Is(err, user.ErrInvalidStatus):
		writeOpenAIError(w, r, http.StatusBadRequest, "invalid_auth_request", "invalid_request_error", err.Error(), "auth")
	case errors.Is(err, user.ErrUserExists):
		writeOpenAIError(w, r, http.StatusConflict, "user_exists", "invalid_request_error", err.Error(), "user")
	case errors.Is(err, user.ErrUserNotFound):
		writeOpenAIError(w, r, http.StatusNotFound, "user_not_found", "invalid_request_error", err.Error(), "user")
	case errors.Is(err, user.ErrUserDisabled):
		writeOpenAIError(w, r, http.StatusForbidden, "user_disabled", "permission_error", err.Error(), "user")
	case errors.Is(err, user.ErrTenantNotFound):
		writeOpenAIError(w, r, http.StatusNotFound, "tenant_not_found", "invalid_request_error", err.Error(), "tenant")
	case errors.Is(err, user.ErrSessionTokenInvalid), errors.Is(err, user.ErrSessionTokenExpired):
		writeOpenAIError(w, r, http.StatusUnauthorized, "invalid_session", "authentication_error", err.Error(), "authorization")
	case errors.Is(err, user.ErrForbidden), strings.Contains(err.Error(), "forbidden"):
		writeOpenAIError(w, r, http.StatusForbidden, "forbidden", "permission_error", "forbidden", "role")
	default:
		writeOpenAIError(w, r, http.StatusInternalServerError, "internal_error", "server_error", "internal server error", "")
	}
}

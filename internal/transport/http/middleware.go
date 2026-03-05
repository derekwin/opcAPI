package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/derekwin/opcAPI/internal/auth"
	"github.com/derekwin/opcAPI/internal/billing"
	"github.com/derekwin/opcAPI/internal/quota"
	"github.com/derekwin/opcAPI/internal/user"
)

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	apiKeyKey    contextKey = "api_key"
	sessionKey   contextKey = "session"
	sessionToken contextKey = "session_token"
)

var requestCounter uint64

func requestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := nextRequestID()
			ctx := context.WithValue(r.Context(), requestIDKey, requestID)
			w.Header().Set("X-Request-ID", requestID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func apiKeyAuth(manager *auth.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if manager == nil || !manager.Enabled() {
				next.ServeHTTP(w, r)
				return
			}

			apiKey, err := manager.Authenticate(r.Header.Get("Authorization"))
			if err != nil {
				status := http.StatusUnauthorized
				code := "invalid_api_key"
				msg := "invalid API key"
				switch {
				case errors.Is(err, auth.ErrMissingAuthorization):
					code = "missing_api_key"
					msg = "missing Authorization header"
				case errors.Is(err, auth.ErrInvalidAuthorization):
					code = "invalid_authorization_header"
					msg = "invalid Authorization header format"
				}
				writeOpenAIError(w, r, status, code, "authentication_error", msg, "authorization")
				return
			}

			ctx := context.WithValue(r.Context(), apiKeyKey, apiKey)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func requestQuota(limiter *quota.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil {
				next.ServeHTTP(w, r)
				return
			}

			subject := quotaSubjectFromContext(r.Context())

			decision := limiter.AllowRequest(subject)
			if !decision.Allowed {
				if decision.RetryAfterSec > 0 {
					w.Header().Set("Retry-After", fmt.Sprintf("%d", decision.RetryAfterSec))
				}
				writeOpenAIError(w, r, http.StatusTooManyRequests, decision.Reason, "rate_limit_error", "request rate limit exceeded", "rate_limit")
				return
			}

			if decision.Limit > 0 {
				w.Header().Set("X-RateLimit-Limit-Minute", fmt.Sprintf("%d", decision.Limit))
				w.Header().Set("X-RateLimit-Remaining-Minute", fmt.Sprintf("%d", decision.Remaining))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func membershipQuotaPolicy(billingService *billing.Service, limiter *quota.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if billingService == nil || limiter == nil {
				next.ServeHTTP(w, r)
				return
			}
			claims := SessionClaimsFromContext(r.Context())
			userID := strings.TrimSpace(claims.UserID)
			if userID != "" {
				p, err := billingService.ResolveUsagePolicy(r.Context(), userID)
				if err != nil {
					p = billing.FreeUsagePolicy()
				}
				limiter.SetKeyPolicy(quotaSubjectFromContext(r.Context()), quota.KeyPolicy{
					RPMLimit:           p.RPMLimit,
					DailyTokenLimit:    0,
					FiveHourTokenLimit: p.FiveHourTokenLimit,
					MonthlyTokenLimit:  p.MonthlyTokenLimit,
				})
			}
			next.ServeHTTP(w, r)
		})
	}
}

func quotaSubjectFromContext(ctx context.Context) string {
	claims := SessionClaimsFromContext(ctx)
	if userID := strings.TrimSpace(claims.UserID); userID != "" {
		return "user:" + userID
	}
	apiKey := APIKeyFromContext(ctx)
	if apiKey != "" {
		return apiKey
	}
	return "anonymous"
}

func sessionAuth(userService *user.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if userService == nil {
				writeOpenAIError(w, r, http.StatusServiceUnavailable, "user_service_unavailable", "server_error", "user service unavailable", "service")
				return
			}

			token, err := bearerToken(r.Header.Get("Authorization"))
			if err != nil {
				writeOpenAIError(w, r, http.StatusUnauthorized, "invalid_session", "authentication_error", err.Error(), "authorization")
				return
			}

			claims, err := userService.ValidateSessionToken(token)
			if err != nil {
				writeOpenAIError(w, r, http.StatusUnauthorized, "invalid_session", "authentication_error", err.Error(), "authorization")
				return
			}

			ctx := context.WithValue(r.Context(), sessionKey, claims)
			ctx = context.WithValue(ctx, sessionToken, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func requireAdminRole() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := SessionClaimsFromContext(r.Context())
			if claims.Role != "admin" {
				writeOpenAIError(w, r, http.StatusForbidden, "forbidden", "permission_error", "forbidden", "role")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func accessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)

			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"bytes", rw.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestIDFromContext(r.Context()),
			)
		})
	}
}

func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered", "panic", rec, "request_id", RequestIDFromContext(r.Context()))
					writeOpenAIError(w, r, http.StatusInternalServerError, "internal_error", "server_error", "internal server error", "")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

func APIKeyFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(apiKeyKey).(string); ok {
		return v
	}
	return ""
}

func SessionClaimsFromContext(ctx context.Context) user.SessionClaims {
	if v, ok := ctx.Value(sessionKey).(user.SessionClaims); ok {
		return v
	}
	return user.SessionClaims{}
}

func SessionTokenFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(sessionToken).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func bearerToken(header string) (string, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", auth.ErrMissingAuthorization
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", auth.ErrInvalidAuthorization
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", auth.ErrInvalidAuthorization
	}
	return token, nil
}

func nextRequestID() string {
	n := atomic.AddUint64(&requestCounter, 1)
	return fmt.Sprintf("req-%d-%d", time.Now().UnixNano(), n)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusRecorder) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Flush keeps streaming handlers working when wrapped by middleware.
func (w *statusRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

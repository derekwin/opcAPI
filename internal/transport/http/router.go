package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/derekwin/opcAPI/internal/audit"
	"github.com/derekwin/opcAPI/internal/auth"
	"github.com/derekwin/opcAPI/internal/billing"
	"github.com/derekwin/opcAPI/internal/keymgmt"
	"github.com/derekwin/opcAPI/internal/promptapi"
	"github.com/derekwin/opcAPI/internal/provider"
	"github.com/derekwin/opcAPI/internal/quota"
	"github.com/derekwin/opcAPI/internal/user"
)

type handler struct {
	logger                *slog.Logger
	gateway               provider.Gateway
	authManager           *auth.Manager
	requireTenantBoundKey bool
	limiter               *quota.Limiter
	policySync            *keymgmt.PolicySyncer
	userService           *user.Service
	billing               *billing.Service
	promptAPI             *promptapi.Service
	audit                 *audit.Service
}

func NewRouter(logger *slog.Logger, gateway provider.Gateway, authManager *auth.Manager, requireTenantBoundKey bool, limiter *quota.Limiter, policySyncer *keymgmt.PolicySyncer, userService *user.Service, billingService *billing.Service, promptService *promptapi.Service, auditService *audit.Service) http.Handler {
	if gateway == nil {
		gateway = provider.NewStubGateway()
	}
	if authManager == nil {
		authManager = auth.NewStaticManager(false, nil)
	}
	if limiter == nil {
		limiter = quota.NewLimiter(0, 0)
	}
	h := &handler{
		logger:                logger,
		gateway:               gateway,
		authManager:           authManager,
		requireTenantBoundKey: requireTenantBoundKey,
		limiter:               limiter,
		policySync:            policySyncer,
		userService:           userService,
		billing:               billingService,
		promptAPI:             promptService,
		audit:                 auditService,
	}

	mux := http.NewServeMux()
	mux.Handle("/healthz", methodHandler(http.MethodGet, http.HandlerFunc(h.healthz)))
	mux.Handle("/admin/login", methodHandlers(map[string]http.Handler{
		http.MethodGet:  http.HandlerFunc(h.adminLoginPage),
		http.MethodPost: http.HandlerFunc(h.adminLoginSubmit),
	}))
	mux.Handle("/admin/logout", methodHandlers(map[string]http.Handler{
		http.MethodGet:  http.HandlerFunc(h.adminLogout),
		http.MethodPost: http.HandlerFunc(h.adminLogout),
	}))
	mux.Handle("/admin", chain(
		methodHandler(http.MethodGet, http.HandlerFunc(h.adminDashboard)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/keys/new", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminKeyCreate)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/keys/delete", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminKeyDelete)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/prompt-apis/new", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminPromptCreate)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/billing/topup", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminBillingTopup)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/users/new", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminUserCreate)),
		adminSessionAuth(h.userService),
		adminRequireOwnerRole(),
	))
	mux.Handle("/admin/users/role", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminUserRoleUpdate)),
		adminSessionAuth(h.userService),
		adminRequireOwnerRole(),
	))
	mux.Handle("/admin/users/status", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminUserStatusUpdate)),
		adminSessionAuth(h.userService),
		adminRequireOwnerRole(),
	))
	mux.Handle("/v1/auth/signup", methodHandler(http.MethodPost, http.HandlerFunc(h.authSignup)))
	mux.Handle("/v1/auth/signin", methodHandler(http.MethodPost, http.HandlerFunc(h.authSignin)))
	mux.Handle("/v1/auth/me", methodHandler(http.MethodGet, chain(
		http.HandlerFunc(h.authMe),
		sessionAuth(h.userService),
	)))
	mux.Handle("/v1/admin/users", methodHandler(http.MethodGet, chain(
		http.HandlerFunc(h.adminUsers),
		sessionAuth(h.userService),
		requireAdminRole(),
	)))
	mux.Handle("/v1/admin/keys", chain(
		methodHandlers(map[string]http.Handler{
			http.MethodGet:    http.HandlerFunc(h.adminKeysList),
			http.MethodPost:   http.HandlerFunc(h.adminKeysUpsert),
			http.MethodDelete: http.HandlerFunc(h.adminKeysDelete),
		}),
		sessionAuth(h.userService),
		requireAdminRole(),
	))
	mux.Handle("/v1/admin/prompt-apis", chain(
		methodHandlers(map[string]http.Handler{
			http.MethodGet:  http.HandlerFunc(h.adminPromptAPIList),
			http.MethodPost: http.HandlerFunc(h.adminPromptAPIUpsert),
		}),
		sessionAuth(h.userService),
		requireAdminRole(),
	))
	mux.Handle("/v1/admin/audit/logs", methodHandler(http.MethodGet, chain(
		http.HandlerFunc(h.adminAuditLogs),
		sessionAuth(h.userService),
		requireAdminRole(),
	)))
	mux.Handle("/v1/billing/balance", methodHandler(http.MethodGet, chain(
		http.HandlerFunc(h.billingBalance),
		sessionAuth(h.userService),
	)))
	mux.Handle("/v1/billing/topup", methodHandler(http.MethodPost, chain(
		http.HandlerFunc(h.billingTopup),
		sessionAuth(h.userService),
	)))
	mux.Handle("/v1/billing/subscribe", methodHandler(http.MethodPost, chain(
		http.HandlerFunc(h.billingSubscribe),
		sessionAuth(h.userService),
	)))
	mux.Handle("/v1/admin/billing/orders", methodHandler(http.MethodGet, chain(
		http.HandlerFunc(h.adminBillingOrders),
		sessionAuth(h.userService),
		requireAdminRole(),
	)))

	mux.Handle("/v1/models", methodHandler(http.MethodGet, chain(
		http.HandlerFunc(h.models),
		apiKeyAuth(h.authManager),
		requestQuota(h.limiter),
	)))
	mux.Handle("/v1/chat/completions", methodHandler(http.MethodPost, chain(
		http.HandlerFunc(h.chatCompletions),
		apiKeyAuth(h.authManager),
		requestQuota(h.limiter),
	)))
	mux.Handle("/v1/prompt-apis/", chain(
		http.HandlerFunc(h.promptAPIInvoke),
		apiKeyAuth(h.authManager),
		requestQuota(h.limiter),
	))

	return chain(
		mux,
		recoverer(logger),
		requestID(),
		accessLog(logger),
	)
}

func (h *handler) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func methodHandler(method string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			writeOpenAIError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "invalid_request_error", "unsupported method", "method")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func methodHandlers(routes map[string]http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if next, ok := routes[r.Method]; ok {
			next.ServeHTTP(w, r)
			return
		}
		writeOpenAIError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "invalid_request_error", "unsupported method", "method")
	})
}

func chain(next http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		next = middlewares[i](next)
	}
	return next
}

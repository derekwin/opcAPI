package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/derekwin/opcAPI/internal/audit"
	"github.com/derekwin/opcAPI/internal/auth"
	"github.com/derekwin/opcAPI/internal/billing"
	"github.com/derekwin/opcAPI/internal/promptapi"
	"github.com/derekwin/opcAPI/internal/provider"
	"github.com/derekwin/opcAPI/internal/quota"
	"github.com/derekwin/opcAPI/internal/upstream"
	"github.com/derekwin/opcAPI/internal/user"
)

type handler struct {
	logger      *slog.Logger
	gateway     provider.Gateway
	authManager *auth.Manager
	limiter     *quota.Limiter
	userService *user.Service
	billing     *billing.Service
	promptAPI   *promptapi.Service
	upstream    *upstream.Service
	audit       *audit.Service
}

func NewRouter(logger *slog.Logger, gateway provider.Gateway, authManager *auth.Manager, limiter *quota.Limiter, userService *user.Service, billingService *billing.Service, promptService *promptapi.Service, upstreamService *upstream.Service, auditService *audit.Service) http.Handler {
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
		logger:      logger,
		gateway:     gateway,
		authManager: authManager,
		limiter:     limiter,
		userService: userService,
		billing:     billingService,
		promptAPI:   promptService,
		upstream:    upstreamService,
		audit:       auditService,
	}

	mux := http.NewServeMux()
	mux.Handle("/healthz", methodHandler(http.MethodGet, http.HandlerFunc(h.healthz)))
	mux.Handle("/admin/login", methodHandlers(map[string]http.Handler{
		http.MethodGet:  http.HandlerFunc(h.adminLoginPage),
		http.MethodPost: http.HandlerFunc(h.adminLoginSubmit),
	}))
	mux.Handle("/admin/setup", methodHandlers(map[string]http.Handler{
		http.MethodGet:  http.HandlerFunc(h.adminSetupPage),
		http.MethodPost: http.HandlerFunc(h.adminSetupSubmit),
	}))
	mux.Handle("/admin/logout", methodHandlers(map[string]http.Handler{
		http.MethodGet:  http.HandlerFunc(h.adminLogout),
		http.MethodPost: http.HandlerFunc(h.adminLogout),
	}))
	mux.Handle("/admin", chain(
		methodHandler(http.MethodGet, http.HandlerFunc(h.adminHome)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/users", chain(
		methodHandler(http.MethodGet, http.HandlerFunc(h.adminUsersPage)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/prompts", chain(
		methodHandler(http.MethodGet, http.HandlerFunc(h.adminPromptsPage)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/orders", chain(
		methodHandler(http.MethodGet, http.HandlerFunc(h.adminOrdersPage)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/upstreams", chain(
		methodHandler(http.MethodGet, http.HandlerFunc(h.adminUpstreamsPage)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/routes", chain(
		methodHandler(http.MethodGet, http.HandlerFunc(h.adminUpstreamsPage)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/prompt-apis/new", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminPromptCreate)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/prompt-apis/preview", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminPromptPreview)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/prompt-apis/delete", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminPromptDelete)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/default-apis/update", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminDefaultAPIUpdate)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/billing/plans", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminBillingPlansUpdate)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/users/new", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminUserCreate)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/users/role", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminUserRoleUpdate)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/users/status", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminUserStatusUpdate)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/users/membership", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminUserMembershipUpdate)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/users/delete", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminUserDelete)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/orders/delete", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminOrderDelete)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/upstreams/new", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminUpstreamCreate)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/upstreams/delete", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminUpstreamDelete)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/upstreams/diagnose", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminUpstreamDiagnose)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/upstreams/toggle", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminUpstreamToggleSubmit)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/routes/new", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminRouteCreate)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/admin/routes/delete", chain(
		methodHandler(http.MethodPost, http.HandlerFunc(h.adminRouteDelete)),
		adminSessionAuth(h.userService),
		adminRequireAdminRole(),
	))
	mux.Handle("/api/auth/signup", methodHandler(http.MethodPost, http.HandlerFunc(h.authSignup)))
	mux.Handle("/api/auth/signin", methodHandler(http.MethodPost, http.HandlerFunc(h.authSignin)))
	mux.Handle("/api/auth/me", methodHandler(http.MethodGet, chain(
		http.HandlerFunc(h.authMe),
		sessionAuth(h.userService),
	)))
	mux.Handle("/api/auth/logout", methodHandler(http.MethodPost, chain(
		http.HandlerFunc(h.authLogout),
		sessionAuth(h.userService),
	)))
	mux.Handle("/api/admin/users", methodHandler(http.MethodGet, chain(
		http.HandlerFunc(h.adminUsers),
		sessionAuth(h.userService),
		requireAdminRole(),
	)))
	mux.Handle("/api/admin/prompt-apis", chain(
		methodHandlers(map[string]http.Handler{
			http.MethodGet:  http.HandlerFunc(h.adminPromptAPIList),
			http.MethodPost: http.HandlerFunc(h.adminPromptAPIUpsert),
		}),
		sessionAuth(h.userService),
		requireAdminRole(),
	))
	mux.Handle("/api/admin/default-apis", chain(
		methodHandlers(map[string]http.Handler{
			http.MethodGet:  http.HandlerFunc(h.adminDefaultAPIList),
			http.MethodPost: http.HandlerFunc(h.adminDefaultAPIUpsert),
		}),
		sessionAuth(h.userService),
		requireAdminRole(),
	))
	mux.Handle("/api/admin/audit/logs", methodHandler(http.MethodGet, chain(
		http.HandlerFunc(h.adminAuditLogs),
		sessionAuth(h.userService),
		requireAdminRole(),
	)))
	mux.Handle("/api/billing/balance", methodHandler(http.MethodGet, chain(
		http.HandlerFunc(h.billingBalance),
		sessionAuth(h.userService),
	)))
	mux.Handle("/api/billing/subscribe", methodHandler(http.MethodPost, chain(
		http.HandlerFunc(h.billingSubscribe),
		sessionAuth(h.userService),
	)))
	mux.Handle("/api/admin/billing/orders", methodHandler(http.MethodGet, chain(
		http.HandlerFunc(h.adminBillingOrders),
		sessionAuth(h.userService),
		requireAdminRole(),
	)))
	mux.Handle("/api/admin/upstreams", chain(
		methodHandlers(map[string]http.Handler{
			http.MethodGet:    http.HandlerFunc(h.adminUpstreamsList),
			http.MethodPost:   http.HandlerFunc(h.adminUpstreamsUpsert),
			http.MethodDelete: http.HandlerFunc(h.adminUpstreamsDelete),
		}),
		sessionAuth(h.userService),
		requireAdminRole(),
	))
	mux.Handle("/api/admin/upstreams/diagnose", methodHandler(http.MethodPost, chain(
		http.HandlerFunc(h.adminUpstreamsDiagnose),
		sessionAuth(h.userService),
		requireAdminRole(),
	)))
	mux.Handle("/api/admin/routes", chain(
		methodHandlers(map[string]http.Handler{
			http.MethodGet:    http.HandlerFunc(h.adminRoutesList),
			http.MethodPost:   http.HandlerFunc(h.adminRoutesUpsert),
			http.MethodDelete: http.HandlerFunc(h.adminRoutesDelete),
		}),
		sessionAuth(h.userService),
		requireAdminRole(),
	))
	mux.Handle("/api/admin/routes/runtime-stats", methodHandler(http.MethodGet, chain(
		http.HandlerFunc(h.adminRoutesRuntimeStats),
		sessionAuth(h.userService),
		requireAdminRole(),
	)))

	mux.Handle("/v1/models", methodHandler(http.MethodGet, chain(
		http.HandlerFunc(h.models),
		sessionAuth(h.userService),
		membershipQuotaPolicy(h.billing, h.limiter),
		requestQuota(h.limiter),
	)))
	mux.Handle("/v1/chat/completions", methodHandler(http.MethodPost, chain(
		http.HandlerFunc(h.chatCompletions),
		sessionAuth(h.userService),
		membershipQuotaPolicy(h.billing, h.limiter),
		requestQuota(h.limiter),
	)))
	mux.Handle("/v1/prompt-apis/", chain(
		http.HandlerFunc(h.promptAPIInvoke),
		sessionAuth(h.userService),
		membershipQuotaPolicy(h.billing, h.limiter),
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

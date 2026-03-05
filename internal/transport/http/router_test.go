package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/derekwin/opcAPI/internal/audit"
	"github.com/derekwin/opcAPI/internal/auth"
	"github.com/derekwin/opcAPI/internal/billing"
	"github.com/derekwin/opcAPI/internal/config"
	"github.com/derekwin/opcAPI/internal/promptapi"
	"github.com/derekwin/opcAPI/internal/provider"
	"github.com/derekwin/opcAPI/internal/quota"
	"github.com/derekwin/opcAPI/internal/upstream"
	"github.com/derekwin/opcAPI/internal/user"
)

type fakeGateway struct {
	chatRes provider.ChatResult
	chatErr error
	models  []provider.ModelInfo
	lastReq provider.ChatRequest
}

func (f *fakeGateway) Chat(_ context.Context, req provider.ChatRequest) (provider.ChatResult, error) {
	f.lastReq = req
	if f.chatErr != nil {
		return provider.ChatResult{}, f.chatErr
	}
	if f.chatRes.Content == "" {
		f.chatRes = provider.ChatResult{Model: "stub/test", Content: "ok"}
	}
	return f.chatRes, nil
}

func (f *fakeGateway) Models(_ context.Context) []provider.ModelInfo {
	if len(f.models) == 0 {
		return []provider.ModelInfo{{ID: "stub/opcapi-mvp", OwnedBy: "opcapi"}}
	}
	return f.models
}

func newTestRouter(t *testing.T, g provider.Gateway) http.Handler {
	t.Helper()
	authManager := auth.NewStaticManager(true, []string{"sk-test"})
	limiter := quota.NewLimiter(100, 1000)
	upstreamService := newTestUpstreamService(t)
	if g == nil {
		g = provider.NewGatewayWithUpstream(config.Config{
			DefaultProvider: "stub",
			ProviderTimeout: 3 * time.Second,
		}, testLogger(), upstreamService)
	}
	return NewRouter(
		testLogger(),
		g,
		authManager,
		limiter,
		newTestUserService(t),
		newTestBillingService(t),
		newTestPromptAPIService(t),
		upstreamService,
		newTestAuditService(t),
	)
}

func newTestUserService(t *testing.T) *user.Service {
	t.Helper()
	store, err := user.NewFileStore(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatalf("new user store failed: %v", err)
	}
	return user.NewService(store, user.NewSessionManager("test-secret", 24*time.Hour), testLogger())
}

func newTestBillingService(t *testing.T) *billing.Service {
	t.Helper()
	store, err := billing.NewFileStore(filepath.Join(t.TempDir(), "billing.json"))
	if err != nil {
		t.Fatalf("new billing store failed: %v", err)
	}
	return billing.NewService(store, testLogger())
}

func newTestPromptAPIService(t *testing.T) *promptapi.Service {
	t.Helper()
	store, err := promptapi.NewFileStore(filepath.Join(t.TempDir(), "prompt_apis.json"))
	if err != nil {
		t.Fatalf("new prompt api store failed: %v", err)
	}
	return promptapi.NewService(store, testLogger())
}

func newTestAuditService(t *testing.T) *audit.Service {
	t.Helper()
	store, err := audit.NewFileStore(filepath.Join(t.TempDir(), "audit.json"))
	if err != nil {
		t.Fatalf("new audit store failed: %v", err)
	}
	return audit.NewService(store, testLogger())
}

func newTestUpstreamService(t *testing.T) *upstream.Service {
	t.Helper()
	store, err := upstream.NewFileStore(filepath.Join(t.TempDir(), "upstreams.json"))
	if err != nil {
		t.Fatalf("new upstream store failed: %v", err)
	}
	return upstream.NewService(store, testLogger())
}

func signup(t *testing.T, router http.Handler, email, password string) map[string]any {
	t.Helper()
	payload := map[string]any{"email": email, "password": password}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	return out
}

func signin(t *testing.T, router http.Handler, email, password string) string {
	t.Helper()
	payload := map[string]any{"email": email, "password": password}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/signin", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("signin expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	token, _ := out["token"].(string)
	if token == "" {
		t.Fatalf("expected token")
	}
	return token
}

func setupAdminWeb(t *testing.T, router http.Handler, email, password string) *http.Cookie {
	t.Helper()
	form := urlEncoded(map[string]string{
		"email":            email,
		"password":         password,
		"password_confirm": password,
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/setup", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("admin setup expected 303, got %d: %s", rr.Code, rr.Body.String())
	}

	var sessionCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == adminSessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("expected admin session cookie from setup")
	}
	return sessionCookie
}

func bootstrapAdminToken(t *testing.T, router http.Handler, email, password string) string {
	t.Helper()
	_ = setupAdminWeb(t, router, email, password)
	return signin(t, router, email, password)
}

func TestHealthz(t *testing.T) {
	router := newTestRouter(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAuthSignupSigninMeLogout(t *testing.T) {
	router := newTestRouter(t, nil)
	res := signup(t, router, "admin@example.com", "Password123")
	token, _ := res["token"].(string)
	if token == "" {
		t.Fatalf("expected token")
	}

	reqMe := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	reqMe.Header.Set("Authorization", "Bearer "+token)
	rrMe := httptest.NewRecorder()
	router.ServeHTTP(rrMe, reqMe)
	if rrMe.Code != http.StatusOK {
		t.Fatalf("me expected 200, got %d: %s", rrMe.Code, rrMe.Body.String())
	}

	reqLogout := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	reqLogout.Header.Set("Authorization", "Bearer "+token)
	rrLogout := httptest.NewRecorder()
	router.ServeHTTP(rrLogout, reqLogout)
	if rrLogout.Code != http.StatusOK {
		t.Fatalf("logout expected 200, got %d: %s", rrLogout.Code, rrLogout.Body.String())
	}

	reqMe2 := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	reqMe2.Header.Set("Authorization", "Bearer "+token)
	rrMe2 := httptest.NewRecorder()
	router.ServeHTTP(rrMe2, reqMe2)
	if rrMe2.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout expected 401, got %d: %s", rrMe2.Code, rrMe2.Body.String())
	}
}

func TestSignupSessionAndModels(t *testing.T) {
	router := newTestRouter(t, nil)
	res := signup(t, router, "u1@example.com", "Password123")
	token, _ := res["token"].(string)
	if token == "" {
		t.Fatalf("expected token from signup")
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("models expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"id":"stub/opcapi-mvp"`) {
		t.Fatalf("models should expose gateway models, got %s", rr.Body.String())
	}
}

func TestV1RequiresSessionToken(t *testing.T) {
	router := newTestRouter(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer sk-test")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("v1 models should require session token, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminUsersRoleEnforcement(t *testing.T) {
	router := newTestRouter(t, nil)
	adminToken := bootstrapAdminToken(t, router, "admin@example.com", "Password123")
	memberRes := signup(t, router, "member@example.com", "Password123")

	memberToken, _ := memberRes["token"].(string)
	if memberToken == "" {
		t.Fatalf("expected tokens")
	}

	req1 := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req1.Header.Set("Authorization", "Bearer "+adminToken)
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("admin users expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req2.Header.Set("Authorization", "Bearer "+memberToken)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusForbidden {
		t.Fatalf("member should be forbidden, got %d: %s", rr2.Code, rr2.Body.String())
	}
}

func TestPromptAPIAdminAndInvoke(t *testing.T) {
	g := &fakeGateway{chatRes: provider.ChatResult{Model: "stub/test", Content: "prompt-ok"}}
	router := newTestRouter(t, g)
	adminToken := bootstrapAdminToken(t, router, "admin@example.com", "Password123")
	memberRes := signup(t, router, "member@example.com", "Password123")
	memberToken, _ := memberRes["token"].(string)
	if adminToken == "" || memberToken == "" {
		t.Fatalf("expected admin token and member session token")
	}

	upsert := `{"slug":"email_summary","name":"Email Summary","model":"stub/test","system_prompt":"You are helpful","user_template":"summarize {{.content}}","variables":["content"],"publish":true}`
	req1 := httptest.NewRequest(http.MethodPost, "/api/admin/prompt-apis", strings.NewReader(upsert))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer "+adminToken)
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("upsert prompt api expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	invoke := `{"input":{"content":"hello opc"}}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/prompt-apis/email_summary/chat/completions", strings.NewReader(invoke))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+memberToken)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("invoke expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}
	if !strings.Contains(rr2.Body.String(), "prompt-ok") {
		t.Fatalf("unexpected invoke body: %s", rr2.Body.String())
	}
}

func TestChatCompletionsWithSessionAuth(t *testing.T) {
	g := &fakeGateway{chatRes: provider.ChatResult{Model: "stub/test", Content: "charged", PromptTokens: 100, CompletionTokens: 50}}
	router := newTestRouter(t, g)
	userRes := signup(t, router, "member@example.com", "Password123")
	token, _ := userRes["token"].(string)
	if token == "" {
		t.Fatalf("expected token")
	}

	chat := `{"messages":[{"role":"user","content":"hello"}]}`
	reqChat := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chat))
	reqChat.Header.Set("Content-Type", "application/json")
	reqChat.Header.Set("Authorization", "Bearer "+token)
	rrChat := httptest.NewRecorder()
	router.ServeHTTP(rrChat, reqChat)
	if rrChat.Code != http.StatusOK {
		t.Fatalf("chat expected 200, got %d: %s", rrChat.Code, rrChat.Body.String())
	}
	if !strings.Contains(rrChat.Body.String(), `"model":"app/default"`) {
		t.Fatalf("chat response should hide internal model, got %s", rrChat.Body.String())
	}

	reqBal := httptest.NewRequest(http.MethodGet, "/api/billing/balance", nil)
	reqBal.Header.Set("Authorization", "Bearer "+token)
	rrBal := httptest.NewRecorder()
	router.ServeHTTP(rrBal, reqBal)
	if rrBal.Code != http.StatusOK {
		t.Fatalf("balance expected 200, got %d: %s", rrBal.Code, rrBal.Body.String())
	}
	var bal map[string]any
	if err := json.Unmarshal(rrBal.Body.Bytes(), &bal); err != nil {
		t.Fatalf("unmarshal balance: %v", err)
	}
	membership, _ := bal["membership"].(map[string]any)
	plan, _ := membership["plan_id"].(string)
	if strings.TrimSpace(plan) == "" {
		t.Fatalf("expected membership plan in balance response, got %s", rrBal.Body.String())
	}
}

func TestDefaultAPIConfigControlsChatModelResolution(t *testing.T) {
	g := &fakeGateway{chatRes: provider.ChatResult{Model: "stub/test", Content: "ok"}}
	router := newTestRouter(t, g)
	adminToken := bootstrapAdminToken(t, router, "admin@example.com", "Password123")
	memberRes := signup(t, router, "member@example.com", "Password123")
	memberToken, _ := memberRes["token"].(string)
	if adminToken == "" || memberToken == "" {
		t.Fatalf("expected tokens")
	}

	cfgBody := `{"api_key":"chat_completions","enabled":true,"default_model":"route-default","allowed_models":["route-default"],"model_id_mappings":[{"model_id":"gpt-4o","model":"route-default"}]}`
	reqCfg := httptest.NewRequest(http.MethodPost, "/api/admin/default-apis", strings.NewReader(cfgBody))
	reqCfg.Header.Set("Authorization", "Bearer "+adminToken)
	reqCfg.Header.Set("Content-Type", "application/json")
	rrCfg := httptest.NewRecorder()
	router.ServeHTTP(rrCfg, reqCfg)
	if rrCfg.Code != http.StatusOK {
		t.Fatalf("upsert default api config expected 200, got %d: %s", rrCfg.Code, rrCfg.Body.String())
	}

	reqDefault := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"hello"}]}`))
	reqDefault.Header.Set("Authorization", "Bearer "+memberToken)
	reqDefault.Header.Set("Content-Type", "application/json")
	rrDefault := httptest.NewRecorder()
	router.ServeHTTP(rrDefault, reqDefault)
	if rrDefault.Code != http.StatusOK {
		t.Fatalf("chat with default model expected 200, got %d: %s", rrDefault.Code, rrDefault.Body.String())
	}
	if g.lastReq.Model != "route-default" {
		t.Fatalf("expected resolved default model route-default, got %s", g.lastReq.Model)
	}

	reqMapped := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`))
	reqMapped.Header.Set("Authorization", "Bearer "+memberToken)
	reqMapped.Header.Set("Content-Type", "application/json")
	rrMapped := httptest.NewRecorder()
	router.ServeHTTP(rrMapped, reqMapped)
	if rrMapped.Code != http.StatusOK {
		t.Fatalf("chat with mapped model expected 200, got %d: %s", rrMapped.Code, rrMapped.Body.String())
	}
	if g.lastReq.Model != "route-default" {
		t.Fatalf("expected mapped model to route-default, got %s", g.lastReq.Model)
	}
	if !strings.Contains(rrMapped.Body.String(), `"model":"gpt-4o"`) {
		t.Fatalf("expected response model to preserve caller model id, got %s", rrMapped.Body.String())
	}

	reqDenied := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"qwen-plus","messages":[{"role":"user","content":"hello"}]}`))
	reqDenied.Header.Set("Authorization", "Bearer "+memberToken)
	reqDenied.Header.Set("Content-Type", "application/json")
	rrDenied := httptest.NewRecorder()
	router.ServeHTTP(rrDenied, reqDenied)
	if rrDenied.Code != http.StatusBadRequest {
		t.Fatalf("chat with disallowed model expected 400, got %d: %s", rrDenied.Code, rrDenied.Body.String())
	}

	reqModels := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	reqModels.Header.Set("Authorization", "Bearer "+memberToken)
	rrModels := httptest.NewRecorder()
	router.ServeHTTP(rrModels, reqModels)
	if rrModels.Code != http.StatusOK {
		t.Fatalf("models expected 200, got %d: %s", rrModels.Code, rrModels.Body.String())
	}
	if !strings.Contains(rrModels.Body.String(), `"id":"gpt-4o"`) {
		t.Fatalf("models should expose configured external model ids, got %s", rrModels.Body.String())
	}
}

func TestAdminWebLoginAndDashboard(t *testing.T) {
	router := newTestRouter(t, nil)

	req0 := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rr0 := httptest.NewRecorder()
	router.ServeHTTP(rr0, req0)
	if rr0.Code != http.StatusSeeOther {
		t.Fatalf("first admin login page expected 303, got %d", rr0.Code)
	}
	if loc := rr0.Result().Header.Get("Location"); loc != "/admin/setup" {
		t.Fatalf("expected redirect to /admin/setup, got %s", loc)
	}

	_ = setupAdminWeb(t, router, "admin@example.com", "Password123")

	reqSetupAgain := httptest.NewRequest(http.MethodGet, "/admin/setup", nil)
	rrSetupAgain := httptest.NewRecorder()
	router.ServeHTTP(rrSetupAgain, reqSetupAgain)
	if rrSetupAgain.Code != http.StatusSeeOther {
		t.Fatalf("second setup page expected 303, got %d", rrSetupAgain.Code)
	}
	if loc := rrSetupAgain.Result().Header.Get("Location"); loc != "/admin/login" {
		t.Fatalf("expected redirect to /admin/login, got %s", loc)
	}

	reqLoginPage := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rrLoginPage := httptest.NewRecorder()
	router.ServeHTTP(rrLoginPage, reqLoginPage)
	if rrLoginPage.Code != http.StatusOK {
		t.Fatalf("admin login page expected 200, got %d: %s", rrLoginPage.Code, rrLoginPage.Body.String())
	}
	body := rrLoginPage.Body.String()
	if strings.Contains(body, "Sign Up") {
		t.Fatalf("login page should not expose sign up action")
	}
	if !strings.Contains(body, "Sign In") {
		t.Fatalf("login page should contain sign in action")
	}

	form := urlEncoded(map[string]string{
		"email":    "admin@example.com",
		"password": "Password123",
	})
	req1 := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form))
	req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusSeeOther {
		t.Fatalf("admin login expected 303, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var sessionCookie *http.Cookie
	for _, c := range rr1.Result().Cookies() {
		if c.Name == adminSessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("expected admin session cookie")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req2.AddCookie(sessionCookie)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusSeeOther {
		t.Fatalf("admin root expected 303, got %d: %s", rr2.Code, rr2.Body.String())
	}
	if loc := rr2.Result().Header.Get("Location"); loc != "/admin/users" {
		t.Fatalf("expected redirect to /admin/users, got %s", loc)
	}

	req3 := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req3.AddCookie(sessionCookie)
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("admin users page expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}
	if !strings.Contains(rr3.Body.String(), "Users") {
		t.Fatalf("expected dashboard title")
	}
}

func TestUpstreamError(t *testing.T) {
	router := newTestRouter(t, &fakeGateway{chatErr: errors.New("boom")})
	res := signup(t, router, "user-upstream-error@example.com", "Password123")
	token, _ := res["token"].(string)
	if token == "" {
		t.Fatalf("expected token")
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"stub/test","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminUpstreamRouteCanServeChatCompletions(t *testing.T) {
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"vendor-model","choices":[{"message":{"content":"from-upstream"}}],"usage":{"prompt_tokens":12,"completion_tokens":5}}`))
	}))
	defer upstreamSrv.Close()

	router := newTestRouter(t, nil)
	adminToken := bootstrapAdminToken(t, router, "admin@example.com", "Password123")
	memberRes := signup(t, router, "member@example.com", "Password123")
	memberToken, _ := memberRes["token"].(string)
	if adminToken == "" || memberToken == "" {
		t.Fatalf("expected admin token and member token")
	}

	upstreamBody := `{"name":"vendor-a","type":"openai_compat","base_url":"` + upstreamSrv.URL + `","api_key":"","enabled":true}`
	reqUp := httptest.NewRequest(http.MethodPost, "/api/admin/upstreams", strings.NewReader(upstreamBody))
	reqUp.Header.Set("Authorization", "Bearer "+adminToken)
	reqUp.Header.Set("Content-Type", "application/json")
	rrUp := httptest.NewRecorder()
	router.ServeHTTP(rrUp, reqUp)
	if rrUp.Code != http.StatusOK {
		t.Fatalf("upsert upstream expected 200, got %d: %s", rrUp.Code, rrUp.Body.String())
	}
	var upResp map[string]any
	if err := json.Unmarshal(rrUp.Body.Bytes(), &upResp); err != nil {
		t.Fatalf("decode upstream response failed: %v", err)
	}
	data, _ := upResp["data"].(map[string]any)
	upstreamID, _ := data["id"].(string)
	if upstreamID == "" {
		t.Fatalf("expected upstream id")
	}

	routeBody := `{"alias":"routed-model","strategy":"fallback","enabled":true,"targets":[{"upstream_id":"` + upstreamID + `","model":"vendor-model","weight":100,"priority":1,"enabled":true}]}`
	reqRoute := httptest.NewRequest(http.MethodPost, "/api/admin/routes", strings.NewReader(routeBody))
	reqRoute.Header.Set("Authorization", "Bearer "+adminToken)
	reqRoute.Header.Set("Content-Type", "application/json")
	rrRoute := httptest.NewRecorder()
	router.ServeHTTP(rrRoute, reqRoute)
	if rrRoute.Code != http.StatusOK {
		t.Fatalf("upsert route expected 200, got %d: %s", rrRoute.Code, rrRoute.Body.String())
	}

	chatReq := `{"model":"routed-model","messages":[{"role":"user","content":"hello"}]}`
	reqChat := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatReq))
	reqChat.Header.Set("Authorization", "Bearer "+memberToken)
	reqChat.Header.Set("Content-Type", "application/json")
	rrChat := httptest.NewRecorder()
	router.ServeHTTP(rrChat, reqChat)
	if rrChat.Code != http.StatusOK {
		t.Fatalf("routed chat expected 200, got %d: %s", rrChat.Code, rrChat.Body.String())
	}
	if !strings.Contains(rrChat.Body.String(), "from-upstream") {
		t.Fatalf("expected upstream content, got %s", rrChat.Body.String())
	}
}

func TestAdminUpstreamHealthCheckAndRuntimeStatsAPI(t *testing.T) {
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"vendor-model","object":"model"}]}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"vendor-model","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstreamSrv.Close()

	router := newTestRouter(t, nil)
	adminToken := bootstrapAdminToken(t, router, "admin@example.com", "Password123")
	memberRes := signup(t, router, "member@example.com", "Password123")
	memberToken, _ := memberRes["token"].(string)
	if adminToken == "" || memberToken == "" {
		t.Fatalf("expected tokens")
	}

	reqUp := httptest.NewRequest(http.MethodPost, "/api/admin/upstreams", strings.NewReader(`{"name":"hc-up","type":"openai_compat","base_url":"`+upstreamSrv.URL+`","enabled":true}`))
	reqUp.Header.Set("Authorization", "Bearer "+adminToken)
	reqUp.Header.Set("Content-Type", "application/json")
	rrUp := httptest.NewRecorder()
	router.ServeHTTP(rrUp, reqUp)
	if rrUp.Code != http.StatusOK {
		t.Fatalf("upsert upstream expected 200, got %d: %s", rrUp.Code, rrUp.Body.String())
	}
	var upResp map[string]any
	if err := json.Unmarshal(rrUp.Body.Bytes(), &upResp); err != nil {
		t.Fatalf("decode upstream response failed: %v", err)
	}
	upstreamID, _ := upResp["data"].(map[string]any)["id"].(string)
	if upstreamID == "" {
		t.Fatalf("expected upstream id")
	}

	reqCheck := httptest.NewRequest(http.MethodPost, "/api/admin/upstreams/diagnose", strings.NewReader(`{"id":"`+upstreamID+`","model":"vendor-model","user_prompt":"hello"}`))
	reqCheck.Header.Set("Authorization", "Bearer "+adminToken)
	reqCheck.Header.Set("Content-Type", "application/json")
	rrCheck := httptest.NewRecorder()
	router.ServeHTTP(rrCheck, reqCheck)
	if rrCheck.Code != http.StatusOK {
		t.Fatalf("upstream diagnose expected 200, got %d: %s", rrCheck.Code, rrCheck.Body.String())
	}
	if !strings.Contains(rrCheck.Body.String(), `"health":`) {
		t.Fatalf("expected health in diagnose response, got %s", rrCheck.Body.String())
	}
	if !strings.Contains(rrCheck.Body.String(), `"probe":`) {
		t.Fatalf("expected probe in diagnose response, got %s", rrCheck.Body.String())
	}
	if !strings.Contains(rrCheck.Body.String(), `"healthy":true`) {
		t.Fatalf("expected healthy diagnose result, got %s", rrCheck.Body.String())
	}
	if !strings.Contains(rrCheck.Body.String(), `"http_status":200`) {
		t.Fatalf("expected http_status in diagnose result, got %s", rrCheck.Body.String())
	}
	if !strings.Contains(rrCheck.Body.String(), `/v1/models`) {
		t.Fatalf("expected endpoint in diagnose result, got %s", rrCheck.Body.String())
	}
	reqLegacyCheck := httptest.NewRequest(http.MethodPost, "/api/admin/upstreams/check", strings.NewReader(`{"id":"`+upstreamID+`"}`))
	reqLegacyCheck.Header.Set("Authorization", "Bearer "+adminToken)
	reqLegacyCheck.Header.Set("Content-Type", "application/json")
	rrLegacyCheck := httptest.NewRecorder()
	router.ServeHTTP(rrLegacyCheck, reqLegacyCheck)
	if rrLegacyCheck.Code != http.StatusNotFound {
		t.Fatalf("legacy /api/admin/upstreams/check should be 404, got %d", rrLegacyCheck.Code)
	}

	reqRoute := httptest.NewRequest(http.MethodPost, "/api/admin/routes", strings.NewReader(`{"alias":"health-route","strategy":"fallback","enabled":true,"targets":[{"upstream_id":"`+upstreamID+`","model":"vendor-model","weight":100,"priority":1,"enabled":true}]}`))
	reqRoute.Header.Set("Authorization", "Bearer "+adminToken)
	reqRoute.Header.Set("Content-Type", "application/json")
	rrRoute := httptest.NewRecorder()
	router.ServeHTTP(rrRoute, reqRoute)
	if rrRoute.Code != http.StatusOK {
		t.Fatalf("upsert route expected 200, got %d: %s", rrRoute.Code, rrRoute.Body.String())
	}

	reqChat := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"health-route","messages":[{"role":"user","content":"hello"}]}`))
	reqChat.Header.Set("Authorization", "Bearer "+memberToken)
	reqChat.Header.Set("Content-Type", "application/json")
	rrChat := httptest.NewRecorder()
	router.ServeHTTP(rrChat, reqChat)
	if rrChat.Code != http.StatusOK {
		t.Fatalf("chat expected 200, got %d: %s", rrChat.Code, rrChat.Body.String())
	}

	reqStats := httptest.NewRequest(http.MethodGet, "/api/admin/routes/runtime-stats", nil)
	reqStats.Header.Set("Authorization", "Bearer "+adminToken)
	rrStats := httptest.NewRecorder()
	router.ServeHTTP(rrStats, reqStats)
	if rrStats.Code != http.StatusOK {
		t.Fatalf("runtime stats expected 200, got %d: %s", rrStats.Code, rrStats.Body.String())
	}
	if !strings.Contains(rrStats.Body.String(), `"model_alias":"health-route"`) {
		t.Fatalf("expected route stats model alias, got %s", rrStats.Body.String())
	}
}

func TestAdminUpstreamDiagnoseWebEndpoint(t *testing.T) {
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"qwen-plus","choices":[{"message":{"content":"probe-ok"}}],"usage":{"prompt_tokens":7,"completion_tokens":4}}`))
	}))
	defer upstreamSrv.Close()

	router := newTestRouter(t, nil)
	sessionCookie := setupAdminWeb(t, router, "admin@example.com", "Password123")
	adminToken := signin(t, router, "admin@example.com", "Password123")
	if adminToken == "" || sessionCookie == nil {
		t.Fatalf("expected admin token and session cookie")
	}

	reqUp := httptest.NewRequest(http.MethodPost, "/api/admin/upstreams", strings.NewReader(`{"name":"probe-up","type":"openai_compat","base_url":"`+upstreamSrv.URL+`","models":["qwen-plus"],"enabled":true}`))
	reqUp.Header.Set("Authorization", "Bearer "+adminToken)
	reqUp.Header.Set("Content-Type", "application/json")
	rrUp := httptest.NewRecorder()
	router.ServeHTTP(rrUp, reqUp)
	if rrUp.Code != http.StatusOK {
		t.Fatalf("upsert upstream expected 200, got %d: %s", rrUp.Code, rrUp.Body.String())
	}
	var upResp map[string]any
	if err := json.Unmarshal(rrUp.Body.Bytes(), &upResp); err != nil {
		t.Fatalf("decode upstream response failed: %v", err)
	}
	upstreamID, _ := upResp["data"].(map[string]any)["id"].(string)
	if upstreamID == "" {
		t.Fatalf("expected upstream id")
	}

	form := urlEncoded(map[string]string{
		"id":            upstreamID,
		"model":         "qwen-plus",
		"system_prompt": "你是一个测试助手",
		"user_prompt":   "hello",
	})
	reqProbe := httptest.NewRequest(http.MethodPost, "/admin/upstreams/diagnose", strings.NewReader(form))
	reqProbe.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqProbe.AddCookie(sessionCookie)
	rrProbe := httptest.NewRecorder()
	router.ServeHTTP(rrProbe, reqProbe)
	if rrProbe.Code != http.StatusOK {
		t.Fatalf("upstream diagnose expected 200, got %d: %s", rrProbe.Code, rrProbe.Body.String())
	}
	if !strings.Contains(rrProbe.Body.String(), `"health":`) {
		t.Fatalf("expected health block in diagnose response, got %s", rrProbe.Body.String())
	}
	if !strings.Contains(rrProbe.Body.String(), `"healthy":true`) {
		t.Fatalf("expected healthy=true in diagnose response, got %s", rrProbe.Body.String())
	}
	if !strings.Contains(rrProbe.Body.String(), `"assistant_reply":"probe-ok"`) {
		t.Fatalf("expected upstream diagnose content, got %s", rrProbe.Body.String())
	}
	if !strings.Contains(rrProbe.Body.String(), `"provider_model":"openai/qwen-plus"`) {
		t.Fatalf("expected provider model in probe response, got %s", rrProbe.Body.String())
	}
	if !strings.Contains(rrProbe.Body.String(), `"http_status":200`) {
		t.Fatalf("expected http status in probe response, got %s", rrProbe.Body.String())
	}
	if !strings.Contains(rrProbe.Body.String(), `"raw_response":`) {
		t.Fatalf("expected raw response in probe response, got %s", rrProbe.Body.String())
	}
	reqLegacyProbe := httptest.NewRequest(http.MethodPost, "/admin/upstreams/test", strings.NewReader(form))
	reqLegacyProbe.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqLegacyProbe.AddCookie(sessionCookie)
	rrLegacyProbe := httptest.NewRecorder()
	router.ServeHTTP(rrLegacyProbe, reqLegacyProbe)
	if rrLegacyProbe.Code != http.StatusNotFound {
		t.Fatalf("legacy /admin/upstreams/test should be 404, got %d", rrLegacyProbe.Code)
	}
}

func TestBuildPromptUpsertRequestFromFormSimpleDefaults(t *testing.T) {
	form := urlEncoded(map[string]string{
		"slug":             "translate",
		"name":             "AI Translate",
		"model":            "kimi-k2.5",
		"task_instruction": "你是一名专业翻译助手",
		"input_key":        "source text",
		"publish":          "on",
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/prompt-apis/new", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("parse form failed: %v", err)
	}

	got := buildPromptUpsertRequestFromForm(req)
	if got.SystemPrompt != "你是一名专业翻译助手" {
		t.Fatalf("unexpected system prompt: %s", got.SystemPrompt)
	}
	if got.UserTemplate != "{{.source_text}}" {
		t.Fatalf("unexpected user template: %s", got.UserTemplate)
	}
	if len(got.Variables) != 1 || got.Variables[0] != "source_text" {
		t.Fatalf("unexpected variables: %+v", got.Variables)
	}
	if !got.Publish {
		t.Fatalf("expected publish=true")
	}
}

func urlEncoded(values map[string]string) string {
	form := url.Values{}
	for k, v := range values {
		form.Set(k, v)
	}
	return form.Encode()
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

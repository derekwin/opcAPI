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
	"github.com/derekwin/opcAPI/internal/keymgmt"
	"github.com/derekwin/opcAPI/internal/promptapi"
	"github.com/derekwin/opcAPI/internal/provider"
	"github.com/derekwin/opcAPI/internal/quota"
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
	return newTestRouterWithOptions(t, g, false)
}

func newTestRouterWithOptions(t *testing.T, g provider.Gateway, requireTenantBoundKey bool) http.Handler {
	t.Helper()
	if g == nil {
		g = provider.NewStubGateway()
	}
	authManager := auth.NewStaticManager(true, []string{"sk-test"})
	limiter := quota.NewLimiter(100, 1000)
	policySyncer := newTestPolicySyncer(t, authManager, limiter)
	return NewRouter(
		testLogger(),
		g,
		authManager,
		requireTenantBoundKey,
		limiter,
		policySyncer,
		newTestUserService(t),
		newTestBillingService(t),
		newTestPromptAPIService(t),
		newTestAuditService(t),
	)
}

func newTestUserService(t *testing.T) *user.Service {
	t.Helper()
	store, err := user.NewFileStore(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatalf("new user store failed: %v", err)
	}
	return user.NewService(store, user.NewSessionManager("test-secret", 24*time.Hour), "000000", testLogger())
}

func newTestBillingService(t *testing.T) *billing.Service {
	t.Helper()
	store, err := billing.NewFileStore(filepath.Join(t.TempDir(), "billing.json"))
	if err != nil {
		t.Fatalf("new billing store failed: %v", err)
	}
	return billing.NewService(store, testLogger())
}

func newTestPolicySyncer(t *testing.T, authManager *auth.Manager, limiter *quota.Limiter) *keymgmt.PolicySyncer {
	t.Helper()
	cfg := config.Config{
		PolicyBackend:   "file",
		PolicyStorePath: filepath.Join(t.TempDir(), "policies.json"),
		PolicyRefresh:   time.Second,
		RequestRPMLimit: 100,
		DailyTokenLimit: 1000,
	}
	syncer := keymgmt.NewPolicySyncer(cfg, testLogger(), authManager, limiter, nil)
	if syncer == nil {
		t.Fatalf("new policy syncer failed")
	}
	if err := syncer.Initialize(context.Background()); err != nil {
		t.Fatalf("policy syncer initialize failed: %v", err)
	}
	return syncer
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

func withAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer sk-test")
}

func TestHealthz(t *testing.T) {
	router := newTestRouter(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", body["status"])
	}
}

func TestAuthSignupSigninMe(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"phone","identifier":"13800138000","verification_code":"000000","tenant_name":"demo"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	token, _ := signupRes["token"].(string)
	if token == "" {
		t.Fatalf("expected signup token")
	}
	if _, ok := signupRes["api_key"].(string); !ok {
		t.Fatalf("expected signup api_key")
	}

	signin := `{"channel":"phone","identifier":"13800138000","verification_code":"000000"}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/auth/signin", bytes.NewBufferString(signin))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("signin expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}

	var signinRes map[string]any
	if err := json.Unmarshal(rr2.Body.Bytes(), &signinRes); err != nil {
		t.Fatalf("unmarshal signin response: %v", err)
	}
	token2, _ := signinRes["token"].(string)
	if token2 == "" {
		t.Fatalf("expected signin token")
	}

	req3 := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	req3.Header.Set("Authorization", "Bearer "+token2)
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("me expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}
}

func TestSignupAutoProvisionAPIKey(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"phone","identifier":"13800138088","verification_code":"000000","tenant_name":"auto-key"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	apiKey, _ := signupRes["api_key"].(string)
	if apiKey == "" {
		t.Fatalf("expected auto provisioned api_key")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req2.Header.Set("Authorization", "Bearer "+apiKey)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("auto provisioned api key should access models, got %d: %s", rr2.Code, rr2.Body.String())
	}
}

func TestAdminUsers(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"wechat","identifier":"wx_openid_001"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	token, _ := signupRes["token"].(string)
	if token == "" {
		t.Fatalf("expected token")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/v1/admin/users", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("admin/users expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}
	if !strings.Contains(rr2.Body.String(), "data") {
		t.Fatalf("unexpected admin/users response: %s", rr2.Body.String())
	}
}

func TestAdminKeysCRUD(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"wechat","identifier":"wx_admin_001"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	token, _ := signupRes["token"].(string)
	if token == "" {
		t.Fatalf("expected token")
	}
	tenant, _ := signupRes["tenant"].(map[string]any)
	tenantID, _ := tenant["id"].(string)
	if tenantID == "" {
		t.Fatalf("expected tenant id")
	}

	upsert := `{"api_key":"sk-dyn-admin","enabled":true,"rpm_limit":1,"daily_token_limit":20}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/admin/keys", bytes.NewBufferString(upsert))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("admin keys upsert expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}
	expectedHash := auth.HashAPIKey("sk-dyn-admin")
	if !strings.Contains(rr2.Body.String(), expectedHash) {
		t.Fatalf("upsert response missing key hash: %s", rr2.Body.String())
	}
	if !strings.Contains(rr2.Body.String(), `"tenant_id":"`+tenantID+`"`) {
		t.Fatalf("upsert response missing tenant_id: %s", rr2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req3.Header.Set("Authorization", "Bearer sk-dyn-admin")
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("dynamic key should access models, got %d: %s", rr3.Code, rr3.Body.String())
	}

	req4 := httptest.NewRequest(http.MethodGet, "/v1/admin/keys", nil)
	req4.Header.Set("Authorization", "Bearer "+token)
	rr4 := httptest.NewRecorder()
	router.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusOK {
		t.Fatalf("admin keys list expected 200, got %d: %s", rr4.Code, rr4.Body.String())
	}
	if !strings.Contains(rr4.Body.String(), expectedHash) {
		t.Fatalf("list response missing key hash: %s", rr4.Body.String())
	}

	req5 := httptest.NewRequest(http.MethodDelete, "/v1/admin/keys?key_hash="+expectedHash, nil)
	req5.Header.Set("Authorization", "Bearer "+token)
	rr5 := httptest.NewRecorder()
	router.ServeHTTP(rr5, req5)
	if rr5.Code != http.StatusOK {
		t.Fatalf("admin keys delete expected 200, got %d: %s", rr5.Code, rr5.Body.String())
	}

	req6 := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req6.Header.Set("Authorization", "Bearer sk-dyn-admin")
	rr6 := httptest.NewRecorder()
	router.ServeHTTP(rr6, req6)
	if rr6.Code != http.StatusUnauthorized {
		t.Fatalf("deleted key should be unauthorized, got %d: %s", rr6.Code, rr6.Body.String())
	}
}

func TestAdminKeysListFilterAndPagination(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"wechat","identifier":"wx_admin_101"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	token, _ := signupRes["token"].(string)
	if token == "" {
		t.Fatalf("expected token")
	}
	tenant, _ := signupRes["tenant"].(map[string]any)
	tenantID, _ := tenant["id"].(string)
	if tenantID == "" {
		t.Fatalf("expected tenant id")
	}

	upsertA := `{"api_key":"sk-k1","enabled":true}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/admin/keys", bytes.NewBufferString(upsertA))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("upsert A expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}

	upsertB := `{"api_key":"sk-k2","enabled":false}`
	req3 := httptest.NewRequest(http.MethodPost, "/v1/admin/keys", bytes.NewBufferString(upsertB))
	req3.Header.Set("Authorization", "Bearer "+token)
	req3.Header.Set("Content-Type", "application/json")
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("upsert B expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}

	req4 := httptest.NewRequest(http.MethodGet, "/v1/admin/keys?tenant_id="+tenantID+"&enabled=false&limit=1&offset=0", nil)
	req4.Header.Set("Authorization", "Bearer "+token)
	rr4 := httptest.NewRecorder()
	router.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusOK {
		t.Fatalf("keys list filter expected 200, got %d: %s", rr4.Code, rr4.Body.String())
	}
	if !strings.Contains(rr4.Body.String(), `"tenant_id":"`+tenantID+`"`) {
		t.Fatalf("expected filtered tenant_id in response: %s", rr4.Body.String())
	}
	if !strings.Contains(rr4.Body.String(), `"total":1`) || !strings.Contains(rr4.Body.String(), `"count":1`) {
		t.Fatalf("unexpected keys list meta: %s", rr4.Body.String())
	}
}

func TestAdminKeysCrossTenantForbidden(t *testing.T) {
	router := newTestRouter(t, nil)

	signupA := `{"channel":"wechat","identifier":"wx_admin_ten_a"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signupA))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup A expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}
	var signupResA map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupResA); err != nil {
		t.Fatalf("unmarshal signup A response: %v", err)
	}
	tokenA, _ := signupResA["token"].(string)
	tenantA, _ := signupResA["tenant"].(map[string]any)
	tenantAID, _ := tenantA["id"].(string)
	if tokenA == "" || tenantAID == "" {
		t.Fatalf("expected token and tenant for signup A")
	}

	signupB := `{"channel":"wechat","identifier":"wx_admin_ten_b"}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signupB))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("signup B expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}
	var signupResB map[string]any
	if err := json.Unmarshal(rr2.Body.Bytes(), &signupResB); err != nil {
		t.Fatalf("unmarshal signup B response: %v", err)
	}
	tokenB, _ := signupResB["token"].(string)
	if tokenB == "" {
		t.Fatalf("expected token for signup B")
	}

	upsertByA := `{"api_key":"sk-cross-tenant","enabled":true}`
	req3 := httptest.NewRequest(http.MethodPost, "/v1/admin/keys", bytes.NewBufferString(upsertByA))
	req3.Header.Set("Authorization", "Bearer "+tokenA)
	req3.Header.Set("Content-Type", "application/json")
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("upsert by A expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}

	forbiddenUpsert := `{"api_key":"sk-cross-denied","tenant_id":"` + tenantAID + `"}`
	req4 := httptest.NewRequest(http.MethodPost, "/v1/admin/keys", bytes.NewBufferString(forbiddenUpsert))
	req4.Header.Set("Authorization", "Bearer "+tokenB)
	req4.Header.Set("Content-Type", "application/json")
	rr4 := httptest.NewRecorder()
	router.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant upsert expected 403, got %d: %s", rr4.Code, rr4.Body.String())
	}

	keyHash := auth.HashAPIKey("sk-cross-tenant")
	req5 := httptest.NewRequest(http.MethodDelete, "/v1/admin/keys?key_hash="+keyHash, nil)
	req5.Header.Set("Authorization", "Bearer "+tokenB)
	rr5 := httptest.NewRecorder()
	router.ServeHTTP(rr5, req5)
	if rr5.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant delete expected 404, got %d: %s", rr5.Code, rr5.Body.String())
	}

	req6 := httptest.NewRequest(http.MethodGet, "/v1/admin/keys?tenant_id="+tenantAID, nil)
	req6.Header.Set("Authorization", "Bearer "+tokenB)
	rr6 := httptest.NewRecorder()
	router.ServeHTTP(rr6, req6)
	if rr6.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant list expected 403, got %d: %s", rr6.Code, rr6.Body.String())
	}
}

func TestAdminAuditLogsForAPIKeys(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"wechat","identifier":"wx_admin_audit"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	token, _ := signupRes["token"].(string)
	if token == "" {
		t.Fatalf("expected token")
	}

	upsert := `{"api_key":"sk-audit-1","enabled":true}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/admin/keys", bytes.NewBufferString(upsert))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("admin keys upsert expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}

	keyHash := auth.HashAPIKey("sk-audit-1")
	req3 := httptest.NewRequest(http.MethodDelete, "/v1/admin/keys?key_hash="+keyHash, nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("admin keys delete expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}

	req4 := httptest.NewRequest(http.MethodGet, "/v1/admin/audit/logs?resource=api_key&limit=10", nil)
	req4.Header.Set("Authorization", "Bearer "+token)
	rr4 := httptest.NewRecorder()
	router.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusOK {
		t.Fatalf("audit list expected 200, got %d: %s", rr4.Code, rr4.Body.String())
	}
	if !strings.Contains(rr4.Body.String(), `"action":"upsert"`) || !strings.Contains(rr4.Body.String(), `"action":"delete"`) {
		t.Fatalf("audit response missing expected actions: %s", rr4.Body.String())
	}

	req5 := httptest.NewRequest(http.MethodGet, "/v1/admin/audit/logs?resource=api_key&action=delete&limit=10", nil)
	req5.Header.Set("Authorization", "Bearer "+token)
	rr5 := httptest.NewRecorder()
	router.ServeHTTP(rr5, req5)
	if rr5.Code != http.StatusOK {
		t.Fatalf("audit delete filter expected 200, got %d: %s", rr5.Code, rr5.Body.String())
	}
	if !strings.Contains(rr5.Body.String(), `"action":"delete"`) || strings.Contains(rr5.Body.String(), `"action":"upsert"`) {
		t.Fatalf("unexpected filtered audit response: %s", rr5.Body.String())
	}
}

func TestPromptAPIAdminAndInvoke(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"phone","identifier":"13800138021","verification_code":"000000","tenant_name":"prompt-demo"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	token, _ := signupRes["token"].(string)
	if token == "" {
		t.Fatalf("expected token")
	}
	apiKey, _ := signupRes["api_key"].(string)
	if apiKey == "" {
		t.Fatalf("expected auto api_key")
	}
	tenant, _ := signupRes["tenant"].(map[string]any)
	if _, ok := tenant["id"].(string); !ok {
		t.Fatalf("expected tenant id")
	}

	upsert := `{"slug":"email_summary","name":"Email Summary","model":"stub/test","system_prompt":"You are helpful","user_template":"Summarize: {{.content}}","variables":["content"],"publish":true}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/admin/prompt-apis", bytes.NewBufferString(upsert))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("admin prompt api upsert expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodGet, "/v1/admin/prompt-apis?status=published&limit=10", nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("admin prompt api list expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}
	if !strings.Contains(rr3.Body.String(), "email_summary") {
		t.Fatalf("list response missing slug: %s", rr3.Body.String())
	}

	invoke := `{"input":{"content":"hello opc"}}`
	req4 := httptest.NewRequest(http.MethodPost, "/v1/prompt-apis/email_summary/chat/completions", bytes.NewBufferString(invoke))
	req4.Header.Set("Authorization", "Bearer "+apiKey)
	req4.Header.Set("Content-Type", "application/json")
	rr4 := httptest.NewRecorder()
	router.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusOK {
		t.Fatalf("prompt invoke expected 200, got %d: %s", rr4.Code, rr4.Body.String())
	}
	if !strings.Contains(rr4.Body.String(), `"definition"`) || !strings.Contains(rr4.Body.String(), `"completion"`) {
		t.Fatalf("unexpected invoke response: %s", rr4.Body.String())
	}

	invokeMismatch := `{"tenant_id":"ten_other","input":{"content":"hello opc"}}`
	req5 := httptest.NewRequest(http.MethodPost, "/v1/prompt-apis/email_summary/chat/completions", bytes.NewBufferString(invokeMismatch))
	req5.Header.Set("Authorization", "Bearer "+apiKey)
	req5.Header.Set("Content-Type", "application/json")
	rr5 := httptest.NewRecorder()
	router.ServeHTTP(rr5, req5)
	if rr5.Code != http.StatusBadRequest {
		t.Fatalf("prompt invoke with tenant_id field expected 400, got %d: %s", rr5.Code, rr5.Body.String())
	}
}

func TestPromptAPIInvokeRequiresPublished(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"wechat","identifier":"wx_prompt_001"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	token, _ := signupRes["token"].(string)
	apiKey, _ := signupRes["api_key"].(string)
	if apiKey == "" {
		t.Fatalf("expected auto api_key")
	}
	tenant, _ := signupRes["tenant"].(map[string]any)
	if _, ok := tenant["id"].(string); !ok {
		t.Fatalf("expected tenant id")
	}

	upsert := `{"slug":"draft_agent","name":"Draft Agent","model":"stub/test","user_template":"Say {{.q}}","variables":["q"]}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/admin/prompt-apis", bytes.NewBufferString(upsert))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("admin prompt api upsert expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}

	invoke := `{"input":{"q":"hello"}}`
	req3 := httptest.NewRequest(http.MethodPost, "/v1/prompt-apis/draft_agent/chat/completions", bytes.NewBufferString(invoke))
	req3.Header.Set("Authorization", "Bearer "+apiKey)
	req3.Header.Set("Content-Type", "application/json")
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusConflict {
		t.Fatalf("expected 409 for draft prompt api, got %d: %s", rr3.Code, rr3.Body.String())
	}
}

func TestPromptAPIInvokeRequiresTenantBinding(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"phone","identifier":"13800138031","verification_code":"000000","tenant_name":"prompt-unbound"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	token, _ := signupRes["token"].(string)
	if token == "" {
		t.Fatalf("expected token")
	}

	upsert := `{"slug":"public_prompt","name":"Public Prompt","model":"stub/test","user_template":"Say {{.q}}","variables":["q"],"publish":true}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/admin/prompt-apis", bytes.NewBufferString(upsert))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("admin prompt api upsert expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}

	invoke := `{"input":{"q":"hello"}}`
	req3 := httptest.NewRequest(http.MethodPost, "/v1/prompt-apis/public_prompt/chat/completions", bytes.NewBufferString(invoke))
	req3.Header.Set("Authorization", "Bearer sk-test")
	req3.Header.Set("Content-Type", "application/json")
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusForbidden {
		t.Fatalf("unbound key invoke expected 403, got %d: %s", rr3.Code, rr3.Body.String())
	}
}

func TestBillingFlow(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"phone","identifier":"13800138001","verification_code":"000000","tenant_name":"billing-demo"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	token, _ := signupRes["token"].(string)
	if token == "" {
		t.Fatalf("expected token")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/v1/billing/balance", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("balance expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}

	topup := `{"channel":"manual","amount":100,"currency":"CNY"}`
	req3 := httptest.NewRequest(http.MethodPost, "/v1/billing/topup", bytes.NewBufferString(topup))
	req3.Header.Set("Authorization", "Bearer "+token)
	req3.Header.Set("Content-Type", "application/json")
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("topup expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}

	subscribe := `{"plan_id":"pro","channel":"stripe","amount":29.9,"currency":"USD","months":1}`
	req4 := httptest.NewRequest(http.MethodPost, "/v1/billing/subscribe", bytes.NewBufferString(subscribe))
	req4.Header.Set("Authorization", "Bearer "+token)
	req4.Header.Set("Content-Type", "application/json")
	rr4 := httptest.NewRecorder()
	router.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusOK {
		t.Fatalf("subscribe expected 200, got %d: %s", rr4.Code, rr4.Body.String())
	}

	req5 := httptest.NewRequest(http.MethodGet, "/v1/admin/billing/orders?limit=10", nil)
	req5.Header.Set("Authorization", "Bearer "+token)
	rr5 := httptest.NewRecorder()
	router.ServeHTTP(rr5, req5)
	if rr5.Code != http.StatusOK {
		t.Fatalf("admin billing orders expected 200, got %d: %s", rr5.Code, rr5.Body.String())
	}
	if !strings.Contains(rr5.Body.String(), "subscription") || !strings.Contains(rr5.Body.String(), "topup") {
		t.Fatalf("unexpected orders response: %s", rr5.Body.String())
	}
}

func TestAdminBillingOrdersPaginationAndFilter(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"phone","identifier":"13800138009","verification_code":"000000","tenant_name":"billing-filter"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	token, _ := signupRes["token"].(string)
	if token == "" {
		t.Fatalf("expected token")
	}

	topup := `{"channel":"manual","amount":66,"currency":"CNY"}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/billing/topup", bytes.NewBufferString(topup))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("topup expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}

	subscribe := `{"plan_id":"pro","channel":"stripe","amount":29.9,"currency":"USD","months":1}`
	req3 := httptest.NewRequest(http.MethodPost, "/v1/billing/subscribe", bytes.NewBufferString(subscribe))
	req3.Header.Set("Authorization", "Bearer "+token)
	req3.Header.Set("Content-Type", "application/json")
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("subscribe expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}

	req4 := httptest.NewRequest(http.MethodGet, "/v1/admin/billing/orders?type=topup&limit=1&offset=0", nil)
	req4.Header.Set("Authorization", "Bearer "+token)
	rr4 := httptest.NewRecorder()
	router.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusOK {
		t.Fatalf("admin billing filter expected 200, got %d: %s", rr4.Code, rr4.Body.String())
	}
	if !strings.Contains(rr4.Body.String(), `"total":1`) || !strings.Contains(rr4.Body.String(), `"count":1`) {
		t.Fatalf("unexpected pagination response: %s", rr4.Body.String())
	}
	if !strings.Contains(rr4.Body.String(), `"type":"topup"`) {
		t.Fatalf("unexpected filter response: %s", rr4.Body.String())
	}
}

func TestModels(t *testing.T) {
	gateway := &fakeGateway{models: []provider.ModelInfo{{ID: "openai/gpt-4o-mini", OwnedBy: "openai"}, {ID: "ollama/qwen2.5:7b", OwnedBy: "ollama"}}}
	router := newTestRouter(t, gateway)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	withAuth(req)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "openai/gpt-4o-mini") || !strings.Contains(body, "ollama/qwen2.5:7b") {
		t.Fatalf("unexpected models body: %s", body)
	}
}

func TestChatCompletions(t *testing.T) {
	router := newTestRouter(t, nil)
	payload := `{"model":"moonshot-v1","messages":[{"role":"user","content":"hello"}]}`

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(payload))
	withAuth(req)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if body["object"] != "chat.completion" {
		t.Fatalf("expected chat.completion object, got %v", body["object"])
	}
}

func TestChatCompletionsStream(t *testing.T) {
	router := newTestRouter(t, nil)
	payload := `{"model":"moonshot-v1","stream":true,"messages":[{"role":"user","content":"hello"}]}`

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(payload))
	withAuth(req)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("expected text/event-stream content type, got %q", got)
	}
	if !strings.Contains(rr.Body.String(), "data: [DONE]") {
		t.Fatalf("expected stream to end with [DONE], got %s", rr.Body.String())
	}
}

func TestChatCompletionsValidation(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		payload string
		want    int
	}{
		{name: "method not allowed", method: http.MethodGet, payload: "", want: http.StatusMethodNotAllowed},
		{name: "invalid json", method: http.MethodPost, payload: `{`, want: http.StatusBadRequest},
		{name: "missing model", method: http.MethodPost, payload: `{"messages":[{"role":"user","content":"hello"}]}`, want: http.StatusBadRequest},
		{name: "missing messages", method: http.MethodPost, payload: `{"model":"stub/test"}`, want: http.StatusBadRequest},
	}

	router := newTestRouter(t, nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader
			if tc.payload != "" {
				body = bytes.NewBufferString(tc.payload)
			}
			req := httptest.NewRequest(tc.method, "/v1/chat/completions", body)
			if tc.method == http.MethodPost {
				withAuth(req)
			}
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), `"error"`) {
				t.Fatalf("expected openai error envelope, got %s", rr.Body.String())
			}
		})
	}
}

func TestOpenAIErrorLocalizationChinese(t *testing.T) {
	router := newTestRouter(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Language"); got != "zh-CN" {
		t.Fatalf("expected Content-Language zh-CN, got %q", got)
	}

	var body map[string]map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error response failed: %v", err)
	}
	errObj, ok := body["error"]
	if !ok {
		t.Fatalf("expected error object, got %s", rr.Body.String())
	}
	if got, _ := errObj["message"].(string); got != "不支持该请求方法" {
		t.Fatalf("unexpected localized message: %q", got)
	}
}

func TestChatCompletionsProviderError(t *testing.T) {
	router := newTestRouter(t, &fakeGateway{chatErr: errors.New("upstream down")})
	payload := `{"model":"openai/gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(payload))
	withAuth(req)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestChatCompletionsNormalizesObjectContent(t *testing.T) {
	gateway := &fakeGateway{chatRes: provider.ChatResult{Model: "stub/test", Content: "ok"}}
	router := newTestRouter(t, gateway)
	payload := `{"model":"stub/test","messages":[{"role":"user","content":{"text":"hello"}}]}`

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(payload))
	withAuth(req)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if gateway.lastReq.Messages[0].Content != `{"text":"hello"}` {
		t.Fatalf("unexpected normalized content: %s", gateway.lastReq.Messages[0].Content)
	}
}

func TestAuthRequired(t *testing.T) {
	router := newTestRouter(t, nil)
	payload := `{"model":"stub/test","messages":[{"role":"user","content":"hello"}]}`

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestInvalidAPIKey(t *testing.T) {
	router := newTestRouter(t, nil)
	payload := `{"model":"stub/test","messages":[{"role":"user","content":"hello"}]}`

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(payload))
	req.Header.Set("Authorization", "Bearer wrong-key")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestChatCompletionsRequireTenantBoundKey(t *testing.T) {
	router := newTestRouterWithOptions(t, nil, true)
	payload := `{"model":"stub/test","messages":[{"role":"user","content":"hello"}]}`

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(payload))
	withAuth(req)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "tenant_binding_required") {
		t.Fatalf("expected tenant_binding_required error, got %s", rr.Body.String())
	}
}

func TestChatCompletionsRequireTenantBoundKeyAllowsBoundKey(t *testing.T) {
	router := newTestRouterWithOptions(t, nil, true)

	signup := `{"channel":"phone","identifier":"13800138126","verification_code":"000000","tenant_name":"strict-mode"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	apiKey, _ := signupRes["api_key"].(string)
	if apiKey == "" {
		t.Fatalf("expected api key")
	}

	payload := `{"model":"stub/test","messages":[{"role":"user","content":"hello"}]}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(payload))
	req2.Header.Set("Authorization", "Bearer "+apiKey)
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}
}

func TestRequestRateLimitExceeded(t *testing.T) {
	router := NewRouter(
		testLogger(),
		provider.NewStubGateway(),
		auth.NewStaticManager(true, []string{"sk-test"}),
		false,
		quota.NewLimiter(1, 1000),
		nil,
		newTestUserService(t),
		newTestBillingService(t),
		newTestPromptAPIService(t),
		newTestAuditService(t),
	)

	req1 := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	withAuth(req1)
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected first request 200, got %d", rr1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	withAuth(req2)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request 429, got %d", rr2.Code)
	}
}

func TestDailyTokenLimitExceeded(t *testing.T) {
	router := NewRouter(
		testLogger(),
		&fakeGateway{chatRes: provider.ChatResult{Model: "stub/test", Content: "ok", PromptTokens: 6, CompletionTokens: 6}},
		auth.NewStaticManager(true, []string{"sk-test"}),
		false,
		quota.NewLimiter(100, 10),
		nil,
		newTestUserService(t),
		newTestBillingService(t),
		newTestPromptAPIService(t),
		newTestAuditService(t),
	)

	payload := `{"model":"stub/test","messages":[{"role":"user","content":"hello"}]}`

	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(payload))
	withAuth(req1)
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected first request 200, got %d", rr1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(payload))
	withAuth(req2)
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request 429, got %d", rr2.Code)
	}
}

func TestChatCompletionsTenantInsufficientBalance(t *testing.T) {
	router := newTestRouter(t, &fakeGateway{
		chatRes: provider.ChatResult{
			Model:            "stub/test",
			Content:          "ok",
			PromptTokens:     100,
			CompletionTokens: 50,
		},
	})

	signup := `{"channel":"phone","identifier":"13800138123","verification_code":"000000","tenant_name":"insufficient"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	apiKey, _ := signupRes["api_key"].(string)
	if apiKey == "" {
		t.Fatalf("expected api key")
	}

	payload := `{"model":"stub/test","messages":[{"role":"user","content":"hello"}]}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(payload))
	req2.Header.Set("Authorization", "Bearer "+apiKey)
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d: %s", rr2.Code, rr2.Body.String())
	}
	if !strings.Contains(rr2.Body.String(), "insufficient_balance") {
		t.Fatalf("expected insufficient_balance error, got %s", rr2.Body.String())
	}
}

func TestChatCompletionsTenantUsageCharge(t *testing.T) {
	router := newTestRouter(t, &fakeGateway{
		chatRes: provider.ChatResult{
			Model:            "stub/test",
			Content:          "ok",
			PromptTokens:     100,
			CompletionTokens: 50,
		},
	})

	signup := `{"channel":"phone","identifier":"13800138124","verification_code":"000000","tenant_name":"charged"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	apiKey, _ := signupRes["api_key"].(string)
	token, _ := signupRes["token"].(string)
	if apiKey == "" || token == "" {
		t.Fatalf("expected api key and token")
	}

	topup := `{"channel":"manual","amount":1,"currency":"CNY"}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/billing/topup", bytes.NewBufferString(topup))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("topup expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodGet, "/v1/billing/balance", nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("balance before expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}
	var before map[string]any
	if err := json.Unmarshal(rr3.Body.Bytes(), &before); err != nil {
		t.Fatalf("unmarshal before balance failed: %v", err)
	}
	beforeAmount, _ := before["amount"].(float64)

	payload := `{"model":"stub/test","messages":[{"role":"user","content":"hello"}]}`
	req4 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(payload))
	req4.Header.Set("Authorization", "Bearer "+apiKey)
	req4.Header.Set("Content-Type", "application/json")
	rr4 := httptest.NewRecorder()
	router.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusOK {
		t.Fatalf("chat expected 200, got %d: %s", rr4.Code, rr4.Body.String())
	}

	req5 := httptest.NewRequest(http.MethodGet, "/v1/billing/balance", nil)
	req5.Header.Set("Authorization", "Bearer "+token)
	rr5 := httptest.NewRecorder()
	router.ServeHTTP(rr5, req5)
	if rr5.Code != http.StatusOK {
		t.Fatalf("balance after expected 200, got %d: %s", rr5.Code, rr5.Body.String())
	}
	var after map[string]any
	if err := json.Unmarshal(rr5.Body.Bytes(), &after); err != nil {
		t.Fatalf("unmarshal after balance failed: %v", err)
	}
	afterAmount, _ := after["amount"].(float64)
	if afterAmount >= beforeAmount {
		t.Fatalf("expected balance deduction, before=%v after=%v", beforeAmount, afterAmount)
	}
}

func TestAdminWebRequiresLogin(t *testing.T) {
	router := newTestRouter(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect to login, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); !strings.HasPrefix(loc, "/admin/login") {
		t.Fatalf("expected redirect location /admin/login, got %s", loc)
	}
}

func TestAdminWebLoginLocalizationChinese(t *testing.T) {
	router := newTestRouter(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/admin/login?msg=signed_out&error=sign_in_required", nil)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Language"); got != "zh-CN" {
		t.Fatalf("expected Content-Language zh-CN, got %q", got)
	}
	if !strings.Contains(rr.Body.String(), "已退出登录") {
		t.Fatalf("expected localized signed out message, got %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "请先登录") {
		t.Fatalf("expected localized sign-in-required error, got %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `<html lang="zh-CN">`) {
		t.Fatalf("expected zh-CN html lang, got %s", rr.Body.String())
	}
}

func TestAdminWebDashboardLocalizationChinese(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"wechat","identifier":"wx_dashboard_zh","tenant_name":"dashboard-zh"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	token, _ := signupRes["token"].(string)
	if token == "" {
		t.Fatalf("expected token")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req2.Header.Set("Accept-Language", "zh-CN")
	req2.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: token, Path: "/"})
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("admin dashboard expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}
	if got := rr2.Header().Get("Content-Language"); got != "zh-CN" {
		t.Fatalf("expected Content-Language zh-CN, got %q", got)
	}
	if !strings.Contains(rr2.Body.String(), "opcAPI 管理控制台") {
		t.Fatalf("missing zh dashboard title: %s", rr2.Body.String())
	}
	if !strings.Contains(rr2.Body.String(), "API 密钥") {
		t.Fatalf("missing zh api key label: %s", rr2.Body.String())
	}
	if !strings.Contains(rr2.Body.String(), "角色=所有者") {
		t.Fatalf("missing zh role enum label: %s", rr2.Body.String())
	}
}

func TestAdminWebLoginAndDashboard(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"phone","identifier":"13800138999","verification_code":"000000","tenant_name":"web-admin"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	loginForm := url.Values{
		"action":            {"signin"},
		"channel":           {"phone"},
		"identifier":        {"13800138999"},
		"verification_code": {"000000"},
	}
	req2 := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(loginForm.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusSeeOther {
		t.Fatalf("admin login expected 303, got %d: %s", rr2.Code, rr2.Body.String())
	}
	if loc := rr2.Header().Get("Location"); loc != "/admin?msg=sign_in_success" {
		t.Fatalf("unexpected redirect after login: %s", loc)
	}

	var adminCookie *http.Cookie
	for _, c := range rr2.Result().Cookies() {
		if c.Name == adminSessionCookieName {
			adminCookie = c
			break
		}
	}
	if adminCookie == nil || adminCookie.Value == "" {
		t.Fatalf("expected admin session cookie")
	}

	req3 := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req3.AddCookie(adminCookie)
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("admin dashboard expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}
	if !strings.Contains(rr3.Body.String(), "opcAPI Admin Console") {
		t.Fatalf("dashboard body missing title: %s", rr3.Body.String())
	}
}

func TestAdminWebFormsCreateKeyAndPromptAPI(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"wechat","identifier":"wx_web_admin_001","tenant_name":"web-forms"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var signupRes map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &signupRes); err != nil {
		t.Fatalf("unmarshal signup response: %v", err)
	}
	token, _ := signupRes["token"].(string)
	if token == "" {
		t.Fatalf("expected token")
	}
	adminCookie := &http.Cookie{Name: adminSessionCookieName, Value: token, Path: "/"}

	keyForm := url.Values{
		"api_key":           {"sk-web-admin-1"},
		"enabled":           {"on"},
		"rpm_limit":         {"9"},
		"daily_token_limit": {"99"},
	}
	req2 := httptest.NewRequest(http.MethodPost, "/admin/keys/new", strings.NewReader(keyForm.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(adminCookie)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusSeeOther {
		t.Fatalf("create key expected 303, got %d: %s", rr2.Code, rr2.Body.String())
	}

	promptForm := url.Values{
		"slug":          {"web_summary"},
		"name":          {"Web Summary"},
		"model":         {"stub/test"},
		"user_template": {"Say {{.content}}"},
		"variables":     {"content"},
		"publish":       {"on"},
	}
	req3 := httptest.NewRequest(http.MethodPost, "/admin/prompt-apis/new", strings.NewReader(promptForm.Encode()))
	req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req3.AddCookie(adminCookie)
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusSeeOther {
		t.Fatalf("create prompt api expected 303, got %d: %s", rr3.Code, rr3.Body.String())
	}

	req4 := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req4.AddCookie(adminCookie)
	rr4 := httptest.NewRecorder()
	router.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusOK {
		t.Fatalf("admin dashboard expected 200, got %d: %s", rr4.Code, rr4.Body.String())
	}
	if !strings.Contains(rr4.Body.String(), auth.HashAPIKey("sk-web-admin-1")) {
		t.Fatalf("dashboard missing key hash: %s", rr4.Body.String())
	}
	if !strings.Contains(rr4.Body.String(), "web_summary") {
		t.Fatalf("dashboard missing prompt api slug: %s", rr4.Body.String())
	}
}

func TestAdminWebOwnerManagesAdminRole(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"wechat","identifier":"wx_owner_manage_web","tenant_name":"owner-manage"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("owner signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	var owner map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &owner); err != nil {
		t.Fatalf("unmarshal owner signup response: %v", err)
	}
	ownerToken, _ := owner["token"].(string)
	if ownerToken == "" {
		t.Fatalf("expected owner token")
	}
	ownerCookie := &http.Cookie{Name: adminSessionCookieName, Value: ownerToken, Path: "/"}

	createUserForm := url.Values{
		"channel":    {"wechat"},
		"identifier": {"wx_created_admin_web"},
		"role":       {"admin"},
	}
	req2 := httptest.NewRequest(http.MethodPost, "/admin/users/new", strings.NewReader(createUserForm.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(ownerCookie)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusSeeOther {
		t.Fatalf("owner create user expected 303, got %d: %s", rr2.Code, rr2.Body.String())
	}

	signinAdmin := `{"channel":"wechat","identifier":"wx_created_admin_web"}`
	req3 := httptest.NewRequest(http.MethodPost, "/v1/auth/signin", bytes.NewBufferString(signinAdmin))
	req3.Header.Set("Content-Type", "application/json")
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("created admin signin expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}
	var adminSignin map[string]any
	if err := json.Unmarshal(rr3.Body.Bytes(), &adminSignin); err != nil {
		t.Fatalf("unmarshal admin signin response: %v", err)
	}
	adminToken, _ := adminSignin["token"].(string)
	if adminToken == "" {
		t.Fatalf("expected admin token")
	}

	req4 := httptest.NewRequest(http.MethodGet, "/v1/admin/users", nil)
	req4.Header.Set("Authorization", "Bearer "+adminToken)
	rr4 := httptest.NewRecorder()
	router.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusOK {
		t.Fatalf("admin users list expected 200, got %d: %s", rr4.Code, rr4.Body.String())
	}

	adminUserObj, _ := adminSignin["user"].(map[string]any)
	adminUserID, _ := adminUserObj["id"].(string)
	if adminUserID == "" {
		t.Fatalf("expected created admin user id")
	}

	roleUpdateForm := url.Values{
		"user_id": {adminUserID},
		"role":    {"member"},
	}
	req5 := httptest.NewRequest(http.MethodPost, "/admin/users/role", strings.NewReader(roleUpdateForm.Encode()))
	req5.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req5.AddCookie(ownerCookie)
	rr5 := httptest.NewRecorder()
	router.ServeHTTP(rr5, req5)
	if rr5.Code != http.StatusSeeOther {
		t.Fatalf("owner update role expected 303, got %d: %s", rr5.Code, rr5.Body.String())
	}

	req6 := httptest.NewRequest(http.MethodPost, "/v1/auth/signin", bytes.NewBufferString(signinAdmin))
	req6.Header.Set("Content-Type", "application/json")
	rr6 := httptest.NewRecorder()
	router.ServeHTTP(rr6, req6)
	if rr6.Code != http.StatusOK {
		t.Fatalf("member signin expected 200, got %d: %s", rr6.Code, rr6.Body.String())
	}
	var memberSignin map[string]any
	if err := json.Unmarshal(rr6.Body.Bytes(), &memberSignin); err != nil {
		t.Fatalf("unmarshal member signin response: %v", err)
	}
	memberToken, _ := memberSignin["token"].(string)
	if memberToken == "" {
		t.Fatalf("expected member token")
	}

	req7 := httptest.NewRequest(http.MethodPost, "/admin/users/new", strings.NewReader(createUserForm.Encode()))
	req7.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req7.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: memberToken, Path: "/"})
	rr7 := httptest.NewRecorder()
	router.ServeHTTP(rr7, req7)
	if rr7.Code != http.StatusForbidden {
		t.Fatalf("non-owner create user expected 403, got %d: %s", rr7.Code, rr7.Body.String())
	}

	req8 := httptest.NewRequest(http.MethodGet, "/v1/admin/users", nil)
	req8.Header.Set("Authorization", "Bearer "+memberToken)
	rr8 := httptest.NewRecorder()
	router.ServeHTTP(rr8, req8)
	if rr8.Code != http.StatusForbidden {
		t.Fatalf("member should not access admin/users, got %d: %s", rr8.Code, rr8.Body.String())
	}
}

func TestAdminWebOwnerRequiredLocalizationChinese(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"wechat","identifier":"wx_owner_i18n","tenant_name":"owner-i18n"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("owner signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}
	var owner map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &owner); err != nil {
		t.Fatalf("unmarshal owner signup response: %v", err)
	}
	ownerToken, _ := owner["token"].(string)
	if ownerToken == "" {
		t.Fatalf("expected owner token")
	}
	ownerCookie := &http.Cookie{Name: adminSessionCookieName, Value: ownerToken, Path: "/"}

	createUserForm := url.Values{
		"channel":    {"wechat"},
		"identifier": {"wx_admin_i18n"},
		"role":       {"admin"},
	}
	req2 := httptest.NewRequest(http.MethodPost, "/admin/users/new", strings.NewReader(createUserForm.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(ownerCookie)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusSeeOther {
		t.Fatalf("owner create admin expected 303, got %d: %s", rr2.Code, rr2.Body.String())
	}

	signinAdmin := `{"channel":"wechat","identifier":"wx_admin_i18n"}`
	req3 := httptest.NewRequest(http.MethodPost, "/v1/auth/signin", bytes.NewBufferString(signinAdmin))
	req3.Header.Set("Content-Type", "application/json")
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("admin signin expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}
	var adminSignin map[string]any
	if err := json.Unmarshal(rr3.Body.Bytes(), &adminSignin); err != nil {
		t.Fatalf("unmarshal admin signin response: %v", err)
	}
	adminToken, _ := adminSignin["token"].(string)
	if adminToken == "" {
		t.Fatalf("expected admin token")
	}

	req4 := httptest.NewRequest(http.MethodPost, "/admin/users/new", strings.NewReader(createUserForm.Encode()))
	req4.Header.Set("Accept-Language", "zh-CN")
	req4.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req4.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: adminToken, Path: "/"})
	rr4 := httptest.NewRecorder()
	router.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusForbidden {
		t.Fatalf("non-owner create user expected 403, got %d: %s", rr4.Code, rr4.Body.String())
	}
	if got := rr4.Header().Get("Content-Language"); got != "zh-CN" {
		t.Fatalf("expected Content-Language zh-CN, got %q", got)
	}
	if !strings.Contains(rr4.Body.String(), "权限不足") {
		t.Fatalf("expected localized forbidden message, got %s", rr4.Body.String())
	}
}

func TestAdminWebOwnerDisablesAndEnablesUser(t *testing.T) {
	router := newTestRouter(t, nil)

	signup := `{"channel":"wechat","identifier":"wx_owner_status_web","tenant_name":"owner-status"}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewBufferString(signup))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("owner signup expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}
	var owner map[string]any
	if err := json.Unmarshal(rr1.Body.Bytes(), &owner); err != nil {
		t.Fatalf("unmarshal owner signup response: %v", err)
	}
	ownerToken, _ := owner["token"].(string)
	if ownerToken == "" {
		t.Fatalf("expected owner token")
	}
	ownerCookie := &http.Cookie{Name: adminSessionCookieName, Value: ownerToken, Path: "/"}

	createUserForm := url.Values{
		"channel":    {"wechat"},
		"identifier": {"wx_admin_status_web"},
		"role":       {"admin"},
	}
	req2 := httptest.NewRequest(http.MethodPost, "/admin/users/new", strings.NewReader(createUserForm.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(ownerCookie)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusSeeOther {
		t.Fatalf("owner create admin expected 303, got %d: %s", rr2.Code, rr2.Body.String())
	}

	signinAdmin := `{"channel":"wechat","identifier":"wx_admin_status_web"}`
	req3 := httptest.NewRequest(http.MethodPost, "/v1/auth/signin", bytes.NewBufferString(signinAdmin))
	req3.Header.Set("Content-Type", "application/json")
	rr3 := httptest.NewRecorder()
	router.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("admin signin expected 200, got %d: %s", rr3.Code, rr3.Body.String())
	}
	var adminSignin map[string]any
	if err := json.Unmarshal(rr3.Body.Bytes(), &adminSignin); err != nil {
		t.Fatalf("unmarshal admin signin response: %v", err)
	}
	adminToken, _ := adminSignin["token"].(string)
	adminUserObj, _ := adminSignin["user"].(map[string]any)
	adminUserID, _ := adminUserObj["id"].(string)
	if adminToken == "" || adminUserID == "" {
		t.Fatalf("expected admin token and user id")
	}

	req4 := httptest.NewRequest(http.MethodGet, "/v1/admin/users", nil)
	req4.Header.Set("Authorization", "Bearer "+adminToken)
	rr4 := httptest.NewRecorder()
	router.ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusOK {
		t.Fatalf("admin users list expected 200, got %d: %s", rr4.Code, rr4.Body.String())
	}

	statusDisableForm := url.Values{
		"user_id": {adminUserID},
		"status":  {"disabled"},
	}
	req5 := httptest.NewRequest(http.MethodPost, "/admin/users/status", strings.NewReader(statusDisableForm.Encode()))
	req5.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req5.AddCookie(ownerCookie)
	rr5 := httptest.NewRecorder()
	router.ServeHTTP(rr5, req5)
	if rr5.Code != http.StatusSeeOther {
		t.Fatalf("disable user expected 303, got %d: %s", rr5.Code, rr5.Body.String())
	}

	req6 := httptest.NewRequest(http.MethodGet, "/v1/admin/users", nil)
	req6.Header.Set("Authorization", "Bearer "+adminToken)
	rr6 := httptest.NewRecorder()
	router.ServeHTTP(rr6, req6)
	if rr6.Code != http.StatusForbidden {
		t.Fatalf("disabled user token should be forbidden, got %d: %s", rr6.Code, rr6.Body.String())
	}
	if !strings.Contains(rr6.Body.String(), "user_disabled") {
		t.Fatalf("expected user_disabled response, got %s", rr6.Body.String())
	}

	req7 := httptest.NewRequest(http.MethodPost, "/v1/auth/signin", bytes.NewBufferString(signinAdmin))
	req7.Header.Set("Content-Type", "application/json")
	rr7 := httptest.NewRecorder()
	router.ServeHTTP(rr7, req7)
	if rr7.Code != http.StatusForbidden {
		t.Fatalf("disabled user signin should be 403, got %d: %s", rr7.Code, rr7.Body.String())
	}

	statusEnableForm := url.Values{
		"user_id": {adminUserID},
		"status":  {"active"},
	}
	req8 := httptest.NewRequest(http.MethodPost, "/admin/users/status", strings.NewReader(statusEnableForm.Encode()))
	req8.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req8.AddCookie(ownerCookie)
	rr8 := httptest.NewRecorder()
	router.ServeHTTP(rr8, req8)
	if rr8.Code != http.StatusSeeOther {
		t.Fatalf("enable user expected 303, got %d: %s", rr8.Code, rr8.Body.String())
	}

	req9 := httptest.NewRequest(http.MethodPost, "/v1/auth/signin", bytes.NewBufferString(signinAdmin))
	req9.Header.Set("Content-Type", "application/json")
	rr9 := httptest.NewRecorder()
	router.ServeHTTP(rr9, req9)
	if rr9.Code != http.StatusOK {
		t.Fatalf("reenabled user signin expected 200, got %d: %s", rr9.Code, rr9.Body.String())
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

package user

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestSignupSigninAndMe(t *testing.T) {
	store, svc := newTestService(t)
	_ = store

	signed, err := svc.Signup(context.Background(), SignupRequest{
		Channel:          "phone",
		Identifier:       "13800138000",
		VerificationCode: "000000",
		TenantName:       "demo-tenant",
	})
	if err != nil {
		t.Fatalf("signup failed: %v", err)
	}
	if signed.Token == "" {
		t.Fatalf("expected signup token")
	}

	if _, err := svc.Signup(context.Background(), SignupRequest{
		Channel:          "phone",
		Identifier:       "13800138000",
		VerificationCode: "000000",
	}); err != ErrUserExists {
		t.Fatalf("expected ErrUserExists, got %v", err)
	}

	signedIn, err := svc.Signin(context.Background(), SigninRequest{
		Channel:          "phone",
		Identifier:       "13800138000",
		VerificationCode: "000000",
	})
	if err != nil {
		t.Fatalf("signin failed: %v", err)
	}
	if signedIn.Token == "" {
		t.Fatalf("expected signin token")
	}

	claims, err := svc.ParseSessionToken(signedIn.Token)
	if err != nil {
		t.Fatalf("parse token failed: %v", err)
	}
	me, err := svc.GetMe(context.Background(), claims)
	if err != nil {
		t.Fatalf("get me failed: %v", err)
	}
	if me.User.ID == "" || me.Tenant.ID == "" {
		t.Fatalf("unexpected me payload: %+v", me)
	}
}

func TestPhoneVerifyCode(t *testing.T) {
	_, svc := newTestService(t)
	_, err := svc.Signup(context.Background(), SignupRequest{
		Channel:          "phone",
		Identifier:       "13800138000",
		VerificationCode: "123456",
	})
	if err != ErrInvalidVerifyCode {
		t.Fatalf("expected ErrInvalidVerifyCode, got %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	svc := NewService(store, NewSessionManager("test-secret", time.Millisecond), "000000", testLogger())

	res, err := svc.Signup(context.Background(), SignupRequest{
		Channel:          "phone",
		Identifier:       "13900000000",
		VerificationCode: "000000",
	})
	if err != nil {
		t.Fatalf("signup failed: %v", err)
	}

	time.Sleep(2 * time.Millisecond)
	if _, err := svc.ParseSessionToken(res.Token); err != ErrSessionTokenExpired {
		t.Fatalf("expected ErrSessionTokenExpired, got %v", err)
	}
}

func TestListTenantUsers(t *testing.T) {
	_, svc := newTestService(t)

	res, err := svc.Signup(context.Background(), SignupRequest{
		Channel:          "wechat",
		Identifier:       "wx_openid_001",
		VerificationCode: "",
	})
	if err != nil {
		t.Fatalf("signup failed: %v", err)
	}

	claims, err := svc.ParseSessionToken(res.Token)
	if err != nil {
		t.Fatalf("parse token failed: %v", err)
	}
	users, err := svc.ListTenantUsers(context.Background(), claims)
	if err != nil {
		t.Fatalf("list tenant users failed: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
}

func TestOwnerCanCreateAndManageAdmins(t *testing.T) {
	_, svc := newTestService(t)

	ownerRes, err := svc.Signup(context.Background(), SignupRequest{
		Channel:    "wechat",
		Identifier: "wx_owner_manage_001",
		TenantName: "manage-admins",
	})
	if err != nil {
		t.Fatalf("owner signup failed: %v", err)
	}
	ownerClaims, err := svc.ParseSessionToken(ownerRes.Token)
	if err != nil {
		t.Fatalf("parse owner token failed: %v", err)
	}

	created, err := svc.CreateTenantUser(context.Background(), ownerClaims, CreateTenantUserRequest{
		Channel:    "wechat",
		Identifier: "wx_admin_manage_001",
		Role:       "admin",
	})
	if err != nil {
		t.Fatalf("create tenant user failed: %v", err)
	}
	if created.Role != "admin" {
		t.Fatalf("expected admin role, got %s", created.Role)
	}

	adminSignin, err := svc.Signin(context.Background(), SigninRequest{
		Channel:    "wechat",
		Identifier: "wx_admin_manage_001",
	})
	if err != nil {
		t.Fatalf("admin signin failed: %v", err)
	}
	adminClaims, err := svc.ParseSessionToken(adminSignin.Token)
	if err != nil {
		t.Fatalf("parse admin token failed: %v", err)
	}
	if adminClaims.Role != "admin" {
		t.Fatalf("expected admin claims role, got %s", adminClaims.Role)
	}

	updated, err := svc.UpdateTenantUserRole(context.Background(), ownerClaims, created.User.ID, "member")
	if err != nil {
		t.Fatalf("update role failed: %v", err)
	}
	if updated.Role != "member" {
		t.Fatalf("expected member role after update, got %s", updated.Role)
	}

	adminSignin2, err := svc.Signin(context.Background(), SigninRequest{
		Channel:    "wechat",
		Identifier: "wx_admin_manage_001",
	})
	if err != nil {
		t.Fatalf("second admin signin failed: %v", err)
	}
	adminClaims2, err := svc.ParseSessionToken(adminSignin2.Token)
	if err != nil {
		t.Fatalf("parse second token failed: %v", err)
	}
	if adminClaims2.Role != "member" {
		t.Fatalf("expected member claims role, got %s", adminClaims2.Role)
	}
}

func TestOwnerCanDisableAndEnableUser(t *testing.T) {
	_, svc := newTestService(t)

	ownerRes, err := svc.Signup(context.Background(), SignupRequest{
		Channel:    "wechat",
		Identifier: "wx_owner_status_001",
		TenantName: "manage-status",
	})
	if err != nil {
		t.Fatalf("owner signup failed: %v", err)
	}
	ownerClaims, err := svc.ParseSessionToken(ownerRes.Token)
	if err != nil {
		t.Fatalf("parse owner token failed: %v", err)
	}

	created, err := svc.CreateTenantUser(context.Background(), ownerClaims, CreateTenantUserRequest{
		Channel:    "wechat",
		Identifier: "wx_member_status_001",
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("create member failed: %v", err)
	}

	if _, err := svc.UpdateTenantUserStatus(context.Background(), ownerClaims, created.User.ID, "disabled"); err != nil {
		t.Fatalf("disable user failed: %v", err)
	}

	if _, err := svc.Signin(context.Background(), SigninRequest{
		Channel:    "wechat",
		Identifier: "wx_member_status_001",
	}); err != ErrUserDisabled {
		t.Fatalf("expected ErrUserDisabled, got %v", err)
	}

	if _, err := svc.UpdateTenantUserStatus(context.Background(), ownerClaims, created.User.ID, "active"); err != nil {
		t.Fatalf("enable user failed: %v", err)
	}

	if _, err := svc.Signin(context.Background(), SigninRequest{
		Channel:    "wechat",
		Identifier: "wx_member_status_001",
	}); err != nil {
		t.Fatalf("signin after enable failed: %v", err)
	}
}

func newTestService(t *testing.T) (*FileStore, *Service) {
	t.Helper()
	store, err := NewFileStore(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	svc := NewService(store, NewSessionManager("test-secret", 24*time.Hour), "000000", testLogger())
	return store, svc
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

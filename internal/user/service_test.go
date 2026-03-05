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
	_, svc := newTestService(t)

	admin, err := svc.BootstrapAdmin(context.Background(), SignupRequest{Email: "admin@example.com", Password: "Password123"})
	if err != nil {
		t.Fatalf("bootstrap admin failed: %v", err)
	}
	if admin.Token == "" || admin.Role != "admin" {
		t.Fatalf("unexpected bootstrap result: %+v", admin)
	}

	if _, err := svc.BootstrapAdmin(context.Background(), SignupRequest{Email: "admin2@example.com", Password: "Password123"}); err != ErrAdminBootstrapClosed {
		t.Fatalf("expected ErrAdminBootstrapClosed, got %v", err)
	}

	member, err := svc.Signup(context.Background(), SignupRequest{Email: "member@example.com", Password: "Password123"})
	if err != nil {
		t.Fatalf("member signup failed: %v", err)
	}
	if member.Role != "member" {
		t.Fatalf("expected member role, got %s", member.Role)
	}

	signedIn, err := svc.Signin(context.Background(), SigninRequest{Email: "admin@example.com", Password: "Password123"})
	if err != nil {
		t.Fatalf("signin failed: %v", err)
	}
	claims, err := svc.ValidateSessionToken(signedIn.Token)
	if err != nil {
		t.Fatalf("validate token failed: %v", err)
	}
	me, err := svc.GetMe(context.Background(), claims)
	if err != nil {
		t.Fatalf("get me failed: %v", err)
	}
	if me.User.Email != "admin@example.com" {
		t.Fatalf("unexpected me email: %s", me.User.Email)
	}
}

func TestInvalidCredentials(t *testing.T) {
	_, svc := newTestService(t)
	_, _ = svc.Signup(context.Background(), SignupRequest{Email: "user@example.com", Password: "Password123"})

	if _, err := svc.Signin(context.Background(), SigninRequest{Email: "user@example.com", Password: "wrongpass"}); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestSessionExpiryAndLogout(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	svc := NewService(store, NewSessionManager("test-secret", time.Millisecond), testLogger())

	res, err := svc.Signup(context.Background(), SignupRequest{Email: "u1@example.com", Password: "Password123"})
	if err != nil {
		t.Fatalf("signup failed: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := svc.ValidateSessionToken(res.Token); err != ErrSessionTokenExpired {
		t.Fatalf("expected ErrSessionTokenExpired, got %v", err)
	}

	svc2 := NewService(store, NewSessionManager("test-secret-2", 24*time.Hour), testLogger())
	res2, err := svc2.Signup(context.Background(), SignupRequest{Email: "u2@example.com", Password: "Password123"})
	if err != nil {
		t.Fatalf("signup failed: %v", err)
	}
	if err := svc2.Logout(context.Background(), res2.Token); err != nil {
		t.Fatalf("logout failed: %v", err)
	}
	if _, err := svc2.ValidateSessionToken(res2.Token); err != ErrSessionRevoked {
		t.Fatalf("expected ErrSessionRevoked, got %v", err)
	}
}

func TestAdminCanManageUsers(t *testing.T) {
	_, svc := newTestService(t)

	adminRes, err := svc.BootstrapAdmin(context.Background(), SignupRequest{Email: "admin@example.com", Password: "Password123"})
	if err != nil {
		t.Fatalf("admin bootstrap failed: %v", err)
	}
	adminClaims, err := svc.ValidateSessionToken(adminRes.Token)
	if err != nil {
		t.Fatalf("validate admin token failed: %v", err)
	}

	created, err := svc.CreateUser(context.Background(), adminClaims, CreateUserRequest{Email: "member@example.com", Password: "Password123", Role: "member"})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	if _, err := svc.UpdateUserRole(context.Background(), adminClaims, created.User.ID, "admin"); err != nil {
		t.Fatalf("update role failed: %v", err)
	}
	if _, err := svc.UpdateUserStatus(context.Background(), adminClaims, created.User.ID, "disabled"); err != nil {
		t.Fatalf("update status failed: %v", err)
	}
	if _, err := svc.Signin(context.Background(), SigninRequest{Email: "member@example.com", Password: "Password123"}); err != ErrUserDisabled {
		t.Fatalf("expected ErrUserDisabled, got %v", err)
	}
}

func TestMemberCannotAccessAdminActions(t *testing.T) {
	_, svc := newTestService(t)
	_, _ = svc.BootstrapAdmin(context.Background(), SignupRequest{Email: "admin@example.com", Password: "Password123"})
	memberRes, err := svc.Signup(context.Background(), SignupRequest{Email: "member@example.com", Password: "Password123"})
	if err != nil {
		t.Fatalf("member signup failed: %v", err)
	}
	memberClaims, err := svc.ValidateSessionToken(memberRes.Token)
	if err != nil {
		t.Fatalf("validate member token failed: %v", err)
	}

	if _, err := svc.ListUsers(context.Background(), memberClaims); err != ErrForbidden {
		t.Fatalf("expected ErrForbidden for list users, got %v", err)
	}
}

func newTestService(t *testing.T) (*FileStore, *Service) {
	t.Helper()
	store, err := NewFileStore(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	svc := NewService(store, NewSessionManager("test-secret", 24*time.Hour), testLogger())
	return store, svc
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

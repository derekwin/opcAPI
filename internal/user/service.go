package user

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/mail"
	"strings"
)

type Service struct {
	store   *FileStore
	session *SessionManager
	logger  *slog.Logger
}

func NewService(store *FileStore, session *SessionManager, logger *slog.Logger) *Service {
	return &Service{store: store, session: session, logger: logger}
}

func (s *Service) Signup(_ context.Context, req SignupRequest) (AuthResult, error) {
	if s.store == nil || s.session == nil {
		return AuthResult{}, ErrStoreUnavailable
	}

	email, err := normalizeEmail(req.Email)
	if err != nil {
		return AuthResult{}, err
	}
	password := strings.TrimSpace(req.Password)
	if err := validatePassword(password); err != nil {
		return AuthResult{}, err
	}

	if _, found, err := s.store.FindCredentialByEmail(email); err != nil {
		return AuthResult{}, err
	} else if found {
		return AuthResult{}, ErrUserExists
	}

	salt, err := newPasswordSalt()
	if err != nil {
		return AuthResult{}, err
	}
	hash := hashPassword(password, salt)

	created, err := s.store.CreateUser(email, hash, salt, "member")
	if err != nil {
		return AuthResult{}, err
	}
	token, claims, err := s.session.Issue(created.ID, created.Role)
	if err != nil {
		return AuthResult{}, err
	}

	if s.logger != nil {
		s.logger.Info("user signup", "user_id", created.ID, "role", created.Role, "email", created.Email)
	}
	return AuthResult{Token: token, User: created, Role: created.Role, Claims: claims}, nil
}

func (s *Service) BootstrapAdmin(_ context.Context, req SignupRequest) (AuthResult, error) {
	if s.store == nil || s.session == nil {
		return AuthResult{}, ErrStoreUnavailable
	}

	email, err := normalizeEmail(req.Email)
	if err != nil {
		return AuthResult{}, err
	}
	password := strings.TrimSpace(req.Password)
	if err := validatePassword(password); err != nil {
		return AuthResult{}, err
	}

	salt, err := newPasswordSalt()
	if err != nil {
		return AuthResult{}, err
	}
	hash := hashPassword(password, salt)

	created, err := s.store.CreateInitialAdmin(email, hash, salt)
	if err != nil {
		return AuthResult{}, err
	}
	token, claims, err := s.session.Issue(created.ID, created.Role)
	if err != nil {
		return AuthResult{}, err
	}

	if s.logger != nil {
		s.logger.Info("admin bootstrapped", "user_id", created.ID, "email", created.Email)
	}
	return AuthResult{Token: token, User: created, Role: created.Role, Claims: claims}, nil
}

func (s *Service) Signin(_ context.Context, req SigninRequest) (AuthResult, error) {
	if s.store == nil || s.session == nil {
		return AuthResult{}, ErrStoreUnavailable
	}

	email, err := normalizeEmail(req.Email)
	if err != nil {
		return AuthResult{}, err
	}
	password := strings.TrimSpace(req.Password)
	if password == "" {
		return AuthResult{}, ErrInvalidCredentials
	}

	cred, found, err := s.store.FindCredentialByEmail(email)
	if err != nil {
		return AuthResult{}, err
	}
	if !found {
		return AuthResult{}, ErrInvalidCredentials
	}
	if cred.User.Status != "active" {
		return AuthResult{}, ErrUserDisabled
	}
	if hashPassword(password, cred.PasswordSalt) != cred.PasswordHash {
		return AuthResult{}, ErrInvalidCredentials
	}

	token, claims, err := s.session.Issue(cred.User.ID, cred.User.Role)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{Token: token, User: cred.User, Role: cred.User.Role, Claims: claims}, nil
}

func (s *Service) ParseSessionToken(token string) (SessionClaims, error) {
	if s == nil || s.session == nil {
		return SessionClaims{}, ErrStoreUnavailable
	}
	return s.session.Parse(token)
}

func (s *Service) ValidateSessionToken(token string) (SessionClaims, error) {
	if s == nil || s.session == nil || s.store == nil {
		return SessionClaims{}, ErrStoreUnavailable
	}
	claims, err := s.session.Parse(token)
	if err != nil {
		return SessionClaims{}, err
	}
	revoked, err := s.store.IsSessionRevoked(claims.JTI)
	if err != nil {
		return SessionClaims{}, err
	}
	if revoked {
		return SessionClaims{}, ErrSessionRevoked
	}
	u, found, err := s.store.GetUserByID(claims.UserID)
	if err != nil {
		return SessionClaims{}, err
	}
	if !found {
		return SessionClaims{}, ErrUserNotFound
	}
	if strings.ToLower(strings.TrimSpace(u.Status)) != "active" {
		return SessionClaims{}, ErrUserDisabled
	}
	claims.Role = u.Role
	return claims, nil
}

func (s *Service) Logout(_ context.Context, token string) error {
	if s == nil || s.session == nil || s.store == nil {
		return ErrStoreUnavailable
	}
	claims, err := s.session.Parse(token)
	if err != nil {
		return err
	}
	if err := s.store.RevokeSession(claims.JTI, claims.Exp); err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Info("user logout", "user_id", claims.UserID, "jti", claims.JTI)
	}
	return nil
}

func (s *Service) HasActiveAdmin(_ context.Context) (bool, error) {
	if s == nil || s.store == nil {
		return false, ErrStoreUnavailable
	}
	count, err := s.store.AdminCount()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Service) HasAdmin(_ context.Context) (bool, error) {
	if s == nil || s.store == nil {
		return false, ErrStoreUnavailable
	}
	count, err := s.store.AnyAdminCount()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Service) GetMe(_ context.Context, claims SessionClaims) (AuthResult, error) {
	if s.store == nil {
		return AuthResult{}, ErrStoreUnavailable
	}

	u, found, err := s.store.GetUserByID(claims.UserID)
	if err != nil {
		return AuthResult{}, err
	}
	if !found {
		return AuthResult{}, ErrUserNotFound
	}
	if strings.ToLower(strings.TrimSpace(u.Status)) != "active" {
		return AuthResult{}, ErrUserDisabled
	}

	claims.Role = u.Role
	return AuthResult{User: u, Role: u.Role, Claims: claims}, nil
}

func (s *Service) ListUsers(_ context.Context, claims SessionClaims) ([]TenantUser, error) {
	if s.store == nil {
		return nil, ErrStoreUnavailable
	}
	if claims.Role != "admin" {
		return nil, ErrForbidden
	}
	return s.store.ListUsers()
}

func (s *Service) CreateUser(_ context.Context, claims SessionClaims, req CreateUserRequest) (TenantUser, error) {
	if s.store == nil {
		return TenantUser{}, ErrStoreUnavailable
	}
	if claims.Role != "admin" {
		return TenantUser{}, ErrForbidden
	}

	email, err := normalizeEmail(req.Email)
	if err != nil {
		return TenantUser{}, err
	}
	password := strings.TrimSpace(req.Password)
	if err := validatePassword(password); err != nil {
		return TenantUser{}, err
	}
	role, err := normalizeManagedRole(req.Role)
	if err != nil {
		return TenantUser{}, err
	}

	salt, err := newPasswordSalt()
	if err != nil {
		return TenantUser{}, err
	}
	hash := hashPassword(password, salt)

	u, err := s.store.CreateUser(email, hash, salt, role)
	if err != nil {
		return TenantUser{}, err
	}
	if s.logger != nil {
		s.logger.Info("user created", "actor_user_id", claims.UserID, "target_user_id", u.ID, "role", role)
	}
	return TenantUser{User: u, Role: role}, nil
}

func (s *Service) UpdateUserRole(_ context.Context, claims SessionClaims, userID, roleRaw string) (TenantUser, error) {
	if s.store == nil {
		return TenantUser{}, ErrStoreUnavailable
	}
	if claims.Role != "admin" {
		return TenantUser{}, ErrForbidden
	}
	role, err := normalizeManagedRole(roleRaw)
	if err != nil {
		return TenantUser{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return TenantUser{}, ErrUserNotFound
	}

	current, found, err := s.store.GetUserByID(userID)
	if err != nil {
		return TenantUser{}, err
	}
	if !found {
		return TenantUser{}, ErrUserNotFound
	}
	if current.Role == "admin" && role != "admin" {
		if admins, err := s.store.AdminCount(); err != nil {
			return TenantUser{}, err
		} else if admins <= 1 {
			return TenantUser{}, ErrForbidden
		}
	}

	updated, err := s.store.UpdateUserRole(userID, role)
	if err != nil {
		return TenantUser{}, err
	}
	if s.logger != nil {
		s.logger.Info("user role updated", "actor_user_id", claims.UserID, "target_user_id", updated.User.ID, "role", updated.Role)
	}
	return updated, nil
}

func (s *Service) UpdateUserStatus(_ context.Context, claims SessionClaims, userID, statusRaw string) (TenantUser, error) {
	if s.store == nil {
		return TenantUser{}, ErrStoreUnavailable
	}
	if claims.Role != "admin" {
		return TenantUser{}, ErrForbidden
	}
	status, err := normalizeManagedStatus(statusRaw)
	if err != nil {
		return TenantUser{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return TenantUser{}, ErrUserNotFound
	}
	if userID == claims.UserID && status != "active" {
		return TenantUser{}, ErrForbidden
	}

	current, found, err := s.store.GetUserByID(userID)
	if err != nil {
		return TenantUser{}, err
	}
	if !found {
		return TenantUser{}, ErrUserNotFound
	}
	if current.Role == "admin" && status != "active" {
		if admins, err := s.store.AdminCount(); err != nil {
			return TenantUser{}, err
		} else if admins <= 1 {
			return TenantUser{}, ErrForbidden
		}
	}

	updated, err := s.store.UpdateUserStatus(userID, status)
	if err != nil {
		return TenantUser{}, err
	}
	if s.logger != nil {
		s.logger.Info("user status updated", "actor_user_id", claims.UserID, "target_user_id", updated.User.ID, "status", updated.User.Status)
	}
	return updated, nil
}

func (s *Service) DeleteUser(_ context.Context, claims SessionClaims, userID string) (User, error) {
	if s.store == nil {
		return User{}, ErrStoreUnavailable
	}
	if claims.Role != "admin" {
		return User{}, ErrForbidden
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return User{}, ErrUserNotFound
	}
	if userID == claims.UserID {
		return User{}, ErrForbidden
	}

	current, found, err := s.store.GetUserByID(userID)
	if err != nil {
		return User{}, err
	}
	if !found {
		return User{}, ErrUserNotFound
	}
	if current.Role == "admin" {
		adminCount, err := s.store.AnyAdminCount()
		if err != nil {
			return User{}, err
		}
		if adminCount <= 1 {
			return User{}, ErrForbidden
		}
	}

	deleted, err := s.store.DeleteUser(userID)
	if err != nil {
		return User{}, err
	}
	if s.logger != nil {
		s.logger.Info("user deleted", "actor_user_id", claims.UserID, "target_user_id", deleted.ID)
	}
	return deleted, nil
}

func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return "", ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || strings.TrimSpace(addr.Address) == "" {
		return "", ErrInvalidEmail
	}
	return strings.ToLower(strings.TrimSpace(addr.Address)), nil
}

func validatePassword(password string) error {
	if len(password) < 8 {
		return ErrInvalidPassword
	}
	if len(password) > 128 {
		return ErrInvalidPassword
	}
	return nil
}

func normalizeManagedRole(raw string) (string, error) {
	role := strings.ToLower(strings.TrimSpace(raw))
	switch role {
	case "admin", "member":
		return role, nil
	default:
		return "", ErrInvalidRole
	}
}

func normalizeManagedStatus(raw string) (string, error) {
	status := strings.ToLower(strings.TrimSpace(raw))
	switch status {
	case "active", "disabled":
		return status, nil
	default:
		return "", ErrInvalidStatus
	}
}

func newPasswordSalt() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashPassword(password, salt string) string {
	seed := []byte(strings.TrimSpace(salt) + ":" + password)
	sum := sha256.Sum256(seed)
	for i := 0; i < 12000; i++ {
		next := append([]byte(salt), sum[:]...)
		sum = sha256.Sum256(next)
	}
	return hex.EncodeToString(sum[:])
}

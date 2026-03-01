package user

import (
	"context"
	"log/slog"
	"strings"
	"unicode"
)

type Service struct {
	store         *FileStore
	session       *SessionManager
	phoneTestCode string
	logger        *slog.Logger
}

func NewService(store *FileStore, session *SessionManager, phoneTestCode string, logger *slog.Logger) *Service {
	if phoneTestCode == "" {
		phoneTestCode = "000000"
	}
	return &Service{store: store, session: session, phoneTestCode: phoneTestCode, logger: logger}
}

func (s *Service) Signup(_ context.Context, req SignupRequest) (AuthResult, error) {
	if s.store == nil || s.session == nil {
		return AuthResult{}, ErrStoreUnavailable
	}

	channel, identifier, err := normalizeIdentity(req.Channel, req.Identifier)
	if err != nil {
		return AuthResult{}, err
	}
	if err := s.verifyIdentity(channel, req.VerificationCode); err != nil {
		return AuthResult{}, err
	}

	if _, found, err := s.store.FindUserByIdentity(channel, identifier); err != nil {
		return AuthResult{}, err
	} else if found {
		return AuthResult{}, ErrUserExists
	}

	user, tenant, membership, err := s.store.CreateTenantOwner(channel, identifier, strings.TrimSpace(req.TenantName))
	if err != nil {
		return AuthResult{}, err
	}

	token, claims, err := s.session.Issue(user.ID, tenant.ID, membership.Role)
	if err != nil {
		return AuthResult{}, err
	}

	if s.logger != nil {
		s.logger.Info("user signup", "user_id", user.ID, "tenant_id", tenant.ID, "channel", channel)
	}
	return AuthResult{Token: token, User: user, Tenant: tenant, Role: membership.Role, Claims: claims}, nil
}

func (s *Service) Signin(_ context.Context, req SigninRequest) (AuthResult, error) {
	if s.store == nil || s.session == nil {
		return AuthResult{}, ErrStoreUnavailable
	}

	channel, identifier, err := normalizeIdentity(req.Channel, req.Identifier)
	if err != nil {
		return AuthResult{}, err
	}
	if err := s.verifyIdentity(channel, req.VerificationCode); err != nil {
		return AuthResult{}, err
	}

	user, found, err := s.store.FindUserByIdentity(channel, identifier)
	if err != nil {
		return AuthResult{}, err
	}
	if !found {
		return AuthResult{}, ErrUserNotFound
	}

	user, membership, tenant, found, err := s.store.GetSessionContext(user.ID)
	if err != nil {
		return AuthResult{}, err
	}
	if !found {
		return AuthResult{}, ErrUserNotFound
	}
	if strings.ToLower(strings.TrimSpace(user.Status)) != "active" {
		return AuthResult{}, ErrUserDisabled
	}

	token, claims, err := s.session.Issue(user.ID, tenant.ID, membership.Role)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{Token: token, User: user, Tenant: tenant, Role: membership.Role, Claims: claims}, nil
}

func (s *Service) ParseSessionToken(token string) (SessionClaims, error) {
	if s == nil || s.session == nil {
		return SessionClaims{}, ErrStoreUnavailable
	}
	return s.session.Parse(token)
}

func (s *Service) GetMe(_ context.Context, claims SessionClaims) (AuthResult, error) {
	if s.store == nil {
		return AuthResult{}, ErrStoreUnavailable
	}

	user, membership, tenant, found, err := s.store.GetSessionContext(claims.UserID)
	if err != nil {
		return AuthResult{}, err
	}
	if !found {
		return AuthResult{}, ErrUserNotFound
	}
	if strings.ToLower(strings.TrimSpace(user.Status)) != "active" {
		return AuthResult{}, ErrUserDisabled
	}

	return AuthResult{User: user, Tenant: tenant, Role: membership.Role, Claims: claims}, nil
}

func (s *Service) ListTenantUsers(_ context.Context, claims SessionClaims) ([]TenantUser, error) {
	if s.store == nil {
		return nil, ErrStoreUnavailable
	}
	if claims.Role != "owner" && claims.Role != "admin" {
		return nil, ErrForbidden
	}
	return s.store.ListTenantUsers(claims.TenantID)
}

func (s *Service) CreateTenantUser(_ context.Context, claims SessionClaims, req CreateTenantUserRequest) (TenantUser, error) {
	if s.store == nil {
		return TenantUser{}, ErrStoreUnavailable
	}
	if claims.Role != "owner" {
		return TenantUser{}, ErrForbidden
	}

	channel, identifier, err := normalizeIdentity(req.Channel, req.Identifier)
	if err != nil {
		return TenantUser{}, err
	}
	if err := s.verifyIdentity(channel, req.VerificationCode); err != nil {
		return TenantUser{}, err
	}
	role, err := normalizeManagedRole(req.Role)
	if err != nil {
		return TenantUser{}, err
	}

	created, err := s.store.CreateTenantUser(claims.TenantID, channel, identifier, role)
	if err != nil {
		return TenantUser{}, err
	}
	if s.logger != nil {
		s.logger.Info("tenant user created", "tenant_id", claims.TenantID, "actor_user_id", claims.UserID, "target_user_id", created.User.ID, "role", created.Role)
	}
	return created, nil
}

func (s *Service) UpdateTenantUserRole(_ context.Context, claims SessionClaims, userID, roleRaw string) (TenantUser, error) {
	if s.store == nil {
		return TenantUser{}, ErrStoreUnavailable
	}
	if claims.Role != "owner" {
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
	if userID == claims.UserID {
		return TenantUser{}, ErrForbidden
	}

	updated, err := s.store.UpdateTenantUserRole(claims.TenantID, userID, role)
	if err != nil {
		return TenantUser{}, err
	}
	if s.logger != nil {
		s.logger.Info("tenant user role updated", "tenant_id", claims.TenantID, "actor_user_id", claims.UserID, "target_user_id", updated.User.ID, "role", updated.Role)
	}
	return updated, nil
}

func (s *Service) UpdateTenantUserStatus(_ context.Context, claims SessionClaims, userID, statusRaw string) (TenantUser, error) {
	if s.store == nil {
		return TenantUser{}, ErrStoreUnavailable
	}
	if claims.Role != "owner" {
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
	if userID == claims.UserID {
		return TenantUser{}, ErrForbidden
	}

	users, err := s.store.ListTenantUsers(claims.TenantID)
	if err != nil {
		return TenantUser{}, err
	}
	for _, item := range users {
		if item.User.ID != userID {
			continue
		}
		if item.Role == "owner" {
			return TenantUser{}, ErrForbidden
		}
		break
	}

	updated, err := s.store.UpdateTenantUserStatus(claims.TenantID, userID, status)
	if err != nil {
		return TenantUser{}, err
	}
	if s.logger != nil {
		s.logger.Info("tenant user status updated", "tenant_id", claims.TenantID, "actor_user_id", claims.UserID, "target_user_id", updated.User.ID, "status", updated.User.Status)
	}
	return updated, nil
}

func (s *Service) verifyIdentity(channel Channel, code string) error {
	if channel == ChannelPhone {
		if strings.TrimSpace(code) != s.phoneTestCode {
			return ErrInvalidVerifyCode
		}
	}
	return nil
}

func normalizeIdentity(channelRaw, identifierRaw string) (Channel, string, error) {
	channel := Channel(strings.ToLower(strings.TrimSpace(channelRaw)))
	identifier := strings.TrimSpace(identifierRaw)
	if identifier == "" {
		return "", "", ErrInvalidIdentifier
	}

	switch channel {
	case ChannelPhone:
		if !isDigits(identifier) {
			return "", "", ErrInvalidIdentifier
		}
	case ChannelWeChat, ChannelFeishu:
		if len(identifier) < 3 {
			return "", "", ErrInvalidIdentifier
		}
	default:
		return "", "", ErrInvalidChannel
	}

	return channel, identifier, nil
}

func isDigits(s string) bool {
	for _, ch := range s {
		if !unicode.IsDigit(ch) {
			return false
		}
	}
	return len(s) > 0
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

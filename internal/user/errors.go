package user

import "errors"

var (
	ErrInvalidEmail         = errors.New("invalid email")
	ErrInvalidPassword      = errors.New("invalid password")
	ErrInvalidRole          = errors.New("invalid role")
	ErrInvalidStatus        = errors.New("invalid status")
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrUserExists           = errors.New("user already exists")
	ErrUserNotFound         = errors.New("user not found")
	ErrUserDisabled         = errors.New("user disabled")
	ErrAdminBootstrapClosed = errors.New("admin bootstrap closed")
	ErrForbidden            = errors.New("forbidden")
	ErrSessionRevoked       = errors.New("session revoked")
	ErrSessionTokenInvalid  = errors.New("session token invalid")
	ErrSessionTokenExpired  = errors.New("session token expired")
	ErrStoreUnavailable     = errors.New("user store unavailable")
)

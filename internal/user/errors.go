package user

import "errors"

var (
	ErrInvalidChannel      = errors.New("invalid auth channel")
	ErrInvalidIdentifier   = errors.New("invalid identifier")
	ErrInvalidRole         = errors.New("invalid role")
	ErrInvalidStatus       = errors.New("invalid status")
	ErrInvalidVerifyCode   = errors.New("invalid verification code")
	ErrUserExists          = errors.New("user already exists")
	ErrUserNotFound        = errors.New("user not found")
	ErrUserDisabled        = errors.New("user disabled")
	ErrTenantNotFound      = errors.New("tenant not found")
	ErrForbidden           = errors.New("forbidden")
	ErrSessionTokenInvalid = errors.New("session token invalid")
	ErrSessionTokenExpired = errors.New("session token expired")
	ErrStoreUnavailable    = errors.New("user store unavailable")
)

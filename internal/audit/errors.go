package audit

import "errors"

var (
	ErrStoreUnavailable = errors.New("audit store unavailable")
	ErrInvalidEvent     = errors.New("invalid audit event")
)

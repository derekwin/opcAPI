package billing

import "errors"

var (
	ErrInvalidAmount       = errors.New("invalid amount")
	ErrInvalidChannel      = errors.New("invalid payment channel")
	ErrInvalidCurrency     = errors.New("invalid currency")
	ErrInvalidPlan         = errors.New("invalid plan")
	ErrInvalidSubscription = errors.New("invalid subscription request")
	ErrInvalidUsage        = errors.New("invalid usage")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrStoreUnavailable    = errors.New("billing store unavailable")
)

package billing

import "errors"

var (
	ErrInvalidAmount       = errors.New("invalid amount")
	ErrInvalidChannel      = errors.New("invalid payment channel")
	ErrInvalidCurrency     = errors.New("invalid currency")
	ErrInvalidLimit        = errors.New("invalid limit")
	ErrInvalidPlan         = errors.New("invalid plan")
	ErrInvalidPeriodEnd    = errors.New("invalid period end")
	ErrInvalidSubscription = errors.New("invalid subscription request")
	ErrInvalidUsage        = errors.New("invalid usage")
	ErrInvalidUserID       = errors.New("invalid user id")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrOrderNotFound       = errors.New("order not found")
	ErrStoreUnavailable    = errors.New("billing store unavailable")
	ErrTopupDisabled       = errors.New("topup disabled")
)

package upstream

import "errors"

var (
	ErrStoreUnavailable  = errors.New("upstream store unavailable")
	ErrInvalidUpstream   = errors.New("invalid upstream")
	ErrUpstreamNotFound  = errors.New("upstream not found")
	ErrInvalidRoute      = errors.New("invalid model route")
	ErrRouteNotFound     = errors.New("model route not found")
	ErrRouteDisabled     = errors.New("model route disabled")
	ErrNoAvailableTarget = errors.New("no available upstream target")
)

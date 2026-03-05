package promptapi

import "errors"

var (
	ErrStoreUnavailable  = errors.New("prompt api store unavailable")
	ErrInvalidSlug       = errors.New("invalid prompt api slug")
	ErrInvalidName       = errors.New("invalid prompt api name")
	ErrInvalidModel      = errors.New("invalid prompt api model")
	ErrInvalidTemplate   = errors.New("invalid prompt api template")
	ErrInvalidDefaultAPI = errors.New("invalid default api config")
	ErrModelNotAllowed   = errors.New("model is not allowed for this api")
	ErrNotFound          = errors.New("prompt api not found")
	ErrNotPublished      = errors.New("prompt api is not published")
	ErrInvalidInput      = errors.New("invalid prompt input")
)

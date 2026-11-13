package llmproviders

import "errors"

var ErrBackendMismatch = errors.New("model API backend is not supported by this endpoint; reload the session after changing api_backend")

var (
	ErrModelNameInvalid       = errors.New("model name must contain at most 100 characters")
	ErrModelCapacityInvalid   = errors.New("model capacities must be positive safe integers")
	ErrModelImageInputInvalid = errors.New("image input limits must be nonnegative integers within the proxy request limit")
	ErrModelModalitiesInvalid = errors.New("model inputs must be a nonempty set of text/image")
)

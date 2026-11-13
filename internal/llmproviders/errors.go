package llmproviders

import "errors"

var ErrBackendMismatch = errors.New("model API backend is not supported by this endpoint; reload the session after changing api_backend")

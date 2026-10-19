package llmproviders

import "errors"

var ErrBackendMismatch = errors.New("model API backend is not supported by this endpoint; chat_completions is only supported in brain mode; reload the session after changing api_backend")

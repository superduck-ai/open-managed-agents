package sessionports

import "errors"

var (
	ErrDisabled    = errors.New("session ports are disabled")
	ErrInvalidSlug = errors.New("invalid session port slug")
	ErrInvalidHost = errors.New("invalid session port host")
	ErrPortBlocked = errors.New("session port is not allowed")
	errInvalidKey  = errors.New("session port slug key must be 32 bytes encoded as base64")
	errInvalidUUID = errors.New("session port slug requires a session UUID")
)

package sessionports

import "errors"

var (
	ErrDisabled     = errors.New("session ports are disabled")
	ErrInvalidLabel = errors.New("invalid session port label")
	ErrInvalidHost  = errors.New("invalid session port host")
	ErrPortBlocked  = errors.New("session port is not allowed")
	errInvalidKey   = errors.New("session port label key must be 32 bytes encoded as base64")
	errInvalidUUID  = errors.New("session port label requires a session UUID")
)

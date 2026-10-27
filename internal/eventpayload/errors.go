package eventpayload

import "errors"

var (
	errPayloadTooLarge    = errors.New("event payload exceeds size limit")
	errStorageUnavailable = errors.New("event payload storage unavailable")
	errSizeMismatch       = errors.New("event payload size mismatch")
	errDigestMismatch     = errors.New("event payload digest mismatch")
)

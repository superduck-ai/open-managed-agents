package transcriptarchive

import "errors"

var (
	errInvalidSegment = errors.New("invalid transcript archive segment")
	errIntegrity      = errors.New("transcript archive integrity check failed")
)

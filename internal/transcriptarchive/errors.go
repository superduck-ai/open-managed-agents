package transcriptarchive

import "errors"

var (
	// ErrInvalidSegment identifies invalid encoded records or segment metadata.
	ErrInvalidSegment = errors.New("invalid transcript archive segment")
	// ErrIntegrity identifies archive bytes that fail integrity verification.
	ErrIntegrity = errors.New("transcript archive integrity check failed")
)

func invalidSegment(cause error) error { return errors.Join(ErrInvalidSegment, cause) }

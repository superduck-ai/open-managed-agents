package sessionfanout

import "errors"

var (
	errNATSBusClosed               = errors.New("nats session fanout is closed")
	errNATSSubscriptionInvalidated = errors.New("nats session subscription was replaced")
	errEmptySessionID              = errors.New("session fanout session ID is required")
	errInvalidSubject              = errors.New("session fanout session ID contains invalid characters")
)

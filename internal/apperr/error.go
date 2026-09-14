// Package apperr defines application errors that are independent of transport
// protocols and carry an explicit client-safe message. They contain no HTTP
// status or wire error type.
package apperr

// Kind is a small application-error category interpreted by transport adapters.
type Kind uint8

const (
	InvalidArgument Kind = iota + 1
	Unauthenticated
	Billing
	PermissionDenied
	NotFound
	Conflict
	RateLimited
	Timeout
	Internal
	Overloaded
	InvalidState
	PreconditionFailed
	RequestTooLarge
	Unavailable
)

// Error carries an application error's public contract and private cause.
// Only PublicMessage is safe to expose to clients; Error() is not.
type Error struct {
	Kind Kind
	// PublicMessage must be non-empty and safe to expose to clients.
	PublicMessage string
	// ErrorType overrides the wire error code chosen by transport adapters.
	ErrorType string
	cause     error
}

// New creates a client-presentable application error while preserving its
// private internal cause.
func New(kind Kind, publicMessage string, cause error) *Error {
	return &Error{
		Kind:          kind,
		PublicMessage: publicMessage,
		cause:         cause,
	}
}

// NewWithType creates an application error with an explicit wire error type,
// used when the transport must emit a code more specific than the kind default.
func NewWithType(kind Kind, errorType, publicMessage string, cause error) *Error {
	return &Error{
		Kind:          kind,
		PublicMessage: publicMessage,
		ErrorType:     errorType,
		cause:         cause,
	}
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	return "application error"
}

// Unwrap exposes the internal cause to errors.Is and errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

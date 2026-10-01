package hashicorpvault

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
)

// RequestError exposes only locally classified failures and HTTP status.
// Raw errors, response bodies, URLs and tokens are deliberately not retained.
type RequestError struct {
	Operation  string
	Code       string
	StatusCode int
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("hashicorp_vault: %s: %s (status: %d)", e.Operation, e.Code, e.StatusCode)
}

// LogValue supports structured logs without exposing untrusted remote messages.
func (e *RequestError) LogValue() slog.Value {
	return slog.GroupValue(slog.String("provider", providerName), slog.String("operation", e.Operation), slog.String("code", e.Code), slog.Int("status", e.StatusCode))
}

func failure(operation, code string, status int) error {
	return &RequestError{Operation: operation, Code: code, StatusCode: status}
}

func transportError(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("hashicorp_vault: %s: %w", operation, ctx.Err())
	}
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, sentinel) {
			return fmt.Errorf("hashicorp_vault: %s: %w", operation, sentinel)
		}
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return fmt.Errorf("hashicorp_vault: %s: %w", operation, context.DeadlineExceeded)
	}
	return failure(operation, "NetworkError", 0)
}

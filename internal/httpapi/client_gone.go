package httpapi

import (
	"log/slog"
	"net/http"
)

func ClientGone(r *http.Request) bool {
	return r.Context().Err() != nil
}

func LogFailure(logger *slog.Logger, r *http.Request, message string, attrs ...any) {
	if ClientGone(r) {
		logger.InfoContext(r.Context(), message, append(attrs, "client_canceled", true)...)
		return
	}
	logger.ErrorContext(r.Context(), message, attrs...)
}

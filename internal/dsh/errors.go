package dsh

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

var ErrUnauthenticated = errors.New("invalid DSR session credential")

type protocolError struct {
	status     int
	code       string
	message    string
	nextOffset *int64
}

func (e *protocolError) Error() string { return e.message }

func invalidRequest() error {
	return &protocolError{status: http.StatusBadRequest, code: "invalid_request", message: "Invalid DSR request"}
}
func unsupportedProtocol() error {
	return &protocolError{status: http.StatusBadRequest, code: "unsupported_protocol", message: "Unsupported DSR protocol"}
}
func unauthorized() error {
	return &protocolError{status: http.StatusUnauthorized, code: "unauthorized", message: "Invalid DSR session token"}
}
func payloadTooLarge() error {
	return &protocolError{status: http.StatusRequestEntityTooLarge, code: "payload_too_large", message: "DSR payload exceeds the size limit"}
}

func writeError(w http.ResponseWriter, r *http.Request, err error, nextOffset *int64) {
	var protocol *protocolError
	if !errors.As(err, &protocol) {
		protocol = storageError(err, nextOffset)
	}
	w.Header().Set("DSR-Error", protocol.code)
	if protocol.nextOffset != nil {
		w.Header().Set("DSR-Next-Offset", strconv.FormatInt(*protocol.nextOffset, 10))
	}
	kind := "invalid_request_error"
	if protocol.status >= 500 {
		kind = "api_error"
	} else if protocol.status == 401 {
		kind = "authentication_error"
	} else if protocol.status == 404 {
		kind = "not_found_error"
	} else if protocol.status == 409 {
		kind = "conflict_error"
	}
	httpapi.WriteError(w, r, httpapi.NewError(protocol.status, kind, protocol.message))
}

func storageError(err error, nextOffset *int64) *protocolError {
	result := &protocolError{status: http.StatusInternalServerError, code: "internal_error", message: "DSR operation failed"}
	switch {
	case errors.Is(err, ErrUnauthenticated):
		result.status, result.code, result.message = 401, "unauthorized", "Invalid DSR session token"
	case errors.Is(err, db.ErrWorkerEpochMismatch), errors.Is(err, db.ErrPreconditionFailed):
		result.status, result.code, result.message = 409, "epoch_superseded", "Worker epoch has been superseded"
	case errors.Is(err, db.ErrWorkerLeaseExpired):
		result.status, result.code, result.message = 410, "lease_expired", "Worker lease has expired"
	case errors.Is(err, db.ErrDSHSessionTerminated):
		result.status, result.code, result.message = 410, "session_terminated", "Session has been terminated"
	case errors.Is(err, db.ErrDSHSessionArchived):
		result.status, result.code, result.message = 410, "session_archived", "Session has been archived"
	case errors.Is(err, db.ErrNotFound):
		result.status, result.code, result.message = 404, "session_not_found", "Session or transcript not found"
	case errors.Is(err, db.ErrDSHHeaderRequired):
		result.status, result.code, result.message = 409, "transcript_header_required", "Transcript header is required"
	case errors.Is(err, db.ErrDSHTranscriptConflict):
		result.status, result.code, result.message = 409, "transcript_conflict", "Transcript content conflicts with confirmed history"
	case errors.Is(err, db.ErrDSHTranscriptGap):
		result.status, result.code, result.message, result.nextOffset = 409, "transcript_gap", "Transcript has a sequence gap", nextOffset
	case errors.Is(err, db.ErrDSHInputNotEligible):
		result.status, result.code, result.message = 409, "input_not_eligible", "Input is not eligible for this operation"
	case errors.Is(err, db.ErrDSHFinishConflict):
		result.status, result.code, result.message = 409, "finish_idempotency_mismatch", "Finish differs from confirmed result"
	case errors.Is(err, db.ErrDSHPermissionConflict):
		result.status, result.code, result.message = 409, "permission_idempotency_mismatch", "Permission differs from confirmed request"
	case errors.Is(err, db.ErrDSHPublicHistoryInconsistent):
		result.status, result.code, result.message = 409, "public_event_history_inconsistent", "Public receipt and history are inconsistent"
	case errors.Is(err, db.ErrDSHPublicConflict):
		result.status, result.code, result.message = 409, "public_event_idempotency_mismatch", "Public event differs from confirmed request"
	case errors.Is(err, db.ErrInvalidState):
		result.status, result.code, result.message = 400, "invalid_request", "Invalid DSR request"
	}
	return result
}

func configureNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, &protocolError{status: http.StatusNotFound, code: "route_not_found", message: "DSR route not found"}, nil)
}

func configureMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, &protocolError{status: http.StatusMethodNotAllowed, code: "invalid_request", message: "Method not allowed"}, nil)
}

func SetLifecycleErrorHeaders(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrWorkerEpochMismatch):
		w.Header().Set("DSR-Error", "epoch_superseded")
	case errors.Is(err, db.ErrWorkerLeaseExpired):
		w.Header().Set("DSR-Error", "lease_expired")
	}
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) { writeError(w, r, err, nil) }

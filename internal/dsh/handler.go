package dsh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const maxRequestBytes = 16 << 20
const maxEventBytes = 15 << 20
const maxPageEvents = 200

type Database interface {
	AppendDSHTranscript(context.Context, db.DSHScope, string, *string, []db.DSHTranscriptRecord) (db.DSHTranscriptAppendResult, error)
	ListDSHTranscripts(context.Context, db.DSHScope, string, int) ([]db.DSHTranscript, error)
	ReadDSHTranscript(context.Context, db.DSHScope, string, int64, int) (db.DSHTranscript, []db.DSHTranscriptRecord, error)
}

type Authenticator func(*http.Request, string) (db.DSHScope, error)

type Handler struct {
	database     Database
	authenticate Authenticator
	logger       *slog.Logger
}

func NewHandler(database Database, authenticate Authenticator, logger *slog.Logger) *Handler {
	return &Handler{database: database, authenticate: authenticate, logger: logging.LoggerOrDefault(logger)}
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	router.Route("/sessions/{code_session_id}/transcripts", func(router chi.Router) {
		router.Use(h.authorize)
		router.Get("/", h.list)
		router.Get("/{dsh_session_id}/records", h.read)
		router.Post("/{dsh_session_id}/records", h.appendRecords)
	})
}

type scopeKey struct{}

func (h *Handler) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("DSR-Protocol") != "1" {
			writeError(w, r, unsupportedProtocol(), nil)
			return
		}
		scope, err := h.authenticate(r, chi.URLParam(r, "code_session_id"))
		if err != nil {
			h.fail(w, r, err, nil)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), scopeKey{}, scope)))
	})
}

func scopeFrom(r *http.Request) db.DSHScope { return r.Context().Value(scopeKey{}).(db.DSHScope) }

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error, nextSeq *int64) {
	if storageError(err, nextSeq).status >= 500 {
		var protocol *protocolError
		if !errors.As(err, &protocol) {
			h.logger.ErrorContext(r.Context(), "DSH transcript operation failed", "code_session_id", chi.URLParam(r, "code_session_id"), "error", err)
		}
	}
	writeError(w, r, err, nextSeq)
}

func decodeBody[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var result T
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return result, payloadTooLarge()
		}
		return result, invalidRequest()
	}
	canonical, err := jsoncanonical.Canonicalize(raw)
	if err != nil || len(canonical) == 0 || canonical[0] != '{' {
		return result, invalidRequest()
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, invalidRequest()
	}
	return result, nil
}

func decimal(value string) (int64, error) {
	if value == "" || len(value) > 1 && value[0] == '0' {
		return 0, invalidRequest()
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, invalidRequest()
		}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, invalidRequest()
	}
	return parsed, nil
}

func writeJSON(w http.ResponseWriter, value any) { httpapi.WriteJSON(w, http.StatusOK, value) }

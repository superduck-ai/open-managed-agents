package dsh

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type observedRegistrationLifecycle struct {
	testLifecycle
	issues   []RegistrationIssue
	contexts []json.RawMessage
}

func (l *observedRegistrationLifecycle) ObserveOwnedRepositoryRegistration(_ context.Context, _ db.DSHScope, raw json.RawMessage, issue RegistrationIssue) {
	l.issues = append(l.issues, issue)
	l.contexts = append(l.contexts, append(json.RawMessage(nil), raw...))
}

type partialRegistrationWriter struct {
	*httptest.ResponseRecorder
	fail bool
}

func (w partialRegistrationWriter) Write(raw []byte) (int, error) {
	if w.fail {
		return 0, errors.New("local write failure")
	}
	return w.ResponseRecorder.Write(raw[:len(raw)-1])
}

func TestRegistrationIssuePreservesResponseAndMarksIncompleteWrites(t *testing.T) {
	for _, mode := range []string{"short", "error", "complete", "context-rejected", "database-rejected"} {
		t.Run(mode, func(t *testing.T) {
			database := &lifecycleDatabase{}
			lifecycle := &observedRegistrationLifecycle{}
			if mode == "context-rejected" {
				lifecycle.registrationErr = errors.New("local context failure")
			}
			if mode == "database-rejected" {
				database.err = errors.New("local register failure")
			}
			router := chi.NewRouter()
			NewControlHandler(database, lifecycle, func(*http.Request, string) (db.DSHScope, error) { return db.DSHScope{Epoch: 7}, nil }, func(*http.Request) error { return nil }, nil).RegisterRoutes(router)
			recorder := httptest.NewRecorder()
			var writer http.ResponseWriter = recorder
			if mode == "short" || mode == "error" {
				writer = partialRegistrationWriter{recorder, mode == "error"}
			}
			req := httptest.NewRequest("POST", "/sessions/cse_root/register", strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer local-test")
			req.Header.Set("DSR-Protocol", "1")
			router.ServeHTTP(writer, req)
			if mode == "context-rejected" || mode == "database-rejected" {
				if len(lifecycle.issues) != 0 {
					t.Fatal("rejected registration observed as issued")
				}
				return
			}
			if len(lifecycle.issues) != 1 {
				t.Fatal("actual handler issuance missing")
			}
			issue := lifecycle.issues[0]
			if issue.ResponseEpoch != 7 || issue.Status != 200 || issue.StartedAt.IsZero() || issue.FinishedAt.Before(issue.StartedAt) {
				t.Fatal("wrong actual issuance provenance", issue)
			}
			if issue.Complete != (mode == "complete") {
				t.Fatal("partial response claimed complete")
			}
			if mode == "complete" {
				baseline := transcriptRequest(controlRouter(&lifecycleDatabase{}), "POST", "/sessions/cse_root/register", `{}`, "1")
				if recorder.Code != baseline.Code || recorder.Body.String() != baseline.Body.String() || recorder.Header().Get("Content-Type") != baseline.Header().Get("Content-Type") || string(lifecycle.contexts[0]) != `{"models":{}}` {
					t.Fatal("observer changed actual response")
				}
			}
		})
	}
}

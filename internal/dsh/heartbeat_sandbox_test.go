package dsh

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestHeartbeatDoesNotRenewSandboxBeforeScopedLeaseValidation(t *testing.T) {
	for _, tc := range []struct {
		name           string
		authErr, dbErr error
		status         int
	}{{"unauthenticated", ErrUnauthenticated, nil, 401}, {"foreign epoch", nil, db.ErrWorkerEpochMismatch, 409}, {"expired", nil, db.ErrWorkerLeaseExpired, 410}, {"unknown session", nil, db.ErrNotFound, 404}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			database := &lifecycleDatabase{err: tc.dbErr}
			router := chi.NewRouter()
			NewControlHandler(database, testLifecycle{renew: func(context.Context, db.DSHScope) error { calls++; return nil }}, func(*http.Request, string) (db.DSHScope, error) {
				return db.DSHScope{CodeSessionID: "owned", Epoch: 7}, tc.authErr
			}, nil, nil).RegisterRoutes(router)
			response := transcriptRequest(router, "POST", "/sessions/owned/heartbeat", `{}`, "1")
			if response.Code != tc.status || calls != 0 {
				t.Fatal("foreign sandbox renewal", response.Code, calls)
			}
		})
	}
}

func TestHeartbeatSandboxFailureIsUnavailableAndRetriesSameTTL(t *testing.T) {
	database := &lifecycleDatabase{}
	calls := 0
	unavailable := true
	router := chi.NewRouter()
	elapsed := time.Duration(0)
	expiry := time.Duration(0)
	ttl := 30 * time.Second
	scope := db.DSHScope{CodeSessionID: "owned", SessionID: "session", WorkspaceUUID: "workspace", OrganizationUUID: "org", Epoch: 7}
	NewControlHandler(database, testLifecycle{renew: func(_ context.Context, actual db.DSHScope) error {
		calls++
		if actual != scope {
			t.Error("scope changed")
		}
		if unavailable {
			return errors.New("provider unavailable")
		}
		expiry = elapsed + ttl
		return nil
	}}, func(*http.Request, string) (db.DSHScope, error) { return scope, nil }, nil, nil).RegisterRoutes(router)
	response := transcriptRequest(router, "POST", "/sessions/owned/heartbeat", `{}`, "1")
	if response.Code != 503 || response.Header().Get("DSR-Error") != "sandbox_unavailable" || calls != 1 {
		t.Fatal("failed renewal acknowledged", response.Code, calls)
	}
	unavailable = false
	for elapsed = 0; elapsed <= 36*time.Second; elapsed += 6 * time.Second {
		response = transcriptRequest(router, "POST", "/sessions/owned/heartbeat", `{}`, "1")
		if response.Code != 200 || expiry != elapsed+ttl {
			t.Fatal("heartbeat did not renew original TTL", elapsed, expiry)
		}
	}
	if expiry != 66*time.Second || calls != 8 {
		t.Fatal("renewal stopped at original expiry", expiry, calls)
	}
}

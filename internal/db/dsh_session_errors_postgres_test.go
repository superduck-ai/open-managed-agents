//go:build integration

package db

import (
	"errors"
	"testing"
	"uuid"
)

func TestDSHSessionStateErrorsPostgres(t *testing.T) {
	for _, test := range []struct {
		name, status string
		archived     bool
		want         error
	}{
		{"terminated", "terminated", false, ErrDSHSessionTerminated},
		{"archived", "idle", true, ErrDSHSessionArchived},
		{"terminated_and_archived", "terminated", true, ErrDSHSessionTerminated},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newDSHLockFixture(t)
			execClaimTest(t, f.ctx, f.database, `UPDATE sessions SET status=$1, archived_at=CASE WHEN $2 THEN now() ELSE NULL END WHERE uuid=$3`, test.status, test.archived, f.code.SessionUUID)
			if _, _, err := f.store.FinishDSHInput(f.ctx, f.scope, "input", DSHFinish{Result: "success"}); !errors.Is(err, test.want) {
				t.Fatalf("finish error=%v want=%v", err, test.want)
			}
			result, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{{ID: "event", Payload: []byte(`{}`)}})
			if !errors.Is(err, test.want) || result.Accepted != 0 {
				t.Fatalf("publish=%+v err=%v", result, err)
			}
			if err := f.store.ValidateDSHControlInputs(f.ctx, f.scope, []string{"control"}); !errors.Is(err, test.want) {
				t.Fatalf("delivery error=%v", err)
			}
			other := f.scope
			other.OrganizationUUID = uuid.NewV4().String()
			if _, _, err := f.store.FinishDSHInput(f.ctx, other, "input", DSHFinish{Result: "success"}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("cross-tenant state leaked: %v", err)
			}
			_, records, err := f.store.ReadDSHTranscript(f.ctx, f.scope, "root", 0, 200)
			if err != nil || len(records) != 1 {
				t.Fatalf("history unavailable: records=%d err=%v", len(records), err)
			}
		})
	}
}

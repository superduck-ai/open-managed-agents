package db

import (
	"reflect"
	"slices"
	"testing"
	"time"
	"uuid"
)

func TestTranscriptArchiveTerminalCandidatesPostgres(t *testing.T) {
	store, database := newTranscriptArchiveTestDB(t)
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	var want []TranscriptScope
	for _, scenario := range []struct {
		name, sessionSQL, workerSQL string
		eligible, sibling           bool
	}{
		{name: "terminated", sessionSQL: "UPDATE sessions SET status='terminated' WHERE uuid=$1"},
		{name: "running", sessionSQL: "UPDATE sessions SET status='running' WHERE uuid=$1"},
		{name: "dwell", sessionSQL: "UPDATE sessions SET archived_at=now() WHERE uuid=$1"},
		{name: "live lease", workerSQL: "UPDATE code_sessions SET worker_lease_expires_at=now()+interval '1 hour' WHERE uuid=$1"},
		{name: "running worker", workerSQL: "UPDATE code_sessions SET worker_status='running' WHERE uuid=$1"},
		{name: "active sibling", sibling: true},
		{name: "archived null lease", eligible: true},
		{name: "archived expired lease", workerSQL: "UPDATE code_sessions SET worker_lease_expires_at=now()-interval '1 hour' WHERE uuid=$1", eligible: true},
		{name: "deleted", sessionSQL: "UPDATE sessions SET deleted_at=now()-interval '2 days' WHERE uuid=$1", eligible: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			scope := transcriptTestScope()
			// Distinct tenants ensure global sweep returns the exact scope metadata.
			if scenario.name == "deleted" {
				scope.OrganizationUUID = uuid.NewV4().String()
				scope.WorkspaceUUID = uuid.NewV4().String()
			}
			sessionUUID := seedTranscriptArchiveSession(t, database, scope)
			seedTranscriptArchiveEvent(t, store, scope, 1, nil, false, time.Now().Add(-8*24*time.Hour))
			sql := scenario.sessionSQL
			if sql == "" {
				sql = "UPDATE sessions SET archived_at=now()-interval '2 days' WHERE uuid=$1"
			}
			if _, err := database.ExecContext(t.Context(), sql, sessionUUID); err != nil {
				t.Fatal(err)
			}
			if scenario.workerSQL != "" {
				if _, err := database.ExecContext(t.Context(), scenario.workerSQL, scope.CodeSessionUUID); err != nil {
					t.Fatal(err)
				}
			}
			if scenario.sibling {
				sibling := transcriptTestScope()
				seedTranscriptArchiveSession(t, database, sibling)
				if _, err := database.ExecContext(t.Context(), "UPDATE code_sessions SET session_uuid=$1,worker_status='running' WHERE uuid=$2", sessionUUID, sibling.CodeSessionUUID); err != nil {
					t.Fatal(err)
				}
			}
			events, err := store.ListArchivableInternalEvents(t.Context(), TranscriptArchiveQuery{Scope: scope, Terminal: true, Cutoff: cutoff, Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			expected := 0
			if scenario.eligible {
				expected = 1
				want = append(want, scope)
			}
			if len(events) != expected {
				t.Fatalf("terminal rows: %d, want %d", len(events), expected)
			}
			if len(events) > 0 && (events[0].SequenceNum != 1 || events[0].CodeSessionUUID != scope.CodeSessionUUID) {
				t.Fatalf("wrong event: %+v", events[0])
			}
		})
	}
	assertTranscriptCandidatePages(t, store, TranscriptArchiveQuery{Terminal: true, Cutoff: cutoff, Limit: 1}, want)
}

func TestTranscriptArchiveBoundaryCandidatesPostgres(t *testing.T) {
	store, database := newTranscriptArchiveTestDB(t)
	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	a, b := "agent_a", "agent_b"
	var want []TranscriptScope
	for i := 0; i < 2; i++ {
		scope := transcriptTestScope()
		if i == 1 {
			scope.WorkspaceUUID = uuid.NewV4().String()
		}
		seedTranscriptArchiveSession(t, database, scope)
		want = append(want, scope)
		for j, event := range []struct {
			agent           *string
			compact, recent bool
		}{
			{nil, false, false}, {&a, false, false}, {&b, false, false},
			{nil, true, false}, {&a, true, false}, {&b, false, false},
			{nil, false, true}, {nil, true, false}, {&a, false, true}, {&a, true, false},
		} {
			created := old
			if event.recent {
				created = time.Now().UTC()
			}
			seedTranscriptArchiveEvent(t, store, scope, int64(j+1), event.agent, event.compact, created)
		}
		query := TranscriptArchiveQuery{Scope: scope, Cutoff: cutoff, Limit: 2}
		var sequences []int64
		for {
			events, err := store.ListArchivableInternalEvents(t.Context(), query)
			if err != nil {
				t.Fatal(err)
			}
			if len(events) == 0 {
				break
			}
			for _, event := range events {
				if event.OrganizationUUID != scope.OrganizationUUID || event.WorkspaceUUID != scope.WorkspaceUUID || event.CodeSessionUUID != scope.CodeSessionUUID {
					t.Fatal("event tenant mismatch")
				}
				sequences = append(sequences, event.SequenceNum)
			}
			next := events[len(events)-1].SequenceNum
			if next <= query.AfterSequence {
				t.Fatal("event cursor did not advance")
			}
			query.AfterSequence = next
		}
		if !reflect.DeepEqual(sequences, []int64{1, 2, 4, 5}) {
			t.Fatalf("boundary rows: %v", sequences)
		}
		// Range reads include boundary and non-compacted scopes, with exact bounds.
		query.AfterSequence = 2
		query.ToSequence = 5
		query.Limit = 10
		rows, err := store.ReadTranscriptArchiveRange(t.Context(), query)
		if err != nil || len(rows) != 3 || rows[0].SequenceNum != 3 || rows[2].SequenceNum != 5 {
			t.Fatalf("range rows: %+v %v", rows, err)
		}
		if rows[0].AgentID == nil || *rows[0].AgentID != b || rows[1].AgentID != nil {
			t.Fatal("nullable agent scan changed")
		}
		for _, foreignOrg := range []bool{false, true} {
			foreign := scope
			if foreignOrg {
				foreign.OrganizationUUID = uuid.NewV4().String()
			} else {
				foreign.WorkspaceUUID = uuid.NewV4().String()
			}
			query.Scope = foreign
			query.AfterSequence = 0
			events, err := store.ListArchivableInternalEvents(t.Context(), query)
			if err != nil || len(events) != 0 {
				t.Fatalf("foreign events: %d %v", len(events), err)
			}
			rows, err := store.ReadTranscriptArchiveRange(t.Context(), query)
			if err != nil || len(rows) != 0 {
				t.Fatalf("foreign range: %d %v", len(rows), err)
			}
		}
	}
	// A scope without compaction must not become a sweep candidate.
	untouched := transcriptTestScope()
	seedTranscriptArchiveSession(t, database, untouched)
	seedTranscriptArchiveEvent(t, store, untouched, 1, nil, false, old)
	assertTranscriptCandidatePages(t, store, TranscriptArchiveQuery{Cutoff: cutoff, Limit: 1}, want)
}

func assertTranscriptCandidatePages(t *testing.T, store *DB, query TranscriptArchiveQuery, want []TranscriptScope) {
	t.Helper()
	slices.SortFunc(want, func(a, b TranscriptScope) int {
		if a.CodeSessionUUID < b.CodeSessionUUID {
			return -1
		}
		if a.CodeSessionUUID > b.CodeSessionUUID {
			return 1
		}
		return 0
	})
	var got []TranscriptScope
	for {
		page, err := store.ListArchivableTranscriptSessions(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) > query.Limit {
			t.Fatal("candidate page exceeded limit")
		}
		got = append(got, page...)
		next := page[len(page)-1].CodeSessionUUID
		if next <= query.AfterUUID {
			t.Fatal("candidate cursor did not advance")
		}
		query.AfterUUID = next
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate scopes: %+v, want %+v", got, want)
	}
}

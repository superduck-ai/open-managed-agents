package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var segmentBase = time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

type fakeStore struct {
	sessions     map[string]bool
	sessionErr   error
	agentErr     error
	threads      map[string]db.SessionThread
	threadErr    error
	codeSessions []db.CodeSessionSegment
	codeErr      error
	summaries    []db.SessionTrajectorySummary
	summaryErr   error
	summaryIDs   []string
	threadNames  map[string]string
	namesErr     error
}

func (s *fakeStore) ListSubagentThreadNames(context.Context, string, []string) (map[string]string, error) {
	return s.threadNames, s.namesErr
}

func (s *fakeStore) GetSession(_ context.Context, workspaceUUID, sessionID string) (db.Session, bool, error) {
	if s.sessionErr != nil {
		return db.Session{}, false, s.sessionErr
	}
	return db.Session{ExternalID: sessionID, WorkspaceUUID: workspaceUUID}, s.sessions[sessionID], nil
}

func (s *fakeStore) GetAgent(context.Context, string, string) (db.Agent, error) {
	return db.Agent{}, s.agentErr
}

func (s *fakeStore) GetSessionThread(_ context.Context, _, _, threadID string) (db.SessionThread, error) {
	if s.threadErr != nil {
		return db.SessionThread{}, s.threadErr
	}
	thread, found := s.threads[threadID]
	if !found {
		return db.SessionThread{}, db.ErrNotFound
	}
	return thread, nil
}

func (s *fakeStore) ListCodeSessionSegments(context.Context, string, string) ([]db.CodeSessionSegment, error) {
	return s.codeSessions, s.codeErr
}

func (s *fakeStore) ListSessionTrajectorySummaries(_ context.Context, _ string, ids []string) ([]db.SessionTrajectorySummary, error) {
	s.summaryIDs = ids
	return s.summaries, s.summaryErr
}

type fakeBackend struct {
	activity ActivityList
	rows     map[string][]Row
	queries  []string
	request  DashboardRequest
	query    ListQuery
	scope    Scope
	id       string
	after    int64
	calls    int
}

func (b *fakeBackend) List(_ context.Context, scope Scope, _ Window, query ListQuery) (ActivityList, error) {
	b.scope = scope
	b.query = query
	b.calls++
	return b.activity, nil
}

func (b *fakeBackend) Dashboard(_ context.Context, scope Scope, _ Window, queries []string, request DashboardRequest) (map[string][]Row, error) {
	b.scope = scope
	b.queries = queries
	b.request = request
	return b.rows, nil
}

func (b *fakeBackend) Page(_ context.Context, scope Scope, _ Window, id string, after int64) (Page, error) {
	b.scope = scope
	b.id = id
	b.after = after
	b.calls++
	return Page{NextSeq: after}, nil
}

func trajectoryStore() *fakeStore {
	parent := "primary-uuid"
	return &fakeStore{
		sessions: map[string]bool{"sesn_live": true},
		threads: map[string]db.SessionThread{
			"sthr_primary": {ExternalID: "sthr_primary"},
			"sthr_child":   {ExternalID: "sthr_child", ParentThreadUUID: &parent, CreatedAt: segmentBase.Add(time.Minute)},
		},
		codeSessions: []db.CodeSessionSegment{{ExternalID: "cse_old", CreatedAt: segmentBase}, {ExternalID: "cse_new", CreatedAt: segmentBase.Add(time.Hour)}},
	}
}

var trajectoryScope = Scope{OrganizationUUID: "org", WorkspaceUUID: "ws", AgentID: "agent_ignored"}

func requireKind(t *testing.T, err error, kind apperr.Kind) {
	t.Helper()
	var appError *apperr.Error
	if !errors.As(err, &appError) || appError.Kind != kind {
		t.Fatalf("error = %v, want %v", err, kind)
	}
}

func TestTrajectoryPageRejectsInvisibleTargets(t *testing.T) {
	storeFailure := errors.New("database down")
	for _, test := range []struct {
		name   string
		mutate func(*fakeStore)
		scope  Scope
		target Target
		kind   apperr.Kind
	}{
		{"missing session id", nil, trajectoryScope, Target{}, apperr.InvalidArgument},
		{"missing workspace", nil, Scope{OrganizationUUID: "org"}, Target{SessionID: "sesn_live"}, apperr.NotFound},
		{"deleted or unknown session", nil, trajectoryScope, Target{SessionID: "sesn_deleted"}, apperr.NotFound},
		{"thread outside session", nil, trajectoryScope, Target{SessionID: "sesn_live", ThreadID: "sthr_foreign"}, apperr.NotFound},
		{"segment outside lane", nil, trajectoryScope, Target{SessionID: "sesn_live", Segment: "cse_foreign"}, apperr.NotFound},
		{"child lane rejects code session segment", nil, trajectoryScope, Target{SessionID: "sesn_live", ThreadID: "sthr_child", Segment: "cse_new"}, apperr.NotFound},
		{"segment on empty lane", func(s *fakeStore) { s.codeSessions = nil }, trajectoryScope, Target{SessionID: "sesn_live", Segment: "cse_new"}, apperr.NotFound},
		{"session lookup failure", func(s *fakeStore) { s.sessionErr = storeFailure }, trajectoryScope, Target{SessionID: "sesn_live"}, apperr.Unavailable},
		{"thread lookup failure", func(s *fakeStore) { s.threadErr = storeFailure }, trajectoryScope, Target{SessionID: "sesn_live", ThreadID: "sthr_child"}, apperr.Unavailable},
		{"segment lookup failure", func(s *fakeStore) { s.codeErr = storeFailure }, trajectoryScope, Target{SessionID: "sesn_live"}, apperr.Unavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := trajectoryStore()
			if test.mutate != nil {
				test.mutate(store)
			}
			backend := &fakeBackend{}
			_, err := New(backend, store).Page(context.Background(), test.scope, Window{}, test.target, -1)
			requireKind(t, err, test.kind)
			if backend.calls != 0 {
				t.Fatal("queried storage for an invisible target")
			}
		})
	}
}

func TestTrajectoryPageResolvesLanesToNativeSessions(t *testing.T) {
	for _, test := range []struct {
		name     string
		target   Target
		native   string
		segments int
	}{
		{"main lane defaults to latest code session", Target{SessionID: "sesn_live"}, "cse_new", 2},
		{"primary thread id is the main lane", Target{SessionID: "sesn_live", ThreadID: "sthr_primary"}, "cse_new", 2},
		{"main lane selects an earlier segment", Target{SessionID: "sesn_live", Segment: "cse_old"}, "cse_old", 2},
		{"child lane uses the thread session", Target{SessionID: "sesn_live", ThreadID: "sthr_child"}, "sthr_child", 1},
		{"child lane accepts its own segment", Target{SessionID: "sesn_live", ThreadID: "sthr_child", Segment: "sthr_child"}, "sthr_child", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := &fakeBackend{}
			page, err := New(backend, trajectoryStore()).Page(context.Background(), trajectoryScope, Window{}, test.target, 41)
			if err != nil {
				t.Fatal(err)
			}
			if backend.id != test.native || page.Segment != test.native || len(page.Segments) != test.segments || backend.after != 41 {
				t.Fatalf("page = %+v, backend = %+v", page, backend)
			}
			if backend.scope.SessionID != "sesn_live" || backend.scope.AgentID != "" || backend.scope.WorkspaceUUID != "ws" {
				t.Fatalf("storage scope = %+v", backend.scope)
			}
		})
	}
	store := trajectoryStore()
	store.codeSessions = nil
	backend := &fakeBackend{}
	page, err := New(backend, store).Page(context.Background(), trajectoryScope, Window{}, Target{SessionID: "sesn_live"}, 3)
	if err != nil || backend.calls != 0 || page.NextSeq != 3 || page.Events == nil || page.Segments == nil || len(page.Segments) != 0 {
		t.Fatalf("session without runtime: %+v %v", page, err)
	}
}

func TestTrajectoryListGroupsBySessionAndHidesDeletedSessions(t *testing.T) {
	t.Run("agent lookup failures", func(t *testing.T) {
		for _, test := range []struct {
			err  error
			kind apperr.Kind
		}{{db.ErrNotFound, apperr.NotFound}, {errors.New("down"), apperr.Unavailable}} {
			store := trajectoryStore()
			store.agentErr = test.err
			backend := &fakeBackend{}
			_, err := New(backend, store).List(context.Background(), Scope{OrganizationUUID: "org", WorkspaceUUID: "ws", AgentID: "agent_a"}, Window{}, ListQuery{})
			requireKind(t, err, test.kind)
			if backend.calls != 0 {
				t.Fatal("queried storage for an invisible agent")
			}
		}
	})
	t.Run("unknown session filter", func(t *testing.T) {
		backend := &fakeBackend{}
		_, err := New(backend, trajectoryStore()).List(context.Background(), Scope{OrganizationUUID: "org", WorkspaceUUID: "ws", SessionID: "sesn_missing"}, Window{}, ListQuery{})
		requireKind(t, err, apperr.NotFound)
		if backend.calls != 0 {
			t.Fatal("unknown session reached storage")
		}
	})
	t.Run("summary failure", func(t *testing.T) {
		store := trajectoryStore()
		store.summaryErr = errors.New("down")
		backend := &fakeBackend{activity: ActivityList{Items: []SessionActivity{{SessionID: "sesn_live"}}}}
		_, err := New(backend, store).List(context.Background(), trajectoryScope, Window{}, ListQuery{})
		requireKind(t, err, apperr.Unavailable)
	})
	t.Run("enriches visible sessions in activity order", func(t *testing.T) {
		title := "Live"
		store := trajectoryStore()
		store.summaries = []db.SessionTrajectorySummary{
			{ExternalID: "sesn_b", AgentExternalID: "agent_b", AgentVersion: 1, Status: "running"},
			{ExternalID: "sesn_a", AgentExternalID: "agent_a", AgentVersion: 4, Title: &title, Status: "idle", SubagentCount: 2},
		}
		next := 50
		backend := &fakeBackend{activity: ActivityList{Items: []SessionActivity{
			{SessionID: "sesn_a", StartedAt: 1, LastEventAt: 9},
			{SessionID: "sesn_deleted", StartedAt: 2, LastEventAt: 8},
			{SessionID: "sesn_b", StartedAt: 3, LastEventAt: 7},
		}, NextOffset: &next}}
		list, err := New(backend, store).List(context.Background(), Scope{OrganizationUUID: "org", WorkspaceUUID: "ws", SessionID: "sesn_live"}, Window{}, ListQuery{Sort: SortErrors})
		if err != nil {
			t.Fatal(err)
		}
		if backend.scope.SessionID != "sesn_live" || len(store.summaryIDs) != 3 {
			t.Fatalf("list scope = %+v ids = %v", backend.scope, store.summaryIDs)
		}
		if len(list.Items) != 2 || list.NextOffset == nil || *list.NextOffset != 50 {
			t.Fatalf("list = %+v", list)
		}
		first, second := list.Items[0], list.Items[1]
		if first.SessionID != "sesn_a" || first.Title == nil || *first.Title != "Live" || first.AgentVersion != 4 || first.SubagentCount != 2 || first.LastEventAt != 9 {
			t.Fatalf("first = %+v", first)
		}
		if second.SessionID != "sesn_b" || second.Status != "running" || second.StartedAt != 3 {
			t.Fatalf("second = %+v", second)
		}
	})
	t.Run("empty activity", func(t *testing.T) {
		list, err := New(&fakeBackend{}, trajectoryStore()).List(context.Background(), trajectoryScope, Window{}, ListQuery{})
		if err != nil || list.Items == nil || len(list.Items) != 0 || list.NextOffset != nil {
			t.Fatalf("list = %+v, error = %v", list, err)
		}
	})
}

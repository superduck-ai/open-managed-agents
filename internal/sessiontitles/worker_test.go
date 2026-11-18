package sessiontitles

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type fakeStore struct {
	session   db.Session
	completed bool
	title     string
}

func (s *fakeStore) LeaseSessionTitleJobs(context.Context, string, int) ([]db.SessionTitleJob, error) {
	return nil, nil
}
func (s *fakeStore) GetSession(context.Context, string, string) (db.Session, bool, error) {
	return s.session, true, nil
}
func (s *fakeStore) CompleteSessionTitleJob(_ context.Context, _ db.SessionTitleJob, _, title string) error {
	s.completed, s.title = true, title
	return nil
}

type fakeGenerator struct {
	calls  int
	err    error
	cancel bool
}

func (m *fakeGenerator) Generate(ctx context.Context, _ db.Session, _ string) (string, error) {
	m.calls++
	if m.cancel {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return "Generated title", m.err
}

func TestWorkerTitlePolicy(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(*db.Session)
		modelErr  error
		wantTitle string
		wantCalls int
	}{
		{"manual null", func(s *db.Session) { s.TitleManual = true }, nil, "", 0},
		{"existing title", func(s *db.Session) { s.Title = new("User title") }, nil, "", 0},
		{"archived", func(s *db.Session) { s.ArchivedAt = new(time.Now()) }, nil, "", 0},
		{"deleted", func(s *db.Session) { s.DeletedAt = new(time.Now()) }, nil, "", 0},
		{"terminated", func(s *db.Session) { s.Status = "terminated" }, nil, "", 0},
		{"wrong session", func(s *db.Session) { s.UUID = "other" }, nil, "", 0},
		{"model failure fallback", func(*db.Session) {}, errors.New("failure"), "Fix the session title", 1},
		{"budget reached fallback", func(s *db.Session) { s.BudgetReachedAt = new(time.Now()) }, nil, "Fix the session title", 0},
		{"success", func(*db.Session) {}, nil, "Generated title", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &fakeStore{session: db.Session{UUID: "session", Status: "running"}}
			tc.mutate(&s.session)
			m := &fakeGenerator{err: tc.modelErr}
			w := &Worker{store: s, model: m, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			if err := w.process(t.Context(), db.SessionTitleJob{SessionUUID: "session", Input: "Fix the session title"}, "worker"); err != nil {
				t.Fatal(err)
			}
			if !s.completed || s.title != tc.wantTitle || m.calls != tc.wantCalls {
				t.Fatalf("completed=%t title=%q calls=%d", s.completed, s.title, m.calls)
			}
		})
	}
}

func TestWorkerCancellationLeavesJobRecoverable(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s := &fakeStore{session: db.Session{UUID: "session", Status: "running"}}
	w := &Worker{store: s, model: &fakeGenerator{cancel: true}}
	if err := w.process(ctx, db.SessionTitleJob{SessionUUID: "session", Input: "first input"}, "worker"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if s.completed {
		t.Fatal("cancelled attempt completed job")
	}
}

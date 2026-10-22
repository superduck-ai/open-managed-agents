package observability

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type Backend interface {
	List(context.Context, Scope, Window, int) (List, error)
	Page(context.Context, Scope, Window, string, int64) (Page, error)
}

type Service struct {
	backend Backend
	db      *db.DB
}

func New(backend Backend, database *db.DB) *Service {
	return &Service{backend: backend, db: database}
}

func (s *Service) authorize(ctx context.Context, scope Scope) error {
	if scope.OrganizationUUID == "" || scope.WorkspaceUUID == "" {
		return notFound()
	}
	if scope.SessionID != "" {
		_, found, err := s.db.GetSession(ctx, scope.WorkspaceUUID, scope.SessionID)
		if err != nil {
			return unavailable(err)
		}
		if !found {
			return notFound()
		}
	}
	if scope.AgentID != "" {
		if _, err := s.db.GetAgent(ctx, scope.WorkspaceUUID, scope.AgentID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return notFound()
			}
			return unavailable(err)
		}
	}
	return nil
}

func (s *Service) List(ctx context.Context, scope Scope, window Window, offset int) (List, error) {
	if err := s.authorize(ctx, scope); err != nil {
		return List{}, err
	}
	return s.backend.List(ctx, scope, window, offset)
}

func (s *Service) Page(ctx context.Context, scope Scope, window Window, id string, after int64) (Page, error) {
	if id == "" {
		return Page{}, MissingTrajectoryID()
	}
	if err := s.authorize(ctx, scope); err != nil {
		return Page{}, err
	}
	return s.backend.Page(ctx, scope, window, id, after)
}

func ParseWindow(start, end string) (Window, error) {
	now := time.Now().UTC()
	w := Window{Start: now.AddDate(0, -1, 0), End: now.Add(time.Hour)}
	for _, field := range []struct {
		raw    string
		target *time.Time
	}{{start, &w.Start}, {end, &w.End}} {
		raw := strings.TrimSpace(field.raw)
		if raw == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return Window{}, InvalidTimeRange()
		}
		*field.target = parsed
	}
	if !w.End.After(w.Start) || w.End.Sub(w.Start) > 366*24*time.Hour {
		return Window{}, InvalidTimeWindow()
	}
	return w, nil
}

func ParseCursor(raw string) (int64, error) {
	if raw == "" {
		return -1, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < -1 {
		return 0, InvalidCursor()
	}
	return value, nil
}

func ParseOffset(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value < 0 || value > 100000 {
		return 0, InvalidOffset()
	}
	return int(value), nil
}

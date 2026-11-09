package observability

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	SortRecent   = "recent"
	SortDuration = "duration"
	SortErrors   = "errors"
)

type Backend interface {
	List(context.Context, Scope, Window, ListQuery) (ActivityList, error)
	Page(context.Context, Scope, Window, string, int64) (Page, error)
	Dashboard(context.Context, Scope, Window, []string, DashboardRequest) (map[string][]Row, error)
}

type Store interface {
	GetSession(context.Context, string, string) (db.Session, bool, error)
	GetAgent(context.Context, string, string) (db.Agent, error)
	GetSessionThread(context.Context, string, string, string) (db.SessionThread, error)
	ListCodeSessionSegments(context.Context, string, string) ([]db.CodeSessionSegment, error)
	ListSessionTrajectorySummaries(context.Context, string, []string) ([]db.SessionTrajectorySummary, error)
	ListSubagentThreadNames(context.Context, string, []string) (map[string]string, error)
}

type Service struct {
	backend Backend
	store   Store
}

func New(backend Backend, store Store) *Service {
	return &Service{backend: backend, store: store}
}

func (s *Service) authorize(ctx context.Context, scope Scope) error {
	if scope.OrganizationUUID == "" || scope.WorkspaceUUID == "" {
		return notFound()
	}
	if scope.AgentID != "" {
		if _, err := s.store.GetAgent(ctx, scope.WorkspaceUUID, scope.AgentID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return notFound()
			}
			return unavailable(err)
		}
	}
	return nil
}

func (s *Service) authorizeFilters(ctx context.Context, scope Scope) error {
	if err := s.authorize(ctx, scope); err != nil || scope.SessionID == "" {
		return err
	}
	if _, found, err := s.store.GetSession(ctx, scope.WorkspaceUUID, scope.SessionID); err != nil {
		return unavailable(err)
	} else if !found {
		return notFound()
	}
	return nil
}

func (s *Service) visibleSessions(ctx context.Context, workspaceUUID string, ids []string) (map[string]db.SessionTrajectorySummary, error) {
	summaries, err := s.store.ListSessionTrajectorySummaries(ctx, workspaceUUID, slices.Compact(slices.Sorted(slices.Values(ids))))
	if err != nil {
		return nil, unavailable(err)
	}
	byID := make(map[string]db.SessionTrajectorySummary, len(summaries))
	for _, summary := range summaries {
		byID[summary.ExternalID] = summary
	}
	return byID, nil
}

func (s *Service) List(ctx context.Context, scope Scope, window Window, query ListQuery) (List, error) {
	if query.Sort == "" {
		query.Sort = SortRecent
	}
	if _, found := listOrders[query.Sort]; !found {
		return List{}, InvalidSort()
	}
	if err := s.authorizeFilters(ctx, scope); err != nil {
		return List{}, err
	}
	activity, err := s.backend.List(ctx, scope, window, query)
	if err != nil {
		return List{}, err
	}
	ids := make([]string, 0, len(activity.Items))
	for _, item := range activity.Items {
		ids = append(ids, item.SessionID)
	}
	byID, err := s.visibleSessions(ctx, scope.WorkspaceUUID, ids)
	if err != nil {
		return List{}, err
	}
	list := List{Items: make([]SessionTrajectory, 0, len(activity.Items)), NextOffset: activity.NextOffset}
	for _, item := range activity.Items {
		summary, found := byID[item.SessionID]
		if !found {
			continue
		}
		list.Items = append(list.Items, SessionTrajectory{
			SessionID:     item.SessionID,
			Title:         summary.Title,
			AgentID:       summary.AgentExternalID,
			AgentVersion:  summary.AgentVersion,
			Status:        summary.Status,
			SubagentCount: summary.SubagentCount,
			StartedAt:     item.StartedAt,
			LastEventAt:   item.LastEventAt,
			DurationMs:    item.LastEventAt - item.StartedAt,
			ErrorTurns:    item.ErrorTurns,
		})
	}
	return list, nil
}

func (s *Service) Page(ctx context.Context, scope Scope, window Window, target Target, after int64) (Page, error) {
	if target.SessionID == "" {
		return Page{}, MissingTrajectoryID()
	}
	scope.SessionID = target.SessionID
	scope.AgentID = ""
	if err := s.authorize(ctx, scope); err != nil {
		return Page{}, err
	}
	segments, err := s.segments(ctx, scope.WorkspaceUUID, target)
	if err != nil {
		return Page{}, err
	}
	if len(segments) == 0 {
		if target.Segment != "" {
			return Page{}, notFound()
		}
		return Page{Events: []json.RawMessage{}, NextSeq: after, Segments: segments}, nil
	}
	selected := segments[len(segments)-1].ID
	if target.Segment != "" {
		if !slices.ContainsFunc(segments, func(segment Segment) bool { return segment.ID == target.Segment }) {
			return Page{}, notFound()
		}
		selected = target.Segment
	}
	page, err := s.backend.Page(ctx, scope, window, selected, after)
	if err != nil {
		return Page{}, err
	}
	page.Segment = selected
	page.Segments = segments
	return page, nil
}

func (s *Service) segments(ctx context.Context, workspaceUUID string, target Target) ([]Segment, error) {
	if _, found, err := s.store.GetSession(ctx, workspaceUUID, target.SessionID); err != nil {
		return nil, unavailable(err)
	} else if !found {
		return nil, notFound()
	}
	if target.ThreadID != "" {
		thread, err := s.store.GetSessionThread(ctx, workspaceUUID, target.SessionID, target.ThreadID)
		if errors.Is(err, db.ErrNotFound) {
			return nil, notFound()
		}
		if err != nil {
			return nil, unavailable(err)
		}
		if thread.ParentThreadUUID != nil {
			return []Segment{{ID: thread.ExternalID, StartedAt: thread.CreatedAt}}, nil
		}
	}
	codeSessions, err := s.store.ListCodeSessionSegments(ctx, workspaceUUID, target.SessionID)
	if err != nil {
		return nil, unavailable(err)
	}
	segments := make([]Segment, 0, len(codeSessions))
	for _, codeSession := range codeSessions {
		segments = append(segments, Segment{ID: codeSession.ExternalID, StartedAt: codeSession.CreatedAt})
	}
	return segments, nil
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

func ParseAgentVersions(raw []string) ([]int, error) {
	versions := make([]int, 0, len(raw))
	for _, value := range raw {
		version, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || version <= 0 {
			return nil, InvalidAgentVersion()
		}
		versions = append(versions, version)
	}
	return slices.Compact(slices.Sorted(slices.Values(versions))), nil
}

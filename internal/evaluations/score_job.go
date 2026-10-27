package evaluations

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/riverjobs"
)

const (
	ScoreQueue            = "evaluation_score"
	scoreSweepID          = "evaluation_score_sweep"
	scoreSweepCron        = "* * * * *"
	scoreJobTimeout       = 20 * time.Minute
	evaluatorScoreTimeout = 45 * time.Second
	scoreWriteTimeout     = 5 * time.Second
	scoreEnqueueTimeout   = 2 * time.Second
	scoreRescueLimit      = 50
	scoreFileReadAttempts = 4
	scoreFileReadDelay    = 10 * time.Second
)

// ScoreEnqueuer schedules scoring off the session-event request.
type ScoreEnqueuer interface {
	EnqueueItem(ctx context.Context, organizationUUID, workspaceUUID, experimentID, itemID, sessionID string) error
	EnqueueScoreFile(ctx context.Context, organizationUUID, workspaceUUID, sessionID string, attempt int) error
}

// ScoreJobs runs evaluation scoring on the shared River client.
type ScoreJobs struct {
	mu      sync.RWMutex
	handler *Handler
	client  *river.Client[*sql.Tx]
}

func NewScoreJobs() *ScoreJobs {
	return &ScoreJobs{}
}

func (s *ScoreJobs) Bind(handler *Handler) {
	s.mu.Lock()
	s.handler = handler
	s.mu.Unlock()
}

func (s *ScoreJobs) Register(workers *river.Workers) {
	river.AddWorker(workers, &scoreItemWorker{jobs: s})
	river.AddWorker(workers, &scoreFileWorker{jobs: s})
	river.AddWorker(workers, &scoreSweepWorker{jobs: s})
}

func (s *ScoreJobs) Configure(ctx context.Context, client *river.Client[*sql.Tx]) error {
	s.mu.Lock()
	s.client = client
	s.mu.Unlock()
	existing, err := client.DurablePeriodicJobGet(ctx, scoreSweepID)
	if err != nil && !errors.Is(err, river.ErrNotFound) {
		return err
	}
	if err == nil && riverjobs.MatchesActiveCron(existing, scoreSweepID, ScoreQueue, scoreSweepCron, "UTC") {
		return nil
	}
	_, err = client.DurablePeriodicJobUpsert(ctx, &river.DurablePeriodicJobUpsertOpts{
		ID: scoreSweepID, Kind: scoreSweepID, Queue: ScoreQueue,
		Schedule: &river.DurablePeriodicJobSchedule{CronExpression: scoreSweepCron, CronTimezone: "UTC"},
	})
	return err
}

func (s *ScoreJobs) Kick(ctx context.Context) error {
	client := s.riverClient()
	if client == nil {
		return errors.New("evaluation score jobs are not configured")
	}
	_, err := client.Insert(ctx, scoreSweepArgs{}, nil)
	return err
}

func (s *ScoreJobs) EnqueueItem(ctx context.Context, organizationUUID, workspaceUUID, experimentID, itemID, sessionID string) error {
	client := s.riverClient()
	if client == nil {
		return errors.New("evaluation score jobs are not configured")
	}
	_, err := client.Insert(ctx, scoreItemArgs{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUID,
		ExperimentID:     experimentID,
		ItemID:           itemID,
		SessionID:        sessionID,
	}, nil)
	return err
}

func (s *ScoreJobs) EnqueueScoreFile(ctx context.Context, organizationUUID, workspaceUUID, sessionID string, attempt int) error {
	client := s.riverClient()
	if client == nil {
		return errors.New("evaluation score jobs are not configured")
	}
	if attempt < 1 {
		attempt = 1
	}
	_, err := client.Insert(ctx, scoreFileArgs{
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUID,
		SessionID:        sessionID,
		Attempt:          attempt,
	}, &river.InsertOpts{
		Queue:       ScoreQueue,
		MaxAttempts: 3,
		ScheduledAt: time.Now().Add(scoreFileReadDelay * time.Duration(attempt)),
		UniqueOpts:  scoreJobUniqueOpts(),
	})
	return err
}

func (s *ScoreJobs) riverClient() *river.Client[*sql.Tx] {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}

func (s *ScoreJobs) boundHandler() *Handler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.handler
}

type scoreItemArgs struct {
	OrganizationUUID string `json:"organization_uuid"`
	WorkspaceUUID    string `json:"workspace_uuid"`
	ExperimentID     string `json:"experiment_id"`
	ItemID           string `json:"item_id"`
	SessionID        string `json:"session_id"`
}

func (scoreItemArgs) Kind() string { return "evaluation_score_item" }

func (scoreItemArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: ScoreQueue, MaxAttempts: 3, UniqueOpts: scoreJobUniqueOpts()}
}

type scoreFileArgs struct {
	OrganizationUUID string `json:"organization_uuid"`
	WorkspaceUUID    string `json:"workspace_uuid"`
	SessionID        string `json:"session_id"`
	Attempt          int    `json:"attempt"`
}

func (scoreFileArgs) Kind() string { return "evaluation_score_file" }

func (scoreFileArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: ScoreQueue, MaxAttempts: 3, UniqueOpts: scoreJobUniqueOpts()}
}

func scoreJobUniqueOpts() river.UniqueOpts {
	return river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
		rivertype.JobStateAvailable,
		rivertype.JobStatePending,
		rivertype.JobStateRunning,
		rivertype.JobStateRetryable,
		rivertype.JobStateScheduled,
	}}
}

type scoreSweepArgs struct{}

func (scoreSweepArgs) Kind() string { return scoreSweepID }

func (scoreSweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: ScoreQueue, UniqueOpts: scoreJobUniqueOpts()}
}

type scoreItemWorker struct {
	river.WorkerDefaults[scoreItemArgs]
	jobs *ScoreJobs
}

func (w *scoreItemWorker) Work(ctx context.Context, job *river.Job[scoreItemArgs]) error {
	handler := w.jobs.boundHandler()
	if handler == nil {
		return nil
	}
	return handler.scoreQueuedItem(ctx, job.Args)
}

func (*scoreItemWorker) Timeout(*river.Job[scoreItemArgs]) time.Duration { return scoreJobTimeout }

type scoreFileWorker struct {
	river.WorkerDefaults[scoreFileArgs]
	jobs *ScoreJobs
}

func (w *scoreFileWorker) Work(ctx context.Context, job *river.Job[scoreFileArgs]) error {
	handler := w.jobs.boundHandler()
	if handler == nil {
		return nil
	}
	handler.readDeferredScoreFile(ctx, job.Args)
	return nil
}

func (*scoreFileWorker) Timeout(*river.Job[scoreFileArgs]) time.Duration { return scoreJobTimeout }

type scoreSweepWorker struct {
	river.WorkerDefaults[scoreSweepArgs]
	jobs *ScoreJobs
}

func (w *scoreSweepWorker) Work(ctx context.Context, _ *river.Job[scoreSweepArgs]) error {
	handler := w.jobs.boundHandler()
	if handler == nil {
		return nil
	}
	return handler.rescueUnscoredItems(ctx)
}

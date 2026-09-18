// Package transcriptretention orchestrates archival without participating in resume reads.
package transcriptretention

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/transcriptarchive"
)

// Policy is evaluated by workers at execution time, including already queued jobs.
type Policy struct {
	Enabled               bool          `yaml:"enabled"`
	DryRun                bool          `yaml:"dry_run"`
	TerminalSweepEnabled  bool          `yaml:"terminal_sweep_enabled"`
	BoundarySweepEnabled  bool          `yaml:"boundary_sweep_enabled"`
	HardDeleteEnabled     bool          `yaml:"hard_delete_enabled"`
	TerminalDwell         time.Duration `yaml:"terminal_dwell"`
	ArchiveMinAge         time.Duration `yaml:"archive_min_age"`
	SoftDeleteWindow      time.Duration `yaml:"soft_delete_window"`
	TargetSegmentRawBytes int           `yaml:"target_segment_raw_bytes"`
	DeleteBatchRows       int           `yaml:"delete_batch_rows"`
	MaxRowsPerJob         int           `yaml:"max_rows_per_job"`
}

type Service struct {
	database *db.DB
	objects  storage.ObjectStore
	payloads *eventpayload.Store
	policy   Policy
	logger   *slog.Logger
}

func New(database *db.DB, objects storage.ObjectStore, policy Policy, logger *slog.Logger) *Service {
	return &Service{database: database, objects: objects, payloads: eventpayload.New(database, objects), policy: policy, logger: logging.LoggerOrDefault(logger)}
}

func (s *Service) query(scope db.TranscriptScope, terminal bool) db.TranscriptArchiveQuery {
	age := s.policy.ArchiveMinAge
	if terminal {
		age = s.policy.TerminalDwell
	}
	return db.TranscriptArchiveQuery{Scope: scope, Terminal: terminal, Cutoff: time.Now().UTC().Add(-age), Limit: 32}
}

func (s *Service) Archive(ctx context.Context, scope db.TranscriptScope, terminal bool) error {
	if !terminal {
		return nil
	}
	if !s.policy.Enabled || (terminal && !s.policy.TerminalSweepEnabled) || (!terminal && !s.policy.BoundarySweepEnabled) {
		return nil
	}
	query := s.query(scope, terminal)
	remaining := s.policy.MaxRowsPerJob
	if !s.policy.DryRun {
		used, err := s.finishRegistered(ctx, query, remaining)
		if err != nil {
			return err
		}
		remaining -= used
	}
	return s.archiveNew(ctx, query, remaining)
}

func (s *Service) restorePayload(ctx context.Context, event db.CodeSessionInternalEvent) (db.CodeSessionInternalEvent, error) {
	restored, err := s.payloads.RestoreInternal(ctx, event)
	return restored, err
}

func (s *Service) createSegment(ctx context.Context, scope db.TranscriptScope, events []db.CodeSessionInternalEvent) (db.TranscriptArchive, error) {
	encoded, err := transcriptarchive.Encode(events)
	if err != nil {
		return db.TranscriptArchive{}, err
	}
	if s.objects == nil {
		return db.TranscriptArchive{}, errStorage
	}
	segmentUUID := uuid.NewV4().String()
	a := db.TranscriptArchive{UUID: segmentUUID, ExternalID: "tarc_" + segmentUUID, OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, CodeSessionUUID: scope.CodeSessionUUID, CodeSessionExternalID: scope.CodeSessionExternalID, FromSequence: events[0].SequenceNum, ToSequence: events[len(events)-1].SequenceNum, EventCount: len(events), Codec: "jsonl+zstd", Bucket: s.objects.Name(), Key: transcriptarchive.ObjectKey(scope, segmentUUID), Size: int64(len(encoded.Body)), RawBytes: encoded.RawBytes, SHA256: encoded.SHA256, State: "pending"}
	if err := s.database.RegisterTranscriptArchive(ctx, a); err != nil {
		return db.TranscriptArchive{}, err
	}
	uploadCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := s.objects.Upload(uploadCtx, a.Key, bytes.NewReader(encoded.Body), storage.UploadOptions{Size: a.Size, ContentType: "application/zstd"})
	if err != nil {
		return a, err
	}
	if result.Size != a.Size {
		return a, errIntegrity
	}
	return a, nil
}

// ReadSegment always verifies the object, including before physical deletion.
func (s *Service) ReadSegment(ctx context.Context, a db.TranscriptArchive) (map[int64]transcriptarchive.DecodedEvent, error) {
	if s.objects == nil || s.objects.Name() != a.Bucket || a.Codec != "jsonl+zstd" {
		return nil, errStorage
	}
	object, err := s.objects.Open(ctx, a.Key, nil)
	if err != nil {
		return nil, err
	}
	defer object.Body.Close()
	if object.Size >= 0 && object.Size != a.Size {
		return nil, errIntegrity
	}
	return transcriptarchive.Decode(object.Body, transcriptarchive.SegmentExpectation{Size: a.Size, RawBytes: a.RawBytes, SHA256: a.SHA256, EventCount: a.EventCount, FromSequence: a.FromSequence, ToSequence: a.ToSequence})
}

func (s *Service) attachVerified(ctx context.Context, a db.TranscriptArchive, events []db.CodeSessionInternalEvent) error {
	decoded, err := s.ReadSegment(ctx, a)
	if err != nil {
		return err
	}
	if len(decoded) != len(events) {
		return errIntegrity
	}
	for _, event := range events {
		restored, err := s.restorePayload(ctx, event)
		if err != nil {
			return err
		}
		archived, ok := decoded[event.SequenceNum]
		if !ok || archived.UUID != event.UUID || !bytes.Equal(archived.Payload, restored.Payload) {
			return errIntegrity
		}
	}
	if err := s.database.AttachTranscriptArchive(ctx, a.Scope(), a.UUID); err != nil {
		existing, found, readErr := s.database.FindTranscriptArchive(ctx, a.Scope(), a.FromSequence)
		if readErr != nil {
			return readErr
		}
		if !found || existing.UUID != a.UUID || existing.State != "attached" {
			return err
		}
	}
	return nil
}

func (s *Service) logSegment(ctx context.Context, scope db.TranscriptScope, count int, rawBytes, size int64) {
	s.logger.InfoContext(ctx, "transcript archive segment", "code_session_id", scope.CodeSessionExternalID, "workspace_id", scope.WorkspaceUUID, "segment_count", 1, "event_count", count, "raw_bytes", rawBytes, "size_bytes", size, "dry_run", s.policy.DryRun)
}

func archiveConflict(err error) bool { return errors.Is(err, db.ErrDuplicate) }

package transcriptretention

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// HardDelete requires fresh object verification and registry coverage in every delete transaction.
func (s *Service) HardDelete(ctx context.Context, scope db.TranscriptScope) error {
	if !s.policy.Enabled || s.policy.DryRun || !s.policy.HardDeleteEnabled {
		return nil
	}
	if err := config.ValidateTranscriptArchive(s.policy); err != nil {
		return err
	}
	unprotected, err := s.database.HasUnprotectedDeletedTranscript(ctx, scope, time.Now().UTC().Add(-s.policy.SoftDeleteWindow))
	if err != nil {
		return err
	}
	if unprotected {
		return db.ErrInvalidState
	}
	remaining := s.policy.MaxRowsPerJob
	after := int64(0)
	for remaining > 0 {
		archives, err := s.database.ListTranscriptArchives(ctx, scope, after, 100, true)
		if err != nil {
			return err
		}
		if len(archives) == 0 {
			return nil
		}
		for _, a := range archives {
			count, err := s.deleteExpiredSegment(ctx, a, remaining)
			if err != nil {
				return err
			}
			remaining -= count
			if remaining == 0 {
				return nil
			}
		}
		after = archives[len(archives)-1].FromSequence
	}
	return nil
}

func (s *Service) deleteExpiredSegment(ctx context.Context, a db.TranscriptArchive, budget int) (int, error) {
	cutoff := time.Now().UTC().Add(-s.policy.SoftDeleteWindow)
	rows, err := s.database.ReadTranscriptArchiveRange(ctx, db.TranscriptArchiveQuery{Scope: a.Scope(), AfterSequence: a.FromSequence - 1, ToSequence: a.ToSequence, Limit: a.EventCount + 1})
	if err != nil {
		return 0, err
	}
	var expired []db.CodeSessionInternalEvent
	for _, row := range rows {
		if row.DeletedAt != nil && row.DeletedAt.Before(cutoff) {
			expired = append(expired, row)
		}
	}
	if len(expired) == 0 {
		return 0, nil
	}
	decoded, err := s.ReadSegment(ctx, a)
	if err != nil {
		return 0, err
	}
	expired = expired[:min(len(expired), budget)]
	sequences := make([]int64, 0, len(expired))
	for _, row := range expired {
		event, ok := decoded[row.SequenceNum]
		if !ok || event.UUID != row.UUID {
			return 0, errIntegrity
		}
		sequences = append(sequences, row.SequenceNum)
	}
	count := 0
	for start := 0; start < len(sequences); start += s.policy.DeleteBatchRows {
		end := min(start+s.policy.DeleteBatchRows, len(sequences))
		deleted, err := s.database.HardDeleteInternalEventsBatch(ctx, db.TranscriptDeleteBatch{Scope: a.Scope(), ArchiveUUID: a.UUID, Sequences: sequences[start:end], Cutoff: cutoff})
		if err != nil {
			return count, err
		}
		count += int(deleted)
	}
	return count, nil
}

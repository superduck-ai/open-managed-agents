package transcriptretention

import (
	"context"
	"slices"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/transcriptarchive"
)

// Segments stop at sequence gaps so ranges never claim unarchived interleaved scopes.
func (s *Service) archiveNew(ctx context.Context, query db.TranscriptArchiveQuery, remaining int) error {
	var segment []db.CodeSessionInternalEvent
	rawBytes := 0
	flush := func() error {
		if len(segment) == 0 {
			return nil
		}
		if s.policy.DryRun {
			s.logSegment(ctx, query.Scope, len(segment), int64(rawBytes), 0)
		} else {
			a, err := s.createSegment(ctx, query.Scope, segment)
			if archiveConflict(err) {
				segment = nil
				rawBytes = 0
				return nil
			}
			if err != nil {
				return err
			}
			decoded, err := s.ReadSegment(ctx, a)
			if err != nil {
				return err
			}
			if err := s.attachVerified(ctx, a, segment, decoded); err != nil {
				return err
			}
			if _, err := s.softDeleteSegment(ctx, a, query, segment, decoded); err != nil {
				return err
			}
			s.logSegment(ctx, query.Scope, len(segment), a.RawBytes, a.Size)
		}
		segment = nil
		rawBytes = 0
		return nil
	}
	for remaining > 0 {
		query.Limit = min(32, remaining)
		events, err := s.database.ListArchivableInternalEvents(ctx, query)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			break
		}
		for _, event := range events {
			restored, err := s.payloads.RestoreInternal(ctx, event)
			if err != nil {
				return err
			}
			record, err := transcriptarchive.EncodeRecord(restored)
			if err != nil {
				return err
			}
			if len(segment) > 0 && (rawBytes+len(record) > s.policy.TargetSegmentRawBytes || event.SequenceNum != segment[len(segment)-1].SequenceNum+1) {
				if err := flush(); err != nil {
					return err
				}
			}
			segment = append(segment, restored)
			rawBytes += len(record)
			remaining--
			query.AfterSequence = event.SequenceNum
		}
	}
	return flush()
}

func (s *Service) finishRegistered(ctx context.Context, query db.TranscriptArchiveQuery, budget int) (int, error) {
	used := 0
	after := int64(0)
	for used < budget {
		archives, err := s.database.ListTranscriptArchives(ctx, query.Scope, after, 100, false)
		if err != nil {
			return used, err
		}
		if len(archives) == 0 {
			break
		}
		for _, a := range archives {
			rows, err := s.database.ReadTranscriptArchiveRange(ctx, db.TranscriptArchiveQuery{Scope: query.Scope, AfterSequence: a.FromSequence - 1, ToSequence: a.ToSequence, Limit: a.EventCount + 1})
			if err != nil {
				return used, err
			}
			live := slices.ContainsFunc(rows, func(e db.CodeSessionInternalEvent) bool { return e.DeletedAt == nil })
			if !live {
				continue
			}
			decoded, err := s.ReadSegment(ctx, a)
			if err != nil {
				return used, err
			}
			if a.State == "pending" {
				for i := range rows {
					rows[i], err = s.payloads.RestoreInternal(ctx, rows[i])
					if err != nil {
						return used, err
					}
				}
				if err := s.attachVerified(ctx, a, rows, decoded); err != nil {
					return used, err
				}
			}
			liveRows := slices.DeleteFunc(rows, func(e db.CodeSessionInternalEvent) bool { return e.DeletedAt != nil })
			liveRows = liveRows[:min(len(liveRows), budget-used)]
			count, err := s.softDeleteSegment(ctx, a, query, liveRows, decoded)
			used += count
			if err != nil {
				return used, err
			}
			if used == budget {
				return used, nil
			}
		}
		after = archives[len(archives)-1].FromSequence
	}
	return used, nil
}

func (s *Service) softDeleteSegment(ctx context.Context, a db.TranscriptArchive, query db.TranscriptArchiveQuery, rows []db.CodeSessionInternalEvent, decoded map[int64]transcriptarchive.DecodedEvent) (int, error) {
	sequences := make([]int64, 0, len(rows))
	for _, row := range rows {
		event, ok := decoded[row.SequenceNum]
		if !ok || event.UUID != row.UUID {
			return 0, errIntegrity
		}
		if row.DeletedAt == nil {
			sequences = append(sequences, row.SequenceNum)
		}
	}
	total := 0
	for start := 0; start < len(sequences); start += s.policy.DeleteBatchRows {
		end := min(start+s.policy.DeleteBatchRows, len(sequences))
		count, err := s.database.SoftDeleteInternalEventsBatch(ctx, db.TranscriptDeleteBatch{Scope: a.Scope(), ArchiveUUID: a.UUID, Sequences: sequences[start:end], Eligibility: query})
		if err != nil {
			return total, err
		}
		total += int(count)
	}
	return total, nil
}

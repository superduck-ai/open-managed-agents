package transcriptretention

import (
	"context"
	"io"
	"math"
	"slices"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/transcriptarchive"
)

func decodedEvents(a db.TranscriptArchive, decoded map[int64]transcriptarchive.DecodedEvent) []db.CodeSessionInternalEvent {
	sequences := make([]int64, 0, len(decoded))
	for sequence := range decoded {
		sequences = append(sequences, sequence)
	}
	slices.Sort(sequences)
	events := make([]db.CodeSessionInternalEvent, 0, len(sequences))
	for _, sequence := range sequences {
		e := decoded[sequence]
		events = append(events, db.CodeSessionInternalEvent{UUID: e.UUID, ExternalID: e.ExternalID, OrganizationUUID: a.OrganizationUUID, WorkspaceUUID: a.WorkspaceUUID, CodeSessionUUID: a.CodeSessionUUID, CodeSessionExternalID: a.CodeSessionExternalID, SequenceNum: e.SequenceNum, EventType: e.EventType, PayloadUUID: e.PayloadUUID, AgentID: e.AgentID, IsCompaction: e.IsCompaction, Payload: e.Payload, PayloadHash: e.PayloadHash, IdempotencyKey: e.IdempotencyKey, EventMetadata: e.EventMetadata, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt})
	}
	return events
}

// Restore is an explicit maintenance operation, never part of the resume path.
func (s *Service) Restore(ctx context.Context, scope db.TranscriptScope) error {
	return s.walkArchives(ctx, scope, func(a db.TranscriptArchive, events []db.CodeSessionInternalEvent) error {
		for start := 0; start < len(events); start += 500 {
			end := min(start+500, len(events))
			batch := events[start:end]
			for i := range batch {
				prepared, err := s.payloads.PrepareInternalRestore(ctx, batch[i])
				if err != nil {
					return err
				}
				batch[i] = prepared
			}
			if err := s.database.RestoreTranscriptEventsBatch(ctx, a, batch); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) walkArchives(ctx context.Context, scope db.TranscriptScope, visit func(db.TranscriptArchive, []db.CodeSessionInternalEvent) error) error {
	after := int64(0)
	for {
		archives, err := s.database.ListTranscriptArchives(ctx, scope, after, 100, true)
		if err != nil {
			return err
		}
		for _, a := range archives {
			decoded, err := s.ReadSegment(ctx, a)
			if err != nil {
				return err
			}
			if err := visit(a, decodedEvents(a, decoded)); err != nil {
				return err
			}
		}
		if len(archives) < 100 {
			return nil
		}
		after = archives[len(archives)-1].FromSequence
	}
}

// Export streams archive history plus remaining PG rows in sequence order.
// Operators pause archive/delete workers during export or restore for a stable view.
func (s *Service) Export(ctx context.Context, scope db.TranscriptScope, writer io.Writer) error {
	cursor := int64(0)
	err := s.walkArchives(ctx, scope, func(a db.TranscriptArchive, events []db.CodeSessionInternalEvent) error {
		if err := s.exportDatabaseRange(ctx, scope, cursor, a.FromSequence-1, writer); err != nil {
			return err
		}
		for _, event := range events {
			if err := writeEvent(writer, event); err != nil {
				return err
			}
		}
		cursor = a.ToSequence
		return nil
	})
	if err != nil {
		return err
	}
	return s.exportDatabaseRange(ctx, scope, cursor, math.MaxInt64, writer)
}

func (s *Service) exportDatabaseRange(ctx context.Context, scope db.TranscriptScope, after, to int64, writer io.Writer) error {
	query := db.TranscriptArchiveQuery{Scope: scope, AfterSequence: after, ToSequence: to, Limit: 32}
	for {
		events, err := s.database.ReadTranscriptArchiveRange(ctx, query)
		if err != nil {
			return err
		}
		for _, event := range events {
			restored, err := s.payloads.RestoreInternal(ctx, event)
			if err != nil {
				return err
			}
			if err := writeEvent(writer, restored); err != nil {
				return err
			}
		}
		if len(events) < query.Limit {
			return nil
		}
		query.AfterSequence = events[len(events)-1].SequenceNum
	}
}

func writeEvent(writer io.Writer, event db.CodeSessionInternalEvent) error {
	record, err := transcriptarchive.EncodeRecord(event)
	if err != nil {
		return err
	}
	_, err = writer.Write(record)
	return err
}

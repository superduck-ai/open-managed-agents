package codesessions

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	subagentMappingGraceSequences int64 = 200
	subagentCursorAdvanceTimeout        = 5 * time.Second
)

type subagentPublicationCursor interface {
	SubagentPublicationProgress(ctx context.Context, workspaceUUID, codeSessionExternalID string) (db.SubagentPublicationProgress, error)
	AdvanceSubagentPublishedSequence(ctx context.Context, workspaceUUID, codeSessionExternalID string, sequenceNum int64) error
}

type internalEventPager interface {
	ListCodeSessionInternalEventsPage(ctx context.Context, params db.ListCodeSessionInternalEventsPageParams) ([]db.CodeSessionInternalEvent, bool, error)
}

type subagentPublication struct {
	cursor   subagentPublicationCursor
	events   internalEventPager
	mappings func(context.Context, db.CodeSession) (map[string]string, error)
	publish  func(context.Context, db.CodeSession, []json.RawMessage) error
	logger   *slog.Logger

	advanceTimeout time.Duration
}

type subagentPayloadScan struct {
	payloads       []json.RawMessage
	committed      int64
	scannedThrough int64
	unmapped       []int64
}

func (s *Service) subagentPublication() subagentPublication {
	return subagentPublication{
		cursor:   s.db,
		events:   s.eventPayloads,
		mappings: s.subagentThreadMappings,
		publish:  s.publishCodeSessionPayloads,
		logger:   s.logger,

		advanceTimeout: subagentCursorAdvanceTimeout,
	}
}

func (s *Service) reconcileSubagentInternalEvents(ctx context.Context, codeSession db.CodeSession) error {
	if s.sink == nil {
		return nil
	}
	return s.subagentPublication().reconcile(ctx, codeSession)
}

func (s *Service) PublishSubagentInternalEvents(ctx context.Context, codeSession db.CodeSession) error {
	if s == nil || s.sink == nil {
		return nil
	}
	return s.subagentPublication().rebuild(ctx, codeSession)
}

func (p subagentPublication) reconcile(ctx context.Context, codeSession db.CodeSession) error {
	progress, err := p.cursor.SubagentPublicationProgress(ctx, codeSession.WorkspaceUUID, codeSession.ExternalID)
	if err != nil || progress.CommittedSequenceNum <= progress.PublishedSequenceNum {
		return err
	}
	mappings, err := p.mappings(ctx, codeSession)
	if err != nil {
		return err
	}
	for progress.PublishedSequenceNum < progress.CommittedSequenceNum {
		if err := ctx.Err(); err != nil {
			return err
		}
		through, complete, err := p.reconcilePage(ctx, codeSession, mappings, progress)
		if err != nil || !complete || through <= progress.PublishedSequenceNum {
			return err
		}
		progress.PublishedSequenceNum = through
	}
	return nil
}

func (p subagentPublication) reconcilePage(ctx context.Context, codeSession db.CodeSession, mappings map[string]string, progress db.SubagentPublicationProgress) (int64, bool, error) {
	scan, err := p.scanPage(ctx, codeSession, mappings, progress)
	if err != nil {
		return 0, false, err
	}
	if len(scan.payloads) > 0 {
		if err := p.publish(ctx, codeSession, scan.payloads); err != nil {
			return 0, false, err
		}
	}
	through, abandoned := scan.publishedThrough()
	if abandoned > 0 {
		p.logger.WarnContext(ctx, "skip subagent internal events without thread mapping",
			"code_session_id", codeSession.ExternalID, "skipped_count", abandoned, "published_through", through)
	}
	if through <= progress.PublishedSequenceNum {
		return through, false, nil
	}
	if err := p.advance(ctx, codeSession, through); err != nil {
		return 0, false, err
	}
	return through, through == scan.scannedThrough, nil
}

func (p subagentPublication) advance(ctx context.Context, codeSession db.CodeSession, through int64) error {
	advanceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.advanceTimeout)
	defer cancel()
	return p.cursor.AdvanceSubagentPublishedSequence(advanceCtx, codeSession.WorkspaceUUID, codeSession.ExternalID, through)
}

func (p subagentPublication) rebuild(ctx context.Context, codeSession db.CodeSession) error {
	mappings, err := p.mappings(ctx, codeSession)
	if err != nil || len(mappings) == 0 {
		return err
	}
	payloads := make([]json.RawMessage, 0, 32)
	after := int64(0)
	for {
		events, hasMore, err := p.page(ctx, codeSession, after)
		if err != nil {
			return err
		}
		for _, event := range events {
			after = event.SequenceNum
			eventPayloads, _, err := mappedSubagentPayloads(codeSession.ExternalID, event, mappings)
			if err != nil {
				return err
			}
			payloads = append(payloads, eventPayloads...)
		}
		if !hasMore || len(events) == 0 {
			break
		}
	}
	if len(payloads) == 0 {
		return nil
	}
	return p.publish(ctx, codeSession, payloads)
}

func (p subagentPublication) page(ctx context.Context, codeSession db.CodeSession, after int64) ([]db.CodeSessionInternalEvent, bool, error) {
	return p.events.ListCodeSessionInternalEventsPage(ctx, db.ListCodeSessionInternalEventsPageParams{
		WorkspaceUUID:         codeSession.WorkspaceUUID,
		CodeSessionExternalID: codeSession.ExternalID,
		Subagents:             true,
		AfterSequence:         after,
		Limit:                 internalEventsPageSize,
	})
}

func (p subagentPublication) scanPage(ctx context.Context, codeSession db.CodeSession, mappings map[string]string, progress db.SubagentPublicationProgress) (subagentPayloadScan, error) {
	events, hasMore, err := p.page(ctx, codeSession, progress.PublishedSequenceNum)
	if err != nil {
		return subagentPayloadScan{}, err
	}
	scan := subagentPayloadScan{committed: progress.CommittedSequenceNum, scannedThrough: progress.CommittedSequenceNum}
	for _, event := range events {
		if event.SequenceNum > scan.committed {
			return scan, nil
		}
		payloads, unmapped, err := mappedSubagentPayloads(codeSession.ExternalID, event, mappings)
		if err != nil {
			return subagentPayloadScan{}, err
		}
		if unmapped {
			scan.unmapped = append(scan.unmapped, event.SequenceNum)
		}
		scan.payloads = append(scan.payloads, payloads...)
	}
	if hasMore && len(events) > 0 {
		scan.scannedThrough = events[len(events)-1].SequenceNum
	}
	return scan, nil
}

func mappedSubagentPayloads(codeSessionID string, event db.CodeSessionInternalEvent, mappings map[string]string) ([]json.RawMessage, bool, error) {
	if event.AgentID == nil {
		return nil, false, nil
	}
	threadID := mappings[strings.TrimSpace(*event.AgentID)]
	if threadID == "" {
		return nil, true, nil
	}
	payloads, err := publicPayloadsFromInternalSubagentEvent(codeSessionID, event, threadID)
	return payloads, false, err
}

func (scan subagentPayloadScan) publishedThrough() (int64, int) {
	for index, sequence := range scan.unmapped {
		if scan.committed-sequence < subagentMappingGraceSequences {
			return sequence - 1, index
		}
	}
	return scan.scannedThrough, len(scan.unmapped)
}

func containsThreadCreated(payloads []json.RawMessage) bool {
	for _, raw := range payloads {
		var event struct {
			Type string `json:"type"`
		}
		if jsonv2.Unmarshal(raw, &event) == nil && event.Type == "session.thread_created" {
			return true
		}
	}
	return false
}

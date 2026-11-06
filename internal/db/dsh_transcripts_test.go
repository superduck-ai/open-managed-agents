package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

type dshAppendExecutor struct {
	yourbatis.Executor
	statements []string
	bindings   []yourbatis.BoundSQL
	insertRows int64
	updateRows int64
	insertErr  error
	updateErr  error
}

func (e *dshAppendExecutor) Exec(_ context.Context, statement yourbatis.Statement, bound yourbatis.BoundSQL) (sql.Result, error) {
	e.statements = append(e.statements, statement.ID)
	e.bindings = append(e.bindings, bound)
	if statement.ID == "DSHTranscriptMapper.InsertEvents" {
		return driver.RowsAffected(e.insertRows), e.insertErr
	}
	return driver.RowsAffected(e.updateRows), e.updateErr
}

func dshBatchEvents(start, count int) []DSHTranscriptEvent {
	events := make([]DSHTranscriptEvent, count)
	for index := range events {
		seq := int64(start + index)
		events[index] = DSHTranscriptEvent{Seq: seq, Payload: []byte(fmt.Sprintf(`{"seq":%d,"value":1}`, seq))}
	}
	return events
}

func TestAppendDSHEventsRejectsBeforeWriting(t *testing.T) {
	tests := []struct {
		name     string
		next     int64
		events   []DSHTranscriptEvent
		existing []dshTranscriptEventRow
		want     error
	}{
		{name: "negative sequence", events: []DSHTranscriptEvent{{Seq: -1}}, want: ErrInvalidState},
		{name: "unsafe sequence", events: []DSHTranscriptEvent{{Seq: 9007199254740991}}, want: ErrInvalidState},
		{name: "gap", events: dshBatchEvents(1, 2), want: ErrDSHTranscriptGap},
		{name: "invalid later sequence", events: []DSHTranscriptEvent{{Seq: 0}, {Seq: 2}}, want: ErrInvalidState},
		{name: "missing duplicate", next: 1, events: dshBatchEvents(0, 2), want: ErrDSHTranscriptConflict},
		{name: "conflicting duplicate", next: 1, events: dshBatchEvents(0, 2), existing: []dshTranscriptEventRow{{Seq: 0, Payload: []byte(`{"value":2}`)}}, want: ErrDSHTranscriptConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &dshAppendExecutor{Executor: newMapperTestExecutor(t, mapperTestResponse{})}
			result := DSHTranscriptAppendResult{NextSeq: test.next}
			err := appendDSHEvents(t.Context(), NewDSHTranscriptMapper(executor), DSHScope{}, dshTranscriptRow{UUID: "transcript", NextSeq: test.next}, test.events, test.existing, &result)
			if !errors.Is(err, test.want) || len(executor.statements) != 0 || result.NextSeq != test.next {
				t.Fatalf("error=%v statements=%v next=%d", err, executor.statements, result.NextSeq)
			}
		})
	}
}

func TestAppendDSHEventsWriteFailures(t *testing.T) {
	failure := errors.New("batch write failed")
	for _, test := range []struct {
		name                   string
		insertRows, updateRows int64
		insertErr, updateErr   error
		want                   error
		calls                  int
	}{
		{name: "insert fails", insertErr: failure, want: failure, calls: 1},
		{name: "insert count mismatch", insertRows: 1, want: ErrInvalidState, calls: 1},
		{name: "watermark fails", insertRows: 2, updateErr: failure, want: failure, calls: 2},
		{name: "watermark count mismatch", insertRows: 2, updateRows: 0, want: ErrInvalidState, calls: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := &dshAppendExecutor{Executor: newMapperTestExecutor(t, mapperTestResponse{}), insertRows: test.insertRows, updateRows: test.updateRows, insertErr: test.insertErr, updateErr: test.updateErr}
			result := DSHTranscriptAppendResult{}
			err := appendDSHEvents(t.Context(), NewDSHTranscriptMapper(executor), DSHScope{}, dshTranscriptRow{UUID: "transcript"}, dshBatchEvents(0, 2), nil, &result)
			if !errors.Is(err, test.want) || len(executor.statements) != test.calls || result.NextSeq != 0 {
				t.Fatalf("error=%v statements=%v result=%+v", err, executor.statements, result)
			}
		})
	}
}

func TestAppendDSHEventsUsesOneInsertForTheNewSuffix(t *testing.T) {
	for _, test := range []struct {
		name       string
		next       int64
		count      int
		duplicates int
	}{
		{name: "single event", count: 1},
		{name: "full batch", count: 200},
		{name: "duplicate prefix and new suffix", next: 2, count: 200, duplicates: 2},
		{name: "only duplicates", next: 200, count: 200, duplicates: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			events := dshBatchEvents(0, test.count)
			existing := make([]dshTranscriptEventRow, test.duplicates)
			for index := range existing {
				existing[index] = dshTranscriptEventRow{Seq: int64(index), Payload: []byte(fmt.Sprintf(`{"value":1.0,"seq":%d}`, index))}
			}
			executor := &dshAppendExecutor{Executor: newMapperTestExecutor(t, mapperTestResponse{}), insertRows: int64(test.count - test.duplicates), updateRows: 1}
			scope := DSHScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", CodeSessionUUID: "code"}
			result := DSHTranscriptAppendResult{NextSeq: test.next}
			err := appendDSHEvents(t.Context(), NewDSHTranscriptMapper(executor), scope, dshTranscriptRow{UUID: "transcript", NextSeq: test.next}, events, existing, &result)
			if err != nil || result.NextSeq != int64(test.count) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if test.duplicates == test.count {
				if len(executor.statements) != 0 {
					t.Fatalf("duplicate-only writes=%v", executor.statements)
				}
				return
			}
			if !reflect.DeepEqual(executor.statements, []string{"DSHTranscriptMapper.InsertEvents", "DSHTranscriptMapper.SetNextSeq"}) {
				t.Fatalf("statements=%v", executor.statements)
			}
			values := executor.bindings[0].Values()
			if len(values) != (test.count-test.duplicates)*6 {
				t.Fatalf("insert arguments=%d", len(values))
			}
			for index, event := range events[test.duplicates:] {
				want := []any{"org", "workspace", "code", "transcript", event.Seq, event.Payload}
				if !reflect.DeepEqual(values[index*6:index*6+6], want) {
					t.Fatalf("row %d arguments=%v", index, values[index*6:index*6+6])
				}
			}
		})
	}
}

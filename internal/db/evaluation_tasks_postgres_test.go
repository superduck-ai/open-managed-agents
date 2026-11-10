//go:build integration

package db

import (
	"encoding/json"
	"testing"
	"time"
	"uuid"
)

func TestEvaluationTaskUsageAndAnswerPostgres(t *testing.T) {
	d, _ := newTranscriptArchiveTestDB(t)
	now := time.Now().UTC()
	org, ws := uuid.NewV4().String(), uuid.NewV4().String()
	task := EvaluationTask{
		UUID: uuid.NewV4().String(), ExternalID: "evaltask_usage", OrganizationUUID: org, WorkspaceUUID: ws, Name: "usage", Mode: "evaluate",
		Status: "running", AgentExternalID: "agent_usage", AgentVersion: 1, EnvironmentExternalID: "env_usage", DatasetExternalID: "evalds_usage",
		DatasetVersionExternalID: "edv_usage", RubricExternalID: "evalrb_usage", RubricVersionExternalID: "erv_usage", PassScore: 60,
		VaultIDs: json.RawMessage(`[]`), Resources: json.RawMessage(`[]`), CreatedAt: now,
	}
	item := EvaluationTaskItem{UUID: uuid.NewV4().String(), ExternalID: "evalitem_usage", CaseExternalID: "evalcase_usage", Round: 1, Position: 1, Query: "q"}
	created, err := d.CreateEvaluationTask(t.Context(), task, []EvaluationTaskItem{item})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.MarkEvaluationItemRunning(t.Context(), org, ws, created.Items[0].UUID, "sesn_eval_usage", now); err != nil {
		t.Fatal(err)
	}
	for _, event := range []struct {
		session string
		kind    string
		payload string
	}{
		{"sesn_eval_usage", "span.model_request_end", `{"model_usage":{"input_tokens":10,"output_tokens":4,"cache_read_input_tokens":2}}`},
		{"sesn_eval_usage", "span.model_request_end", `{"usage":{"input_tokens":"5","output_tokens":"bad"}}`},
		{"sesn_eval_usage", "agent.message", `{"model_usage":{"input_tokens":100}}`},
		{"sesn_other", "span.model_request_end", `{"model_usage":{"input_tokens":1000}}`},
	} {
		_, err := NewSessionEventMapper(d.mapperDB).Insert(t.Context(), sessionEventWriteParams{
			UUID: uuid.NewV4().String(), ExternalID: "event_" + uuid.NewV4().String(), OrganizationUUID: org, WorkspaceUUID: ws,
			SessionUUID: uuid.NewV4().String(), SessionExternalID: event.session, EventType: event.kind, Payload: []byte(event.payload),
			CreatedAt: now, ProcessedAt: new(now),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	usage, err := d.SumEvaluationTaskUsage(t.Context(), org, ws, created.Task.UUID)
	if err != nil {
		t.Fatal(err)
	}
	if usage != (EvaluationTaskUsage{InputTokens: 15, OutputTokens: 4, CacheReadInputTokens: 2}) {
		t.Fatalf("usage = %+v", usage)
	}
	empty, err := d.SumEvaluationTaskUsage(t.Context(), org, uuid.NewV4().String(), created.Task.UUID)
	if err != nil || empty != (EvaluationTaskUsage{}) {
		t.Fatalf("other workspace usage = %+v err=%v", empty, err)
	}

	score, err := d.CreateEvaluationTaskScore(t.Context(), EvaluationTaskScore{
		UUID: uuid.NewV4().String(), ExternalID: "evalscore_usage", OrganizationUUID: org, WorkspaceUUID: ws, TaskUUID: created.Task.UUID,
		ItemUUID: created.Items[0].UUID, Status: "completed", Score: 90, Passed: true, Answer: "最终回答", Dimensions: json.RawMessage(`[]`), CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if score.Answer != "最终回答" {
		t.Fatalf("answer = %q", score.Answer)
	}
}

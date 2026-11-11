//go:build integration

package db

import (
	"encoding/json"
	"errors"
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

func TestEvaluationTaskItemInputsPostgres(t *testing.T) {
	d, _ := newTranscriptArchiveTestDB(t)
	now := time.Now().UTC()
	org, ws := uuid.NewV4().String(), uuid.NewV4().String()
	task := EvaluationTask{
		UUID: uuid.NewV4().String(), ExternalID: "evaltask_inputs", OrganizationUUID: org, WorkspaceUUID: ws, Name: "inputs", Mode: "evaluate",
		Status: "running", AgentExternalID: "agent_inputs", AgentVersion: 1, EnvironmentExternalID: "env_inputs", DatasetExternalID: "evalds_inputs",
		DatasetVersionExternalID: "edv_inputs", RubricExternalID: "evalrb_inputs", RubricVersionExternalID: "erv_inputs", PassScore: 60,
		VaultIDs: json.RawMessage(`[]`), Resources: json.RawMessage(`[]`), CreatedAt: now,
	}
	inputs := json.RawMessage(`[{"type":"text","text":"看图"},{"type":"image","source":{"type":"file","file_id":"file_demo"}}]`)
	created, err := d.CreateEvaluationTask(t.Context(), task, []EvaluationTaskItem{
		{UUID: uuid.NewV4().String(), ExternalID: "evalitem_plain_" + uuid.NewV4().String(), CaseExternalID: "c1", Round: 1, Position: 1, Query: "q"},
		{UUID: uuid.NewV4().String(), ExternalID: "evalitem_image_" + uuid.NewV4().String(), CaseExternalID: "c2", Round: 1, Position: 2, Query: "看图", Inputs: inputs},
	})
	if err != nil {
		t.Fatal(err)
	}
	var plain, image []map[string]any
	if err := json.Unmarshal(created.Items[0].Inputs, &plain); err != nil || len(plain) != 0 {
		t.Fatalf("plain inputs = %s err=%v", created.Items[0].Inputs, err)
	}
	if err := json.Unmarshal(created.Items[1].Inputs, &image); err != nil || len(image) != 2 || image[1]["type"] != "image" {
		t.Fatalf("image inputs = %s err=%v", created.Items[1].Inputs, err)
	}
}

func TestEvaluationTaskRetryAndSweepPostgres(t *testing.T) {
	d, _ := newTranscriptArchiveTestDB(t)
	ctx := t.Context()
	now := time.Now().UTC()
	org, ws := uuid.NewV4().String(), uuid.NewV4().String()
	task := EvaluationTask{
		UUID: uuid.NewV4().String(), ExternalID: "evaltask_retry_" + uuid.NewV4().String(), OrganizationUUID: org, WorkspaceUUID: ws, Name: "retry",
		Mode: "evaluate", Status: "running", AgentExternalID: "agent_retry", AgentVersion: 1, EnvironmentExternalID: "env_retry",
		DatasetExternalID: "evalds_retry", DatasetVersionExternalID: "edv_retry", RubricExternalID: "evalrb_retry", RubricVersionExternalID: "erv_retry",
		PassScore: 60, VaultIDs: json.RawMessage(`[]`), Resources: json.RawMessage(`[]`), CreatedAt: now,
	}
	items := []EvaluationTaskItem{
		{UUID: uuid.NewV4().String(), ExternalID: "evalitem_r1_" + uuid.NewV4().String(), CaseExternalID: "c1", Round: 1, Position: 1, Query: "q1"},
		{UUID: uuid.NewV4().String(), ExternalID: "evalitem_r2_" + uuid.NewV4().String(), CaseExternalID: "c2", Round: 1, Position: 2, Query: "q2"},
	}
	created, err := d.CreateEvaluationTask(ctx, task, items)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.RetryEvaluationTask(ctx, org, ws, created.Task.UUID, 1, now); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("running retry err = %v", err)
	}
	for _, item := range created.Items {
		if _, err := d.MarkEvaluationItemRunning(ctx, org, ws, item.UUID, "sesn_"+item.ExternalID, now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	stale, err := d.ListStaleRunningEvaluationItems(ctx, now.Add(-time.Minute), 1000)
	if err != nil || !containsItem(stale, created.Items[0].UUID) {
		t.Fatalf("stale err=%v count=%d", err, len(stale))
	}
	if err := d.TouchRunningEvaluationItem(ctx, org, ws, created.Items[0].UUID, now); err != nil {
		t.Fatal(err)
	}
	stale, err = d.ListStaleRunningEvaluationItems(ctx, now.Add(-time.Minute), 1000)
	if err != nil || containsItem(stale, created.Items[0].UUID) || !containsItem(stale, created.Items[1].UUID) {
		t.Fatalf("touched item still stale err=%v", err)
	}
	for _, item := range created.Items {
		if _, err := d.FinishRunningEvaluationItem(ctx, org, ws, item.UUID, "completed", "", now); err != nil {
			t.Fatal(err)
		}
	}
	refs, err := d.ListSettleableEvaluationTasks(ctx, now.Add(time.Minute), 1000)
	if err != nil || !containsRef(refs, created.Task.ExternalID) {
		t.Fatalf("settleable err=%v refs=%d", err, len(refs))
	}
	for index, status := range []string{"completed", "failed"} {
		if _, err := d.CreateEvaluationTaskScore(ctx, EvaluationTaskScore{
			UUID: uuid.NewV4().String(), ExternalID: "evalscore_retry_" + uuid.NewV4().String(), OrganizationUUID: org, WorkspaceUUID: ws,
			TaskUUID: created.Task.UUID, ItemUUID: created.Items[index].UUID, Status: status, Dimensions: json.RawMessage(`[]`),
			JudgeModel: "judge-model", JudgePromptVersion: "score-v2", CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.UpdateEvaluationTaskStatus(ctx, org, ws, created.Task.ExternalID, "completed", "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RetryEvaluationTask(ctx, org, uuid.NewV4().String(), created.Task.UUID, 1, now); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("other workspace retry err = %v", err)
	}
	retried, err := d.RetryEvaluationTask(ctx, org, ws, created.Task.UUID, 1, now)
	if err != nil || retried != (EvaluationTaskRetry{ClearedScores: 1}) {
		t.Fatalf("retry = %+v err=%v", retried, err)
	}
	detail, err := d.GetEvaluationTask(ctx, org, ws, created.Task.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Task.Status != "running" || len(detail.Scores) != 1 || detail.Scores[0].Status != "completed" || detail.Scores[0].JudgeModel != "judge-model" {
		t.Fatalf("detail status=%s scores=%+v", detail.Task.Status, detail.Scores)
	}
	if err := d.UpdateEvaluationTaskStatus(ctx, org, ws, created.Task.ExternalID, "completed", "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RetryEvaluationTask(ctx, org, ws, created.Task.UUID, 1, now); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("empty retry err = %v", err)
	}
	detail, err = d.GetEvaluationTask(ctx, org, ws, created.Task.ExternalID)
	if err != nil || detail.Task.Status != "completed" {
		t.Fatalf("empty retry must roll back status=%s err=%v", detail.Task.Status, err)
	}
}

func containsItem(items []EvaluationTaskItem, itemUUID string) bool {
	for _, item := range items {
		if item.UUID == itemUUID {
			return true
		}
	}
	return false
}

func containsRef(refs []EvaluationTaskRef, externalID string) bool {
	for _, ref := range refs {
		if ref.ExternalID == externalID {
			return true
		}
	}
	return false
}

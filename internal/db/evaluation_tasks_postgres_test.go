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
	startItem(t, d, org, ws, created.Items[0].UUID, "sesn_eval_usage", now)
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

	score, err := d.CreateEvaluationTaskScore(t.Context(), EvaluationTaskScore{
		UUID: uuid.NewV4().String(), ExternalID: "evalscore_usage", OrganizationUUID: org, WorkspaceUUID: ws, TaskUUID: created.Task.UUID,
		ItemUUID: created.Items[0].UUID, Status: "completed", Score: 90, Passed: true, Answer: "最终回答", Dimensions: json.RawMessage(`[]`),
		JudgeInputTokens: 300, JudgeOutputTokens: 40, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if score.Answer != "最终回答" || score.JudgeInputTokens != 300 {
		t.Fatalf("score = %+v", score)
	}
	usage, err := d.SumEvaluationTaskUsage(t.Context(), org, ws, created.Task.UUID)
	if err != nil {
		t.Fatal(err)
	}
	want := EvaluationTaskUsage{InputTokens: 15, OutputTokens: 4, CacheReadInputTokens: 2, JudgeInputTokens: 300, JudgeOutputTokens: 40}
	if usage != want {
		t.Fatalf("usage = %+v", usage)
	}
	empty, err := d.SumEvaluationTaskUsage(t.Context(), org, uuid.NewV4().String(), created.Task.UUID)
	if err != nil || empty != (EvaluationTaskUsage{}) {
		t.Fatalf("other workspace usage = %+v err=%v", empty, err)
	}
}

func TestEvaluationTaskStartAndCancelPostgres(t *testing.T) {
	d, _ := newTranscriptArchiveTestDB(t)
	ctx := t.Context()
	now := time.Now().UTC()
	org, ws := uuid.NewV4().String(), uuid.NewV4().String()
	created, err := d.CreateEvaluationTask(ctx, evaluationTaskFixture(org, ws, "cancel"), []EvaluationTaskItem{
		{UUID: uuid.NewV4().String(), ExternalID: "evalitem_c1_" + uuid.NewV4().String(), CaseExternalID: "c1", Round: 1, Position: 1, Query: "q1"},
		{UUID: uuid.NewV4().String(), ExternalID: "evalitem_c2_" + uuid.NewV4().String(), CaseExternalID: "c2", Round: 1, Position: 2, Query: "q2"},
		{UUID: uuid.NewV4().String(), ExternalID: "evalitem_c3_" + uuid.NewV4().String(), CaseExternalID: "c3", Round: 1, Position: 3, Query: "q3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	running, unstarted, waiting := created.Items[0], created.Items[1], created.Items[2]
	startItem(t, d, org, ws, running.UUID, "sesn_cancel", now)
	if claimed, err := d.ClaimEvaluationItem(ctx, org, ws, running.UUID, now); err != nil || claimed {
		t.Fatalf("second claim = %v err=%v", claimed, err)
	}
	if attached, err := d.AttachEvaluationItemSession(ctx, org, ws, running.UUID, "sesn_other", now); err != nil || attached {
		t.Fatalf("second attach = %v err=%v", attached, err)
	}
	if claimed, err := d.ClaimEvaluationItem(ctx, org, ws, unstarted.UUID, now.Add(-time.Hour)); err != nil || !claimed {
		t.Fatalf("claim = %v err=%v", claimed, err)
	}
	listed, err := d.ListUnstartedEvaluationItems(ctx, now.Add(-time.Minute), 1000)
	if err != nil || !containsItem(listed, unstarted.UUID) || containsItem(listed, running.UUID) {
		t.Fatalf("unstarted = %d err=%v", len(listed), err)
	}
	if abandoned, err := d.AbandonEvaluationItem(ctx, org, ws, running.UUID, "中断", now); err != nil || abandoned {
		t.Fatalf("abandon started item = %v err=%v", abandoned, err)
	}
	if finished, err := d.FinishEvaluationTask(ctx, org, ws, created.Task.ExternalID, "completed", now); err != nil || finished {
		t.Fatalf("finish with open items = %v err=%v", finished, err)
	}

	cancelled, err := d.CancelEvaluationTask(ctx, org, ws, created.Task.UUID, now)
	if err != nil || len(cancelled) != 3 {
		t.Fatalf("cancelled = %+v err=%v", cancelled, err)
	}
	sessions := map[string]string{}
	for _, item := range cancelled {
		sessions[item.UUID] = item.SessionExternalID
	}
	if sessions[running.UUID] != "sesn_cancel" || sessions[waiting.UUID] != "" {
		t.Fatalf("cancelled sessions = %v", sessions)
	}
	if _, err := d.CancelEvaluationTask(ctx, org, ws, created.Task.UUID, now); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("second cancel err = %v", err)
	}
	if finished, err := d.FinishEvaluationTask(ctx, org, ws, created.Task.ExternalID, "completed", now); err != nil || finished {
		t.Fatalf("finish cancelled = %v err=%v", finished, err)
	}
	detail, err := d.GetEvaluationTask(ctx, org, ws, created.Task.ExternalID)
	if err != nil || detail.Task.Status != "cancelled" {
		t.Fatalf("status = %s err=%v", detail.Task.Status, err)
	}
}

func evaluationTaskFixture(org, ws, name string) EvaluationTask {
	return EvaluationTask{
		UUID: uuid.NewV4().String(), ExternalID: "evaltask_" + name + "_" + uuid.NewV4().String(), OrganizationUUID: org, WorkspaceUUID: ws, Name: name,
		Mode: "evaluate", Status: "running", AgentExternalID: "agent_" + name, AgentVersion: 1, EnvironmentExternalID: "env_" + name,
		DatasetExternalID: "evalds_" + name, DatasetVersionExternalID: "edv_" + name, RubricExternalID: "evalrb_" + name, RubricVersionExternalID: "erv_" + name,
		PassScore: 60, VaultIDs: json.RawMessage(`[]`), Resources: json.RawMessage(`[]`), CreatedAt: time.Now().UTC(),
	}
}

func startItem(t *testing.T, d *DB, org, ws, itemUUID, sessionID string, now time.Time) {
	t.Helper()
	if claimed, err := d.ClaimEvaluationItem(t.Context(), org, ws, itemUUID, now); err != nil || !claimed {
		t.Fatalf("claim = %v err=%v", claimed, err)
	}
	if attached, err := d.AttachEvaluationItemSession(t.Context(), org, ws, itemUUID, sessionID, now); err != nil || !attached {
		t.Fatalf("attach = %v err=%v", attached, err)
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
		startItem(t, d, org, ws, item.UUID, "sesn_"+item.ExternalID, now.Add(-time.Hour))
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
	if finished, err := d.FinishEvaluationTask(ctx, org, ws, created.Task.ExternalID, "completed", now); err != nil || finished {
		t.Fatalf("finish before scoring = %v err=%v", finished, err)
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
	if finished, err := d.FinishEvaluationTask(ctx, org, ws, created.Task.ExternalID, "completed", now); err != nil || !finished {
		t.Fatalf("finish = %v err=%v", finished, err)
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
	if finished, err := d.FinishEvaluationTask(ctx, org, ws, created.Task.ExternalID, "completed", now); err != nil || finished {
		t.Fatalf("finish with unscored item = %v err=%v", finished, err)
	}
	if _, err := d.CreateEvaluationTaskScore(ctx, EvaluationTaskScore{
		UUID: uuid.NewV4().String(), ExternalID: "evalscore_rescore_" + uuid.NewV4().String(), OrganizationUUID: org, WorkspaceUUID: ws,
		TaskUUID: created.Task.UUID, ItemUUID: created.Items[1].UUID, Status: "completed", Dimensions: json.RawMessage(`[]`), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if finished, err := d.FinishEvaluationTask(ctx, org, ws, created.Task.ExternalID, "completed", now); err != nil || !finished {
		t.Fatalf("finish = %v err=%v", finished, err)
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

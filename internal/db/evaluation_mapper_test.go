package db

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestEvaluationRubricVersionsListNewestFirst(t *testing.T) {
	bound := buildEvaluationRubricMapperListRubricVersions(yourbatis.DialectPostgres, "org", "ws", "evalrb_test")
	if evaluationRubricMapperListRubricVersionsStatement.ID != "EvaluationRubricMapper.ListRubricVersions" {
		t.Fatalf("statement = %+v", evaluationRubricMapperListRubricVersionsStatement)
	}
	if strings.Contains(bound.SQL, "#{") || !strings.Contains(bound.SQL, "ORDER BY v.version DESC") {
		t.Fatalf("SQL = %s", bound.SQL)
	}
	want := []any{"org", "ws", "evalrb_test"}
	if !reflect.DeepEqual(bound.Values(), want) {
		t.Fatalf("values = %#v", bound.Values())
	}
}

func TestEvaluationCaseTraceBindsOneCase(t *testing.T) {
	bound := buildEvaluationDatasetMapperFindCaseTrace(yourbatis.DialectPostgres, "org", "ws", "version", "evalcase_test")
	if evaluationDatasetMapperFindCaseTraceStatement.ID != "EvaluationDatasetMapper.FindCaseTrace" {
		t.Fatalf("statement = %+v", evaluationDatasetMapperFindCaseTraceStatement)
	}
	if strings.Contains(bound.SQL, "#{") || !strings.Contains(bound.SQL, "expected_tool_trace") || strings.Contains(bound.SQL, "query") {
		t.Fatalf("SQL = %s", bound.SQL)
	}
	want := []any{"org", "ws", "version", "evalcase_test"}
	if !reflect.DeepEqual(bound.Values(), want) {
		t.Fatalf("values = %#v", bound.Values())
	}
}

func TestEvaluationTaskUsageScopesItemSessions(t *testing.T) {
	bound := buildEvaluationTaskMapperSumTaskUsage(yourbatis.DialectPostgres, "org", "ws", "task")
	if evaluationTaskMapperSumTaskUsageStatement.ID != "EvaluationTaskMapper.SumTaskUsage" {
		t.Fatalf("statement = %+v", evaluationTaskMapperSumTaskUsageStatement)
	}
	for _, fragment := range []string{"span.model_request_end", "FROM evaluation_task_items i", "i.task_uuid", "input_tokens", "cache_read_input_tokens"} {
		if !strings.Contains(bound.SQL, fragment) {
			t.Fatalf("SQL missing %q: %s", fragment, bound.SQL)
		}
	}
	if strings.Contains(bound.SQL, "#{") {
		t.Fatalf("SQL = %s", bound.SQL)
	}
	want := []any{"ws", "org", "ws", "task"}
	if !reflect.DeepEqual(bound.Values(), want) {
		t.Fatalf("values = %#v", bound.Values())
	}
}

func TestEvaluationScoreInsertBindsAnswerAndJudge(t *testing.T) {
	now := time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC)
	params := insertEvaluationTaskScoreParams{
		UUID: "score", ExternalID: "evalscore_test", OrganizationUUID: "org", WorkspaceUUID: "ws", TaskUUID: "task", ItemUUID: "item",
		Status: "completed", Score: 80, Passed: true, Explanation: "ok", Answer: "最终回答", Dimensions: []byte(`[]`),
		JudgeModel: "judge-model", JudgePromptVersion: "score-v2", JudgeSystem: "系统", JudgePrompt: "提示词", JudgeResponse: "模型返回", CreatedAt: now,
	}
	bound := buildEvaluationTaskMapperInsertScore(yourbatis.DialectPostgres, params)
	if !strings.Contains(bound.SQL, "judge_system, judge_prompt, judge_response") || !strings.Contains(bound.SQL, "RETURNING") {
		t.Fatalf("SQL = %s", bound.SQL)
	}
	values := bound.Values()
	if len(values) != 19 || values[10] != "最终回答" || values[13] != "judge-model" || values[15] != "系统" || values[17] != "模型返回" {
		t.Fatalf("values = %#v", values)
	}
}

func TestEvaluationItemInsertBindsInputs(t *testing.T) {
	params := insertEvaluationTaskItemParams{
		UUID: "item", ExternalID: "evalitem_test", OrganizationUUID: "org", WorkspaceUUID: "ws", TaskUUID: "task", TaskExternalID: "evaltask_test",
		CaseExternalID: "evalcase_test", Round: 1, Position: 1, Query: "看图",
		Inputs: []byte(`[{"type":"image","source":{"type":"file","file_id":"file_demo"}}]`), CreatedAt: time.Date(2026, 11, 11, 7, 0, 0, 0, time.UTC),
	}
	bound := buildEvaluationTaskMapperInsertItem(yourbatis.DialectPostgres, params)
	if !strings.Contains(bound.SQL, "acceptance, inputs, status") || !strings.Contains(bound.SQL, "COALESCE(CAST(") {
		t.Fatalf("SQL = %s", bound.SQL)
	}
	found := false
	for _, value := range bound.Values() {
		if reflect.DeepEqual(value, params.Inputs) {
			found = true
		}
	}
	if !found {
		t.Fatalf("values = %#v", bound.Values())
	}
}

func TestEvaluationRetryStatementsScopeRound(t *testing.T) {
	now := time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC)
	params := evaluationRoundParams{OrganizationUUID: "org", WorkspaceUUID: "ws", TaskUUID: "task", Round: 1, UpdatedAt: now}
	deleted := buildEvaluationTaskMapperDeleteFailedScores(yourbatis.DialectPostgres, params)
	for _, fragment := range []string{"DELETE FROM evaluation_task_scores", "s.status = 'failed'", "i.round", "i.status = 'completed'"} {
		if !strings.Contains(deleted.SQL, fragment) {
			t.Fatalf("delete SQL missing %q: %s", fragment, deleted.SQL)
		}
	}
	if want := []any{"org", "ws", "task", "org", "ws", "task", 1}; !reflect.DeepEqual(deleted.Values(), want) {
		t.Fatalf("delete values = %#v", deleted.Values())
	}
	restarted := buildEvaluationTaskMapperRestartTask(yourbatis.DialectPostgres, params)
	if !strings.Contains(restarted.SQL, "status IN ('completed', 'failed')") || !strings.Contains(restarted.SQL, "uuid = ") {
		t.Fatalf("restart SQL = %s", restarted.SQL)
	}
	if want := []any{now, "org", "ws", "task"}; !reflect.DeepEqual(restarted.Values(), want) {
		t.Fatalf("restart values = %#v", restarted.Values())
	}
}

func TestEvaluationSweepStatementsFilterRunningWork(t *testing.T) {
	cutoff := time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC)
	tasks := buildEvaluationTaskMapperListSettleableTasks(yourbatis.DialectPostgres, cutoff, 100)
	for _, fragment := range []string{"t.status = 'running'", "NOT EXISTS", "i.status IN ('pending', 'running')", "i.task_uuid = t.uuid", "LIMIT"} {
		if !strings.Contains(tasks.SQL, fragment) {
			t.Fatalf("tasks SQL missing %q: %s", fragment, tasks.SQL)
		}
	}
	if want := []any{cutoff, 100}; !reflect.DeepEqual(tasks.Values(), want) {
		t.Fatalf("tasks values = %#v", tasks.Values())
	}
	items := buildEvaluationTaskMapperListStaleRunningItems(yourbatis.DialectPostgres, cutoff, 50)
	for _, fragment := range []string{"status = 'running'", "session_external_id <> ''", "ORDER BY updated_at ASC"} {
		if !strings.Contains(items.SQL, fragment) {
			t.Fatalf("items SQL missing %q: %s", fragment, items.SQL)
		}
	}
	if want := []any{cutoff, 50}; !reflect.DeepEqual(items.Values(), want) {
		t.Fatalf("items values = %#v", items.Values())
	}
	touched := buildEvaluationTaskMapperTouchRunningItem(yourbatis.DialectPostgres, evaluationItemStatusParams{UUID: "item", OrganizationUUID: "org", WorkspaceUUID: "ws", UpdatedAt: cutoff})
	if !strings.Contains(touched.SQL, "status = 'running'") || !reflect.DeepEqual(touched.Values(), []any{cutoff, "item", "org", "ws"}) {
		t.Fatalf("touch SQL = %s values = %#v", touched.SQL, touched.Values())
	}
}

func TestEvaluationDatasetInsertBindsTenant(t *testing.T) {
	now := time.Date(2026, 11, 3, 7, 0, 0, 0, time.UTC)
	params := insertEvaluationDatasetParams{
		UUID:             "00000000-0000-4000-8000-000000000010",
		ExternalID:       "evalds_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001",
		WorkspaceUUID:    "00000000-0000-4000-8000-000000000002",
		Name:             "销售回归",
		Description:      "发布前",
		CreatedAt:        now,
	}
	bound := buildEvaluationDatasetMapperInsertDataset(yourbatis.DialectPostgres, params)
	if evaluationDatasetMapperInsertDatasetStatement.ID != "EvaluationDatasetMapper.InsertDataset" || evaluationDatasetMapperInsertDatasetStatement.Kind != yourbatis.StatementInsert {
		t.Fatalf("statement = %+v", evaluationDatasetMapperInsertDatasetStatement)
	}
	if strings.Contains(bound.SQL, "#{") || !strings.Contains(bound.SQL, "workspace_uuid") || !strings.Contains(bound.SQL, "organization_uuid") {
		t.Fatalf("SQL = %s", bound.SQL)
	}
	want := []any{params.UUID, params.ExternalID, params.OrganizationUUID, params.WorkspaceUUID, params.Name, params.Description, now, now}
	if !reflect.DeepEqual(bound.Values(), want) {
		t.Fatalf("values = %#v", bound.Values())
	}
}

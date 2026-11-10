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

func TestEvaluationScoreInsertBindsAnswer(t *testing.T) {
	now := time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC)
	params := insertEvaluationTaskScoreParams{
		UUID: "score", ExternalID: "evalscore_test", OrganizationUUID: "org", WorkspaceUUID: "ws", TaskUUID: "task", ItemUUID: "item",
		Status: "completed", Score: 80, Passed: true, Explanation: "ok", Answer: "最终回答", Dimensions: []byte(`[]`), CreatedAt: now,
	}
	bound := buildEvaluationTaskMapperInsertScore(yourbatis.DialectPostgres, params)
	if !strings.Contains(bound.SQL, "explanation, answer, dimensions") || !strings.Contains(bound.SQL, "RETURNING") {
		t.Fatalf("SQL = %s", bound.SQL)
	}
	values := bound.Values()
	if len(values) != 14 || values[10] != "最终回答" {
		t.Fatalf("values = %#v", values)
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

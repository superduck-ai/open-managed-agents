package db

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

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

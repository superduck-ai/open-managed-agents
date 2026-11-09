package evaluations

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionTurnsStartAtEachUserMessage(t *testing.T) {
	events := []db.SessionEvent{
		{EventType: "agent.message", Payload: json.RawMessage(`{"text":"开头不算一轮"}`)},
		{EventType: "user.message", Payload: json.RawMessage(`{"content":[{"type":"text","text":"第一问"}]}`)},
		{EventType: "agent.tool_use", Payload: json.RawMessage(`{"name":"lookup"}`)},
		{EventType: "agent.message", Payload: json.RawMessage(`{"content":[{"type":"text","text":"第一答"}]}`)},
		{EventType: "agent.message", Payload: json.RawMessage(`{"text":"补充"}`)},
		{EventType: "user.message", Payload: json.RawMessage(`{"text":"第二问"}`)},
	}
	turns := sessionTurns(events)
	if len(turns) != 2 || turns[0].Index != 1 || turns[0].Query != "第一问" || turns[0].Answer != "第一答\n补充" || turns[1].Query != "第二问" || turns[1].Answer != "" {
		t.Fatalf("turns = %+v", turns)
	}
	selected, err := selectedSessionTurns(turns, []int{2, 2})
	if err != nil || len(selected) != 1 || selected[0].Query != "第二问" {
		t.Fatalf("selected = %+v err=%v", selected, err)
	}
	if _, err := selectedSessionTurns(turns, []int{3}); err == nil {
		t.Fatal("expected unknown turn to fail")
	}
}

func TestParseCaseDocumentAcceptsJSONAndCSV(t *testing.T) {
	parsed, err := parseCaseDocument(`[
	  {"query":"汇总销售额","expectedAnswer":"按月列出"},
	  {"query":"缺数据时怎么说","expected_answer":"说明缺失","expected_tool_trace":["read"]}
	]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 2 || parsed[0].ExpectedAnswer != "按月列出" || string(parsed[1].ExpectedToolTrace) != `["read"]` {
		t.Fatalf("parsed = %+v", parsed)
	}
	csvCases, err := parseCaseDocument("query,expectedAnswer\n边界输入,拒绝空文件\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(csvCases) != 1 || csvCases[0].Query != "边界输入" || csvCases[0].ExpectedAnswer != "拒绝空文件" {
		t.Fatalf("csv = %+v", csvCases)
	}
}

func TestRubricGenerateRejectsEmptyInput(t *testing.T) {
	if rubricGenerateReady(rubricWriteRequest{}) {
		t.Fatal("expected a goal or a sample")
	}
}

func TestRubricGenerateAcceptsGoalOrSample(t *testing.T) {
	if !rubricGenerateReady(rubricWriteRequest{Goal: "判断是否解决问题"}) {
		t.Fatal("expected a goal to be enough")
	}
	if !rubricGenerateReady(rubricWriteRequest{DatasetID: "evalds_1"}) {
		t.Fatal("expected a dataset to be enough")
	}
	if !rubricGenerateReady(rubricWriteRequest{Negative: "编造了退款政策"}) {
		t.Fatal("expected a negative example to be enough")
	}
}

func TestParseCaseDocumentRejectsMissingQuery(t *testing.T) {
	if _, err := parseCaseDocument(`[{"expectedAnswer":"只有答案"}]`); err == nil {
		t.Fatal("expected query to be required")
	}
}

func TestParseCaseDocumentRejectsInvalidCSVPolarity(t *testing.T) {
	if _, err := parseCaseDocument("query,polarity\n汇总销售额,maybe\n"); err == nil {
		t.Fatal("expected polarity to be rejected")
	}
}

func TestParseCaseDocumentReadsOptionalCSVColumns(t *testing.T) {
	parsed, err := parseCaseDocument("query,expected_answer,expected_tool_trace,polarity,acceptance\n汇总销售额,按渠道列出,\"[\"\"lookup_sales\"\"]\",positive,包含合计\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 || parsed[0].Polarity != "positive" || parsed[0].Acceptance != "包含合计" || string(parsed[0].ExpectedToolTrace) != `["lookup_sales"]` {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestSummarizeRoundExcludesSystemFailures(t *testing.T) {
	items := []db.EvaluationTaskItem{
		{UUID: "a", Round: 1, Status: "completed"},
		{UUID: "b", Round: 1, Status: "completed"},
		{UUID: "c", Round: 1, Status: "failed"},
	}
	scores := []db.EvaluationTaskScore{
		{ItemUUID: "a", Status: "completed", Score: 80, Passed: true},
		{ItemUUID: "b", Status: "failed"},
	}
	summary := SummarizeRound(items, scores, 1)
	if summary.Total != 3 || summary.Scored != 1 || summary.Passed != 1 || summary.Exceptions != 2 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.Score == nil || *summary.Score != 80 || summary.PassRate == nil || *summary.PassRate != 1 {
		t.Fatalf("rates = %+v", summary)
	}
}

func TestParseScoreUsesThresholdWhenPassedIsOmitted(t *testing.T) {
	parsed, err := parseScoreText("结果：{\"score\":59,\"explanation\":\"漏了月份\",\"dimensions\":[{\"name\":\"完整性\",\"score\":40}]}", 60)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Passed || parsed.Score != 59 || parsed.Explanation == "" || !json.Valid(parsed.Dimensions) {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestTerminalStatusPrefersFailureOverEndTurn(t *testing.T) {
	status, _, ok := terminalStatus([]db.SessionEvent{
		{EventType: "session.status_idle", Payload: json.RawMessage(`{"stop_reason":{"type":"end_turn"}}`)},
		{EventType: "session.status_terminated", Payload: json.RawMessage(`{}`)},
	})
	if !ok || status != "failed" {
		t.Fatalf("status = %s ok=%v", status, ok)
	}
}

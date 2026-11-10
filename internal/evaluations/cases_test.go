package evaluations

import (
	"encoding/json"
	"errors"
	"strings"
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

func TestNormalizeNameCountsCharacters(t *testing.T) {
	name := strings.Repeat("测", 255)
	if _, err := normalizeName(name); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeName(name + "测"); err == nil {
		t.Fatal("expected a 256-character name to be rejected")
	}
}

func TestParseCaseDocumentReadsMultilineJSONL(t *testing.T) {
	parsed, err := parseCaseDocument("{\"query\":\"第一问\"}\n{\"query\":\"第二问\",\"expectedAnswer\":\"答案\"}\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 2 || parsed[0].Query != "第一问" || parsed[1].Query != "第二问" || parsed[1].ExpectedAnswer != "答案" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestParseCaseDocumentAcceptsCaseEnvelope(t *testing.T) {
	parsed, err := parseCaseDocument("{\n  \"cases\": [{\"query\": \"汇总销售额\", \"expectedAnswer\": \"按月列出\"}]\n}")
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 || parsed[0].Query != "汇总销售额" || parsed[0].ExpectedAnswer != "按月列出" {
		t.Fatalf("parsed = %+v", parsed)
	}
	single, err := parseCaseDocument(`{"query":"一条 JSONL"}`)
	if err != nil || len(single) != 1 || single[0].Query != "一条 JSONL" {
		t.Fatalf("jsonl = %+v err=%v", single, err)
	}
}

func TestCaseUpdateKeepsTraceWhenOmitted(t *testing.T) {
	var body caseUpdateRequest
	if err := json.Unmarshal([]byte(`{"query":"改过的问题"}`), &body); err != nil {
		t.Fatal(err)
	}
	if !body.keepsTrace() {
		t.Fatal("expected omitted trace to be preserved")
	}
	kept := json.RawMessage(`["lookup_sales"]`)
	body.ExpectedToolTrace = &kept
	draft, err := body.draft()
	if err != nil || string(draft.ExpectedToolTrace) != `["lookup_sales"]` || draft.Query != "改过的问题" {
		t.Fatalf("draft = %+v err=%v", draft, err)
	}
}

func TestSessionEventCursorStopsAtPageLimit(t *testing.T) {
	cursor, err := nextSessionEventCursor(0, true, "evt_1")
	if err != nil || cursor == nil || cursor.ExternalID != "evt_1" {
		t.Fatalf("cursor = %+v err=%v", cursor, err)
	}
	if _, err := nextSessionEventCursor(maxSessionEventPages-1, true, "evt_last"); !errors.Is(err, errSessionTooLarge) {
		t.Fatalf("err = %v", err)
	}
	done, err := nextSessionEventCursor(3, false, "evt_3")
	if err != nil || done != nil {
		t.Fatalf("done = %+v err=%v", done, err)
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

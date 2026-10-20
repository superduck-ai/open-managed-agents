package db

import (
	"encoding/json"
	"testing"
)

func TestSessionUsageRecordUnknownAndCache(t *testing.T) {
	for _, payload := range []string{`{`, `{}`, `{"billing":{"list_cost":"-2","currency":"USD"}}`, `{"billing":{"list_cost":"2","currency":"EUR"}}`} {
		record := sessionUsageRecord(SessionEvent{Payload: json.RawMessage(payload)})
		if record.ModelCents != nil || record.Complete {
			t.Fatalf("未知计量被转换为已确认费用：%+v", record)
		}
	}
	payload := `{"model":"priced-model","model_usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":30,"cache_creation_input_tokens":40,"cache_creation":{"ephemeral_5m_input_tokens":15,"ephemeral_1h_input_tokens":25},"server_tool_use":{"web_search_requests":3}},"billing":{"list_cost":"7","currency":"USD"}}`
	record := sessionUsageRecord(SessionEvent{ExternalID: "event", Payload: json.RawMessage(payload)})
	if !record.Complete || record.ModelCents == nil || *record.ModelCents != 7 || record.SearchCents != 3 || record.CacheWriteTokens != 40 || record.CacheWrite5mTokens != 15 || record.CacheWrite1hTokens != 25 {
		t.Fatalf("缓存或费用映射错误：%+v", record)
	}
}

package sessions

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
)

func TestDecodeSessionUsageEventRejectsInvalidPayload(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `{}`, `{"Type":"session.usage","usage":{}}`,
		`{"type":"agent.message","Type":"session.usage","usage":{}}`,
		`{"type":"session.usage"}`, `{"type":"session.usage","usage":null}`,
		`{"type":"session.usage","usage":[]}`, `{"type":"session.usage","usage":{}} {}`,
		`{"type":"session.usage","usage":{"list_cost":{"amount":"-1","Amount":"1","currency":"USD"}}}`,
		`{"type":"session.usage","usage":{"cache_creation":null,"Cache_Creation":{}}}`,
		`{"type":"session.usage","usage":{"server_tool_use":null,"Server_Tool_Use":{}}}`,
		`{"type":"session.usage","usage":{"list_cost":null,"List_Cost":{"amount":"1","currency":"USD"}}}`,
		`{"type":"session.usage","usage":{},"budget":{"type":"other","Type":"limit","max_list_cost":{"amount":"1","currency":"USD"}}}`,
	} {
		t.Run(raw, func(t *testing.T) { assertInvalidSessionUsage(t, raw) })
	}
	for _, field := range []string{"thread_id", "session_thread_id", "owner_session_thread_id", "_owner_session_thread_id"} {
		for _, value := range []string{`null`, `"sthr_child"`} {
			assertInvalidSessionUsage(t, `{"type":"session.usage","usage":{},"`+field+`":`+value+`}`)
		}
	}
}

func TestDecodeSessionUsageEventRejectsInvalidMeasurements(t *testing.T) {
	for _, field := range []string{"input_tokens", "output_tokens", "cache_read_input_tokens"} {
		for _, value := range []string{`null`, `-1`, `0.5`, `2147483648`, `"private-value"`, `true`, `{}`} {
			assertInvalidSessionUsage(t, `{"type":"session.usage","usage":{"`+field+`":`+value+`}}`)
		}
	}
	for _, value := range []string{`null`, `-0.5`, `1e309`, `"private-value"`, `true`, `{}`} {
		assertInvalidSessionUsage(t, `{"type":"session.usage","usage":{"active_seconds":`+value+`}}`)
	}
	for _, field := range []string{"cache_creation", "server_tool_use", "list_cost"} {
		for _, value := range []string{`null`, `[]`, `0`, `"private-value"`} {
			assertInvalidSessionUsage(t, `{"type":"session.usage","usage":{"`+field+`":`+value+`}}`)
		}
	}
	for _, measurement := range []string{
		`"cache_creation":{"ephemeral_1h_input_tokens":null}`,
		`"cache_creation":{"ephemeral_5m_input_tokens":-1}`,
		`"server_tool_use":{"web_search_requests":0.5}`,
		`"server_tool_use":{"web_fetch_requests":2147483648}`,
	} {
		assertInvalidSessionUsage(t, `{"type":"session.usage","usage":{`+measurement+`}}`)
	}
}

func TestDecodeSessionUsageEventRejectsInvalidMoneyAndBudget(t *testing.T) {
	for _, money := range []string{
		`{}`, `{"amount":"0"}`, `{"currency":"USD"}`,
		`{"amount":1,"currency":"USD"}`, `{"amount":"1","currency":"EUR"}`,
		`{"amount":"1","currency":"usd"}`, `{"amount":null,"currency":"USD"}`,
	} {
		assertInvalidSessionUsage(t, `{"type":"session.usage","usage":{"list_cost":`+money+`}}`)
	}
	for _, amount := range []string{"", "00", "01", "-1", "+1", "1.0", "1e2", " 1", "١"} {
		encoded, err := json.Marshal(amount)
		if err != nil {
			t.Fatal(err)
		}
		assertInvalidSessionUsage(t, `{"type":"session.usage","usage":{"list_cost":{"amount":`+string(encoded)+`,"currency":"USD"}}}`)
	}
	for _, budget := range []string{
		`[]`, `0`, `"private-value"`, `{}`, `{"type":"limit"}`,
		`{"type":"limit","max_list_cost":null}`,
		`{"type":"other","max_list_cost":{"amount":"1","currency":"USD"}}`,
		`{"type":"limit","max_list_cost":{"amount":"0","currency":"USD"}}`,
		`{"type":"limit","max_list_cost":{"amount":"01","currency":"USD"}}`,
	} {
		assertInvalidSessionUsage(t, `{"type":"session.usage","usage":{},"budget":`+budget+`}`)
	}
}

func TestDecodeSessionUsageEventPreservesSnapshot(t *testing.T) {
	for _, usage := range []string{
		`{}`,
		`{ "input_tokens": 0, "output_tokens":2147483647,"cache_read_input_tokens":0,"active_seconds":0.125 }`,
		`{"cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":2147483647},"server_tool_use":{"web_search_requests":0,"web_fetch_requests":0}}`,
		`{"cache_creation":{},"server_tool_use":{},"list_cost":{"amount":"0","currency":"USD"}}`,
		`{"list_cost":{"amount":"123456789012345678901234567890","currency":"USD","future":null},"future":{"huge":9007199254740993,"unknown":null}}`,
		`{"input_tokens":0,"INPUT_TOKENS":"unknown","CACHE_CREATION":null,"list_cost":{"amount":"0","Amount":"unknown","currency":"USD","Currency":"unknown"}}`,
	} {
		for _, budget := range []string{"", `,"budget":null`, ",\"budget\": \n null \t", `,"budget":{"type":"limit","max_list_cost":{"amount":"1","currency":"USD"}}`} {
			raw := `{"type":"session.usage","session_id":"cse_ingress","usage":` + usage + budget + `}`
			got, err := decodeSessionUsageEvent(json.RawMessage(raw))
			if err != nil {
				t.Fatalf("valid snapshot rejected: %v", err)
			}
			if string(got) != usage {
				t.Fatalf("snapshot changed: got %s, want %s", got, usage)
			}
		}
	}
}

func assertInvalidSessionUsage(t *testing.T, raw string) {
	t.Helper()
	usage, err := decodeSessionUsageEvent(json.RawMessage(raw))
	if !errors.Is(err, codesessions.ErrProtocol) || usage != nil {
		t.Fatalf("invalid event accepted: usage=%s error=%v", usage, err)
	}
	if err.Error() != "code session protocol error: invalid session.usage payload" {
		t.Fatalf("error exposes variable diagnostic content: %v", err)
	}
}

package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func nativeHit(seq int64) eventHit {
	return eventHit{Seq: seq, Headers: []string{`{"version":4,"id":"native"}`}, Contents: []string{fmt.Sprintf(`{"seq":%d,"time":100,"type":"turn/start","data":{}}`, seq)}}
}

func TestNativePageRejectsConflictsAndGapsSeparately(t *testing.T) {
	conflict := nativeHit(0)
	conflict.Contents = append(conflict.Contents, `{"seq":0,"time":100,"type":"turn/end","data":{}}`)
	headerConflict := nativeHit(0)
	headerConflict.Headers = append(headerConflict.Headers, `{"version":4,"id":"other"}`)
	for _, test := range []struct {
		name  string
		hits  []eventHit
		cause error
	}{
		{"first gap", []eventHit{nativeHit(1)}, ErrHistoryGap},
		{"middle gap", []eventHit{nativeHit(0), nativeHit(2)}, ErrHistoryGap},
		{"content conflict", []eventHit{conflict}, ErrEventConflict},
		{"header conflict", []eventHit{headerConflict}, ErrEventConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := nativePage("native", -1, test.hits); !errors.Is(err, test.cause) {
				t.Fatalf("error %v, want %v", err, test.cause)
			}
		})
	}
	for _, hit := range []eventHit{{}, {Seq: 0, Headers: []string{`{}`}, Contents: []string{`{}`}}, {Seq: 0, Headers: []string{`{"version":4,"id":"other"}`}, Contents: nativeHit(0).Contents}} {
		if _, err := nativePage("native", -1, []eventHit{hit}); err == nil {
			t.Fatal("accepted malformed event")
		}
	}
}

func TestNativePageAcceptsEquivalentReplaysAndPreservesOriginal(t *testing.T) {
	hit := nativeHit(0)
	original := `{"seq":0,"time":100,"type":"turn/start","data":{"n":9007199254740993,"huge":1e400}}`
	hit.Contents = []string{original, `{ "data":{"huge":1e400,"n":9007199254740993},"type":"turn/start","time":100,"seq":0}`}
	hit.Headers = append(hit.Headers, `{ "id":"native", "version":4 }`)
	page, err := nativePage("native", -1, []eventHit{hit})
	if err != nil {
		t.Fatal(err)
	}
	if string(page.Events[0]) != original {
		t.Fatal("original event rewritten")
	}
	hit.Contents[1] = strings.ReplaceAll(hit.Contents[1], "9007199254740993", "9007199254740992")
	if _, err := nativePage("native", -1, []eventHit{hit}); !errors.Is(err, ErrEventConflict) {
		t.Fatalf("lost numeric conflict: %v", err)
	}
}

func TestNativePageTreatsMissingHeaderDelegationDepthAsRoot(t *testing.T) {
	live := `{"version":4,"id":"native","createdAt":100,"cwd":"/home/user","isSeeded":false}`
	for _, test := range []struct {
		name     string
		headers  []string
		contents []string
	}{
		{"missing versus nested depth", []string{live, `{"version":4,"id":"native","createdAt":100,"cwd":"/home/user","isSeeded":false,"delegationDepth":1}`}, nil},
		{"different restored depths", []string{`{"version":4,"id":"native","delegationDepth":0}`, `{"version":4,"id":"native","delegationDepth":2}`}, nil},
		{"content is not normalized", nil, []string{`{"seq":0,"time":100,"type":"turn/start","data":{}}`, `{"seq":0,"time":100,"type":"turn/start","data":{"delegationDepth":0}}`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			hit := nativeHit(0)
			if test.headers != nil {
				hit.Headers = test.headers
			}
			if test.contents != nil {
				hit.Contents = test.contents
			}
			if _, err := nativePage("native", -1, []eventHit{hit}); !errors.Is(err, ErrEventConflict) {
				t.Fatalf("error %v, want %v", err, ErrEventConflict)
			}
		})
	}
	restored := `{"version":4,"id":"native","createdAt":100,"cwd":"/home/user","isSeeded":false,"delegationDepth":0}`
	for _, headers := range [][]string{{live, restored}, {restored, live}} {
		hit := nativeHit(0)
		hit.Headers = headers
		page, err := nativePage("native", -1, []eventHit{hit})
		if err != nil {
			t.Fatal(err)
		}
		if string(page.Header) != headers[0] || len(page.Events) != 1 {
			t.Fatalf("invalid page: %+v", page)
		}
	}
}

func TestNativePageCursorAndLookahead(t *testing.T) {
	hits := make([]eventHit, trajectoryPageSize+1)
	for i := range hits {
		hits[i] = nativeHit(int64(i + 8))
	}
	page, err := nativePage("native", 7, hits)
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || len(page.Events) != trajectoryPageSize || page.NextSeq != 507 {
		t.Fatalf("invalid page: %+v", page)
	}
	next, err := nativePage("native", page.NextSeq, hits[trajectoryPageSize:])
	if err != nil {
		t.Fatal(err)
	}
	if next.HasMore || len(next.Events) != 1 || next.NextSeq != 508 {
		t.Fatalf("invalid final page: %+v", next)
	}
}

func TestNativeOpenObserveQueryUsesTenantScopeAndPreservesEvents(t *testing.T) {
	header := `{"version":4,"id":"native","createdAt":100}`
	event := `{"seq":0,"time":100,"type":"turn/start","data":{"turn":1},"ignorable":true}`
	var sql string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oma/_search" || r.URL.Query().Get("type") != "logs" {
			t.Errorf("unexpected path %s", r.URL)
		}
		username, password, _ := r.BasicAuth()
		if username != "reader" || password != "query-secret" {
			t.Error("query credentials not used")
		}
		var request struct {
			Query searchQuery `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		sql = request.Query.SQL
		if request.Query.Size != trajectoryPageSize+1 {
			t.Error("detail query must fetch one extra unique event")
		}
		if strings.Contains(request.Query.SQL, "OFFSET") {
			t.Error("detail query must use the sequence cursor")
		}
		if request.Query.StartTime != 1 {
			t.Error("detail query must include history before list window")
		}
		_ = json.NewEncoder(w).Encode(struct {
			Hits []eventHit `json:"hits"`
		}{Hits: []eventHit{{Seq: 0, Headers: []string{header}, Contents: []string{event}}}})
	}))
	defer server.Close()
	backend := NewOpenObserve(config.OpenObserveConfig{BaseURL: server.URL, Organization: "oma", LogsStream: "oma_dsh", Query: config.BackendQueryConfig{Username: "reader", Password: "query-secret"}}, server.Client())
	scope := Scope{OrganizationUUID: "org'1", WorkspaceUUID: "ws_1", SessionID: "session_1"}
	page, err := backend.Page(context.Background(), scope, Window{Start: time.Unix(0, 0), End: time.Now()}, "native", -1)
	if err != nil {
		t.Fatal(err)
	}
	if string(page.Header) != header || len(page.Events) != 1 || string(page.Events[0]) != event || page.NextSeq != 0 {
		t.Fatalf("native event changed: %+v", page)
	}
	for _, condition := range []string{"oma_organization_uuid = 'org''1'", "oma_workspace_uuid = 'ws_1'", "oma_session_id = 'session_1'", "dsh_session_id = 'native'", "GROUP BY dsh_session_id, CAST(dsh_event_seq AS BIGINT)", "ARRAY_AGG(DISTINCT dsh_header) AS headers", "ARRAY_AGG(DISTINCT content) AS contents", "LIMIT 501"} {
		if !strings.Contains(sql, condition) {
			t.Errorf("missing scoped/deduplicated query %s: %s", condition, sql)
		}
	}
}

func TestNativeQueryRejectsMissingScopeAndUnsafeStream(t *testing.T) {
	for _, cfg := range []config.OpenObserveConfig{{LogsStream: "oma_dsh"}, {LogsStream: "x; DROP TABLE y"}} {
		backend := NewOpenObserve(cfg, nil)
		if _, err := backend.scopedQuery(Scope{}); err == nil {
			t.Fatal("accepted missing scope")
		}
	}
}

func TestTrajectoryListReturnsServerPagination(t *testing.T) {
	for _, count := range []int{0, trajectoryListSize, trajectoryListSize + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var sql string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Query searchQuery `json:"query"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				sql = request.Query.SQL
				hits := make([]SessionActivity, count)
				_ = json.NewEncoder(w).Encode(struct {
					Hits []SessionActivity `json:"hits"`
				}{hits})
			}))
			defer server.Close()
			backend := NewOpenObserve(config.OpenObserveConfig{BaseURL: server.URL, Organization: "oma", LogsStream: "oma_dsh"}, server.Client())
			list, err := backend.List(context.Background(), Scope{OrganizationUUID: "org", WorkspaceUUID: "ws", AgentID: "agent'1"}, Window{}, ListQuery{Offset: 17, Sort: SortRecent})
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range []string{"oma_agent_id = 'agent''1'", "oma_session_id IS NOT NULL", "GROUP BY oma_session_id", "ORDER BY last_event_at DESC, session_id ASC", "LIMIT 51 OFFSET 17"} {
				if !strings.Contains(sql, fragment) {
					t.Errorf("session list query missing %s: %s", fragment, sql)
				}
			}
			if strings.Contains(sql, "GROUP BY dsh_session_id") {
				t.Error("session list must not group by native session")
			}
			if len(list.Items) != min(count, trajectoryListSize) {
				t.Fatal("invalid list size")
			}
			if count > trajectoryListSize {
				if list.NextOffset == nil || *list.NextOffset != 67 {
					t.Fatalf("invalid next offset: %+v", list)
				}
			} else if list.NextOffset != nil {
				t.Fatal("unexpected next page")
			}
		})
	}
}

func TestSearchTreatsOnlyMissingStructuredSchemaAsEmpty(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		empty  bool
		err    bool
	}{
		{"missing stream", http.StatusBadRequest, `{"code":20002,"message":"stream not found"}`, true, false},
		{"unknown structured field", http.StatusBadRequest, `{"code":20004,"message":"unknown field 'dsh_turn'"}`, true, false},
		{"unknown structured field in another format", http.StatusBadRequest, `{"code":20004,"message":"Search SQL error: Unknown field \"gen_ai_request_model\" in table"}`, true, false},
		{"unknown unrelated field", http.StatusBadRequest, `{"code":20004,"message":"unknown field 'secret_column'"}`, false, true},
		{"other sql error", http.StatusBadRequest, `{"code":20004,"message":"syntax error near dsh_turn"}`, false, true},
		{"non JSON error", http.StatusInternalServerError, `upstream failed`, false, true},
		{"success", http.StatusOK, `{"hits":[{"n":1}]}`, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			backend := NewOpenObserve(config.OpenObserveConfig{BaseURL: server.URL, Organization: "oma", LogsStream: "oma_dsh"}, server.Client())
			body, err := backend.searchRaw(context.Background(), Window{Start: time.UnixMilli(0), End: time.UnixMilli(1)}, "SELECT 1", 1)
			switch {
			case test.err:
				if err == nil {
					t.Fatalf("body = %s, want error", body)
				}
			case err != nil:
				t.Fatal(err)
			case test.empty && string(body) != string(emptySearch):
				t.Fatalf("body = %s, want empty search", body)
			case !test.empty && string(body) != test.body:
				t.Fatalf("body = %s", body)
			}
		})
	}
}

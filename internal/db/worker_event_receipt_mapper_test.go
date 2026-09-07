package db

import (
	"reflect"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestWorkerEventReceiptMapperScopesAndBlockOrder(t *testing.T) {
	find := buildWorkerEventReceiptMapperFind(yourbatis.DialectPostgres, "workspace", "worker", 7, "source-event")
	assertMapperSQLContains(t, find, "workspace_uuid = $1 AND code_session_uuid = $2")
	assertMapperSQLContains(t, find, "worker_epoch = $3 AND event_external_id = $4")
	if !reflect.DeepEqual(find.Values(), []any{"workspace", "worker", int64(7), "source-event"}) {
		t.Fatalf("receipt lookup arguments: %v", find.Values())
	}
	latest := buildWorkerEventReceiptMapperLatestBlockIndices(yourbatis.DialectPostgres, "workspace", "worker", 7, []string{"request-a", "request-b"})
	assertMapperSQLContains(t, latest, "PARTITION BY model_request_id ORDER BY id DESC")
	assertMapperSQLContains(t, latest, "block_index IS NOT NULL AND model_request_id IN ( $4 , $5 )")
	if !reflect.DeepEqual(latest.Values(), []any{"workspace", "worker", int64(7), "request-a", "request-b"}) {
		t.Fatalf("active request lookup arguments: %v", latest.Values())
	}
	for _, index := range []*int{nil, new(1)} {
		receipt := WorkerEventReceipt{EventID: "source", Scope: "scope", RequestID: "request", Hash: "digest", BlockIndex: index}
		insert := buildWorkerEventReceiptMapperInsert(yourbatis.DialectPostgres, "workspace", "worker", 7, receipt)
		if !reflect.DeepEqual(insert.Values(), []any{"workspace", "worker", int64(7), "source", "scope", "request", "digest", index}) {
			t.Fatalf("receipt insert arguments: %v", insert.Values())
		}
	}
	remove := buildWorkerEventReceiptMapperDeleteBySession(yourbatis.DialectPostgres, "workspace", "session")
	assertMapperSQLContains(t, remove, "DELETE FROM code_session_worker_event_receipts")
	assertMapperSQLContains(t, remove, "session_uuid = $3")
	if !reflect.DeepEqual(remove.Values(), []any{"workspace", "workspace", "session"}) {
		t.Fatalf("receipt cleanup arguments: %v", remove.Values())
	}
}

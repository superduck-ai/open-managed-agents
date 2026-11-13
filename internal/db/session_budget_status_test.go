package db

import (
	"encoding/json"
	"testing"
)

func TestBudgetAllowsStatus(t *testing.T) {
	for _, tc := range []struct {
		name, kind, thread, payload string
		mirror, want, wantError     bool
	}{
		{name: "迟到主状态", kind: "session.status_running"},
		{name: "迟到主线程", kind: "session.thread_status_running", thread: "primary"},
		{name: "普通结束", kind: "session.status_idle", payload: `{"stop_reason":{"type":"end_turn"}}`},
		{name: "工具暂停", kind: "session.status_idle", payload: `{"stop_reason":{"type":"requires_action"}}`},
		{name: "非法状态", kind: "session.status_idle", payload: `{`, wantError: true},
		{name: "预算暂停", kind: "session.status_idle", payload: `{"stop_reason":{"type":"budget_reached"}}`, want: true},
		{name: "子线程结束", kind: "session.thread_status_idle", thread: "child", want: true},
		{name: "线程镜像", kind: "session.thread_status_idle", mirror: true, want: true},
		{name: "在途结算", kind: "span.model_request_end", want: true},
		{name: "删除", kind: "session.deleted", want: true},
		{name: "终止", kind: "session.status_terminated", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := budgetAllowsStatus(SessionThread{ExternalID: "primary"}, SessionEvent{EventType: tc.kind, StatusThreadID: tc.thread, Payload: json.RawMessage(tc.payload), threadStatusMirror: tc.mirror})
			if got != tc.want || (err != nil) != tc.wantError {
				t.Fatalf("允许=%v, 错误=%v", got, err)
			}
		})
	}
}

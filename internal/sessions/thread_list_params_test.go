package sessions

import (
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestThreadListParamsRejectInvalidFilterAndPagination(t *testing.T) {
	for _, query := range []string{"statuses[]=unknown", "statuses[]=idle&statuses[]=unknown", "limit=1001", "page=not-a-cursor"} {
		r := httptest.NewRequest("GET", "/sessions/session/threads?"+query, nil)
		if _, err := parseThreadListParams(r, db.Session{}); err == nil {
			t.Fatalf("accepted invalid thread query %s", query)
		}
	}
}

func TestThreadListParamsBindRepeatedStatusesAndAuthorizedScope(t *testing.T) {
	now := time.Date(2026, time.October, 19, 0, 0, 0, 123456000, time.UTC)
	threadUUID := "00000000-0000-4000-8000-000000000006"
	cursor := encodeThreadCursor(db.SessionThread{UUID: threadUUID, CreatedAt: now})
	r := httptest.NewRequest("GET", "/sessions/session/threads?statuses[]=running&statuses[]=idle&limit=1&page="+url.QueryEscape(cursor)+"&workspace_uuid=untrusted", nil)
	params, err := parseThreadListParams(r, db.Session{WorkspaceUUID: "authorized-workspace", ExternalID: "authorized-session"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(params.Statuses, []string{"running", "idle"}) || params.Limit != 1 || params.Cursor == nil || !params.Cursor.CreatedAt.Equal(now) || params.Cursor.UUID != threadUUID || params.WorkspaceUUID != "authorized-workspace" || params.SessionExternalID != "authorized-session" {
		t.Fatalf("thread query binding lost: %+v", params)
	}
}

func TestThreadListParamsSingleAndOmittedFilterKeepDefaults(t *testing.T) {
	for _, test := range []struct {
		query    string
		statuses []string
	}{{"", nil}, {"statuses[]=terminated", []string{"terminated"}}} {
		params, err := parseThreadListParams(httptest.NewRequest("GET", "/sessions/session/threads?"+test.query, nil), db.Session{})
		if err != nil || params.Limit != 500 || params.Cursor != nil || !reflect.DeepEqual(params.Statuses, test.statuses) {
			t.Fatalf("thread default query changed: %+v, %v", params, err)
		}
	}
}

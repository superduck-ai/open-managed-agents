package dsh

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type transcriptDatabase struct {
	scope      db.DSHScope
	transcript db.DSHTranscript
	events     []db.DSHTranscriptRecord
	calls      int
	err        error
	readLimit  int
	readBudget int64
}

func (d *transcriptDatabase) AppendDSHTranscript(_ context.Context, scope db.DSHScope, id string, parent *string, records []db.DSHTranscriptRecord) (db.DSHTranscriptAppendResult, error) {
	d.calls++
	d.scope = scope
	d.events = records
	d.transcript = db.DSHTranscript{DSHSessionID: id, ParentDSHSessionID: parent, NextOffset: 7}
	return db.DSHTranscriptAppendResult{NextOffset: 7}, d.err
}
func (d *transcriptDatabase) ListDSHTranscripts(_ context.Context, scope db.DSHScope, _ string, _ int) ([]db.DSHTranscript, error) {
	d.calls++
	d.scope = scope
	return []db.DSHTranscript{d.transcript}, d.err
}
func (d *transcriptDatabase) ReadDSHTranscript(_ context.Context, scope db.DSHScope, _ string, from int64, limit int, budget int64) (db.DSHTranscript, []db.DSHTranscriptRecord, error) {
	d.calls++
	d.scope = scope
	d.readLimit = limit
	d.readBudget = budget
	events := make([]db.DSHTranscriptRecord, 0, limit)
	used := int64(0)
	for _, event := range d.events {
		if event.Offset >= from && len(events) < limit && used < budget {
			events = append(events, event)
			used += int64(len(event.Payload))
		}
	}
	return d.transcript, events, d.err
}
func transcriptRouter(database Database, authorized bool) http.Handler {
	router := chi.NewRouter()
	authenticate := func(_ *http.Request, _ string) (db.DSHScope, error) {
		if !authorized {
			return db.DSHScope{}, ErrUnauthenticated
		}
		return db.DSHScope{OrganizationUUID: "organization", WorkspaceUUID: "workspace", CodeSessionUUID: "code-session", Epoch: 3}, nil
	}
	NewHandler(database, authenticate, nil).RegisterRoutes(router)
	return router
}
func transcriptRequest(router http.Handler, method, path, body, version string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-ndjson")
	if version != "" {
		request.Header.Set("DSR-Protocol", version)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestTranscriptRejectsInvalidRequests(t *testing.T) {
	for _, test := range []struct{ name, body, query string }{
		{"empty", "", "0"},
		{"missing final LF", `{}`, "0"},
		{"null seq", "{\"seq\":null}\n", "0"},
		{"string seq", "{\"seq\":\"0\"}\n", "1"},
		{"fraction seq", "{\"seq\":0.1}\n", "1"},
		{"negative seq", "{\"seq\":-1}\n", "0"},
		{"unsafe seq", "{\"seq\":9007199254740992}\n", "1"},
		{"duplicate seq", "{\"seq\":0,\"seq\":0}\n", "1"},
		{"array", "[]\n", "0"},
		{"scalar", "null\n", "0"},
		{"blank", "\n", "0"},
		{"middle header", "{}\n{}\n", "0"},
		{"late header", "{}\n", "1"},
		{"wrong offset", "{\"seq\":0}\n", "2"},
		{"gap", "{}\n{\"seq\":1}\n", "0"},
		{"negative offset", "{}\n", "-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := &transcriptDatabase{}
			response := transcriptRequest(transcriptRouter(database, true), "POST", "/sessions/cse_root/transcripts/cse_root/records?from_offset="+test.query, test.body, "1")
			if response.Code != 400 || database.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, database.calls, response.Body.String())
			}
		})
	}
}

func TestTranscriptErrorsHaveDistinctRecoveryCodes(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{db.ErrWorkerEpochMismatch, 409, "epoch_superseded"},
		{db.ErrWorkerLeaseExpired, 410, "lease_expired"},
		{db.ErrDSHHeaderRequired, 409, "transcript_header_required"},
		{db.ErrDSHTranscriptGap, 409, "transcript_gap"},
		{db.ErrDSHTranscriptConflict, 409, "transcript_conflict"},
	} {
		t.Run(test.code, func(t *testing.T) {
			response := transcriptRequest(transcriptRouter(&transcriptDatabase{err: test.err}, true), "POST", "/sessions/cse_root/transcripts/cse_root/records?from_offset=8", "{\"seq\":7}\n", "1")
			if response.Code != test.status || response.Header().Get("DSR-Error") != test.code {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
			if test.code == "transcript_gap" && response.Header().Get("DSR-Next-Offset") != "7" {
				t.Fatal(response.Header())
			}
		})
	}
}

func TestTranscriptRecordsPreserveBytes(t *testing.T) {
	database := &transcriptDatabase{}
	body := " {\"version\":99, \"future\":9007199254740993} \n{\"seq\":0,\"unknown\":true}\n"
	router := transcriptRouter(database, true)
	response := transcriptRequest(router, "POST", "/sessions/cse_root/transcripts/cse_root/records?from_offset=0", body, "1")
	if response.Code != 200 || len(database.events) != 2 || database.events[1].Offset != 1 || database.scope.Epoch != 3 {
		t.Fatalf("response=%d %s records=%v", response.Code, response.Body.String(), database.events)
	}
	database.transcript.NextOffset = 2
	response = transcriptRequest(router, "GET", "/sessions/cse_root/transcripts/cse_root/records?from_offset=0", "", "1")
	if response.Body.String() != body || response.Header().Get("Content-Type") != "application/x-ndjson" || response.Header().Get("DSR-Next-Offset") != "2" {
		t.Fatalf("response=%s %v", response.Body.String(), response.Header())
	}
}

func TestTranscriptReadPaginatesByRecordCount(t *testing.T) {
	database := &transcriptDatabase{transcript: db.DSHTranscript{NextOffset: 201}}
	for offset := int64(0); offset < 201; offset++ {
		database.events = append(database.events, db.DSHTranscriptRecord{Offset: offset, Payload: []byte(`{}`)})
	}
	for _, from := range []int{0, 200, 201} {
		response := transcriptRequest(transcriptRouter(database, true), "GET", fmt.Sprintf("/sessions/cse_root/transcripts/cse_root/records?from_offset=%d", from), "", "1")
		want := min(200, 201-from)
		if response.Code != 200 || strings.Count(response.Body.String(), "\n") != want || database.readLimit != 200 {
			t.Fatalf("response=%d %s", response.Code, response.Body.String())
		}
		if (response.Header().Get("DSR-Next-From-Offset") == "200") != (from == 0) {
			t.Fatal(response.Header())
		}
	}
}

func TestTranscriptReadRejectsInvalidByteBudget(t *testing.T) {
	for _, value := range []string{"0", "abc", "-1", "01", fmt.Sprint(maxPageBytes + 1)} {
		database := &transcriptDatabase{}
		response := transcriptRequest(transcriptRouter(database, true), "GET", "/sessions/cse_root/transcripts/cse_root/records?from_offset=0&limit_bytes="+value, "", "1")
		if response.Code != 400 || database.calls != 0 {
			t.Fatalf("limit_bytes=%s status=%d calls=%d", value, response.Code, database.calls)
		}
	}
}

func TestTranscriptReadPaginatesByByteBudget(t *testing.T) {
	database := &transcriptDatabase{transcript: db.DSHTranscript{NextOffset: 10}}
	for offset := int64(0); offset < 10; offset++ {
		database.events = append(database.events, db.DSHTranscriptRecord{Offset: offset, Payload: []byte(`{"v":"xxxx"}`)})
	}
	response := transcriptRequest(transcriptRouter(database, true), "GET", "/sessions/cse_root/transcripts/cse_root/records?from_offset=0", "", "1")
	if response.Code != 200 || database.readBudget != defaultPageBytes {
		t.Fatalf("status=%d budget=%d", response.Code, database.readBudget)
	}
	response = transcriptRequest(transcriptRouter(database, true), "GET", "/sessions/cse_root/transcripts/cse_root/records?from_offset=0&limit_bytes=25", "", "1")
	if response.Code != 200 || database.readBudget != 25 || strings.Count(response.Body.String(), "\n") != 3 {
		t.Fatalf("status=%d budget=%d body=%q", response.Code, database.readBudget, response.Body.String())
	}
	if response.Header().Get("DSR-Next-From-Offset") != "3" || response.Header().Get("DSR-Next-Offset") != "10" {
		t.Fatal(response.Header())
	}
}

func TestTranscriptTransportAndScopeBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, protocol, contentType, path, body string
		authorized                              bool
		status                                  int
	}{
		{"missing protocol", "", "application/x-ndjson", "cse_root?from_offset=0", "{}\n", true, 400},
		{"invalid credential", "1", "application/x-ndjson", "cse_root?from_offset=0", "{}\n", false, 401},
		{"JSON envelope", "1", "application/json", "cse_root?from_offset=0", "{}\n", true, 400},
		{"child missing parent", "1", "application/x-ndjson", "child?from_offset=0", "{}\n", true, 400},
		{"root parent", "1", "application/x-ndjson", "cse_root?from_offset=0&parent_dsh_session_id=other", "{}\n", true, 400},
		{"self parent", "1", "application/x-ndjson", "child?from_offset=0&parent_dsh_session_id=child", "{}\n", true, 400},
		{"late parent", "1", "application/x-ndjson", "child?from_offset=1&parent_dsh_session_id=cse_root", "{\"seq\":0}\n", true, 400},
		{"line limit", "1", "application/x-ndjson", "cse_root?from_offset=0", "{\"v\":\"" + strings.Repeat("x", maxEventBytes) + "\"}\n", true, 413},
		{"body limit", "1", "application/x-ndjson", "cse_root?from_offset=0", strings.Repeat("x", maxRequestBytes+1), true, 413},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := &transcriptDatabase{}
			path := strings.Replace(test.path, "?", "/records?", 1)
			request := httptest.NewRequest("POST", "/sessions/cse_root/transcripts/"+path, strings.NewReader(test.body))
			request.Header.Set("DSR-Protocol", test.protocol)
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			transcriptRouter(database, test.authorized).ServeHTTP(response, request)
			if response.Code != test.status || database.calls != 0 {
				t.Fatalf("status=%d calls=%d", response.Code, database.calls)
			}
		})
	}
}

func TestTranscriptParentIsIndependentOfNativeFields(t *testing.T) {
	database := &transcriptDatabase{}
	response := transcriptRequest(transcriptRouter(database, true), "POST", "/sessions/cse_root/transcripts/child/records?from_offset=0&parent_dsh_session_id=cse_root", "{\"parentSession\":\"different\",\"version\":99}\n", "1")
	if response.Code != 200 || database.transcript.ParentDSHSessionID == nil || *database.transcript.ParentDSHSessionID != "cse_root" {
		t.Fatalf("%d %+v", response.Code, database.transcript)
	}
}

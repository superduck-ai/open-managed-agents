package dsh

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type transcriptDatabase struct {
	scope      db.DSHScope
	transcript db.DSHTranscript
	events     []db.DSHTranscriptEvent
	calls      int
	err        error
	readLimit  int
}

func (d *transcriptDatabase) PutDSHTranscript(_ context.Context, scope db.DSHScope, id string, parent *string, header []byte) (db.DSHTranscript, error) {
	d.calls++
	d.scope = scope
	if d.err != nil {
		return db.DSHTranscript{}, d.err
	}
	d.transcript = db.DSHTranscript{DSHSessionID: id, ParentDSHSessionID: parent, Header: header}
	return d.transcript, nil
}
func (d *transcriptDatabase) AppendDSHTranscript(_ context.Context, scope db.DSHScope, _ string, events []db.DSHTranscriptEvent) (db.DSHTranscriptAppendResult, error) {
	d.calls++
	d.scope = scope
	return db.DSHTranscriptAppendResult{NextSeq: 7}, d.err
}
func (d *transcriptDatabase) ListDSHTranscripts(_ context.Context, scope db.DSHScope, _ string, _ int) ([]db.DSHTranscript, error) {
	d.calls++
	d.scope = scope
	return []db.DSHTranscript{d.transcript}, d.err
}
func (d *transcriptDatabase) ReadDSHTranscript(_ context.Context, scope db.DSHScope, _ string, from int64, limit int) (db.DSHTranscript, []db.DSHTranscriptEvent, error) {
	d.calls++
	d.scope = scope
	d.readLimit = limit
	events := make([]db.DSHTranscriptEvent, 0, limit)
	for _, event := range d.events {
		if event.Seq >= from && len(events) < limit {
			events = append(events, event)
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
	if version != "" {
		request.Header.Set("DSR-Protocol", version)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestTranscriptRejectsInvalidRequests(t *testing.T) {
	cases := []struct {
		name, method, path, body, version string
		authorized                        bool
		status                            int
		code                              string
	}{
		{"missing protocol", "GET", "/sessions/cse_root/transcripts", "", "", true, 400, "unsupported_protocol"},
		{"unknown protocol", "GET", "/sessions/cse_root/transcripts", "", "2", true, 400, "unsupported_protocol"},
		{"invalid token", "GET", "/sessions/cse_root/transcripts", "", "1", false, 401, "unauthorized"},
		{"duplicate envelope key", "POST", "/sessions/cse_root/transcripts/cse_root/events", `{"events":[],"events":[]}`, "1", true, 400, "invalid_request"},
		{"empty batch", "POST", "/sessions/cse_root/transcripts/cse_root/events", `{"events":[]}`, "1", true, 400, "invalid_request"},
		{"gap inside batch", "POST", "/sessions/cse_root/transcripts/cse_root/events", `{"events":[{"seq":0,"type":"turn/start","data":{}},{"seq":2,"type":"turn/end","data":{}}]}`, "1", true, 400, "invalid_request"},
		{"unsafe sequence", "POST", "/sessions/cse_root/transcripts/cse_root/events", `{"events":[{"seq":9007199254740992,"type":"turn/start","data":{}}]}`, "1", true, 400, "invalid_request"},
		{"header identity", "PUT", "/sessions/cse_root/transcripts/cse_root/header", `{"header":{"version":4,"id":"different","createdAt":0,"isSeeded":false}}`, "1", true, 400, "invalid_request"},
		{"header version", "PUT", "/sessions/cse_root/transcripts/cse_root/header", `{"header":{"version":3,"id":"cse_root"}}`, "1", true, 400, "unsupported_transcript_version"},
		{"child missing parent", "PUT", "/sessions/cse_root/transcripts/child/header", `{"header":{"version":4,"id":"child","createdAt":0,"isSeeded":false}}`, "1", true, 400, "invalid_request"},
		{"negative cursor", "GET", "/sessions/cse_root/transcripts/cse_root?from_seq=-1", "", "1", true, 400, "invalid_request"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			database := &transcriptDatabase{}
			response := transcriptRequest(transcriptRouter(database, test.authorized), test.method, test.path, test.body, test.version)
			if response.Code != test.status || response.Header().Get("DSR-Error") != test.code || database.calls != 0 {
				t.Fatalf("response=%d %s calls=%d", response.Code, response.Body.String(), database.calls)
			}
		})
	}
}

func TestTranscriptErrorsHaveDistinctRecoveryCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{db.ErrWorkerEpochMismatch, 409, "epoch_superseded"},
		{db.ErrWorkerLeaseExpired, 410, "lease_expired"},
		{db.ErrDSHHeaderRequired, 409, "transcript_header_required"},
		{db.ErrDSHTranscriptGap, 409, "transcript_gap"},
		{db.ErrDSHTranscriptConflict, 409, "transcript_conflict"},
	}
	for _, test := range cases {
		t.Run(test.code, func(t *testing.T) {
			database := &transcriptDatabase{err: test.err}
			response := transcriptRequest(transcriptRouter(database, true), "POST", "/sessions/cse_root/transcripts/cse_root/events", `{"events":[{"seq":7,"type":"turn/start","data":{"turn":1}}]}`, "1")
			if response.Code != test.status || response.Header().Get("DSR-Error") != test.code {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
			if test.code == "transcript_gap" && response.Header().Get("DSR-Next-Seq") != "7" {
				t.Fatalf("missing gap watermark: %v", response.Header())
			}
		})
	}
}

func TestTranscriptHeaderAndSequenceZero(t *testing.T) {
	database := &transcriptDatabase{}
	router := transcriptRouter(database, true)
	response := transcriptRequest(router, "PUT", "/sessions/cse_root/transcripts/cse_root/header", `{"header":{"version":4,"id":"cse_root","createdAt":0,"isSeeded":false}}`, "1")
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if database.scope.Epoch != 3 || database.scope.WorkspaceUUID != "workspace" {
		t.Fatalf("scope=%+v", database.scope)
	}
	database.transcript.NextSeq = 2
	database.events = []db.DSHTranscriptEvent{{Seq: 0, Payload: []byte(`{"seq":0,"type":"turn/start","data":{"turn":1}}`)}, {Seq: 1, Payload: []byte(`{"seq":1,"type":"turn/end","data":{"turn":1}}`)}}
	response = transcriptRequest(router, "GET", "/sessions/cse_root/transcripts/cse_root?from_seq=0", "", "1")
	var page transcriptPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Header) == 0 || len(page.Events) != 2 || page.NextFromSeq != nil {
		t.Fatalf("page=%+v", page)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
}

func TestTranscriptReadPaginatesByEventCount(t *testing.T) {
	database := &transcriptDatabase{transcript: db.DSHTranscript{Header: []byte(`{"id":"cse_root"}`), NextSeq: 201}}
	for seq := int64(0); seq < 201; seq++ {
		database.events = append(database.events, db.DSHTranscriptEvent{Seq: seq, Payload: []byte(`{"seq":` + strconv.FormatInt(seq, 10) + `}`)})
	}
	router := transcriptRouter(database, true)
	for _, from := range []string{"0", "200", "201"} {
		response := transcriptRequest(router, "GET", "/sessions/cse_root/transcripts/cse_root?from_seq="+from, "", "1")
		var page transcriptPage
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if database.readLimit != 200 {
			t.Fatalf("read limit=%d", database.readLimit)
		}
		if from == "0" {
			if len(page.Header) == 0 || len(page.Events) != 200 || page.NextFromSeq == nil || *page.NextFromSeq != "200" {
				t.Fatalf("first page=%+v", page)
			}
		} else {
			want := 0
			if from == "200" {
				want = 1
			}
			if string(page.Header) != "null" || len(page.Events) != want || page.NextFromSeq != nil {
				t.Fatalf("from=%s page=%+v", from, page)
			}
		}
	}
}

func TestTranscriptReadKeepsLargeEventsInOnePage(t *testing.T) {
	payload := []byte(`{"text":"` + strings.Repeat("x", 3<<20) + `"}`)
	database := &transcriptDatabase{
		transcript: db.DSHTranscript{Header: []byte(`{"id":"cse_root"}`), NextSeq: 2},
		events:     []db.DSHTranscriptEvent{{Seq: 0, Payload: payload}, {Seq: 1, Payload: payload}},
	}
	response := transcriptRequest(transcriptRouter(database, true), "GET", "/sessions/cse_root/transcripts/cse_root", "", "1")
	var page transcriptPage
	if response.Code != 200 {
		t.Fatal(response.Code)
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.NextFromSeq != nil || response.Body.Len() <= 4<<20 {
		t.Fatalf("events=%d next=%v bytes=%d", len(page.Events), page.NextFromSeq, response.Body.Len())
	}
}

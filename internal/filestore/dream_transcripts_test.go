package filestore

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type fakeDreamTranscriptStore struct {
	dream     db.Dream
	relations []db.DreamSessionTranscript
	sessions  map[string]db.Session
	events    []db.SessionEvent
	hasMore   bool
}

func (f *fakeDreamTranscriptStore) GetDreamByInternalSessionUUID(_ context.Context, _ string, sessionUUID string) (db.Dream, error) {
	if sessionUUID != f.dream.InternalSessionUUID {
		return db.Dream{}, db.ErrNotFound
	}
	return f.dream, nil
}

func (f *fakeDreamTranscriptStore) ListDreamSessionTranscripts(_ context.Context, _ string, dreamUUID string) ([]db.DreamSessionTranscript, error) {
	if dreamUUID != f.dream.UUID {
		return nil, db.ErrNotFound
	}
	return f.relations, nil
}

func (f *fakeDreamTranscriptStore) GetSessionByUUID(_ context.Context, _ string, sessionUUID string) (db.Session, bool, error) {
	session, found := f.sessions[sessionUUID]
	return session, found, nil
}

func (f *fakeDreamTranscriptStore) ListSessionEventsPage(_ context.Context, _ db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error) {
	return f.events, f.hasMore, nil
}

func TestDreamTranscriptBackendListsAndReadsOnlyBoundSessions(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	store := &fakeDreamTranscriptStore{
		dream:     db.Dream{UUID: "dream-uuid", InternalSessionUUID: "internal-session-uuid"},
		relations: []db.DreamSessionTranscript{{DreamUUID: "dream-uuid", SourceSessionUUID: "selected-uuid", SourceSessionExternalID: "sesn_selected", CreatedAt: now}},
		sessions:  map[string]db.Session{"selected-uuid": {UUID: "selected-uuid", ExternalID: "sesn_selected"}},
		events: []db.SessionEvent{
			{ExternalID: "event-user", EventType: "user.message", CreatedAt: now, Payload: json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"remember tea"}]}`)},
			{ExternalID: "event-span", EventType: "session.status_idle", CreatedAt: now, Payload: json.RawMessage(`{"type":"session.status_idle"}`)},
			{ExternalID: "event-tool", EventType: "agent.tool_use", CreatedAt: now, Payload: json.RawMessage(`{"name":"read_file","path":"/tmp/note.md"}`)},
		},
	}
	backend := &dreamTranscriptPathBackend{store: store, now: func() time.Time { return now }}
	principal := Principal{WorkspaceUUID: "workspace-uuid"}
	filesystem := db.FilestoreFilesystem{ExternalID: "filesystem", SessionUUID: "internal-session-uuid"}

	listing, apiErr := backend.listDirectory(context.Background(), principal, filesystem, listDirectoryRequest{FilesystemID: "filesystem", Path: dreamTranscriptNamespace}, directoryCursor{}, 20)
	if apiErr != nil || len(listing.Entries) != 1 || listing.Entries[0].File == nil || listing.Entries[0].File.Path != "/transcripts/dream/sesn_selected.jsonl" || listing.Entries[0].File.File.Size <= 0 {
		t.Fatalf("listDirectory() = %#v, %v", listing, apiErr)
	}
	parentListing, apiErr := backend.listDirectory(context.Background(), principal, filesystem, listDirectoryRequest{FilesystemID: "filesystem", Path: dreamTranscriptParent}, directoryCursor{}, 20)
	if apiErr != nil || len(parentListing.Entries) != 1 || parentListing.Entries[0].Directory == nil || parentListing.Entries[0].Directory.Path != dreamTranscriptNamespace {
		t.Fatalf("parent listDirectory() = %#v, %v", parentListing, apiErr)
	}

	result, apiErr := backend.readFile(context.Background(), principal, filesystem, readFileRequest{FilesystemID: "filesystem", Path: "/transcripts/dream/sesn_selected.jsonl"})
	if apiErr != nil {
		t.Fatalf("readFile() error = %v", apiErr)
	}
	content, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if got := string(content); !containsAll(got, "user.message", "remember tea", "read_file", "/tmp/note.md", `"session_id":"sesn_selected"`) || containsAll(got, "session.status_idle") {
		t.Fatalf("transcript = %s", got)
	}
	if _, apiErr = backend.readFile(context.Background(), principal, filesystem, readFileRequest{FilesystemID: "filesystem", Path: "/transcripts/dream/sesn_guessed.jsonl"}); apiErr == nil || apiErr.Status != 404 {
		t.Fatalf("guessed transcript error = %v, want not found", apiErr)
	}
}

func TestCompactDreamTranscriptEventIncludesToolInput(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 6, 17, 22, 24, 0, time.UTC)
	line, include := compactDreamTranscriptEvent("sesn_selected", db.SessionEvent{
		ExternalID: "sevt_write",
		EventType:  "agent.tool_use",
		CreatedAt:  now,
		Payload:    json.RawMessage(`{"name":"write","input":{"file_path":"/mnt/memory/aumemory/yanghonghai_preference.md","content":"yanghonghai 喜欢吃香蕉。"}}`),
	})
	if !include {
		t.Fatal("write tool_use was dropped")
	}
	got := string(line)
	if !containsAll(got, `"session_id":"sesn_selected"`, `"name":"write"`, `yanghonghai_preference.md`, `喜欢吃香蕉`, `"path":"/mnt/memory/aumemory/yanghonghai_preference.md"`) {
		t.Fatalf("compact tool_use = %s", got)
	}
}

func TestCompactDreamTranscriptEventIncludesToolResult(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 6, 17, 22, 25, 0, time.UTC)
	line, include := compactDreamTranscriptEvent("sesn_selected", db.SessionEvent{
		ExternalID: "sevt_result",
		EventType:  "agent.tool_result",
		CreatedAt:  now,
		Payload:    json.RawMessage(`{"tool_use_id":"sevt_write","is_error":true,"content":[{"type":"text","text":"Write failed"}]}`),
	})
	if !include {
		t.Fatal("agent.tool_result was dropped")
	}
	got := string(line)
	if !containsAll(got, `"session_id":"sesn_selected"`, `"tool_use_id":"sevt_write"`, `"is_error":true`, `Write failed`) {
		t.Fatalf("compact tool_result = %s", got)
	}
}

func TestCompactDreamTranscriptEventDefaultsMissingIsError(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 6, 17, 20, 52, 0, time.UTC)
	line, include := compactDreamTranscriptEvent("sesn_orVtucr", db.SessionEvent{
		ExternalID: "sevt_unchanged",
		EventType:  "agent.tool_result",
		CreatedAt:  now,
		Payload:    json.RawMessage(`{"session_id":"3cd10ba5-951b-49b1-8539-19879f8b00ec","tool_use_id":"sevt_read","content":[{"type":"text","text":"File unchanged since last read."}],"tool_use_result":{"type":"file_unchanged"}}`),
	})
	if !include {
		t.Fatal("agent.tool_result without is_error was dropped")
	}
	var object map[string]any
	if err := json.Unmarshal(line, &object); err != nil {
		t.Fatalf("unmarshal compact line: %v", err)
	}
	if object["is_error"] != false {
		t.Fatalf("is_error = %#v, want false", object["is_error"])
	}
}

func TestCompactDreamTranscriptEventIncludesOriginSessionID(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 6, 17, 22, 24, 0, time.UTC)
	line, include := compactDreamTranscriptEvent("sesn_riq2", db.SessionEvent{
		ExternalID: "sevt_write",
		EventType:  "agent.tool_use",
		CreatedAt:  now,
		Payload:    json.RawMessage(`{"session_id":"65dfe945-2b8e-4232-869b-c55ed05e5335","name":"write","input":{"file_path":"/mnt/memory/jianghao_hometown.md","content":"---\noriginSessionId: 65dfe945-2b8e-4232-869b-c55ed05e5335\n"}}`),
	})
	if !include {
		t.Fatal("write tool_use was dropped")
	}
	got := string(line)
	if !containsAll(got, `"session_id":"sesn_riq2"`, `"origin_session_id":"65dfe945-2b8e-4232-869b-c55ed05e5335"`) {
		t.Fatalf("compact tool_use = %s", got)
	}
}

func TestCompactDreamTranscriptEventBoundsOversizedInput(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 6, 17, 22, 24, 0, time.UTC)
	content := strings.Repeat("x", maxDreamTranscriptToolPayloadBytes+32)
	payload, err := json.Marshal(map[string]any{
		"name": "write",
		"input": map[string]any{
			"file_path": "/mnt/memory/big.md",
			"content":   content,
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	line, include := compactDreamTranscriptEvent("sesn_selected", db.SessionEvent{
		ExternalID: "sevt_big",
		EventType:  "agent.tool_use",
		CreatedAt:  now,
		Payload:    payload,
	})
	if !include {
		t.Fatal("oversized write tool_use was dropped")
	}
	var object map[string]any
	if err := json.Unmarshal(line, &object); err != nil {
		t.Fatalf("unmarshal compact line: %v", err)
	}
	input, ok := object["input"].(map[string]any)
	if !ok {
		t.Fatalf("input = %#v", object["input"])
	}
	if input["file_path"] != "/mnt/memory/big.md" {
		t.Fatalf("file_path = %#v", input["file_path"])
	}
	if input["truncated"] != true {
		t.Fatalf("truncated = %#v, want true", input["truncated"])
	}
	gotContent, _ := input["content"].(string)
	if gotContent == content || !strings.HasPrefix(content, gotContent) {
		t.Fatalf("bounded content length = %d, want truncated prefix", len(gotContent))
	}
}

func TestDreamTranscriptBackendEmitsTruncationWarning(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	store := &fakeDreamTranscriptStore{
		dream:     db.Dream{UUID: "dream-uuid", InternalSessionUUID: "internal-session-uuid"},
		relations: []db.DreamSessionTranscript{{DreamUUID: "dream-uuid", SourceSessionUUID: "selected-uuid", SourceSessionExternalID: "sesn_selected", CreatedAt: now}},
		sessions:  map[string]db.Session{"selected-uuid": {UUID: "selected-uuid", ExternalID: "sesn_selected"}},
		events:    []db.SessionEvent{{ExternalID: "event", EventType: "user.message", CreatedAt: now, Payload: json.RawMessage(`{"type":"user.message"}`)}},
		hasMore:   true,
	}
	backend := &dreamTranscriptPathBackend{store: store, now: func() time.Time { return now }}
	content, apiErr := backend.renderTranscript(context.Background(), Principal{WorkspaceUUID: "workspace-uuid"}, store.relations[0])
	if apiErr != nil || !containsAll(string(content), "dream.transcript_truncated", "max_events") {
		t.Fatalf("renderTranscript() = %s, %v", content, apiErr)
	}
}

func containsAll(value string, wants ...string) bool {
	for _, want := range wants {
		if !strings.Contains(value, want) {
			return false
		}
	}
	return true
}

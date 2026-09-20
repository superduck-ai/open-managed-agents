package filestore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	dreamTranscriptNamespace           = "/transcripts/dream"
	dreamTranscriptParent              = "/transcripts"
	maxDreamTranscriptEvents           = 5000
	maxDreamTranscriptBytes            = 2 * 1024 * 1024
	maxDreamTranscriptToolPayloadBytes = 32 * 1024
)

// dreamTranscriptFilestoreStore is deliberately narrower than filestoreDatabase
// so existing Filestore fakes do not accidentally gain access to transcripts.
// A real DB supplies it and the virtual backend is then registered.
type dreamTranscriptFilestoreStore interface {
	GetDreamByInternalSessionUUID(context.Context, string, string) (db.Dream, error)
	ListDreamSessionTranscripts(context.Context, string, string) ([]db.DreamSessionTranscript, error)
	GetSessionByUUID(context.Context, string, string) (db.Session, bool, error)
	ListSessionEventsPage(context.Context, db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error)
}

type dreamTranscriptPathBackend struct {
	store    dreamTranscriptFilestoreStore
	fallback pathBackend
	now      func() time.Time
}

func (*dreamTranscriptPathBackend) namespaceRoot() string { return dreamTranscriptNamespace }

func (*dreamTranscriptPathBackend) containsPath(value string) bool {
	return value == dreamTranscriptNamespace || strings.HasPrefix(value, dreamTranscriptNamespace+"/")
}

func (b *dreamTranscriptPathBackend) matchesRead(operation readOperation, value string) bool {
	// rclone discovers the virtual directory by listing its parent before it
	// ever asks for /transcripts/dream. Route only that listing here; metadata
	// for the ordinary /transcripts root remains persistent.
	return b.containsPath(value) || (operation == readOperationListDirectory && value == dreamTranscriptParent)
}

func (b *dreamTranscriptPathBackend) listDirectory(ctx context.Context, principal Principal, filesystem db.FilestoreFilesystem, request listDirectoryRequest, cursor directoryCursor, limit int) (listDirectoryResponse, *apiError) {
	dream, relations, apiErr := b.resolveDream(ctx, principal, filesystem)
	if apiErr != nil {
		if request.Path == dreamTranscriptParent && apiErr.Status == http.StatusNotFound && b.fallback != nil {
			return b.fallback.listDirectory(ctx, principal, filesystem, request, cursor, limit)
		}
		return listDirectoryResponse{}, apiErr
	}
	if request.Path == dreamTranscriptParent {
		if cursor.LastPath != "" {
			return listDirectoryResponse{Entries: []entryPayload{}}, nil
		}
		return listDirectoryResponse{Entries: []entryPayload{{Directory: &directoryPayload{
			FilesystemID: filesystem.ExternalID,
			Path:         dreamTranscriptNamespace,
			CreatedAt:    dream.CreatedAt.UTC().Format(time.RFC3339),
		}}}}, nil
	}
	if request.Path != dreamTranscriptNamespace {
		return listDirectoryResponse{}, notFound("resource does not exist")
	}
	_ = dream
	start := 0
	if cursor.LastPath != "" {
		for i, relation := range relations {
			if dreamTranscriptPath(relation.SourceSessionExternalID) > cursor.LastPath {
				start = i
				break
			}
			start = len(relations)
		}
	}
	items := relations[start:]
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	response := listDirectoryResponse{Entries: make([]entryPayload, 0, len(items))}
	for _, relation := range items {
		// rclone treats an omitted size as an empty file and never issues the
		// subsequent readFile request. Calculate the bounded virtual payload
		// here so a listed transcript remains readable through FUSE.
		content, contentErr := b.renderTranscript(ctx, principal, relation)
		if contentErr != nil {
			return listDirectoryResponse{}, contentErr
		}
		response.Entries = append(response.Entries, entryPayload{File: &filesystemFilePayload{FilesystemID: filesystem.ExternalID, Path: dreamTranscriptPath(relation.SourceSessionExternalID), File: filePayload{
			CreatedAt: relation.CreatedAt.UTC().Format(time.RFC3339), Size: protoInt64(len(content)), MediaType: "application/x-ndjson", Downloadable: false, FilesystemID: filesystem.ExternalID,
		}}})
	}
	if hasMore && len(items) > 0 {
		encoded, err := encodeDirectoryCursor(directoryCursor{FilesystemID: request.FilesystemID, Path: request.Path, Recursive: request.Recursive, LastPath: dreamTranscriptPath(items[len(items)-1].SourceSessionExternalID)})
		if err != nil {
			return listDirectoryResponse{}, internalError("encode Dream transcript cursor", err)
		}
		response.Cursor = encoded
	}
	return response, nil
}

func (b *dreamTranscriptPathBackend) readMetadata(ctx context.Context, principal Principal, filesystem db.FilestoreFilesystem, entryPath string) (entryPayload, *apiError) {
	if entryPath == dreamTranscriptNamespace {
		if _, _, apiErr := b.resolveDream(ctx, principal, filesystem); apiErr != nil {
			return entryPayload{}, apiErr
		}
		return entryPayload{Directory: &directoryPayload{FilesystemID: filesystem.ExternalID, Path: entryPath, CreatedAt: b.now().UTC().Format(time.RFC3339)}}, nil
	}
	_, relation, apiErr := b.resolveRelation(ctx, principal, filesystem, entryPath)
	if apiErr != nil {
		return entryPayload{}, apiErr
	}
	content, apiErr := b.renderTranscript(ctx, principal, relation)
	if apiErr != nil {
		return entryPayload{}, apiErr
	}
	file := filesystemFilePayload{FilesystemID: filesystem.ExternalID, Path: entryPath, File: filePayload{CreatedAt: relation.CreatedAt.UTC().Format(time.RFC3339), Size: protoInt64(len(content)), MediaType: "application/x-ndjson", Downloadable: false, FilesystemID: filesystem.ExternalID}}
	return entryPayload{File: &file}, nil
}

func (b *dreamTranscriptPathBackend) readFile(ctx context.Context, principal Principal, filesystem db.FilestoreFilesystem, request readFileRequest) (readFileResult, *apiError) {
	_, relation, apiErr := b.resolveRelation(ctx, principal, filesystem, request.Path)
	if apiErr != nil {
		return readFileResult{}, apiErr
	}
	content, apiErr := b.renderTranscript(ctx, principal, relation)
	if apiErr != nil {
		return readFileResult{}, apiErr
	}
	objectRange, responseSize, apiErr := resolveReadRange(request.Range, int64(len(content)))
	if apiErr != nil {
		return readFileResult{}, apiErr
	}
	if objectRange == nil {
		return readFileResult{Body: io.NopCloser(bytes.NewReader(content)), Size: int64(len(content)), MediaType: "application/x-ndjson"}, nil
	}
	end := objectRange.Offset + responseSize
	return readFileResult{Body: io.NopCloser(bytes.NewReader(content[objectRange.Offset:end])), Size: responseSize, MediaType: "application/x-ndjson"}, nil
}

func (b *dreamTranscriptPathBackend) resolveDream(ctx context.Context, principal Principal, filesystem db.FilestoreFilesystem) (db.Dream, []db.DreamSessionTranscript, *apiError) {
	dream, err := b.store.GetDreamByInternalSessionUUID(ctx, principal.WorkspaceUUID, filesystem.SessionUUID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return db.Dream{}, nil, notFound("Dream transcript view is unavailable")
		}
		return db.Dream{}, nil, mapDatabaseError("load Dream transcript view", err)
	}
	relations, err := b.store.ListDreamSessionTranscripts(ctx, principal.WorkspaceUUID, dream.UUID)
	if err != nil {
		return db.Dream{}, nil, mapDatabaseError("list Dream transcript relations", err)
	}
	return dream, relations, nil
}

func (b *dreamTranscriptPathBackend) resolveRelation(ctx context.Context, principal Principal, filesystem db.FilestoreFilesystem, entryPath string) (db.Dream, db.DreamSessionTranscript, *apiError) {
	dream, relations, apiErr := b.resolveDream(ctx, principal, filesystem)
	if apiErr != nil {
		return db.Dream{}, db.DreamSessionTranscript{}, apiErr
	}
	for _, relation := range relations {
		if entryPath == dreamTranscriptPath(relation.SourceSessionExternalID) {
			return dream, relation, nil
		}
	}
	return db.Dream{}, db.DreamSessionTranscript{}, notFound("resource does not exist")
}

func (b *dreamTranscriptPathBackend) renderTranscript(ctx context.Context, principal Principal, relation db.DreamSessionTranscript) ([]byte, *apiError) {
	session, found, err := b.store.GetSessionByUUID(ctx, principal.WorkspaceUUID, relation.SourceSessionUUID)
	if err != nil {
		return nil, mapDatabaseError("load Dream source Session", err)
	}
	if !found || session.ArchivedAt != nil {
		return nil, failedPrecondition("Dream source Session is unavailable")
	}
	events, hasMore, err := b.store.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{WorkspaceUUID: principal.WorkspaceUUID, SessionExternalID: relation.SourceSessionExternalID, PrimaryOnly: true, Limit: maxDreamTranscriptEvents})
	if err != nil {
		return nil, mapDatabaseError("read Dream source Session events", err)
	}
	lines := make([][]byte, 0, len(events))
	used, truncated := 0, hasMore
	for _, event := range events {
		line, include := compactDreamTranscriptEvent(relation.SourceSessionExternalID, event)
		if !include {
			continue
		}
		if used+len(line)+1 > maxDreamTranscriptBytes {
			truncated = true
			break
		}
		used += len(line) + 1
		lines = append(lines, line)
	}
	var output bytes.Buffer
	if truncated {
		warning, _ := json.Marshal(map[string]any{
			"type":       "dream.transcript_truncated",
			"session_id": relation.SourceSessionExternalID,
			"max_events": maxDreamTranscriptEvents,
			"max_bytes":  maxDreamTranscriptBytes,
		})
		output.Write(warning)
		output.WriteByte('\n')
	}
	for _, line := range lines {
		output.Write(line)
		output.WriteByte('\n')
	}
	return output.Bytes(), nil
}

func compactDreamTranscriptEvent(sessionID string, event db.SessionEvent) ([]byte, bool) {
	base := map[string]any{
		"type":       event.EventType,
		"event_id":   event.ExternalID,
		"session_id": sessionID,
		"created_at": event.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if originSessionID := dreamOriginSessionID(event.Payload); originSessionID != "" {
		base["origin_session_id"] = originSessionID
	}
	switch event.EventType {
	case "user.message", "agent.message":
		var payload any
		if json.Unmarshal(event.Payload, &payload) != nil {
			return nil, false
		}
		base["message"] = payload
	case "agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use":
		return compactDreamToolUse(base, event.Payload)
	case "agent.tool_result", "agent.mcp_tool_result", "tool.result":
		return compactDreamToolResult(base, event.Payload)
	default:
		return nil, false
	}
	line, err := json.Marshal(base)
	return line, err == nil
}

func compactDreamToolUse(base map[string]any, raw json.RawMessage) ([]byte, bool) {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil, false
	}
	name := jsonStringValue(payload, "name")
	input := payload["input"]
	toolPath := dreamToolPath(payload, input)
	if name == "" && toolPath == "" && input == nil {
		return nil, false
	}
	if name != "" {
		base["name"] = name
	}
	if toolPath != "" {
		base["path"] = toolPath
	}
	if input != nil {
		base["input"] = boundDreamJSONValue(input)
	}
	line, err := json.Marshal(base)
	return line, err == nil
}

func compactDreamToolResult(base map[string]any, raw json.RawMessage) ([]byte, bool) {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil, false
	}
	if jsonStringValue(payload, "tool_use_id") == "" && payload["content"] == nil && payload["tool_use_result"] == nil {
		if _, ok := payload["is_error"].(bool); !ok {
			return nil, false
		}
	}
	if toolUseID := jsonStringValue(payload, "tool_use_id"); toolUseID != "" {
		base["tool_use_id"] = toolUseID
	}
	base["is_error"] = false
	if isError, ok := payload["is_error"].(bool); ok {
		base["is_error"] = isError
	}
	if content, ok := payload["content"]; ok && content != nil {
		base["content"] = boundDreamJSONValue(content)
	}
	if result, ok := payload["tool_use_result"]; ok && result != nil {
		base["tool_use_result"] = boundDreamJSONValue(result)
	}
	line, err := json.Marshal(base)
	return line, err == nil
}

func dreamOriginSessionID(raw json.RawMessage) string {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	return jsonStringValue(payload, "session_id")
}

func dreamToolPath(payload map[string]any, input any) string {
	if value := jsonStringValue(payload, "path"); value != "" {
		return value
	}
	if value := jsonStringValue(payload, "file_path"); value != "" {
		return value
	}
	nested, ok := input.(map[string]any)
	if !ok {
		return ""
	}
	if value := jsonStringValue(nested, "file_path"); value != "" {
		return value
	}
	return jsonStringValue(nested, "path")
}

func boundDreamJSONValue(value any) any {
	data, err := json.Marshal(value)
	if err != nil || len(data) <= maxDreamTranscriptToolPayloadBytes {
		return value
	}
	nested, ok := value.(map[string]any)
	if !ok {
		return map[string]any{"truncated": true, "size_bytes": len(data)}
	}
	bounded := map[string]any{"truncated": true}
	for _, key := range []string{"file_path", "path", "command"} {
		if field := jsonStringValue(nested, key); field != "" {
			bounded[key] = field
		}
	}
	limit := maxDreamTranscriptToolPayloadBytes / 4
	for _, key := range []string{"content", "old_string", "new_string", "stdout", "stderr"} {
		field, ok := nested[key].(string)
		if !ok || field == "" {
			continue
		}
		bounded[key] = truncateDreamJSONString(field, limit)
	}
	return bounded
}

func jsonStringValue(payload map[string]any, key string) string {
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return value
}

func truncateDreamJSONString(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	cut := value[:maxBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func dreamTranscriptPath(sessionID string) string {
	return path.Join(dreamTranscriptNamespace, fmt.Sprintf("%s.jsonl", sessionID))
}

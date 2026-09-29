package livefiles

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func (e *filesEnv) jsonRequest(t *testing.T, method, path string, value any, status int) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	requireOK(t, err)
	data, _ := e.request(t, method, path, e.token, "application/json", bytes.NewReader(body), true, status)
	return data
}

func (e *filesEnv) newSession(t *testing.T) string {
	t.Helper()
	now := time.Now().UTC()
	agent, err := e.database.CreateAgent(e.ctx, db.Agent{UUID: uuid.NewString(), ExternalID: "agent_" + uuid.NewString(), WorkspaceUUID: e.key.WorkspaceUUID.String(), CreatedByAPIKeyUUID: e.key.UUID.String(), Name: "Files verification", Model: json.RawMessage(`{"id":"verification-model"}`), MCPServers: json.RawMessage(`[]`), Metadata: json.RawMessage(`{}`), Multiagent: json.RawMessage(`{}`), Skills: json.RawMessage(`[]`), Tools: json.RawMessage(`[]`), CreatedAt: now, UpdatedAt: now}, "aver_"+uuid.NewString())
	requireOK(t, err)
	var environment, session struct {
		ID string `json:"id"`
	}
	requireOK(t, json.Unmarshal(e.jsonRequest(t, "POST", "/v1/environments?beta=true", map[string]string{"name": "Files verification"}, 200), &environment))
	requireOK(t, json.Unmarshal(e.jsonRequest(t, "POST", "/v1/sessions?beta=true", map[string]string{"agent": agent.ExternalID, "environment_id": environment.ID}, 200), &session))
	return session.ID
}

func TestFilesAttachments(t *testing.T) {
	e := newFilesEnv(t)
	session := e.newSession(t)
	base := "/v1/sessions/" + session
	missing := map[string]string{"type": "file", "file_id": "file_missing"}
	e.jsonRequest(t, "POST", base+"/resources?beta=true", missing, 404)
	input := e.record(t, e.upload(t, "attached.bin", []byte("shared source"), 200).ID)
	var resource struct {
		ID string `json:"id"`
	}
	requireOK(t, json.Unmarshal(e.jsonRequest(t, "POST", base+"/resources?beta=true", map[string]string{"type": "file", "file_id": input.ExternalID}, 200), &resource))
	e.request(t, "DELETE", "/v1/files/"+input.ExternalID, e.token, "", nil, true, 409)
	e.assertObject(t, input.S3Key, []byte("shared source"))
	e.proof(t, "referenced_delete_rejected")
	e.jsonRequest(t, "DELETE", base+"/resources/"+resource.ID+"?beta=true", nil, 200)
	e.delete(t, input)
	e.proof(t, "unlink_then_delete")
	input = e.record(t, e.upload(t, "retained-input.bin", []byte("retain after session deletion"), 200).ID)
	e.jsonRequest(t, "POST", base+"/resources?beta=true", map[string]string{"type": "file", "file_id": input.ExternalID}, 200)
	output := e.projectOutput(t, session, "/outputs/owned.bin", nil)
	expiredAt := time.Now().Add(3 * time.Second)
	expired := e.projectOutput(t, session, "/outputs/expired.bin", &expiredAt)
	e.await(t, "output expiry", func() bool { return time.Now().After(expiredAt) })
	e.request(t, "GET", "/v1/files/"+expired.ExternalID, e.token, "", nil, true, 404)
	e.request(t, "GET", "/v1/files/"+expired.ExternalID+"/content", e.token, "", nil, true, 404)
	for _, record := range e.page(t, e.token, "?scope_id="+session).Data {
		if record.ID == expired.ExternalID {
			t.Fatal("expired output visible")
		}
	}
	e.proof(t, "expired_output_hidden")
	e.jsonRequest(t, "POST", base+"/archive?beta=true", nil, 200)
	e.request(t, "GET", "/v1/files/"+output.ExternalID+"/content", e.token, "", nil, true, 200)
	e.assertObject(t, output.S3Key, []byte("projected output"))
	e.proof(t, "archive_preserves_output")
	e.jsonRequest(t, "DELETE", base+"?beta=true", nil, 200)
	e.await(t, "session-owned object cleanup", func() bool { return e.objectAbsent(t, output.S3Key) && e.objectAbsent(t, expired.S3Key) })
	e.request(t, "GET", "/v1/files/"+output.ExternalID, e.token, "", nil, true, 404)
	e.assertObject(t, input.S3Key, []byte("retain after session deletion"))
	e.delete(t, input)
	usage, err := e.database.WorkspaceStorageBytes(e.ctx, e.key.WorkspaceUUID.String())
	requireOK(t, err)
	if usage != 0 {
		t.Fatalf("session cleanup left quota usage %d", usage)
	}
	e.proof(t, "session_cleanup_preserves_upload")
}

func (e *filesEnv) projectOutput(t *testing.T, session, path string, expires *time.Time) db.FileRecord {
	t.Helper()
	filesystem, err := e.database.GetFilestoreFilesystemBySession(e.ctx, e.key.WorkspaceUUID.String(), session)
	requireOK(t, err)
	key := "outputs/" + uuid.NewString()
	content := []byte("projected output")
	sum := sha256.Sum256(content)
	uploaded, err := e.objects.Upload(e.ctx, key, bytes.NewReader(content), storage.UploadOptions{Size: int64(len(content)), ContentType: "application/octet-stream"})
	requireOK(t, err)
	_, err = e.database.PutFilestoreFile(e.ctx, db.PutFilestoreFileInput{WorkspaceUUID: e.key.WorkspaceUUID.String(), FilesystemUUID: filesystem.UUID, Path: path, Blob: db.FilestoreFileBlob{SizeBytes: int64(len(content)), MediaType: "application/octet-stream", MD5: uploaded.ETag, SHA256: hex.EncodeToString(sum[:]), S3Bucket: e.objects.Name(), S3Key: key, S3ETag: uploaded.ETag, Downloadable: true, ExpiresAt: expires}, WorkspaceStorageLimitBytes: 98304})
	requireOK(t, err)
	records, err := e.database.ListFiles(e.ctx, e.key.WorkspaceUUID.String(), session)
	requireOK(t, err)
	for _, record := range records {
		if record.S3Key == key {
			return record
		}
	}
	t.Fatal("projected File missing")
	return db.FileRecord{}
}

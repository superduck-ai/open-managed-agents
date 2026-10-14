package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func TestVerifyMemoryIntegrity(t *testing.T) {
	f := newMemoryVerification(t)
	assertFilestoreError(t, f.fx.createFile(t, f.path("/bad.txt"), []byte{0xff, 0xfe}), 400, "invalid_argument")
	assertFilestoreError(t, f.fx.createFile(t, f.path("/bad.txt"), bytes.Repeat([]byte("x"), 102401)), 413, "resource_exhausted")
	assertFilestoreError(t, f.fx.createFileWithParams(t, f.path("/bad.txt"), []byte("bad"), map[string]any{"ttlSeconds": "1"}), 400, "invalid_argument")
	assertFilestoreError(t, f.fx.createFile(t, "/memory/"+f.fx.slug()+"/../escape.txt", []byte("bad")), 400, "invalid_argument")
	if len(f.objects.keys) != 0 {
		t.Fatal("invalid input uploaded objects")
	}
	memoryVerifyWrite(t, f.fx, f.path("/tree"), "original\n中文")
	assertFilestoreError(t, f.fx.createFile(t, f.path("/tree/child"), []byte("bad")), 409, "already_exists")
	memoryVerifyRead(t, f.fx, f.path("/tree"), "original\n中文")
	f.proof(t, "invalid_writes_rejected")
	head, found := findMemoryByPath(t, f.app, f.fx.store.ID, "/tree")
	if !found {
		t.Fatal("memory head missing")
	}
	record, err := f.app.db.GetMemory(t.Context(), f.fx.workspaceUUID, f.fx.store.ID, head.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.CurrentVersionExternalID == "" {
		t.Fatal("memory has no head version")
	}
	if err := f.objects.Delete(t.Context(), record.S3Key, storage.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	memoryVerifyResponse(t, f.fx.json(t, "readFile", map[string]any{"filesystemId": f.fx.filesystem.ExternalID, "path": f.path("/tree")}), 404)
	resp := doMemoryRequest(t, f.app, "GET", "/v1/memory_stores/"+f.fx.store.ID+"/memories/"+head.ID+"?beta=true&view=full", nil, defaultTestKey, true)
	memoryVerifyResponse(t, resp, http.StatusInternalServerError)
	var unchanged memoryAPIResponse
	metadata := memoryVerifyResponse(t, doMemoryRequest(t, f.app, "GET", "/v1/memory_stores/"+f.fx.store.ID+"/memories/"+head.ID+"?beta=true&view=basic", nil, defaultTestKey, true), 200)
	if err := json.Unmarshal(metadata, &unchanged); err != nil {
		t.Fatal(err)
	}
	if unchanged.MemoryVersionID != record.CurrentVersionExternalID || unchanged.ContentSHA256 != record.ContentSHA256 || unchanged.ContentSizeBytes != record.ContentSizeBytes || unchanged.Path != record.Path {
		t.Fatal("failed read changed the head")
	}
	f.proof(t, "missing_object_rejected")
	_, err = f.objects.Upload(t.Context(), record.S3Key, strings.NewReader("original\n中文"), storage.UploadOptions{Size: int64(len("original\n中文"))})
	if err != nil {
		t.Fatal(err)
	}
	memoryVerifyRead(t, f.fx, f.path("/tree"), "original\n中文")
	assertMemoryContent(t, retrieveMemoryFull(t, f.app, f.fx.store.ID, head.ID), "original\n中文")
	f.proof(t, "repaired_memory_matches")
}

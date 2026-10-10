package tests

import (
	"net/http"
	"strings"
	"testing"
)

func TestVerifyMemoryIsolation(t *testing.T) {
	f := newMemoryVerification(t)
	seed := createMemory(t, f.app, f.fx.store.ID, "/secret.md", "keep\n")
	ro := f.attach(t, "read_only")
	assertFilestoreError(t, ro.createFile(t, f.path("/secret.md"), []byte("deny")), 403, "permission_denied")
	assertFilestoreError(t, ro.json(t, "removeFile", map[string]any{"filesystemId": ro.filesystem.ExternalID, "path": f.path("/secret.md")}), 403, "permission_denied")
	assertFilestoreError(t, f.fx.createFile(t, "/memory/unmounted/secret.md", []byte("deny")), 404, "not_found")
	response := f.fx.json(t, "readFile", map[string]any{"filesystemId": ro.filesystem.ExternalID, "path": f.path("/secret.md")})
	memoryVerifyResponse(t, response, 403)
	otherKey := "sk-ant-local-memory-verify-other"
	seedWorkspaceKey(t, f.app.pool, "org_memory_verify_other", "workspace_memory_verify_other", "api_key_memory_verify_other", otherKey)
	memoryVerifyResponse(t, doMemoryRequest(t, f.app, "GET", "/v1/memory_stores/"+f.fx.store.ID+"?beta=true", nil, otherKey, true), 404)
	memoryVerifyRead(t, ro, f.path("/secret.md"), "keep\n")
	if len(listMemoryVersions(t, f.app, f.fx.store.ID, "memory_id="+seed.ID).Data) != 1 {
		t.Fatal("denied mutation created a version")
	}
	f.proof(t, "readonly_and_scope_enforced")
	memoryVerifyResponse(t, doMemoryRequest(t, f.app, "POST", "/v1/memory_stores/"+f.fx.store.ID+"/archive?beta=true", strings.NewReader(`{}`), defaultTestKey, true), 200)
	assertFilestoreError(t, f.fx.createFile(t, f.path("/secret.md"), []byte("deny")), 403, "permission_denied")
	memoryVerifyRead(t, f.fx, f.path("/secret.md"), "keep\n")
	f.proof(t, "archived_store_readonly")
	deleteSession(t, f.app, f.fx.session.ID)
	memoryVerifyResponse(t, f.fx.json(t, "readFile", map[string]any{"filesystemId": f.fx.filesystem.ExternalID, "path": f.path("/secret.md")}), http.StatusUnauthorized)
	memoryVerifyRead(t, ro, f.path("/secret.md"), "keep\n")
	f.proof(t, "deleted_session_token_revoked")
}

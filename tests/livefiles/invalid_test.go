package livefiles

import (
	"bytes"
	"slices"
	"testing"
)

func TestFilesInvalid(t *testing.T) {
	e := newFilesEnv(t)
	before := e.objectKeys(t)
	for _, token := range []string{"", "sk-ant-invalid"} {
		e.request(t, "POST", "/v1/files", token, "application/json", bytes.NewBufferString(`{}`), true, 401)
	}
	e.request(t, "POST", "/v1/files", e.token, "application/json", bytes.NewBufferString(`{}`), false, 400)
	e.request(t, "POST", "/v1/files", e.token, "application/json", bytes.NewBufferString(`{}`), true, 400)
	e.request(t, "POST", "/v1/files", e.token, "multipart/form-data; boundary=broken", bytes.NewBufferString("truncated"), true, 400)
	for _, invalid := range []struct {
		name   string
		size   int
		status int
	}{{"", 0, 400}, {"bad:name.bin", 1, 400}, {"oversize.bin", 65537, 413}, {"body-limit.bin", 65536 + (1 << 20) + 1, 413}} {
		e.upload(t, invalid.name, bytes.Repeat([]byte{7}, invalid.size), invalid.status)
	}
	if !slices.Equal(before, e.objectKeys(t)) || len(e.page(t, e.token, "").Data) != 0 {
		t.Fatal("rejected uploads left metadata or objects")
	}
	e.proof(t, "invalid_uploads_rejected")
	kept := e.upload(t, "quota.bin", bytes.Repeat([]byte{3}, 65536), 200)
	keys := e.objectKeys(t)
	e.upload(t, "quota-over.bin", bytes.Repeat([]byte{4}, 32769), 403)
	if !slices.Equal(keys, e.objectKeys(t)) || len(e.page(t, e.token, "").Data) != 1 {
		t.Fatal("quota rejection left metadata or orphan object")
	}
	usage, err := e.database.WorkspaceStorageBytes(e.ctx, e.key.WorkspaceUUID.String())
	requireOK(t, err)
	if usage != 65536 {
		t.Fatalf("quota after rejection=%d want=65536", usage)
	}
	e.proof(t, "quota_rollback_verified")
	e.delete(t, e.record(t, kept.ID))
	accepted := e.upload(t, "accepted.bin", []byte("valid after rejection"), 200)
	e.assertObject(t, e.record(t, accepted.ID).S3Key, []byte("valid after rejection"))
	e.delete(t, e.record(t, accepted.ID))
	e.proof(t, "valid_upload_after_rejection")
}

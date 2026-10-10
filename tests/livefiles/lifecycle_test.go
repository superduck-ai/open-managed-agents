package livefiles

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func TestFilesLifecycle(t *testing.T) {
	e := newFilesEnv(t)
	content := bytes.Repeat([]byte{0, 1, 127, 255, 13, 10}, 1024)
	uploaded := e.upload(t, "验证.bin", content, 200)
	if uploaded.Type != "file" || uploaded.Filename != "验证.bin" || uploaded.SizeBytes != int64(len(content)) || uploaded.Downloadable || uploaded.MimeType != "application/octet-stream" {
		t.Fatal("uploaded metadata differs")
	}
	_, err := time.Parse(time.RFC3339Nano, uploaded.CreatedAt)
	requireOK(t, err)
	record := e.record(t, uploaded.ID)
	sum := sha256.Sum256(content)
	if record.SHA256 != hex.EncodeToString(sum[:]) || record.S3Bucket != e.objects.Name() {
		t.Fatal("stored digest or bucket differs")
	}
	e.assertObject(t, record.S3Key, content)
	e.request(t, "GET", "/v1/files/"+uploaded.ID+"/content", e.token, "", nil, true, 400)
	e.proof(t, "upload_stored")
	data, _ := e.request(t, "GET", "/v1/files/"+uploaded.ID, e.token, "", nil, true, 200)
	var retrieved metadata
	requireOK(t, json.Unmarshal(data, &retrieved))
	if retrieved != uploaded {
		t.Fatal("retrieved metadata differs from upload")
	}
	output := e.downloadable(t, content)
	first := e.page(t, e.token, "?limit=1")
	if len(first.Data) != 1 || !first.HasMore || first.LastID != first.Data[0].ID {
		t.Fatal("first page contract differs")
	}
	second := e.page(t, e.token, "?limit=1&after_id="+first.LastID)
	if len(second.Data) != 1 || second.HasMore || second.Data[0].ID == first.Data[0].ID {
		t.Fatal("second page contract differs")
	}
	ids := map[string]bool{first.Data[0].ID: true, second.Data[0].ID: true}
	if !ids[uploaded.ID] || !ids[output.ExternalID] {
		t.Fatal("listing omitted fixture files")
	}
	e.proof(t, "metadata_and_listing")
	data, header := e.request(t, "GET", "/v1/files/"+output.ExternalID+"/content", e.token, "", nil, true, 200)
	if !bytes.Equal(data, content) || header.Get("Content-Type") != output.MimeType {
		t.Fatal("download bytes or content type differ")
	}
	e.proof(t, "download_bytes_match")
	e.delete(t, record)
	e.delete(t, output)
	if len(e.page(t, e.token, "").Data) != 0 || len(e.objectKeys(t)) != 0 {
		t.Fatal("deleted files remain in listing or storage")
	}
	usage, err := e.database.WorkspaceStorageBytes(e.ctx, e.key.WorkspaceUUID.String())
	requireOK(t, err)
	if usage != 0 {
		t.Fatalf("deleted files still consume quota: %d", usage)
	}
	e.proof(t, "deleted_objects_absent")
}

func (e *filesEnv) page(t *testing.T, token, query string) filePage {
	t.Helper()
	data, _ := e.request(t, "GET", "/v1/files"+query, token, "", nil, true, 200)
	var page filePage
	requireOK(t, json.Unmarshal(data, &page))
	return page
}

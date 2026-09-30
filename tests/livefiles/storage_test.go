package livefiles

import (
	"bytes"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func TestFilesStorage(t *testing.T) {
	e := newFilesEnv(t)
	missing := e.downloadable(t, []byte("removed underneath metadata"))
	requireOK(t, e.objects.Delete(e.ctx, missing.S3Key, storage.DeleteOptions{}))
	e.request(t, "GET", "/v1/files/"+missing.ExternalID+"/content", e.token, "", nil, true, 500)
	e.delete(t, missing)
	e.proof(t, "missing_object_rejected")
	retained := e.upload(t, "retained.bin", []byte("retain me"), 200)
	record := e.record(t, retained.ID)
	orphan := record.S3Key + ".orphan"
	_, err := e.objects.Upload(e.ctx, orphan, bytes.NewBufferString("orphan"), storage.UploadOptions{Size: 6, ContentType: "application/octet-stream"})
	requireOK(t, err)
	e.assertObject(t, orphan, []byte("orphan"))
	requireOK(t, e.database.EnqueueObjectCleanupJob(e.ctx, record.WorkspaceUUID, record.S3Bucket, orphan, "file_verify_orphan"))
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for !e.objectAbsent(t, orphan) {
		select {
		case <-e.ctx.Done():
			t.Fatal("BE_TIMEOUT waiting for backend object cleanup loop")
		case <-tick.C:
		}
	}
	e.assertObject(t, record.S3Key, []byte("retain me"))
	e.proof(t, "background_object_cleanup")
	e.delete(t, record)
	e.verifyStorageBoundaries(t)
}

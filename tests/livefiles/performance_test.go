package livefiles

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"
)

type performanceSample struct {
	Index    int     `json:"index"`
	Upload   float64 `json:"upload_ms"`
	Metadata float64 `json:"metadata_ms"`
	List     float64 `json:"list_ms"`
	Download float64 `json:"download_ms"`
	Delete   float64 `json:"delete_ms"`
}

func TestFilesPerformance(t *testing.T) {
	e := newFilesEnv(t)
	content := bytes.Repeat([]byte("0123456789abcdef"), 2048)
	for i := -2; i < 20; i++ {
		if i == 0 {
			requireOK(t, os.WriteFile(os.Getenv("VERIFY_BE_PROFILE_READY"), []byte("ready"), 0o600))
		}
		sample := e.measureFileRound(t, content, i)
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-e.ctx.Done():
			timer.Stop()
			t.Fatal("BE_TIMEOUT Files performance workload")
		case <-timer.C:
		}
		if i >= 0 {
			data, err := json.Marshal(sample)
			requireOK(t, err)
			t.Logf("FILES_SAMPLE %s", data)
		}
	}
	e.proof(t, "files_load_completed")
	usage, err := e.database.WorkspaceStorageBytes(e.ctx, e.key.WorkspaceUUID.String())
	requireOK(t, err)
	if usage != 0 || len(e.page(t, e.token, "").Data) != 0 || len(e.objectKeys(t)) != 0 {
		t.Fatal("performance workload leaked metadata, quota or objects")
	}
	e.proof(t, "files_load_cleaned")
}

func milliseconds(start time.Time) float64 {
	return float64(time.Since(start)) / float64(time.Millisecond)
}

func (e *filesEnv) measureFileRound(t *testing.T, content []byte, index int) performanceSample {
	t.Helper()
	sample := performanceSample{Index: index}
	start := time.Now()
	uploaded := e.upload(t, "performance.bin", content, 200)
	sample.Upload = milliseconds(start)
	record := e.record(t, uploaded.ID)
	e.assertObject(t, record.S3Key, content)
	start = time.Now()
	data, _ := e.request(t, "GET", "/v1/files/"+uploaded.ID, e.token, "", nil, true, 200)
	sample.Metadata = milliseconds(start)
	var got metadata
	requireOK(t, json.Unmarshal(data, &got))
	if got.ID != uploaded.ID || got.SizeBytes != int64(len(content)) {
		t.Fatal("performance metadata differs")
	}
	start = time.Now()
	page := e.page(t, e.token, "")
	sample.List = milliseconds(start)
	if len(page.Data) != 1 || page.Data[0].ID != uploaded.ID {
		t.Fatal("performance listing differs")
	}
	download := e.downloadable(t, content)
	start = time.Now()
	data, _ = e.request(t, "GET", "/v1/files/"+download.ExternalID+"/content", e.token, "", nil, true, 200)
	sample.Download = milliseconds(start)
	if !bytes.Equal(data, content) {
		t.Fatal("performance download differs")
	}
	e.delete(t, download)
	start = time.Now()
	data, _ = e.request(t, "DELETE", "/v1/files/"+uploaded.ID, e.token, "", nil, true, 200)
	sample.Delete = milliseconds(start)
	var deleted struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	requireOK(t, json.Unmarshal(data, &deleted))
	if deleted.ID != uploaded.ID || deleted.Type != "file_deleted" || !e.objectAbsent(t, record.S3Key) {
		t.Fatal("performance deletion differs")
	}
	e.request(t, "GET", "/v1/files/"+uploaded.ID, e.token, "", nil, true, 404)
	return sample
}

package livefiles

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type objectAttempt struct {
	Method string    `json:"method"`
	Path   string    `json:"path"`
	At     time.Time `json:"at"`
	Denied bool      `json:"denied"`
}

func (e *filesEnv) faultRequest(t *testing.T, method string, body []byte) []byte {
	t.Helper()
	endpoint := os.Getenv("VERIFY_BE_STORAGE_CONTROL")
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		t.Fatal("isolated storage control required")
	}
	req, err := http.NewRequestWithContext(e.ctx, method, endpoint, bytes.NewReader(body))
	requireOK(t, err)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	requireOK(t, err)
	defer response.Body.Close()
	if response.StatusCode != 200 && response.StatusCode != 204 {
		t.Fatalf("fault control status=%d", response.StatusCode)
	}
	var data json.RawMessage
	if response.StatusCode == 200 {
		requireOK(t, json.NewDecoder(response.Body).Decode(&data))
	}
	return data
}

func (e *filesEnv) deny(t *testing.T, method, prefix string, enabled bool) {
	t.Helper()
	status := 0
	if enabled {
		status = 403
	}
	body, err := json.Marshal(struct {
		Method string `json:"method"`
		Prefix string `json:"prefix"`
		Status int    `json:"status"`
	}{method, "/" + e.objects.Name() + "/" + prefix, status})
	requireOK(t, err)
	e.faultRequest(t, http.MethodPut, body)
}

func (e *filesEnv) deleteAttempts(t *testing.T, key string) []objectAttempt {
	t.Helper()
	var all []objectAttempt
	requireOK(t, json.Unmarshal(e.faultRequest(t, http.MethodGet, nil), &all))
	var selected []objectAttempt
	for _, attempt := range all {
		if attempt.Method == "DELETE" && attempt.Path == "/"+e.objects.Name()+"/"+key {
			selected = append(selected, attempt)
		}
	}
	return selected
}

func (e *filesEnv) await(t *testing.T, description string, condition func() bool) {
	t.Helper()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for !condition() {
		select {
		case <-e.ctx.Done():
			t.Fatal("BE_TIMEOUT " + description)
		case <-tick.C:
		}
	}
}

func TestFilesRecovery(t *testing.T) {
	e := newFilesEnv(t)
	retained := e.record(t, e.upload(t, "retained.bin", bytes.Repeat([]byte("r"), 65536), 200).ID)
	deleted := e.record(t, e.upload(t, "delete.bin", []byte("delete me"), 200).ID)
	prefix := "workspaces/" + e.key.WorkspaceUUID.String() + "/files/"
	e.deny(t, "DELETE", prefix, true)
	e.request(t, "DELETE", "/v1/files/"+deleted.ExternalID, e.token, "", nil, true, 200)
	e.request(t, "GET", "/v1/files/"+deleted.ExternalID, e.token, "", nil, true, 404)
	e.assertObject(t, deleted.S3Key, []byte("delete me"))
	e.upload(t, "over-quota.bin", bytes.Repeat([]byte("q"), 32769), 403)
	keys := e.objectKeys(t)
	var orphan string
	for _, key := range keys {
		if key != retained.S3Key && key != deleted.S3Key {
			if orphan != "" {
				t.Fatal("unexpected extra orphan")
			}
			orphan = key
		}
	}
	if orphan == "" {
		t.Fatal("quota rejection did not leave the deliberately blocked orphan")
	}
	if len(e.page(t, e.token, "").Data) != 1 {
		t.Fatal("rejected/deleted metadata remains visible")
	}
	e.proof(t, "cleanup_failures_enqueued")
	e.await(t, "background cleanup retries", func() bool {
		return len(e.deleteAttempts(t, deleted.S3Key)) >= 2 && len(e.deleteAttempts(t, orphan)) >= 2
	})
	failed := e.deleteAttempts(t, deleted.S3Key)
	failedOrphan := e.deleteAttempts(t, orphan)
	if len(failed) != 2 || len(failedOrphan) != 2 || !failed[0].Denied || !failed[1].Denied {
		t.Fatal("expected HTTP cleanup failure and one background failure per object")
	}
	e.deny(t, "DELETE", prefix, false)
	e.await(t, "automatic retry after storage recovery", func() bool { return e.objectAbsent(t, deleted.S3Key) && e.objectAbsent(t, orphan) })
	for _, key := range []string{deleted.S3Key, orphan} {
		attempts := e.deleteAttempts(t, key)
		if len(attempts) != 3 || attempts[2].Denied || attempts[2].At.Sub(attempts[1].At) < time.Minute {
			t.Fatalf("cleanup retry did not respect one-minute backoff for %s: %+v", key, attempts)
		}
	}
	e.proof(t, "backoff_and_recovery")
	e.assertObject(t, retained.S3Key, bytes.Repeat([]byte("r"), 65536))
	usage, err := e.database.WorkspaceStorageBytes(e.ctx, e.key.WorkspaceUUID.String())
	requireOK(t, err)
	if usage != retained.SizeBytes {
		t.Fatalf("quota after compensation=%d", usage)
	}
	e.delete(t, retained)
	if len(e.objectKeys(t)) != 0 {
		t.Fatal("compensation leaked objects")
	}
	e.proof(t, "compensation_preserved_owner")
}

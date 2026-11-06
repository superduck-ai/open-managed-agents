//go:build integration

package liveworker

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type uploadSample struct {
	Count int
	Bytes int
	First int64
	Last  int64
}

type uploadFault struct {
	mode       string
	mutex      sync.Mutex
	samples    []uploadSample
	statuses   []int
	first      chan struct{}
	tail       chan struct{}
	cancelled  chan struct{}
	firstOnce  sync.Once
	tailOnce   sync.Once
	cancelOnce sync.Once
}

func newUploadFault(t *testing.T, target string, mode string) (*uploadFault, *httptest.Server) {
	t.Helper()
	fault := &uploadFault{mode: mode, first: make(chan struct{}), tail: make(chan struct{}), cancelled: make(chan struct{})}
	parsed, err := url.Parse(target)
	requireOK(t, err)
	proxy := httputil.NewSingleHostReverseProxy(parsed)
	proxy.ModifyResponse = func(response *http.Response) error {
		fault.mutex.Lock()
		fault.statuses = append(fault.statuses, response.StatusCode)
		fault.mutex.Unlock()
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) { w.WriteHeader(http.StatusBadGateway) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/events") {
			if !fault.intercept(t, r) {
				return
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { fault.releaseFirst(); fault.releaseTail(); server.CloseClientConnections(); server.Close() })
	return fault, server
}

func (fault *uploadFault) intercept(t *testing.T, r *http.Request) bool {
	t.Helper()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		t.Error("read upload request:", err)
		return false
	}
	_ = r.Body.Close()
	var envelope struct {
		Events []struct {
			Seq int64 `json:"seq"`
		} `json:"events"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope.Events) == 0 {
		t.Error("invalid upload envelope")
		return false
	}
	sample := uploadSample{Count: len(envelope.Events), Bytes: len(raw), First: envelope.Events[0].Seq, Last: envelope.Events[len(envelope.Events)-1].Seq}
	fault.mutex.Lock()
	fault.samples = append(fault.samples, sample)
	fault.mutex.Unlock()
	if fault.mode == "oversize" {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Error(err)
			return false
		}
		fields["padding"] = json.RawMessage(`"` + strings.Repeat("x", 17<<20) + `"`)
		raw, err = json.Marshal(fields)
		if err != nil {
			t.Error(err)
			return false
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	r.TransferEncoding = nil
	var gate <-chan struct{}
	switch {
	case fault.mode == "timeout":
		gate = make(chan struct{})
	case fault.mode == "pressure" && sample.First == 0:
		gate = fault.first
	case fault.mode == "pressure" && sample.First >= 30:
		gate = fault.tail
	}
	if gate != nil {
		select {
		case <-gate:
		case <-r.Context().Done():
			fault.cancelOnce.Do(func() { close(fault.cancelled) })
			return false
		case <-t.Context().Done():
			return false
		}
	}
	return true
}

func (fault *uploadFault) releaseFirst() { fault.firstOnce.Do(func() { close(fault.first) }) }
func (fault *uploadFault) releaseTail()  { fault.tailOnce.Do(func() { close(fault.tail) }) }
func (fault *uploadFault) snapshot() ([]uploadSample, []int) {
	fault.mutex.Lock()
	defer fault.mutex.Unlock()
	return append([]uploadSample(nil), fault.samples...), append([]int(nil), fault.statuses...)
}

type uploadProbeEvent struct {
	ID           string `json:"id"`
	Phase        string `json:"phase"`
	Permanent    bool   `json:"permanent"`
	CapacityDone bool   `json:"capacityDone"`
	Message      string `json:"message"`
	LocalCount   int    `json:"localCount"`
	Status       struct {
		PendingEvents int  `json:"pendingEvents"`
		PendingBytes  int  `json:"pendingBytes"`
		InFlight      bool `json:"inFlight"`
	} `json:"status"`
}

type uploadProbe struct {
	input   io.WriteCloser
	output  chan uploadProbeEvent
	backlog map[string][]uploadProbeEvent
	next    int
}

func startUploadProbe(t *testing.T, f *liveSession, proxyURL string) *uploadProbe {
	t.Helper()
	command := exec.Command("bun", "run", "../../oma-brain-dsh/tests/transcript-live-worker.ts")
	input, err := command.StdinPipe()
	requireOK(t, err)
	output, err := command.StdoutPipe()
	requireOK(t, err)
	log, err := os.Create(filepath.Join(filepath.Dir(os.Getenv("CONFIG_FILE")), "replication-"+f.code.ExternalID+".log"))
	requireOK(t, err)
	command.Stderr = log
	requireOK(t, command.Start())
	probe := &uploadProbe{input: input, output: make(chan uploadProbeEvent, 32), backlog: map[string][]uploadProbeEvent{}}
	done := make(chan error, 1)
	go func() {
		defer close(probe.output)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			var event uploadProbeEvent
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				done <- fmt.Errorf("invalid probe response: %w", err)
				return
			}
			probe.output <- event
		}
		if err := scanner.Err(); err != nil {
			done <- err
			return
		}
		done <- command.Wait()
	}()
	t.Cleanup(func() {
		_ = input.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error("replication probe:", err)
			}
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-done
			t.Error("replication probe cleanup timed out")
		}
		_ = log.Close()
	})
	configuration := map[string]any{"url": proxyURL, "directory": t.TempDir(), "claim": map[string]string{
		"code_session_id": f.code.ExternalID, "worker_epoch": fmt.Sprint(f.code.CurrentWorkerEpoch),
		"session_token": dsrOwnerToken(t, f), "model_token": "unused", "claim_expires_at": time.Now().Add(time.Minute).Format(time.RFC3339),
	}}
	requireOK(t, json.NewEncoder(input).Encode(configuration))
	probe.expect(t, "", "ready")
	return probe
}

func (probe *uploadProbe) send(t *testing.T, action string, count, payloadBytes int) string {
	t.Helper()
	probe.next++
	id := fmt.Sprintf("%s-%d", action, probe.next)
	requireOK(t, json.NewEncoder(probe.input).Encode(map[string]any{"id": id, "action": action, "count": count, "payloadBytes": payloadBytes}))
	return id
}

func (probe *uploadProbe) expect(t *testing.T, id, phase string) uploadProbeEvent {
	t.Helper()
	key := id + ":" + phase
	if events := probe.backlog[key]; len(events) > 0 {
		probe.backlog[key] = events[1:]
		return events[0]
	}
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event, open := <-probe.output:
			if !open {
				t.Fatal("replication probe exited before expected response")
			}
			if event.ID == id && event.Phase == phase {
				return event
			}
			if event.Phase == "failed" && phase != "failed" {
				t.Fatalf("probe operation %s failed: %s", event.ID, event.Message)
			}
			eventKey := event.ID + ":" + event.Phase
			probe.backlog[eventKey] = append(probe.backlog[eventKey], event)
		case <-timer.C:
			t.Fatalf("probe %s did not reach %s", id, phase)
		case <-t.Context().Done():
			t.Fatal("replication scenario cancelled")
		}
	}
}

func assertUploaded(t *testing.T, f *liveSession, count, payloadBytes int) {
	t.Helper()
	scope := db.DSHScope{OrganizationUUID: f.code.OrganizationUUID, WorkspaceUUID: f.code.WorkspaceUUID, CodeSessionUUID: f.code.UUID}
	expected := strings.Repeat("x", payloadBytes)
	for from := 0; ; {
		header, events, err := f.env.database.ReadDSHTranscript(t.Context(), scope, f.code.ExternalID, int64(from), 200)
		requireOK(t, err)
		if header.NextSeq != int64(count) {
			t.Fatalf("stored watermark=%d want=%d", header.NextSeq, count)
		}
		for _, event := range events {
			var record struct {
				Seq  int `json:"seq"`
				Data struct {
					Index   int    `json:"index"`
					Payload string `json:"payload"`
				} `json:"data"`
			}
			requireOK(t, json.Unmarshal(event.Payload, &record))
			if record.Seq != from || record.Data.Index != from || record.Data.Payload != expected {
				t.Fatalf("stored transcript differs at seq=%d", from)
			}
			from++
		}
		if from == count {
			return
		}
		if len(events) == 0 {
			t.Fatal("missing persisted transcript tail")
		}
	}
}

func TestChatTranscriptReplication(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	t.Run("count_and_byte_batches", func(t *testing.T) {
		for _, workload := range []struct{ count, size int }{{405, 0}, {6, 1500 << 10}} {
			f := e.newSession(t)
			fault, gateway := newUploadFault(t, e.url, "pass")
			probe := startUploadProbe(t, f, gateway.URL)
			probe.expect(t, probe.send(t, "prepare", workload.count, workload.size), "done")
			probe.expect(t, probe.send(t, "flush", 0, 0), "done")
			samples, _ := fault.snapshot()
			var counts []int
			for _, sample := range samples {
				if sample.Count > 200 || sample.Bytes > 4<<20 {
					t.Fatalf("batch exceeds budget: %+v", sample)
				}
				counts = append(counts, sample.Count)
			}
			expected := "[200 200 5]"
			if workload.size > 0 {
				expected = "[2 2 2]"
			}
			if fmt.Sprint(counts) != expected {
				t.Fatalf("batch counts=%v want=%s", counts, expected)
			}
			assertUploaded(t, f, workload.count, workload.size)
			t.Logf("count=%d payload_bytes=%d batches=%v", workload.count, workload.size, counts)
		}
	})
	if t.Failed() {
		return
	}
	chatProof(t, started, "replication_batches")
	t.Run("real_inflight_backpressure", func(t *testing.T) {
		f := e.newSession(t)
		fault, gateway := newUploadFault(t, e.url, "pressure")
		probe := startUploadProbe(t, f, gateway.URL)
		probe.expect(t, probe.send(t, "prepare", 40, 1<<20), "done")
		flush := probe.send(t, "flush", 0, 0)
		waitRealWorker(t, "held first upload", func() bool { samples, _ := fault.snapshot(); return len(samples) > 0 })
		capacity := probe.send(t, "capacity", 0, 0)
		waiting := probe.expect(t, capacity, "started")
		if waiting.Status.PendingBytes < 32<<20 || !waiting.Status.InFlight {
			t.Fatal("in-flight batch absent from backlog")
		}
		probe.expect(t, probe.send(t, "append", 1, 1<<20), "done")
		status := probe.expect(t, probe.send(t, "status", 0, 0), "done")
		if status.CapacityDone {
			t.Fatal("capacity admitted while upload remained blocked")
		}
		fault.releaseFirst()
		resumed := probe.expect(t, capacity, "done")
		if resumed.Status.PendingBytes > 16<<20 || resumed.Status.PendingEvents == 0 {
			t.Fatal("capacity did not resume at low watermark with remaining backlog")
		}
		fault.releaseTail()
		probe.expect(t, flush, "done")
		probe.expect(t, probe.send(t, "flush", 0, 0), "done")
		assertUploaded(t, f, 41, 1<<20)
		t.Logf("high_backlog_bytes=%d resumed_backlog_bytes=%d local_append_count=%d", waiting.Status.PendingBytes, resumed.Status.PendingBytes, status.LocalCount)
	})
	if t.Failed() {
		return
	}
	chatProof(t, started, "replication_backpressure")
	t.Run("backend_413_is_terminal", func(t *testing.T) {
		f := e.newSession(t)
		fault, gateway := newUploadFault(t, e.url, "oversize")
		probe := startUploadProbe(t, f, gateway.URL)
		probe.expect(t, probe.send(t, "prepare", 2, 0), "done")
		failure := probe.expect(t, probe.send(t, "flush", 0, 0), "failed")
		if !failure.Permanent || failure.LocalCount != 2 {
			t.Fatal("413 did not preserve local log and fail permanently")
		}
		probe.expect(t, probe.send(t, "flush", 0, 0), "failed")
		samples, statuses := fault.snapshot()
		if len(samples) != 1 || !containsUploadStatus(statuses, 413) {
			t.Fatalf("413 was retried or not returned by backend: requests=%d statuses=%v", len(samples), statuses)
		}
		assertUploaded(t, f, 0, 0)
	})
	if t.Failed() {
		return
	}
	chatProof(t, started, "replication_413_terminal")
	t.Run("default_shutdown_deadline", func(t *testing.T) {
		f := e.newSession(t)
		fault, gateway := newUploadFault(t, e.url, "timeout")
		probe := startUploadProbe(t, f, gateway.URL)
		probe.expect(t, probe.send(t, "prepare", 2, 0), "done")
		flush := probe.send(t, "flush", 0, 0)
		waitRealWorker(t, "held shutdown upload", func() bool { samples, _ := fault.snapshot(); return len(samples) > 0 })
		stopping := time.Now()
		failure := probe.expect(t, probe.send(t, "drain", 0, 0), "failed")
		elapsed := time.Since(stopping)
		if elapsed < 4500*time.Millisecond || elapsed > 8*time.Second || failure.LocalCount != 2 {
			t.Fatalf("shutdown deadline=%s local_count=%d", elapsed, failure.LocalCount)
		}
		probe.expect(t, flush, "failed")
		select {
		case <-fault.cancelled:
		case <-time.After(2 * time.Second):
			t.Fatal("shutdown did not cancel in-flight HTTP")
		}
		probe.expect(t, probe.send(t, "flush", 0, 0), "failed")
		samples, _ := fault.snapshot()
		if len(samples) != 1 {
			t.Fatal("upload restarted after shutdown")
		}
		assertUploaded(t, f, 0, 0)
		t.Logf("shutdown_elapsed_ms=%d pending_events=%d local_count=%d", elapsed.Milliseconds(), failure.Status.PendingEvents, failure.LocalCount)
	})
	if t.Failed() {
		return
	}
	chatProof(t, started, "replication_shutdown_deadline")
}

func containsUploadStatus(statuses []int, want int) bool {
	for _, status := range statuses {
		if status == want {
			return true
		}
	}
	return false
}

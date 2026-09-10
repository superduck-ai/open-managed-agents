package workerevents

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Opt-in regression: the production broker and an isolated three-node NATS
// cluster serve a real Claude Code process. CCR HTTP and model output are test
// fixtures, so this does not use production credentials, a model provider, or MCP.
func TestRealWorkerControlDelivery(t *testing.T) {
	if os.Getenv("OMA_WORKER_CONTROL_PROBE") != "1" {
		t.Skip("set OMA_WORKER_CONTROL_PROBE=1 to run the isolated Docker investigation")
	}
	servers := runNATSCluster(t)
	connection := connectNATS(t, servers[0].ClientURL())
	broker, err := NewJetStream(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	js, err := jetstream.New(connection)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name            string
		tool            bool
		queuedUser      bool
		unknownResponse bool
		interrupt       bool
	}{
		{name: "unknown_response_then_permission", tool: true, unknownResponse: true},
		{name: "interrupt_waiting_task", tool: true, interrupt: true},
		{name: "permission_cycle", tool: true},
		{name: "reply_overtakes_queued_user", tool: true, queuedUser: true},
		{name: "text_only"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			p := newControlCycleProbe(t, broker, scenario.tool)
			if scenario.queuedUser {
				p.modelContinue = make(chan struct{})
			}
			p.publish("initialize", "control_request", map[string]any{
				"request_id": "initialize_probe", "request": map[string]any{"subtype": "initialize"},
			})
			p.startWorker()
			p.waitFor("initialize ACK", 35*time.Second, func() bool { return p.processed("initialize") })
			p.publish("user_probe", "user", map[string]any{
				"parent_tool_use_id": nil,
				"message":            map[string]any{"role": "user", "content": "Run the requested test tool if provided, then reply done."},
			})
			if !scenario.tool {
				p.waitFor("text-only user ACK", 25*time.Second, func() bool { return p.processed("user_probe") })
				return
			}
			p.waitFor("worker permission request", 25*time.Second, func() bool {
				p.mu.Lock()
				defer p.mu.Unlock()
				return p.permissionRequestID != ""
			})
			if scenario.interrupt {
				p.publish("interrupt_probe", "control_request", map[string]any{"request_id": "interrupt_request", "request": map[string]any{"subtype": "interrupt"}})
				p.waitFor("interrupted user ACK", 20*time.Second, func() bool { return p.processed("user_probe") })
				return
			}
			if scenario.unknownResponse {
				p.publish("unknown_reply", "control_response", map[string]any{"response": map[string]any{"subtype": "success", "request_id": "no_such_request", "response": map[string]any{}}})
				p.waitFor("unknown response ACK", 20*time.Second, func() bool { return p.processed("unknown_reply") })
			}
			p.mu.Lock()
			requestID, toolUseID := p.permissionRequestID, p.toolUseID
			p.mu.Unlock()
			if scenario.queuedUser {
				p.publish("queued_user_probe", "user", map[string]any{
					"parent_tool_use_id": nil,
					"message":            map[string]any{"role": "user", "content": "When the current task finishes, reply queued done."},
				})
			}
			// The queued user is still blocked, including across an SSE reconnect.
			p.assertBlocked(time.Second)
			p.reconnect()
			p.assertBlocked(time.Second)
			p.publish("allow_probe", "control_response", map[string]any{
				"response": map[string]any{"subtype": "success", "request_id": requestID,
					"response": map[string]any{"behavior": "allow", "toolUseID": toolUseID,
						"updatedInput": map[string]any{"file_path": "/tmp/oma-control-probe.txt", "content": "isolated proof"}}},
			})
			p.waitFor("allow response ACK", 20*time.Second, func() bool { return p.processed("allow_probe") })
			if scenario.queuedUser {
				p.waitFor("second model call", 10*time.Second, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.modelCalls >= 2 })
				p.reconnect()
				close(p.modelContinue)
			}
			p.waitFor("original user ACK", 20*time.Second, func() bool { return p.processed("user_probe") })
			if scenario.queuedUser {
				p.waitFor("queued user ACK", 20*time.Second, func() bool { return p.processed("queued_user_probe") })
			}
			proof, err := exec.Command("docker", "exec", p.containerName, "cat", "/tmp/oma-control-probe.txt").Output()
			if err != nil || string(proof) != "isolated proof" {
				t.Fatalf("tool execution proof = %q, error = %v", proof, err)
			}
			t.Log("TOOL_EXECUTED isolated proof file verified")
			for _, lane := range deliveryLanes {
				consumer, err := js.Consumer(t.Context(), StreamName, lane.consumerName(p.sessionID))
				if err != nil {
					t.Fatal(err)
				}
				info, err := consumer.Info(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if info.Config.MaxAckPending != 1 || info.NumAckPending != 0 || info.NumPending != 0 {
					t.Fatalf("lane %q not drained at window 1: %+v", lane, info)
				}
			}
			t.Log("BOTH_LANES_DRAINED max_ack_pending=1")
		})
	}
}

type controlCycleProbe struct {
	t                   *testing.T
	broker              *JetStreamBroker
	sessionID           string
	tool                bool
	baseURL             string
	mu                  sync.Mutex
	ackSubjects         map[string]string
	statuses            map[string][]string
	permissionRequestID string
	toolUseID           string
	modelContinue       chan struct{}
	modelCalls          int
	logFile             string
	containerName       string
	disconnect          context.CancelFunc
	connections         int
}

func newControlCycleProbe(t *testing.T, broker *JetStreamBroker, tool bool) *controlCycleProbe {
	t.Helper()
	p := &controlCycleProbe{t: t, broker: broker, tool: tool,
		sessionID:   "cse_probe_" + fmt.Sprint(time.Now().UnixNano()),
		ackSubjects: make(map[string]string), statuses: make(map[string][]string)}
	server := httptest.NewUnstartedServer(http.HandlerFunc(p.serveHTTP))
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	p.baseURL = fmt.Sprintf("http://host.docker.internal:%d", listener.Addr().(*net.TCPAddr).Port)
	return p
}

func (p *controlCycleProbe) publish(id, eventType string, payload map[string]any) {
	p.t.Helper()
	payload["type"], payload["uuid"], payload["session_id"] = eventType, id, p.sessionID
	body, err := json.Marshal(payload)
	if err != nil {
		p.t.Fatal(err)
	}
	subtype := ""
	if request, ok := payload["request"].(map[string]any); ok {
		subtype, _ = request["subtype"].(string)
	}
	envelope := EventEnvelope(p.sessionID, "csev_"+id, id, eventType, subtype, body, time.Now().Add(time.Hour))
	if err := p.broker.Publish(p.t.Context(), p.sessionID+id, envelope); err != nil {
		p.t.Fatal(err)
	}
	p.t.Logf("PUBLISH id=%s type=%s", id, eventType)
}

func (p *controlCycleProbe) startWorker() {
	p.t.Helper()
	dir := os.Getenv("OMA_WORKER_CONTROL_OUTPUT")
	if dir == "" {
		dir = p.t.TempDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		p.t.Fatal(err)
	}
	p.logFile = filepath.Join(dir, p.sessionID+".log")
	log, err := os.Create(p.logFile)
	if err != nil {
		p.t.Fatal(err)
	}
	p.t.Cleanup(func() { _ = log.Close() })
	image := os.Getenv("OMA_WORKER_CONTROL_IMAGE")
	if image == "" {
		image = "ghcr.io/superduck-ai/managed-agent-sandbox:latest"
	}
	containerName := "oma-control-probe-" + p.sessionID
	p.containerName = containerName
	args := []string{"run", "--rm", "--pull=never", "--name", containerName, "--entrypoint", "/opt/claude-code/bin/claude"}
	for key, value := range map[string]string{
		"ANTHROPIC_BASE_URL": p.baseURL, "ANTHROPIC_API_KEY": "isolated-fake-model-key",
		"CLAUDE_CODE_SESSION_ACCESS_TOKEN": "isolated-fake-ingress-token",
		"CLAUDE_CODE_WORKER_EPOCH":         "1", "CLAUDE_CODE_USE_CCR_V2": "1",
		"CLAUDE_CODE_POST_FOR_SESSION_INGRESS_V2": "1", "CLAUDE_CODE_REMOTE": "true",
		"CLAUDE_CODE_REMOTE_SESSION_ID":                        p.sessionID,
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC":             "1",
		"CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL": "1",
		"CLAUDE_CODE_ENABLE_BACKGROUND_PLUGIN_REFRESH":         "0",
	} {
		args = append(args, "-e", key+"="+value)
	}
	args = append(args, image, "--output-format=stream-json", "--verbose", "--input-format=stream-json",
		"--debug-to-stderr", "--include-partial-messages", "--replay-user-messages",
		"--model", "claude-sonnet-4-6", "--sdk-url", p.baseURL+"/v1/code/sessions/"+p.sessionID)
	cmd := exec.Command("docker", args...)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		p.t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	p.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, "docker", "stop", "--time", "1", containerName).Run()
		select {
		case <-done:
		case <-ctx.Done():
			_ = cmd.Process.Kill()
		}
		p.t.Logf("worker log: %s", p.logFile)
	})
}

func (p *controlCycleProbe) serveHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasSuffix(path, "/worker/events/stream") {
		p.stream(w, r)
		return
	}
	var body map[string]any
	if r.Body != nil && r.ContentLength != 0 {
		var reader io.Reader = r.Body
		if r.Header.Get("Content-Encoding") == "gzip" {
			compressed, err := gzip.NewReader(r.Body)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			defer compressed.Close()
			reader = compressed
		}
		_ = json.NewDecoder(io.LimitReader(reader, 4<<20)).Decode(&body)
	}
	response := map[string]any{"ok": true}
	switch {
	case path == "/v1/messages":
		p.modelResponse(w)
		return
	case strings.HasSuffix(path, "/worker/events/delivery"):
		p.delivery(r.Context(), body)
		response["applied"], response["ignored"] = len(arrayObjects(body["updates"])), 0
	case strings.HasSuffix(path, "/worker/events"):
		for _, event := range arrayObjects(body["events"]) {
			payload, _ := event["payload"].(map[string]any)
			request, _ := payload["request"].(map[string]any)
			if payload["type"] == "control_request" && request["subtype"] == "can_use_tool" {
				p.mu.Lock()
				p.permissionRequestID, _ = payload["request_id"].(string)
				p.toolUseID, _ = request["tool_use_id"].(string)
				p.mu.Unlock()
				p.t.Logf("PERMISSION request_id=%s tool=%v tool_use_id=%s", payload["request_id"], request["tool_name"], request["tool_use_id"])
			}
			if payload["type"] == "result" {
				p.t.Logf("RESULT subtype=%v is_error=%v", payload["subtype"], payload["is_error"])
			}
		}
	case strings.HasSuffix(path, "/worker") && r.Method == "GET":
		response = map[string]any{"worker": map[string]any{}}
	case strings.HasSuffix(path, "/worker"):
		p.t.Logf("WORKER state=%v", body["worker_status"])
		response["worker_epoch"] = "1"
	case strings.HasSuffix(path, "/worker/register"), strings.HasSuffix(path, "/worker/heartbeat"):
		response["worker_epoch"], response["has_subscribers"] = "1", true
	case strings.HasSuffix(path, "/internal_events"):
		response = map[string]any{"data": []any{}, "has_more": false}
	default:
		p.t.Logf("HTTP %s %s", r.Method, path)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func arrayObjects(value any) []map[string]any {
	values, _ := value.([]any)
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if object, ok := value.(map[string]any); ok {
			result = append(result, object)
		}
	}
	return result
}

func (p *controlCycleProbe) stream(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	subscription, err := p.broker.Subscribe(ctx, p.sessionID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer subscription.Close()
	p.mu.Lock()
	p.connections++
	p.disconnect = cancel
	p.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	flusher := w.(http.Flusher)
	flusher.Flush()
	for {
		select {
		case <-ctx.Done():
			return
		case delivery, open := <-subscription.Messages():
			if !open {
				return
			}
			e := delivery.Envelope
			p.mu.Lock()
			p.ackSubjects[e.PayloadEventID] = delivery.AckSubject
			p.mu.Unlock()
			body, _ := json.Marshal(map[string]any{"event_id": e.PayloadEventID, "event_type": e.EventType,
				"type": e.EventType, "sequence_num": e.SequenceNum, "session_id": p.sessionID, "payload": e.Payload})
			if _, err := fmt.Fprintf(w, "id: %d\nevent: client_event\ndata: %s\n\n", e.SequenceNum, body); err != nil {
				return
			}
			flusher.Flush()
			p.t.Logf("DELIVER id=%s type=%s sequence=%d", e.PayloadEventID, e.EventType, e.SequenceNum)
		}
	}
}

func (p *controlCycleProbe) delivery(ctx context.Context, body map[string]any) {
	for _, update := range arrayObjects(body["updates"]) {
		id, _ := update["event_id"].(string)
		status, _ := update["status"].(string)
		p.mu.Lock()
		ack := p.ackSubjects[id]
		p.mu.Unlock()
		var err error
		if status == "processed" {
			err = p.broker.DoubleAck(ctx, ack)
		} else {
			err = p.broker.InProgress(ctx, ack)
		}
		if err != nil {
			p.t.Errorf("ACK id=%s status=%s: %v", id, status, err)
			continue
		}
		p.mu.Lock()
		p.statuses[id] = append(p.statuses[id], status)
		p.mu.Unlock()
		p.t.Logf("ACK id=%s status=%s epoch=%v applied=true", id, status, body["worker_epoch"])
	}
}

func (p *controlCycleProbe) processed(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, status := range p.statuses[id] {
		if status == "processed" {
			return true
		}
	}
	return false
}

func (p *controlCycleProbe) waitFor(label string, timeout time.Duration, ready func() bool) {
	p.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	data, _ := os.ReadFile(p.logFile)
	p.t.Fatalf("timeout waiting for %s; worker tail:\n%s", label, data[max(0, len(data)-8000):])
}

func (p *controlCycleProbe) assertBlocked(duration time.Duration) {
	p.t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		_, delivered := p.ackSubjects["allow_probe"]
		p.mu.Unlock()
		if delivered || p.processed("user_probe") {
			p.t.Fatal("control response or original completion arrived before the intervention")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (p *controlCycleProbe) modelResponse(w http.ResponseWriter) {
	p.mu.Lock()
	p.modelCalls++
	call := p.modelCalls
	p.mu.Unlock()
	p.t.Logf("MODEL fixture_call=%d", call)
	if call == 2 && p.modelContinue != nil {
		select {
		case <-p.modelContinue:
		case <-p.t.Context().Done():
			return
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	write := func(event string, data map[string]any) {
		data["type"] = event
		encoded, _ := json.Marshal(data)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded)
	}
	write("message_start", map[string]any{"message": map[string]any{
		"id": fmt.Sprintf("msg_probe_%d", call), "type": "message", "role": "assistant", "model": "claude-sonnet-4-6",
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 10, "output_tokens": 1}}})
	stopReason := "end_turn"
	if p.tool && call == 1 {
		stopReason = "tool_use"
		write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{
			"type": "tool_use", "id": "toolu_probe", "name": "Write", "input": map[string]any{}}})
		write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{
			"type": "input_json_delta", "partial_json": `{"file_path":"/tmp/oma-control-probe.txt","content":"isolated proof"}`}})
	} else {
		write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
		write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": "done"}})
	}
	write("content_block_stop", map[string]any{"index": 0})
	write("message_delta", map[string]any{"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 12}})
	write("message_stop", map[string]any{})
}

func (p *controlCycleProbe) reconnect() {
	p.t.Helper()
	p.mu.Lock()
	disconnect, previous := p.disconnect, p.connections
	p.mu.Unlock()
	disconnect()
	p.waitFor("worker SSE reconnect", 10*time.Second, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.connections > previous })
}

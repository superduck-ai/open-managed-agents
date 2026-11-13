//go:build integration || e2e || e2b_integration

package liveworker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func verifyLiveClaimProtocol(t *testing.T, e *liveEnv, f *liveSession, started time.Time, sandbox string) {
	t.Helper()
	token, err := e.credentials.Issue(brainCredentialIdentity(t, f, f.code.CurrentWorkerEpoch))
	requireOK(t, err)
	endpoint := "/v2/ccr-sessions/" + f.code.ExternalID + "/threads/root/tool-calls"
	submit := func(id, command string, timeoutMS int) {
		e.request(t, "POST", endpoint, token, map[string]any{"call_id": id, "tool": "bash", "input": map[string]any{"command": command}, "timeout_ms": timeoutMS}, 202)
	}
	wait := func(id, state string) db.SandboxToolCall {
		var call db.SandboxToolCall
		scope := db.SandboxToolCallScope{OrganizationUUID: f.code.OrganizationUUID, WorkspaceUUID: f.code.WorkspaceUUID, CodeSessionID: f.code.ExternalID, WorkerEpoch: f.code.CurrentWorkerEpoch, ThreadID: "root", CallID: id}
		waitRealWorker(t, "durable call "+id+" becomes "+state, func() bool {
			call, err = e.database.GetSandboxToolCall(t.Context(), scope)
			requireOK(t, err)
			return call.State == state
		}, time.Minute)
		return call
	}
	submit("live-once", "printf x >> /tmp/oma-live-once", 30000)
	submit("live-once", "printf x >> /tmp/oma-live-once", 30000)
	wait("live-once", "succeeded")
	output, err := exec.CommandContext(t.Context(), "docker", "exec", sandbox, "cat", "/tmp/oma-live-once").Output()
	requireOK(t, err)
	if string(output) != "x" {
		t.Fatal("same call identity repeated its side effect")
	}
	submit("live-large", `head -c 50000 /dev/zero | tr '\000' x`, 30000)
	large := wait("live-large", "succeeded")
	if large.ResultObjectKey == nil {
		t.Fatal("large result did not use object storage")
	}
	body := e.request(t, "GET", endpoint+"/live-large", token, nil, 200)
	if len(body) <= 32<<10 || !bytes.Contains(body, []byte("xxxxx")) {
		t.Fatal("large result did not roundtrip")
	}
	chatProof(t, started, "live_claim_replay_and_object")
	for _, tc := range []struct {
		id        string
		timeoutMS int
		cancel    bool
	}{
		{"live-cancel", 150000, true}, {"live-deadline", 4000, false},
	} {
		pidFile := "/tmp/" + tc.id + ".pid"
		submit(tc.id, fmt.Sprintf("echo $$ > %s; printf partial-marker; sleep 120; touch /tmp/%s-forbidden", pidFile, tc.id), tc.timeoutMS)
		wait(tc.id, "running")
		waitRealWorker(t, "Bash process exists before stop", func() bool {
			return exec.CommandContext(t.Context(), "docker", "exec", sandbox, "test", "-s", pidFile).Run() == nil
		})
		if tc.cancel {
			e.request(t, "POST", endpoint+"/"+tc.id+"/cancel", token, map[string]any{}, 200)
		}
		wait(tc.id, "cancelled")
		var reply struct {
			Result json.RawMessage `json:"result"`
		}
		requireOK(t, json.Unmarshal(e.request(t, "GET", endpoint+"/"+tc.id, token, nil, 200), &reply))
		if !bytes.Contains(reply.Result, []byte("partial-marker")) {
			t.Fatal("stopped call lost partial output")
		}
		if exec.CommandContext(t.Context(), "docker", "exec", sandbox, "sh", "-c", `kill -0 "$(cat "$1")" 2>/dev/null`, "sh", pidFile).Run() == nil {
			t.Fatal("cancelled Bash process remains alive")
		}
		if exec.CommandContext(t.Context(), "docker", "exec", sandbox, "test", "-e", "/tmp/"+tc.id+"-forbidden").Run() == nil {
			t.Fatal("stopped command continued its side effect")
		}
	}
	chatProof(t, started, "live_claim_cancel_and_deadline")
}

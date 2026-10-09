package liveworker

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestFilesGenerated(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{"version": e.agent.CurrentVersion, "model": map[string]string{"id": "claude-sonnet-4-6"}, "tools": json.RawMessage(`[{"type":"agent_toolset_20260401","default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}}]`)}, 200)
	var calls, queued atomic.Int32
	resume := make(chan struct{})
	close(resume)
	modelURL := realWorkerModelFixture(t, &calls, &queued, false, resume, "/mnt/user-data/outputs/verified.txt")
	configureChatModel(t, e, strings.Replace(modelURL, "host.docker.internal", "127.0.0.1", 1))
	_, stopRunner := startPublicRunner(t, e)
	defer stopRunner()
	f := createPublicChat(t, e)
	client := chatSDK(e)
	before, err := e.database.ListFiles(t.Context(), e.key.WorkspaceUUID.String(), f.session.ExternalID)
	requireOK(t, err)
	if len(before) != 0 {
		t.Fatal("output exists before the Worker turn")
	}
	sendChatSDK(t, &client, f.session.ExternalID, anthropic.BetaManagedAgentsEventParamsUnion{OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{Type: "user.message", Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{OfText: &anthropic.BetaManagedAgentsTextBlockParam{Type: "text", Text: "Write the requested output file and reply done."}}}}})
	var record db.FileRecord
	waitRealWorker(t, "Worker output projected into Files", func() bool {
		files, err := e.database.ListFiles(t.Context(), e.key.WorkspaceUUID.String(), f.session.ExternalID)
		requireOK(t, err)
		for _, file := range files {
			if file.Filename == "verified.txt" {
				record = file
				return true
			}
		}
		return false
	})
	waitRealWorker(t, "generated file tool turn idle", func() bool {
		session, err := client.Beta.Sessions.Get(t.Context(), f.session.ExternalID, anthropic.BetaSessionGetParams{})
		requireOK(t, err)
		return calls.Load() == 2 && session.Status == "idle"
	})
	f.code, err = e.database.GetCodeSessionBySessionExternalID(t.Context(), e.key.WorkspaceUUID.String(), f.session.ExternalID)
	requireOK(t, err)
	registerPublicSandboxCleanup(t, e, f)
	history := chatSDKHistory(t, &client, f.session.ExternalID)
	var toolID string
	for _, event := range history {
		if event.Type == "agent.tool_use" {
			toolID = event.ID
		}
	}
	assertSDKToolHistory(t, history, toolID, "allow", 0)
	chatProof(t, started, "worker_output_projected")
	var session struct {
		Resources []struct {
			FileID    string `json:"file_id"`
			MountPath string `json:"mount_path"`
		} `json:"resources"`
	}
	requireOK(t, json.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID, e.apiKey, nil, 200), &session))
	found := false
	for _, resource := range session.Resources {
		if resource.FileID == record.ExternalID && resource.MountPath == "/outputs/verified.txt" {
			found = true
		}
	}
	if !found || !record.Downloadable {
		t.Fatal("generated output resource or downloadable metadata missing")
	}
	betas := []anthropic.AnthropicBeta{anthropic.AnthropicBetaFilesAPI2025_04_14}
	page, err := client.Beta.Files.List(t.Context(), anthropic.BetaFileListParams{Betas: betas, ScopeID: anthropic.String(f.session.ExternalID)})
	requireOK(t, err)
	if len(page.Data) != 1 || page.Data[0].ID != record.ExternalID {
		t.Fatal("session-scoped Files listing differs")
	}
	const content = "verified control delivery"
	response, err := client.Beta.Files.Download(t.Context(), record.ExternalID, anthropic.BetaFileDownloadParams{Betas: betas})
	requireOK(t, err)
	downloaded, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	requireOK(t, err)
	requireOK(t, response.Body.Close())
	if string(downloaded) != content {
		t.Fatal("generated download differs")
	}
	object, err := e.objects.Open(t.Context(), record.S3Key, nil)
	requireOK(t, err)
	data, err := io.ReadAll(object.Body)
	requireOK(t, err)
	requireOK(t, object.Body.Close())
	if string(data) != content {
		t.Fatal("generated object differs")
	}
	chatProof(t, started, "generated_download_matches")
	_, err = client.Beta.Files.Delete(t.Context(), record.ExternalID, anthropic.BetaFileDeleteParams{Betas: betas})
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 409 {
		t.Fatalf("referenced output deletion error=%v", err)
	}
	chatProof(t, started, "generated_reference_protected")
}

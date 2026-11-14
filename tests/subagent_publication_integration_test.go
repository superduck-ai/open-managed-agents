//go:build integration || e2e || e2b_integration

package tests

import (
	"errors"
	"strings"
	"testing"
)

func subagentWorkerEventsBody(epoch string, payloads ...string) string {
	return internalPayloadRequest(epoch, payloads...)
}

func subagentThreadCreatedPayload(uuid, threadID, agentID string) string {
	return `{"type":"session.thread_created","uuid":` + quoteJSON(uuid) + `,"session_thread_id":` + quoteJSON(threadID) + `,"agent_id":` + quoteJSON(agentID) + `,"agent_name":"analyst","created_at":"2026-11-14T01:00:00Z"}`
}

func subagentAssistantPayload(uuid, agentID, text string) string {
	return `{"type":"assistant","uuid":` + quoteJSON(uuid) + `,"agentId":` + quoteJSON(agentID) + `,"isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":` + quoteJSON(text) + `}]}}`
}

func TestSubagentInternalProjectionFailureRepairedByLaterPublicEvent(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("subagent-projection-repair")}
	app := newPayloadIntegrationApp(t, objects)
	codeSession, epoch := newPayloadIntegrationSession(t, app)
	suffix := strings.TrimPrefix(codeSession.ExternalID, "cse_")
	threadID, agentID := "sthr_repair_"+suffix, "agent_repair_"+suffix

	postCodeSessionWorkerEvents(t, app, codeSession.ExternalID, subagentWorkerEventsBody(epoch, subagentThreadCreatedPayload("created-"+suffix, threadID, agentID)))
	postCodeSessionWorkerInternalEvents(t, app, codeSession.ExternalID, internalPayloadRequest(epoch, subagentAssistantPayload("head-"+suffix, agentID, "projected head")))
	if events := listThreadEvents(t, app, codeSession.SessionExternalID, threadID, defaultTestKey); !eventPageContains(events, "projected head") {
		t.Fatalf("child thread missing projected head: %+v", events.Data)
	}

	tail := "unprojected tail " + strings.Repeat("x", 40000)
	objects.setOpenErr(errors.New("injected payload read failure"))
	postCodeSessionWorkerInternalEvents(t, app, codeSession.ExternalID, internalPayloadRequest(epoch, subagentAssistantPayload("tail-"+suffix, agentID, tail)))
	objects.setOpenErr(nil)
	if events := listThreadEvents(t, app, codeSession.SessionExternalID, threadID, defaultTestKey); eventPageContains(events, "unprojected tail") {
		t.Fatalf("tail projected although payload read failed: %+v", events.Data)
	}

	postCodeSessionWorkerEvents(t, app, codeSession.ExternalID, subagentWorkerEventsBody(epoch,
		`{"type":"agent.message","uuid":"primary-`+suffix+`","content":[{"type":"text","text":"primary progress"}],"created_at":"2026-11-14T01:00:05Z"}`))
	if events := listThreadEvents(t, app, codeSession.SessionExternalID, threadID, defaultTestKey); !eventPageContains(events, "unprojected tail") {
		t.Fatalf("later public event did not repair the unprojected tail: %+v", events.Data)
	}
}

func TestWorkerEventsBatchAttributesAgentOnlyEventAfterThreadCreated(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("subagent-same-batch-owner"))
	codeSession, epoch := newPayloadIntegrationSession(t, app)
	suffix := strings.TrimPrefix(codeSession.ExternalID, "cse_")
	threadID, agentID := "sthr_batch_"+suffix, "agent_batch_"+suffix

	postCodeSessionWorkerEvents(t, app, codeSession.ExternalID, subagentWorkerEventsBody(epoch,
		subagentThreadCreatedPayload("created-"+suffix, threadID, agentID),
		`{"type":"agent.message","uuid":"answer-`+suffix+`","agent_id":`+quoteJSON(agentID)+`,"content":[{"type":"text","text":"child answer in same batch"}],"created_at":"2026-11-14T01:00:01Z"}`,
	))

	primary := listSessionEvents(t, app, codeSession.SessionExternalID, "order=asc", defaultTestKey)
	if eventPageContains(primary, "child answer in same batch") {
		t.Fatalf("agent-only child event landed in the primary thread: %+v", primary.Data)
	}
	child := listThreadEvents(t, app, codeSession.SessionExternalID, threadID, defaultTestKey)
	if !eventPageContains(child, "child answer in same batch") {
		t.Fatalf("agent-only child event missing from child thread: %+v", child.Data)
	}
}

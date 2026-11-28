//go:build integration || e2e || e2b_integration

package tests

import (
	"fmt"
	"testing"
)

func TestBrainLateIdleDoesNotCompleteFollowingInput(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("brain-late-idle"))
	worker, _ := newPayloadIntegrationSession(t, app)
	claimed, err := registerTaskTestWorker(t, app, worker)
	if err != nil {
		t.Fatal(err)
	}
	epoch := fmt.Sprint(claimed.CurrentWorkerEpoch)
	state := func(status string) {
		putCodeSessionWorkerState(t, app, worker.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":%q}`, epoch, status))
	}
	state("running")
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
		`{"type":"session.status_idle","id":"brain-first-idle","uuid":"brain-first-idle","stop_reason":{"type":"end_turn"}}`,
	))
	state("idle")
	assertWorkerIdleReasons(t, app, worker.SessionExternalID, "end_turn")
	queued := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"Next turn"}]}]}`, defaultTestKey)
	state("idle")
	assertWorkerIdleReasons(t, app, worker.SessionExternalID, "end_turn")
	if status := retrieveSession(t, app, worker.SessionExternalID, defaultTestKey).Status; status != "running" {
		t.Fatalf("late idle completed next input: %s", status)
	}
	consumePublicInput(t, app, worker, epoch, sessionEventStringField(t, queued.Data[0], "id"))
	state("running")
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
		`{"type":"session.status_idle","id":"brain-second-idle","uuid":"brain-second-idle","stop_reason":{"type":"end_turn"}}`,
	))
	assertWorkerIdleReasons(t, app, worker.SessionExternalID, "end_turn", "end_turn")
}

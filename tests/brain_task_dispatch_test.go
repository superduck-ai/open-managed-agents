package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type brainTaskRecorder struct {
	*workerevents.MemoryBroker
	tasks []json.RawMessage
}

func (b *brainTaskRecorder) PublishBrainTask(_ context.Context, task workerevents.BrainTask) error {
	payload, err := json.Marshal(task)
	if err != nil {
		return err
	}
	b.tasks = append(b.tasks, payload)
	return nil
}

func TestWorkerTaskRegistrationReusesEpoch(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("worker-task"))
	code, _ := newPayloadIntegrationSession(t, app)
	public, _, err := app.db.GetSession(t.Context(), code.WorkspaceUUID, code.SessionExternalID)
	if err != nil {
		t.Fatal(err)
	}
	broker := &brainTaskRecorder{MemoryBroker: workerevents.NewMemory()}
	service := codesessions.NewServiceWithCredentials(app.db, app.credentials, nil).WithWorkerEventBroker(broker)
	credentials, err := service.RecoverManagedAgentCodeSession(t.Context(), codesessions.ManagedAgentRecoverInput{Session: public, CodeSessionID: code.ExternalID})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DispatchWorker(t.Context(), credentials); err != nil {
		t.Fatal(err)
	}
	if len(broker.tasks) != 1 {
		t.Fatalf("tasks=%d", len(broker.tasks))
	}
	var task workerevents.BrainTask
	if err := json.Unmarshal(broker.tasks[0], &task); err != nil {
		t.Fatal(err)
	}
	if task.IngressToken != credentials.SessionIngressToken || task.ModelToken != credentials.OAuthAccessToken || task.WorkerEpoch != credentials.WorkerEpoch {
		t.Fatal("dispatch replaced CCR credentials")
	}
	for _, attempt := range []struct {
		id     string
		status int
	}{{"one", 200}, {"two", 200}, {"one", 200}} {
		request, err := http.NewRequestWithContext(t.Context(), "POST", app.baseURL+"/v1/code/sessions/"+code.ExternalID+"/worker/register", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+task.IngressToken)
		request.Header.Set("X-Worker-ID", attempt.id)
		response, err := app.client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != attempt.status {
			t.Fatalf("registration status=%d want=%d", response.StatusCode, attempt.status)
		}
	}
	oldToken := task.IngressToken
	if _, err := service.RecoverManagedAgentCodeSession(t.Context(), codesessions.ManagedAgentRecoverInput{Session: public, CodeSessionID: code.ExternalID}); err != nil {
		t.Fatal(err)
	}
	response := doSessionBearerRequest(t, app, "POST", "/v1/code/sessions/"+code.ExternalID+"/worker/register", strings.NewReader(`{}`), oldToken, false)
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatalf("stale credential accepted: %d", response.StatusCode)
	}
}

func TestExternalWorkerRecoveryUsesEnvironmentWorkClaim(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("worker-recovery"))
	code, _ := newPayloadIntegrationSession(t, app)
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET metadata=jsonb_set(metadata,'{config}', '{"worker_transport":"nats","runtime_skills":[{"name":"pinned"}]}'), worker_lease_expires_at=now()-interval '2 minutes' WHERE uuid=$1`, code.UUID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.pool.Exec(t.Context(), `UPDATE environment_work SET state='active', updated_at=now()-interval '2 minutes' WHERE session_uuid=$1`, code.SessionUUID); err != nil {
		t.Fatal(err)
	}
	if work, err := app.db.PollNextEnvironmentWork(t.Context(), "idle", time.Second*5); err != nil || work != nil {
		t.Fatalf("idle worker rescheduled: %v %v", work, err)
	}
	if _, err := app.pool.Exec(t.Context(), `UPDATE sessions SET status='running' WHERE uuid=$1`, code.SessionUUID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET worker_lease_expires_at=now()+interval '1 minute' WHERE uuid=$1`, code.UUID); err != nil {
		t.Fatal(err)
	}
	if work, err := app.db.PollNextEnvironmentWork(t.Context(), "live", time.Second*5); err != nil || work != nil {
		t.Fatalf("live worker rescheduled: %v %v", work, err)
	}
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET worker_lease_expires_at=now()-interval '2 minutes' WHERE uuid=$1`, code.UUID); err != nil {
		t.Fatal(err)
	}
	results := make([]*db.EnvironmentWork, 2)
	failures := make([]error, 2)
	var workers sync.WaitGroup
	for i := range 2 {
		workers.Go(func() {
			results[i], failures[i] = app.db.PollNextEnvironmentWork(t.Context(), fmt.Sprint(i), time.Second*5)
		})
	}
	workers.Wait()
	count := 0
	for i, work := range results {
		if failures[i] != nil {
			t.Fatal(failures[i])
		}
		if work != nil {
			count++
			if work.SessionUUID != code.SessionUUID {
				t.Fatal("wrong work claimed")
			}
		}
	}
	if count != 1 {
		t.Fatalf("competing runners claimed %d works", count)
	}
	public, _, err := app.db.GetSession(t.Context(), code.WorkspaceUUID, code.SessionExternalID)
	if err != nil {
		t.Fatal(err)
	}
	service := newCodeSessionService(app, nil, nil)
	credentials, err := service.RecoverManagedAgentCodeSession(t.Context(), codesessions.ManagedAgentRecoverInput{Session: public, CodeSessionID: code.ExternalID})
	if err != nil {
		t.Fatal(err)
	}
	if credentials.WorkerEpoch != code.CurrentWorkerEpoch+1 {
		t.Fatal("recovery did not rotate epoch")
	}
	stored, _, err := app.db.GetCodeSession(t.Context(), code.ExternalID)
	if err != nil || !strings.Contains(string(stored.Metadata), "pinned") {
		t.Fatal("recovery lost pinned runtime config")
	}
}

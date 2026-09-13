package tests

import (
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

func TestDeploymentsMemoryAttachContract(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("deployments-memory-attach-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-memory-attach-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"deployments-memory-attach-env"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	store := createMemoryStore(t, app, "user-preferences")
	defer deleteMemoryStore(t, app, store.ID)

	t.Run("M1-11 instructions 501 and client mount_path are rejected", func(t *testing.T) {
		resp := doDeploymentRequest(
			t, app, http.MethodPost, "/v1/deployments",
			strings.NewReader(deploymentBodyWithExtra(agent.ID, env.ID, `"resources":[{"type":"memory_store","memory_store_id":`+quoteJSON(store.ID)+`,"instructions":`+quoteJSON(strings.Repeat("i", 501))+`}]`)),
			defaultTestKey, true,
		)
		assertMemoryAttachError(t, resp, http.StatusBadRequest, "invalid_request_error", "at most 500 characters")

		resp = doDeploymentRequest(
			t, app, http.MethodPost, "/v1/deployments",
			strings.NewReader(deploymentBodyWithExtra(agent.ID, env.ID, `"resources":[{"type":"memory_store","memory_store_id":`+quoteJSON(store.ID)+`,"mount_path":"/mnt/memory/custom"}]`)),
			defaultTestKey, true,
		)
		assertMemoryAttachError(t, resp, http.StatusBadRequest, "invalid_request_error", "assigned by the server")

		resp = doDeploymentRequest(
			t, app, http.MethodPost, "/v1/deployments",
			strings.NewReader(deploymentBodyWithExtra(agent.ID, env.ID, `"resources":[{"type":"memory_store","memory_store_id":`+quoteJSON(store.ID)+`,"name":"memory"}]`)),
			defaultTestKey, true,
		)
		assertMemoryAttachError(t, resp, http.StatusBadRequest, "invalid_request_error", "assigned by the server")
	})

	t.Run("M1-11 run snapshots session resources", func(t *testing.T) {
		created := createDeployment(
			t, app,
			deploymentBodyWithExtra(agent.ID, env.ID, `"resources":[{"type":"memory_store","memory_store_id":`+quoteJSON(store.ID)+`,"access":"read_only","instructions":"有新偏好就更新"}]`),
		)
		defer cleanupDeploymentRows(t, app, created.ID)

		run := runDeployment(t, app, created.ID)
		if run.SessionID == nil || *run.SessionID == "" {
			t.Fatalf("deployment run session_id = %+v", run)
		}
		session := retrieveSession(t, app, *run.SessionID, defaultTestKey)
		defer deleteSession(t, app, session.ID)
		if len(session.Resources) != 1 {
			t.Fatalf("run session resources = %d, want 1", len(session.Resources))
		}
		got := decodeMemoryResource(t, session.Resources[0])
		if got.Access != sessionresource.MemoryAccessReadOnly ||
			got.MountPath != "/mnt/memory/user-preferences" ||
			got.Name != store.Name ||
			got.Description != store.Description ||
			got.Instructions != "有新偏好就更新" ||
			got.MemoryStoreID != store.ID {
			t.Fatalf("run snapshot = %+v, store=%+v", got, store)
		}

		stored := persistedMemoryPayload(t, app, session.ID)
		if stored["mount_path"] != "/mnt/memory/user-preferences" || stored["access"] != sessionresource.MemoryAccessReadOnly {
			t.Fatalf("stored run payload = %#v", stored)
		}
	})
}

package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

type memoryResourceAPIResponse struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	MemoryStoreID string `json:"memory_store_id"`
	Access        string `json:"access"`
	Instructions  string `json:"instructions"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	MountPath     string `json:"mount_path"`
}

func TestSessionsMemoryAttachContract(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-memory-attach-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-memory-attach-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-memory-attach-env"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)

	t.Run("M1-02 client identity fields are rejected", func(t *testing.T) {
		store := createMemoryStore(t, app, "client-identity")
		defer deleteMemoryStore(t, app, store.ID)
		for _, field := range []string{
			`"mount_path":"/mnt/memory/custom"`,
			`"name":"memory"`,
			`"description":"copied"`,
		} {
			t.Run(field, func(t *testing.T) {
				resp := doSessionRequest(
					t, app, http.MethodPost, "/v1/sessions?beta=true",
					strings.NewReader(sessionBodyWithMemoryResource(agent.ID, env.ID, store.ID, field)),
					defaultTestKey, true,
				)
				assertMemoryAttachError(t, resp, http.StatusBadRequest, "invalid_request_error", "assigned by the server")
				assertNoSessionsForAgent(t, app, agent.ID)
			})
		}
	})

	t.Run("M1-03 access defaults and rejects illegal values", func(t *testing.T) {
		store := createMemoryStore(t, app, "access-contract")
		defer deleteMemoryStore(t, app, store.ID)

		resp := doSessionRequest(
			t, app, http.MethodPost, "/v1/sessions?beta=true",
			strings.NewReader(sessionBodyWithMemoryResource(agent.ID, env.ID, store.ID, `"access":"rw"`)),
			defaultTestKey, true,
		)
		assertMemoryAttachError(t, resp, http.StatusBadRequest, "invalid_request_error", "read_write or read_only")

		created := createSession(t, app, sessionBodyWithMemoryResource(agent.ID, env.ID, store.ID, ""))
		defer deleteSession(t, app, created.ID)
		got := decodeMemoryResource(t, created.Resources[0])
		if got.Access != sessionresource.MemoryAccessReadWrite {
			t.Fatalf("default access = %q, want read_write", got.Access)
		}

		readonly := createSession(t, app, sessionBodyWithMemoryResource(agent.ID, env.ID, store.ID, `"access":"read_only"`))
		defer deleteSession(t, app, readonly.ID)
		got = decodeMemoryResource(t, readonly.Resources[0])
		if got.Access != sessionresource.MemoryAccessReadOnly {
			t.Fatalf("read_only access = %q", got.Access)
		}
	})

	t.Run("M1-04 instructions use unicode code points and are not truncated", func(t *testing.T) {
		store := createMemoryStore(t, app, "instructions-limit")
		defer deleteMemoryStore(t, app, store.ID)

		limit := strings.Repeat("i", sessionresource.MaxMemoryInstructionsRunes)
		created := createSession(t, app, sessionBodyWithMemoryResource(agent.ID, env.ID, store.ID, `"instructions":`+quoteJSON(limit)))
		defer deleteSession(t, app, created.ID)
		if got := decodeMemoryResource(t, created.Resources[0]).Instructions; got != limit {
			t.Fatalf("500-rune instructions = %d runes", utf8.RuneCountInString(got))
		}

		emoji := strings.Repeat("😀", sessionresource.MaxMemoryInstructionsRunes)
		emojiJSON, err := json.Marshal(emoji)
		if err != nil {
			t.Fatalf("marshal emoji: %v", err)
		}
		emojiSession := createSession(t, app, sessionBodyWithMemoryResource(agent.ID, env.ID, store.ID, `"instructions":`+string(emojiJSON)))
		defer deleteSession(t, app, emojiSession.ID)
		if got := decodeMemoryResource(t, emojiSession.Resources[0]).Instructions; got != emoji {
			t.Fatalf("500-emoji instructions = %d runes", utf8.RuneCountInString(got))
		}

		before := len(listSessions(t, app, "agent_id="+url.QueryEscape(agent.ID)).Data)
		resp := doSessionRequest(
			t, app, http.MethodPost, "/v1/sessions?beta=true",
			strings.NewReader(sessionBodyWithMemoryResource(agent.ID, env.ID, store.ID, `"instructions":`+quoteJSON(strings.Repeat("i", 501)))),
			defaultTestKey, true,
		)
		assertMemoryAttachError(t, resp, http.StatusBadRequest, "invalid_request_error", "at most 500 characters")
		listed := listSessions(t, app, "agent_id="+url.QueryEscape(agent.ID))
		if len(listed.Data) != before {
			t.Fatalf("501-rune request created a session: %+v", listed.Data)
		}
	})

	t.Run("M1-05 empty instructions are allowed", func(t *testing.T) {
		store := createMemoryStore(t, app, "empty-instructions")
		defer deleteMemoryStore(t, app, store.ID)

		omitted := createSession(t, app, sessionBodyWithMemoryResource(agent.ID, env.ID, store.ID, ""))
		defer deleteSession(t, app, omitted.ID)
		if got := decodeMemoryResource(t, omitted.Resources[0]).Instructions; got != "" {
			t.Fatalf("omitted instructions = %q", got)
		}

		empty := createSession(t, app, sessionBodyWithMemoryResource(agent.ID, env.ID, store.ID, `"instructions":""`))
		defer deleteSession(t, app, empty.ID)
		if got := decodeMemoryResource(t, empty.Resources[0]).Instructions; got != "" {
			t.Fatalf("empty instructions = %q", got)
		}
	})

	t.Run("M1-06 missing and archived stores", func(t *testing.T) {
		resp := doSessionRequest(
			t, app, http.MethodPost, "/v1/sessions?beta=true",
			strings.NewReader(sessionBodyWithMemoryResource(agent.ID, env.ID, "memstore_missing", "")),
			defaultTestKey, true,
		)
		assertMemoryAttachError(t, resp, http.StatusNotFound, "not_found_error", "Memory store not found")

		archived := createMemoryStore(t, app, "archived-store")
		defer deleteMemoryStore(t, app, archived.ID)
		archiveMemoryStore(t, app, archived.ID)
		resp = doSessionRequest(
			t, app, http.MethodPost, "/v1/sessions?beta=true",
			strings.NewReader(sessionBodyWithMemoryResource(agent.ID, env.ID, archived.ID, "")),
			defaultTestKey, true,
		)
		assertMemoryAttachError(t, resp, http.StatusBadRequest, "invalid_request_error", "must not be archived")
	})

	t.Run("M1-07 at most eight stores", func(t *testing.T) {
		stores := make([]memoryStoreAPIResponse, sessionresource.MaxMemoryStores+1)
		for i := range stores {
			stores[i] = createMemoryStore(t, app, fmt.Sprintf("limit-store-%d", i))
			defer deleteMemoryStore(t, app, stores[i].ID)
		}

		accepted := make([]string, 0, sessionresource.MaxMemoryStores)
		for _, store := range stores[:sessionresource.MaxMemoryStores] {
			accepted = append(accepted, `{"type":"memory_store","memory_store_id":`+quoteJSON(store.ID)+`}`)
		}
		created := createSession(t, app, sessionBodyWithResources(agent.ID, env.ID, "["+strings.Join(accepted, ",")+"]"))
		defer deleteSession(t, app, created.ID)
		if len(created.Resources) != sessionresource.MaxMemoryStores {
			t.Fatalf("resources = %d, want %d", len(created.Resources), sessionresource.MaxMemoryStores)
		}

		resp := doSessionRequest(
			t, app, http.MethodPost,
			"/v1/sessions/"+created.ID+"/resources?beta=true",
			strings.NewReader(`{"type":"memory_store","memory_store_id":`+quoteJSON(stores[sessionresource.MaxMemoryStores].ID)+`}`),
			defaultTestKey, true,
		)
		assertMemoryAttachError(t, resp, http.StatusBadRequest, "invalid_request_error", "at most 8 memory stores")

		nine := append(append([]string{}, accepted...), `{"type":"memory_store","memory_store_id":`+quoteJSON(stores[sessionresource.MaxMemoryStores].ID)+`}`)
		resp = doSessionRequest(
			t, app, http.MethodPost, "/v1/sessions?beta=true",
			strings.NewReader(sessionBodyWithResources(agent.ID, env.ID, "["+strings.Join(nine, ",")+"]")),
			defaultTestKey, true,
		)
		assertMemoryAttachError(t, resp, http.StatusBadRequest, "invalid_request_error", "at most 8 memory stores")
	})

	t.Run("M1-08 duplicate store ids are rejected", func(t *testing.T) {
		store := createMemoryStore(t, app, "duplicate-store")
		defer deleteMemoryStore(t, app, store.ID)
		resource := `{"type":"memory_store","memory_store_id":` + quoteJSON(store.ID) + `}`
		resp := doSessionRequest(
			t, app, http.MethodPost, "/v1/sessions?beta=true",
			strings.NewReader(sessionBodyWithResources(agent.ID, env.ID, "["+resource+","+resource+"]")),
			defaultTestKey, true,
		)
		assertMemoryAttachError(t, resp, http.StatusBadRequest, "invalid_request_error", "memory_store_id must be unique")
		assertNoSessionsForAgent(t, app, agent.ID)
	})

	t.Run("M1-09 slug comes from store name with fallback and collision suffix", func(t *testing.T) {
		named := createMemoryStore(t, app, "Product Docs-Draft!!")
		defer deleteMemoryStore(t, app, named.ID)
		symbols := createMemoryStore(t, app, "!!!")
		defer deleteMemoryStore(t, app, symbols.ID)
		collision := createMemoryStore(t, app, "Product Docs-Draft")
		defer deleteMemoryStore(t, app, collision.ID)

		created := createSession(t, app, sessionBodyWithResources(agent.ID, env.ID, `[`+
			`{"type":"memory_store","memory_store_id":`+quoteJSON(named.ID)+`},`+
			`{"type":"memory_store","memory_store_id":`+quoteJSON(symbols.ID)+`},`+
			`{"type":"memory_store","memory_store_id":`+quoteJSON(collision.ID)+`}`+
			`]`))
		defer deleteSession(t, app, created.ID)

		byStore := map[string]memoryResourceAPIResponse{}
		for _, resource := range decodeMemoryResources(t, created.Resources) {
			byStore[resource.MemoryStoreID] = resource
		}
		if got := byStore[named.ID].MountPath; got != "/mnt/memory/product-docs-draft" {
			t.Fatalf("named mount_path = %q", got)
		}
		wantFallback := "/mnt/memory/" + sessionresource.SlugifyMemoryName("!!!", symbols.ID)
		if got := byStore[symbols.ID].MountPath; got != wantFallback {
			t.Fatalf("fallback mount_path = %q, want %q", got, wantFallback)
		}
		if got := byStore[collision.ID].MountPath; got != "/mnt/memory/product-docs-draft-2" {
			t.Fatalf("collision mount_path = %q", got)
		}
	})

	t.Run("M1-01 and M1-10 snapshot is frozen after attach", func(t *testing.T) {
		store := createMemoryStore(t, app, "user-preferences")
		defer deleteMemoryStore(t, app, store.ID)

		created := createSession(t, app, sessionBodyWithMemoryResource(agent.ID, env.ID, store.ID, `"instructions":"有新偏好就更新"`))
		defer deleteSession(t, app, created.ID)
		if created.Type != "session" || len(created.Resources) != 1 {
			t.Fatalf("created session = %+v", created)
		}
		got := decodeMemoryResource(t, created.Resources[0])
		if got.Access != sessionresource.MemoryAccessReadWrite ||
			got.MountPath != "/mnt/memory/user-preferences" ||
			got.Name != store.Name ||
			got.Description != store.Description ||
			got.Instructions != "有新偏好就更新" ||
			got.MemoryStoreID != store.ID {
			t.Fatalf("attach response = %+v, store=%+v", got, store)
		}

		stored := persistedMemoryPayload(t, app, created.ID)
		if stored["access"] != sessionresource.MemoryAccessReadWrite ||
			stored["mount_path"] != "/mnt/memory/user-preferences" ||
			stored["name"] != store.Name ||
			stored["description"] != store.Description ||
			stored["instructions"] != "有新偏好就更新" ||
			stored["memory_store_id"] != store.ID {
			t.Fatalf("stored payload = %#v", stored)
		}

		updateMemoryStore(t, app, store.ID, `{"name":"renamed-store","description":"changed later"}`)
		retrieved := retrieveSession(t, app, created.ID, defaultTestKey)
		frozen := decodeMemoryResource(t, retrieved.Resources[0])
		if frozen.Name != "user-preferences" || frozen.Description != store.Description || frozen.MountPath != "/mnt/memory/user-preferences" {
			t.Fatalf("frozen resource = %+v", frozen)
		}
	})

	t.Run("M1-12 file and github attach stay unchanged", func(t *testing.T) {
		file := uploadFile(t, app, "memory-attach-regression.txt", "text/plain", []byte("ok"))
		defer deleteFile(t, app, file.ID)
		created := createSession(t, app, `{
			"agent":`+quoteJSON(agent.ID)+`,
			"environment_id":`+quoteJSON(env.ID)+`,
			"resources":[
				{"type":"file","file_id":`+quoteJSON(file.ID)+`,"mount_path":"/workspace/regression.txt"},
				{"type":"github_repository","url":"https://github.com/example/repo","mount_path":"/workspace/repo"}
			]
		}`)
		defer deleteSession(t, app, created.ID)
		if len(created.Resources) != 2 {
			t.Fatalf("resources = %d, want 2", len(created.Resources))
		}
		var sawFile, sawGithub bool
		for _, raw := range created.Resources {
			var envelope struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatalf("decode resource: %v", err)
			}
			switch envelope.Type {
			case "file":
				sawFile = true
				assertRawContains(t, raw, `"file_id":`+quoteJSON(file.ID))
				assertRawNotContains(t, raw, `"memory_store_id"`)
			case "github_repository":
				sawGithub = true
				assertRawContains(t, raw, `"mount_path":"/workspace/repo"`)
				assertRawNotContains(t, raw, `"memory_store_id"`)
			default:
				t.Fatalf("unexpected resource type %q: %s", envelope.Type, raw)
			}
		}
		if !sawFile || !sawGithub {
			t.Fatalf("missing file or github resource: %v", created.Resources)
		}
	})
}

func TestCreateSessionResourceMemoryInvariantsAreAtomic(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-memory-attach-atomic-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-memory-attach-atomic-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-memory-attach-atomic-env"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)

	t.Run("duplicate memory_store_id", func(t *testing.T) {
		store := createMemoryStore(t, app, "atomic-duplicate")
		defer deleteMemoryStore(t, app, store.ID)
		created := createSession(t, app, sessionBodyWithResources(agent.ID, env.ID, "[]"))
		defer deleteSession(t, app, created.ID)
		session := mustSessionRecord(t, app, created.ID)
		inputs := []db.CreateSessionResourceInput{
			testMemoryStoreResourceInput(t, session, store.ID, newTestSessionResourceID(t)),
			testMemoryStoreResourceInput(t, session, store.ID, newTestSessionResourceID(t)),
		}

		start := make(chan struct{})
		results := make(chan error, 2)
		for _, input := range inputs {
			go func(input db.CreateSessionResourceInput) {
				<-start
				_, err := app.db.CreateSessionResource(context.Background(), input)
				results <- err
			}(input)
		}
		close(start)

		var succeeded, duplicated int
		for range 2 {
			err := <-results
			if err == nil {
				succeeded++
				continue
			}
			var duplicateErr *db.SessionMemoryStoreDuplicateError
			if errors.As(err, &duplicateErr) {
				duplicated++
				continue
			}
			t.Fatalf("concurrent CreateSessionResource error = %v, want nil or duplicate", err)
		}
		if succeeded != 1 || duplicated != 1 {
			t.Fatalf("concurrent duplicate results = success %d duplicate %d, want one of each", succeeded, duplicated)
		}
		assertPersistedMemoryStoreCount(t, app, created.ID, 1)
	})

	t.Run("eight store limit", func(t *testing.T) {
		stores := make([]memoryStoreAPIResponse, sessionresource.MaxMemoryStores+1)
		for i := range stores {
			stores[i] = createMemoryStore(t, app, fmt.Sprintf("atomic-limit-%d", i))
			defer deleteMemoryStore(t, app, stores[i].ID)
		}
		accepted := make([]string, 0, sessionresource.MaxMemoryStores-1)
		for _, store := range stores[:sessionresource.MaxMemoryStores-1] {
			accepted = append(accepted, `{"type":"memory_store","memory_store_id":`+quoteJSON(store.ID)+`}`)
		}
		created := createSession(t, app, sessionBodyWithResources(agent.ID, env.ID, "["+strings.Join(accepted, ",")+"]"))
		defer deleteSession(t, app, created.ID)

		eighth := `{"type":"memory_store","memory_store_id":` + quoteJSON(stores[sessionresource.MaxMemoryStores-1].ID) + `}`
		ninth := `{"type":"memory_store","memory_store_id":` + quoteJSON(stores[sessionresource.MaxMemoryStores].ID) + `}`
		assertOneAcceptedOneInvalidRequest(
			t,
			postSessionResourcesConcurrently(t, app, created.ID, []string{eighth, ninth}),
		)
		assertPersistedMemoryStoreCount(t, app, created.ID, sessionresource.MaxMemoryStores)
	})
}

func sessionBodyWithMemoryResource(agentID, envID, storeID, extra string) string {
	if extra != "" {
		extra = "," + extra
	}
	return sessionBodyWithResources(agentID, envID, `[{"type":"memory_store","memory_store_id":`+quoteJSON(storeID)+extra+`}]`)
}

func sessionBodyWithResources(agentID, envID, resources string) string {
	return `{
		"agent":` + quoteJSON(agentID) + `,
		"environment_id":` + quoteJSON(envID) + `,
		"resources":` + resources + `
	}`
}

func decodeMemoryResource(t *testing.T, raw json.RawMessage) memoryResourceAPIResponse {
	t.Helper()
	var resource memoryResourceAPIResponse
	if err := json.Unmarshal(raw, &resource); err != nil {
		t.Fatalf("decode memory resource: %v", err)
	}
	return resource
}

func decodeMemoryResources(t *testing.T, raws []json.RawMessage) []memoryResourceAPIResponse {
	t.Helper()
	out := make([]memoryResourceAPIResponse, 0, len(raws))
	for _, raw := range raws {
		out = append(out, decodeMemoryResource(t, raw))
	}
	return out
}

func persistedMemoryPayload(t *testing.T, app *testApp, sessionID string) map[string]any {
	t.Helper()
	session := mustSessionRecord(t, app, sessionID)
	resources, err := app.db.ListSessionResources(context.Background(), session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		t.Fatalf("list session resources: %v", err)
	}
	for _, resource := range resources {
		if resource.ResourceType != "memory_store" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(resource.Payload, &payload); err != nil {
			t.Fatalf("decode stored payload: %v", err)
		}
		return payload
	}
	t.Fatalf("session %s has no memory_store resource: %+v", sessionID, resources)
	return nil
}

func assertNoSessionsForAgent(t *testing.T, app *testApp, agentID string) {
	t.Helper()
	listed := listSessions(t, app, "agent_id="+url.QueryEscape(agentID))
	if len(listed.Data) != 0 {
		t.Fatalf("want no sessions for agent %s, got %+v", agentID, listed.Data)
	}
}

type sessionResourceAddResult struct {
	status int
	body   []byte
}

func postSessionResourcesConcurrently(t *testing.T, app *testApp, sessionID string, bodies []string) []sessionResourceAddResult {
	t.Helper()
	start := make(chan struct{})
	results := make(chan sessionResourceAddResult, len(bodies))
	for _, body := range bodies {
		go func(body string) {
			request, err := http.NewRequest(
				http.MethodPost,
				app.baseURL+"/v1/sessions/"+sessionID+"/resources?beta=true",
				strings.NewReader(body),
			)
			if err != nil {
				results <- sessionResourceAddResult{}
				t.Errorf("build concurrent memory attach: %v", err)
				return
			}
			request.Header.Set("X-Api-Key", defaultTestKey)
			request.Header.Set("anthropic-version", "2023-06-01")
			request.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
			request.Header.Set("Content-Type", "application/json")
			<-start
			response, err := app.client.Do(request)
			if err != nil {
				results <- sessionResourceAddResult{}
				t.Errorf("send concurrent memory attach: %v", err)
				return
			}
			defer response.Body.Close()
			payload, err := io.ReadAll(response.Body)
			if err != nil {
				results <- sessionResourceAddResult{status: response.StatusCode}
				t.Errorf("read concurrent memory attach: %v", err)
				return
			}
			results <- sessionResourceAddResult{status: response.StatusCode, body: payload}
		}(body)
	}
	close(start)
	collected := make([]sessionResourceAddResult, 0, len(bodies))
	for range bodies {
		collected = append(collected, <-results)
	}
	return collected
}

func assertOneAcceptedOneInvalidRequest(t *testing.T, results []sessionResourceAddResult) {
	t.Helper()
	statusCounts := map[int]int{}
	for _, result := range results {
		statusCounts[result.status]++
		if result.status != http.StatusBadRequest {
			continue
		}
		var payload struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		if err := json.Unmarshal(result.body, &payload); err != nil {
			t.Fatalf("decode rejected concurrent memory attach: %v", err)
		}
		if payload.Error.Type != "invalid_request_error" {
			t.Fatalf("rejected concurrent memory attach error = %q, want invalid_request_error: %s", payload.Error.Type, result.body)
		}
	}
	if statusCounts[http.StatusOK] != 1 || statusCounts[http.StatusBadRequest] != 1 {
		t.Fatalf("concurrent memory attach statuses = %+v, want one 200 and one 400", statusCounts)
	}
}

func assertPersistedMemoryStoreCount(t *testing.T, app *testApp, sessionID string, want int) {
	t.Helper()
	session := mustSessionRecord(t, app, sessionID)
	persisted, err := app.db.ListSessionResources(context.Background(), session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		t.Fatalf("list resources after concurrent memory attach: %v", err)
	}
	count := 0
	seen := map[string]struct{}{}
	for _, resource := range persisted {
		if resource.ResourceType != sessionresource.MemoryStoreType {
			continue
		}
		count++
		var payload struct {
			MemoryStoreID string `json:"memory_store_id"`
		}
		if err := json.Unmarshal(resource.Payload, &payload); err != nil {
			t.Fatalf("decode persisted memory store: %v", err)
		}
		if payload.MemoryStoreID == "" {
			t.Fatalf("persisted memory store missing id: %s", resource.Payload)
		}
		if _, exists := seen[payload.MemoryStoreID]; exists {
			t.Fatalf("persisted duplicate memory_store_id %q", payload.MemoryStoreID)
		}
		seen[payload.MemoryStoreID] = struct{}{}
	}
	if count != want {
		t.Fatalf("persisted memory stores = %d, want %d", count, want)
	}
}

func newTestSessionResourceID(t *testing.T) string {
	t.Helper()
	resourceID, err := ids.New("sesrsc_")
	if err != nil {
		t.Fatalf("generate session resource id: %v", err)
	}
	return resourceID
}

func testMemoryStoreResourceInput(t *testing.T, session db.Session, storeID, resourceID string) db.CreateSessionResourceInput {
	t.Helper()
	now := time.Now().UTC()
	slug := sessionresource.SlugifyMemoryName("atomic", storeID)
	payload, err := json.Marshal(sessionresource.SnapshotMemoryStore(
		storeID,
		sessionresource.MemoryAccessReadWrite,
		"",
		"atomic",
		"",
		slug,
	).PayloadFields(resourceID))
	if err != nil {
		t.Fatalf("marshal memory store payload: %v", err)
	}
	return db.CreateSessionResourceInput{
		Resource: db.SessionResource{
			UUID:              uuid.NewV4().String(),
			ExternalID:        resourceID,
			OrganizationUUID:  session.OrganizationUUID,
			WorkspaceUUID:     session.WorkspaceUUID,
			SessionExternalID: session.ExternalID,
			ResourceType:      db.SessionResourceTypeMemoryStore,
			Payload:           payload,
			SecretPayload:     json.RawMessage(`{}`),
			CreatedAt:         now,
			UpdatedAt:         now,
		},
	}
}

func assertMemoryAttachError(t *testing.T, resp *http.Response, status int, typ, message string) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != status {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, status, readAll(t, resp.Body))
	}
	var body errorResponse
	decodeJSON(t, resp.Body, &body)
	if body.Type != "error" || body.Error.Type != typ {
		t.Fatalf("error = %+v, want type %s", body, typ)
	}
	if !strings.Contains(body.Error.Message, message) {
		t.Fatalf("error message = %q, want substring %q", body.Error.Message, message)
	}
}

package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type dreamAPIResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Inputs       []dreamInputAPIResponse `json:"inputs"`
	Instructions *string                 `json:"instructions"`
	Status       string                  `json:"status"`
	Outputs      []json.RawMessage       `json:"outputs"`
	CreatedAt    string                  `json:"created_at"`
	UpdatedAt    string                  `json:"updated_at"`
	ArchivedAt   *string                 `json:"archived_at"`
	Error        *string                 `json:"error"`
}

type dreamInputAPIResponse struct {
	Type          string   `json:"type"`
	MemoryStoreID string   `json:"memory_store_id"`
	SessionIDs    []string `json:"session_ids"`
}

type dreamPageAPIResponse struct {
	Data     []dreamAPIResponse `json:"data"`
	NextPage *string            `json:"next_page"`
}

func TestDreamsAPI(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("dreams-api-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"dreams-api-agent"}`)
	env := createEnvironment(t, app, `{"name":"dreams-api-env"}`)
	memoryStore := createMemoryStore(t, app, "dreams-api-memory")
	defer deleteMemoryStore(t, app, memoryStore.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	archiveSession(t, app, session.ID)

	t.Run("failure missing beta header", func(t *testing.T) {
		resp := doDreamRequest(t, app, http.MethodGet, "/v1/dreams", nil, defaultTestKey, false)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure missing api key", func(t *testing.T) {
		resp := doDreamRequest(t, app, http.MethodGet, "/v1/dreams", nil, "", true)
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure memory store not found", func(t *testing.T) {
		body := `{"inputs":[{"type":"memory_store","memory_store_id":"memstore_missing"},{"type":"sessions","session_ids":["` + session.ID + `"]}],"model":"claude-opus-4-6"}`
		resp := doDreamRequest(t, app, http.MethodPost, "/v1/dreams", strings.NewReader(body), defaultTestKey, true)
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})

	t.Run("failure session not found", func(t *testing.T) {
		body := `{"inputs":[{"type":"memory_store","memory_store_id":"` + memoryStore.ID + `"},{"type":"sessions","session_ids":["ses_missing"]}],"model":"claude-opus-4-6"}`
		resp := doDreamRequest(t, app, http.MethodPost, "/v1/dreams", strings.NewReader(body), defaultTestKey, true)
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})

	t.Run("failure too many sessions", func(t *testing.T) {
		sessionIDs := make([]string, 101)
		for index := range sessionIDs {
			sessionIDs[index] = "ses_fake"
		}
		raw, err := json.Marshal(sessionIDs)
		if err != nil {
			t.Fatalf("marshal session ids: %v", err)
		}
		body := `{"inputs":[{"type":"memory_store","memory_store_id":"` + memoryStore.ID + `"},{"type":"sessions","session_ids":` + string(raw) + `}]}`
		resp := doDreamRequest(t, app, http.MethodPost, "/v1/dreams", strings.NewReader(body), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure missing memory store input", func(t *testing.T) {
		body := `{"inputs":[{"type":"sessions","session_ids":["` + session.ID + `"]}]}`
		resp := doDreamRequest(t, app, http.MethodPost, "/v1/dreams", strings.NewReader(body), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("success dream lifecycle", func(t *testing.T) {
		body := `{"inputs":[{"type":"memory_store","memory_store_id":"` + memoryStore.ID + `"},{"type":"sessions","session_ids":["` + session.ID + `"]}],"instructions":"distill the sessions","model":"claude-opus-4-6"}`
		created := createDream(t, app, body)
		if created.Type != "dream" || created.Status != "pending" || created.Instructions == nil || *created.Instructions != "distill the sessions" {
			t.Fatalf("unexpected created dream: %+v", created)
		}
		if len(created.Inputs) != 2 || created.Inputs[0].Type != "memory_store" || created.Inputs[0].MemoryStoreID != memoryStore.ID ||
			len(created.Inputs[1].SessionIDs) != 1 || created.Inputs[1].SessionIDs[0] != session.ID {
			t.Fatalf("unexpected dream inputs: %+v", created.Inputs)
		}
		if len(created.Outputs) != 0 {
			t.Fatalf("pending dream must not have outputs: %+v", created.Outputs)
		}

		retrieved := retrieveDream(t, app, created.ID, defaultTestKey)
		if retrieved.ID != created.ID || retrieved.Status != "pending" {
			t.Fatalf("unexpected retrieved dream: %+v", retrieved)
		}

		page := listDreams(t, app, "limit=10")
		if !containsDream(page.Data, created.ID) {
			t.Fatalf("dream list missing %s: %+v", created.ID, page.Data)
		}

		otherKey := "sk-ant-local-dreams-other"
		seedWorkspaceKey(t, app.pool, "org_dreams_other_test", "workspace_dreams_other_test", "api_key_dreams_other_test", otherKey)
		resp := doDreamRequest(t, app, http.MethodGet, "/v1/dreams/"+created.ID, nil, otherKey, true)
		assertError(t, resp, http.StatusNotFound, "not_found_error")

		cancelled := cancelDream(t, app, created.ID)
		if cancelled.Status != "cancelled" {
			t.Fatalf("cancel dream status = %s, want cancelled", cancelled.Status)
		}
		retrieved = retrieveDream(t, app, created.ID, defaultTestKey)
		if retrieved.Status != "cancelled" {
			t.Fatalf("retrieve after cancel status = %s, want cancelled", retrieved.Status)
		}

		archived := archiveDream(t, app, created.ID)
		// archive 是独立于状态的软删除：status 保持 terminal，archived_at 生效。
		if archived.Status != "cancelled" || archived.ArchivedAt == nil {
			t.Fatalf("unexpected archived dream: %+v", archived)
		}
		retrieved = retrieveDream(t, app, created.ID, defaultTestKey)
		if retrieved.ArchivedAt == nil || retrieved.Status != "cancelled" {
			t.Fatalf("retrieve after archive: %+v", retrieved)
		}
		page = listDreams(t, app, "limit=10")
		if containsDream(page.Data, created.ID) {
			t.Fatalf("archived dream still listed: %+v", page.Data)
		}
	})
}

func doDreamRequest(t *testing.T, app *testApp, method, path string, body io.Reader, key string, betaHeader bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, app.baseURL+path, body)
	if err != nil {
		t.Fatalf("new dream request: %v", err)
	}
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if betaHeader {
		req.Header.Set("anthropic-beta", "dreaming-2026-04-21")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do dream request: %v", err)
	}
	return resp
}

func createDream(t *testing.T, app *testApp, body string) dreamAPIResponse {
	t.Helper()
	resp := doDreamRequest(t, app, http.MethodPost, "/v1/dreams", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create dream status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var dream dreamAPIResponse
	decodeJSON(t, resp.Body, &dream)
	if dream.ID == "" {
		t.Fatalf("create dream returned empty id: %+v", dream)
	}
	return dream
}

func retrieveDream(t *testing.T, app *testApp, dreamID, key string) dreamAPIResponse {
	t.Helper()
	resp := doDreamRequest(t, app, http.MethodGet, "/v1/dreams/"+dreamID, nil, key, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve dream status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var dream dreamAPIResponse
	decodeJSON(t, resp.Body, &dream)
	return dream
}

func listDreams(t *testing.T, app *testApp, query string) dreamPageAPIResponse {
	t.Helper()
	resp := doDreamRequest(t, app, http.MethodGet, "/v1/dreams?"+query, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list dreams status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page dreamPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func cancelDream(t *testing.T, app *testApp, dreamID string) dreamAPIResponse {
	t.Helper()
	resp := doDreamRequest(t, app, http.MethodPost, "/v1/dreams/"+dreamID+"/cancel", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel dream status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var dream dreamAPIResponse
	decodeJSON(t, resp.Body, &dream)
	return dream
}

func archiveDream(t *testing.T, app *testApp, dreamID string) dreamAPIResponse {
	t.Helper()
	resp := doDreamRequest(t, app, http.MethodPost, "/v1/dreams/"+dreamID+"/archive", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive dream status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var dream dreamAPIResponse
	decodeJSON(t, resp.Body, &dream)
	return dream
}

func containsDream(data []dreamAPIResponse, dreamID string) bool {
	for _, dream := range data {
		if dream.ID == dreamID {
			return true
		}
	}
	return false
}

func TestDreamsTableHasNoForeignKeys(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("dreams-fk-bucket"))
	defer app.close()

	var foreignKeyCount int
	if err := app.pool.QueryRow(context.Background(), `
		select count(*)
		from pg_constraint con
		join pg_class cls on cls.oid = con.conrelid
		where con.contype = 'f'
			and cls.relname = 'dreams'
	`).Scan(&foreignKeyCount); err != nil {
		t.Fatalf("count dreams foreign keys: %v", err)
	}
	if foreignKeyCount != 0 {
		t.Fatalf("dreams foreign key count = %d, want 0", foreignKeyCount)
	}
}

package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/modelcatalog"
)

func TestCreateAgentRejectsUnknownCatalogModel(t *testing.T) {
	t.Parallel()
	handler := NewHandlerWithModelCatalogAndSkillPrewarm(
		config.Config{},
		nil,
		agentTestCatalog{models: []string{"provider/known"}},
		nil,
	)
	request := agentCatalogRequest(`{"name":"catalog test","model":"provider/unknown"}`)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "not available") {
		t.Fatalf("body = %s, want unavailable model error", response.Body.String())
	}
}

func TestCreateAgentReportsUnavailableCatalog(t *testing.T) {
	t.Parallel()
	handler := NewHandlerWithModelCatalogAndSkillPrewarm(
		config.Config{},
		nil,
		agentTestCatalog{err: modelcatalog.ErrUnavailable},
		nil,
	)
	request := agentCatalogRequest(`{"name":"catalog test","model":"provider/model"}`)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
}

func TestUpdateAgentRejectsUnknownCatalogModel(t *testing.T) {
	t.Parallel()
	handler := NewHandlerWithModelCatalogAndSkillPrewarm(
		config.Config{},
		nil,
		agentTestCatalog{models: []string{"provider/known"}},
		nil,
	)

	_, err := handler.stateFromUpdate(
		agentCatalogRequest(`{}`),
		auth.Principal{},
		historicalAgent("provider/known"),
		map[string]json.RawMessage{"model": json.RawMessage(`"provider/unknown"`)},
	)
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("stateFromUpdate error = %v, want unavailable model error", err)
	}
}

func TestUpdateAgentPreservesHistoricalModelWhenModelFieldIsOmitted(t *testing.T) {
	t.Parallel()
	handler := NewHandlerWithModelCatalogAndSkillPrewarm(
		config.Config{},
		nil,
		agentTestCatalog{models: []string{"provider/current"}},
		nil,
	)
	current := historicalAgent("provider/retired")

	state, err := handler.stateFromUpdate(
		agentCatalogRequest(`{}`),
		auth.Principal{},
		current,
		map[string]json.RawMessage{"name": json.RawMessage(`"renamed"`)},
	)
	if err != nil {
		t.Fatalf("stateFromUpdate: %v", err)
	}
	if string(state.Model) != string(current.Model) {
		t.Fatalf("model = %s, want preserved historical model %s", state.Model, current.Model)
	}
	if state.Name != "renamed" {
		t.Fatalf("name = %q, want renamed", state.Name)
	}
}

func historicalAgent(modelID string) db.Agent {
	return db.Agent{
		ExternalID:     "agent_catalog_test",
		CurrentVersion: 1,
		Name:           "catalog test",
		Model:          json.RawMessage(`{"id":"` + modelID + `","speed":"standard"}`),
		MCPServers:     json.RawMessage(`[]`),
		Metadata:       json.RawMessage(`{}`),
		Skills:         json.RawMessage(`[]`),
		Tools:          json.RawMessage(`[]`),
	}
}

func agentCatalogRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/?beta=true", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{
		APIKeyID:    1,
		WorkspaceID: 1,
	}))
}

type agentTestCatalog struct {
	models []string
	err    error
}

func (c agentTestCatalog) Snapshot(context.Context) (modelcatalog.Snapshot, error) {
	if c.err != nil {
		return modelcatalog.Snapshot{}, c.err
	}
	models := make([]modelcatalog.Model, 0, len(c.models))
	for _, modelID := range c.models {
		models = append(models, modelcatalog.Model{ID: modelID})
	}
	return modelcatalog.Snapshot{Models: models}, nil
}

func (c agentTestCatalog) ValidateModel(_ context.Context, modelID string) error {
	if c.err != nil {
		return c.err
	}
	for _, knownID := range c.models {
		if knownID == modelID {
			return nil
		}
	}
	return modelcatalog.ErrUnknownModel
}

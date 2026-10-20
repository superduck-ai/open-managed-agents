package batches

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestNormalizeBatchModelsRejectsInvalidReferences(t *testing.T) {
	catalog := llmproviders.NewCatalog([]db.LLMProvider{
		{Slug: "first", ModelIDs: []string{"shared"}},
		{Slug: "second", ModelIDs: []string{"shared"}},
	})
	for _, model := range []string{"shared", "missing"} {
		body := &createRequest{Requests: []createBatchRequest{{CustomID: "request", Params: json.RawMessage(`{"model":"` + model + `"}`)}}}
		err := normalizeBatchModels(catalog, body, []string{model})
		if err == nil || !strings.Contains(err.Error(), "request") {
			t.Fatalf("normalizeBatchModels(%q) error = %v", model, err)
		}
	}
}

func TestNormalizeBatchModelsPinsProviderBeforeCatalogChanges(t *testing.T) {
	providers := []db.LLMProvider{{Slug: "first", ModelIDs: []string{"vendor/model"}}}
	body := &createRequest{Requests: []createBatchRequest{
		{CustomID: "legacy", Params: json.RawMessage(`{"model" : "vendor/model", "metadata":{"model":"opaque"}}`)},
		{CustomID: "qualified", Params: json.RawMessage(`{"model":"first/vendor/model","max_tokens":10}`)},
	}}
	if err := normalizeBatchModels(llmproviders.NewCatalog(providers), body, []string{"vendor/model", "first/vendor/model"}); err != nil {
		t.Fatal(err)
	}
	if got, want := string(body.Requests[0].Params), `{"model" : "first/vendor/model", "metadata":{"model":"opaque"}}`; got != want {
		t.Fatalf("params = %s, want %s", got, want)
	}
	catalog := llmproviders.NewCatalog(append(providers, db.LLMProvider{Slug: "second", ModelIDs: []string{"vendor/model"}}))
	for _, request := range body.Requests {
		model, err := llmproviders.MessageRequestModel(request.Params)
		if err != nil {
			t.Fatal(err)
		}
		selected, err := catalog.Lookup(model)
		if err != nil || selected.Provider.Slug != "first" || selected.UpstreamModelID != "vendor/model" {
			t.Fatalf("Lookup(%q) = %+v, %v", model, selected, err)
		}
	}
}

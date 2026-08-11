package platformapi

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/modelcatalog"
)

func TestPlatformModelCatalogBuildsDeclaredThinkingAndMultimodalCapabilities(t *testing.T) {
	t.Parallel()
	var capabilities modelcatalog.Capabilities
	if err := json.Unmarshal([]byte(`{
		"thinking":{"supported":true,"types":{"enabled":{"supported":true},"adaptive":{"supported":false}}},
		"image_input":{"supported":true},
		"pdf_input":{"supported":false}
	}`), &capabilities); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}

	models := (platformModelCatalog{models: []modelcatalog.Model{{
		ID:           "provider/model",
		DisplayName:  "Provider Model",
		Capabilities: capabilities,
	}}}).bootstrapModels()

	if len(models) != 1 || len(models[0].ThinkingModes) != 1 || models[0].ThinkingModes[0].ID != "extended" {
		t.Fatalf("bootstrap models = %#v, want enabled thinking mode", models)
	}
	if models[0].Capabilities == nil || !models[0].Capabilities.MMImages || models[0].Capabilities.MMPDF {
		t.Fatalf("bootstrap capabilities = %#v, want declared image=true pdf=false", models[0].Capabilities)
	}
}

package models

import (
	"cmp"

	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

const unknownModelCreatedAt = "1970-01-01T00:00:00Z"

type modelResponse struct {
	Type           string    `json:"type"`
	ID             string    `json:"id"`
	DisplayName    string    `json:"display_name"`
	CreatedAt      string    `json:"created_at"`
	MaxInputTokens *int      `json:"max_input_tokens"`
	MaxTokens      *int      `json:"max_tokens"`
	Capabilities   *struct{} `json:"capabilities"`
}

func modelResponses(catalog llmproviders.Catalog) []modelResponse {
	modelIDs := catalog.ModelIDs()
	models := make([]modelResponse, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		selected, _ := catalog.Lookup(modelID)
		models = append(models, newModelResponse(selected))
	}
	return models
}

func newModelResponse(selected llmproviders.CatalogModel) modelResponse {
	modelID := selected.QualifiedModelID()
	name := modelID
	for _, model := range selected.Provider.Models {
		if model.ID == selected.UpstreamModelID {
			name = cmp.Or(model.Name, modelID)
			break
		}
	}
	return modelResponse{
		Type:        "model",
		ID:          modelID,
		DisplayName: name,
		CreatedAt:   unknownModelCreatedAt,
	}
}

package models

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

func modelResponses(modelIDs []string) []modelResponse {
	models := make([]modelResponse, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		models = append(models, modelResponse{
			Type:        "model",
			ID:          modelID,
			DisplayName: modelID,
			CreatedAt:   unknownModelCreatedAt,
		})
	}
	return models
}

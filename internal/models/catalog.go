package models

type modelResponse struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func modelResponses(modelIDs []string) []modelResponse {
	models := make([]modelResponse, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		models = append(models, modelResponse{Type: "model", ID: modelID})
	}
	return models
}

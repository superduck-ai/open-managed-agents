package platformapi

import (
	"context"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/modelcatalog"
)

type platformModelCatalog struct {
	models         []modelcatalog.Model
	defaultModelID string
}

func loadPlatformModelCatalog(ctx context.Context, reader modelcatalog.Reader) platformModelCatalog {
	if reader == nil {
		return platformModelCatalog{}
	}
	snapshot, err := reader.Snapshot(ctx)
	if err != nil {
		return platformModelCatalog{}
	}
	defaultModelID := ""
	if snapshot.DefaultAvailable {
		defaultModelID = strings.TrimSpace(snapshot.DefaultModelID)
	}
	return platformModelCatalog{models: snapshot.Models, defaultModelID: defaultModelID}
}

func (c platformModelCatalog) modelIDs() []string {
	modelIDs := make([]string, 0, len(c.models))
	for _, model := range c.models {
		if modelID := strings.TrimSpace(model.ID); modelID != "" {
			modelIDs = append(modelIDs, modelID)
		}
	}
	return modelIDs
}

func (c platformModelCatalog) defaultModelValue() any {
	if c.defaultModelID == "" {
		return nil
	}
	return c.defaultModelID
}

func (c platformModelCatalog) bootstrapModels() []BootstrapModelOption {
	models := make([]BootstrapModelOption, 0, len(c.models))
	for _, model := range c.models {
		modelID := strings.TrimSpace(model.ID)
		if modelID == "" {
			continue
		}
		name := strings.TrimSpace(model.DisplayName)
		if name == "" {
			name = modelID
		}
		thinkingModes := []BootstrapThinkingModeOption{}
		paprikaModes := []string{}
		if model.Capabilities.Thinking != nil && *model.Capabilities.Thinking {
			thinkingModes = extendedThinkingModes()
			if model.Capabilities.AdaptiveThinking != nil && *model.Capabilities.AdaptiveThinking {
				thinkingModes = adaptiveThinkingModes()
			}
			paprikaModes = []string{"extended"}
		}
		hardLimit := 0
		if model.MaxInputTokens != nil {
			hardLimit = *model.MaxInputTokens
		}
		models = append(models, BootstrapModelOption{
			Model:         modelID,
			Name:          name,
			Description:   strings.TrimSpace(model.Description),
			ThinkingModes: thinkingModes,
			PaprikaModes:  paprikaModes,
			HardLimit:     hardLimit,
		})
	}
	return models
}

package devinit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

type rawProviderModel struct {
	ID              string                 `yaml:"id"`
	Name            string                 `yaml:"name"`
	ContextWindow   *int                   `yaml:"context_window"`
	MaxOutputTokens *int                   `yaml:"max_output_tokens"`
	InputModalities []string               `yaml:"input_modalities"`
	ImageInput      *rawProviderImageInput `yaml:"image_input"`
}

type rawProviderImageInput struct {
	MaxImages            int `yaml:"max_images"`
	MaxRequestImageBytes int `yaml:"max_request_image_bytes"`
	MaxImageBytes        int `yaml:"max_image_bytes"`
	MaxLongEdge          int `yaml:"max_long_edge"`
	MaxPixels            int `yaml:"max_pixels"`
	TokensPerMegapixel   int `yaml:"tokens_per_megapixel"`
}

func resolveProviderModels(provider rawProvider) (db.LLMModels, error) {
	if provider.Models == nil {
		return nil, nil
	}
	if provider.ModelIDs != nil {
		return nil, errors.New("use models or model_ids, not both")
	}
	models := make(db.LLMModels, 0, len(provider.Models))
	for _, raw := range provider.Models {
		model := db.LLMModel{ID: raw.ID, Name: strings.TrimSpace(raw.Name), ContextWindow: raw.ContextWindow,
			MaxOutputTokens: raw.MaxOutputTokens, InputModalities: raw.InputModalities}
		if raw.ImageInput != nil {
			image := db.LLMImageInput(*raw.ImageInput)
			model.ImageInput = &image
		}
		if err := llmproviders.ValidateModelMetadata(model); err != nil {
			return nil, fmt.Errorf("model %q: %w", model.ID, err)
		}
		models = append(models, model)
	}
	return models, nil
}

func configuredProviderModels(existing db.LLMModels, spec ProviderSpec) db.LLMModels {
	if spec.Models != nil {
		return spec.Models
	}
	return existing.WithIDs(spec.ModelIDs)
}

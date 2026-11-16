package llmproviders

import (
	"slices"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func ValidateModelMetadata(model db.LLMModel) error {
	if utf8.RuneCountInString(model.Name) > 100 {
		return ErrModelNameInvalid
	}
	for _, value := range []*int{model.ContextWindow, model.MaxOutputTokens} {
		if value != nil && (*value <= 0 || *value > 9007199254740991) {
			return ErrModelCapacityInvalid
		}
	}
	if image := model.ImageInput; image != nil {
		for _, value := range []int{image.MaxImages, image.MaxRequestImageBytes, image.MaxImageBytes, image.MaxLongEdge, image.MaxPixels, image.TokensPerMegapixel} {
			if value < 0 || int64(value) > MaxMessagesRequestBodyBytes {
				return ErrModelImageInputInvalid
			}
		}
	}
	if model.InputModalities == nil {
		return nil
	}
	if len(model.InputModalities) == 0 || len(model.InputModalities) > 2 {
		return ErrModelModalitiesInvalid
	}
	for i, value := range model.InputModalities {
		if value != "text" && value != "image" || slices.Contains(model.InputModalities[:i], value) {
			return ErrModelModalitiesInvalid
		}
	}
	return nil
}

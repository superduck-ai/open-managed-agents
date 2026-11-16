package main

import (
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/devinit"
)

func configuredVisionModel() (string, error) {
	return configuredRealModel("VERIFY_BE_VISION_PROVIDERS", "VERIFY_BE_VISION_MODEL")
}

func configuredRealModel(providersVariable, modelVariable string) (string, error) {
	model := strings.TrimSpace(os.Getenv(modelVariable))
	providers, err := devinit.LoadModelProviders(strings.TrimSpace(os.Getenv(providersVariable)))
	if err != nil {
		return "", errors.New("model verification requires an explicit valid providers file")
	}
	matches := 0
	for _, provider := range providers.Providers {
		if slices.Contains(provider.ModelIDs, model) {
			matches++
		}
	}
	if model == "" || matches != 1 {
		return "", errors.New("configured model must identify a model in exactly one configured provider")
	}
	return model, nil
}

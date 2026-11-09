package main

import (
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/devinit"
)

func configuredVisionModel() (string, error) {
	model := strings.TrimSpace(os.Getenv("VERIFY_BE_VISION_MODEL"))
	providers, err := devinit.LoadModelProviders(strings.TrimSpace(os.Getenv("VERIFY_BE_VISION_PROVIDERS")))
	if err != nil {
		return "", errors.New("vision verification requires an explicit valid VERIFY_BE_VISION_PROVIDERS file")
	}
	matches := 0
	for _, provider := range providers.Providers {
		if slices.Contains(provider.ModelIDs, model) {
			matches++
		}
	}
	if model == "" || matches != 1 {
		return "", errors.New("VERIFY_BE_VISION_MODEL must identify a model in exactly one configured provider")
	}
	return model, nil
}

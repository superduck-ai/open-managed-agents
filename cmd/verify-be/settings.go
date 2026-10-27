package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const localSettingsFile = ".verify-be.local.json"

func resolveWorkerImage(root, override, environmentValue string) (string, error) {
	for _, value := range []string{override, environmentValue} {
		if image := strings.TrimSpace(value); image != "" {
			return image, nil
		}
	}
	data, err := os.ReadFile(filepath.Join(root, localSettingsFile))
	if os.IsNotExist(err) {
		return defaultWorker, nil
	}
	if err != nil {
		return "", fmt.Errorf("read local verification settings: %w", err)
	}
	var settings struct {
		WorkerImage string `json:"worker_image"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return "", errors.New("invalid local verification settings: expected a JSON object with worker_image")
	}
	image := strings.TrimSpace(settings.WorkerImage)
	if image == "" {
		return "", errors.New("local verification settings require a non-empty worker_image")
	}
	return image, nil
}

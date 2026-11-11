package main

import (
	"bytes"
	"errors"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

func readFridayClient(path string) (legacyClient, error) {
	if path == "" {
		return legacyClient{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return legacyClient{}, errors.New("friday_config_unavailable")
	}
	defer clear(data)
	var client legacyClient
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&client); err != nil {
		return legacyClient{}, errors.New("friday_config_invalid")
	}
	client.ClientID, client.ClientSecret = strings.TrimSpace(client.ClientID), strings.TrimSpace(client.ClientSecret)
	if client.ClientID == "" || client.ClientSecret == "" {
		return legacyClient{}, errors.New("friday_client_incomplete")
	}
	return client, nil
}

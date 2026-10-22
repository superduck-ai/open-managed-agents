package evaluations

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	maxDatasetNameLen              = 255
	maxDatasetDescriptionLen       = 1024
	maxAttachmentBytes             = 1 << 20
	maxImportedCases               = 1000
	maxDatasetBodyBytes      int64 = 4 << 20
	maxImportBodyBytes       int64 = 100 << 20
)

type createDatasetRequest struct {
	Name            string `json:"name"`
	Desc            string `json:"desc"`
	SourceVersionID string `json:"source_version_id"`
}

type caseInput struct {
	Name           string          `json:"name"`
	Desc           string          `json:"desc"`
	Turns          []string        `json:"turns"`
	Checkpoints    json.RawMessage `json:"checkpoints"`
	InputFiles     json.RawMessage `json:"input_files"`
	ReferenceFiles json.RawMessage `json:"reference_files"`
}

type normalizedCase struct {
	Name           string
	Description    string
	Turns          []string
	Checkpoints    json.RawMessage
	InputFiles     json.RawMessage
	ReferenceFiles json.RawMessage
}

func normalizeDataset(body createDatasetRequest) (string, string, error) {
	return normalizeNameAndDescription(body.Name, body.Desc)
}

func normalizeNameAndDescription(name, desc string) (string, string, error) {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return "", "", nameRequired()
	}
	if utf8.RuneCountInString(trimmedName) > maxDatasetNameLen {
		return "", "", nameTooLong()
	}
	description := strings.TrimSpace(desc)
	if utf8.RuneCountInString(description) > maxDatasetDescriptionLen {
		return "", "", descriptionTooLong()
	}
	return trimmedName, description, nil
}

func normalizeCase(input caseInput) (normalizedCase, error) {
	name, description, err := normalizeNameAndDescription(input.Name, input.Desc)
	if err != nil {
		return normalizedCase{}, err
	}
	turns, err := normalizeTurns(input.Turns)
	if err != nil {
		return normalizedCase{}, err
	}
	checkpoints, err := normalizeAttachment(input.Checkpoints, false, checkpointsInvalid)
	if err != nil {
		return normalizedCase{}, err
	}
	inputFiles, err := normalizeAttachment(input.InputFiles, true, inputFilesInvalid)
	if err != nil {
		return normalizedCase{}, err
	}
	referenceFiles, err := normalizeAttachment(input.ReferenceFiles, true, referenceFilesInvalid)
	if err != nil {
		return normalizedCase{}, err
	}
	return normalizedCase{
		Name:           name,
		Description:    description,
		Turns:          turns,
		Checkpoints:    checkpoints,
		InputFiles:     inputFiles,
		ReferenceFiles: referenceFiles,
	}, nil
}

func normalizeTurns(turns []string) ([]string, error) {
	if turns == nil {
		return nil, turnsInvalid()
	}
	if len(turns) == 0 {
		return nil, turnsRequired()
	}
	normalized := make([]string, 0, len(turns))
	for _, turn := range turns {
		value := strings.TrimSpace(turn)
		if value == "" {
			return nil, turnEmpty()
		}
		normalized = append(normalized, value)
	}
	return normalized, nil
}

var errAttachmentTooLarge = errors.New("attachment too large")
var errBadJSONValue = errors.New("bad json value")

func normalizeAttachment(raw json.RawMessage, arrayOnly bool, invalid func() error) (json.RawMessage, error) {
	value, err := normalizeJSONValue(raw, arrayOnly)
	if errors.Is(err, errAttachmentTooLarge) {
		return nil, attachmentTooLarge()
	}
	if err != nil {
		return nil, invalid()
	}
	return value, nil
}

func normalizeJSONValue(raw json.RawMessage, arrayOnly bool) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if len(raw) > maxAttachmentBytes {
		return nil, errAttachmentTooLarge
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return nil, errBadJSONValue
	}
	if trimmed[0] == '[' {
		return raw, nil
	}
	if !arrayOnly && trimmed[0] == '{' {
		return raw, nil
	}
	return nil, errBadJSONValue
}

func normalizeCases(inputs []caseInput) ([]normalizedCase, error) {
	if len(inputs) > maxImportedCases {
		return nil, importTooLarge()
	}
	cases := make([]normalizedCase, 0, len(inputs))
	for _, input := range inputs {
		item, err := normalizeCase(input)
		if err != nil {
			return nil, err
		}
		cases = append(cases, item)
	}
	if len(cases) == 0 {
		return nil, importRequired()
	}
	return cases, nil
}

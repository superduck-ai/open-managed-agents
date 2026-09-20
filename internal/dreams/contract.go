package dreams

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	minDreamSessions = 1
	maxDreamSessions = 100
	maxModelChars    = 256
)

type dreamInputRequest struct {
	Type          string   `json:"type"`
	MemoryStoreID string   `json:"memory_store_id"`
	SessionIDs    []string `json:"session_ids"`
}

type dreamInputSelection struct {
	MemoryStoreID string
	SessionIDs    []string
}

type dreamMemoryStoreInput struct {
	Type          string `json:"type"`
	MemoryStoreID string `json:"memory_store_id"`
}

type dreamSessionsInput struct {
	Type       string   `json:"type"`
	SessionIDs []string `json:"session_ids"`
}

type dreamPublicOutput struct {
	Type          string `json:"type"`
	MemoryStoreID string `json:"memory_store_id"`
}

type dreamModelResponse struct {
	ID string `json:"id"`
}

func parseRequestedDreamInputs(inputs []dreamInputRequest) (dreamInputSelection, error) {
	if len(inputs) == 0 {
		return dreamInputSelection{}, errDreamInputsShape
	}
	var store *dreamInputRequest
	var sessions *dreamInputRequest
	for index, input := range inputs {
		switch input.Type {
		case "memory_store":
			if store != nil {
				return dreamInputSelection{}, errors.New("inputs must include exactly one memory_store")
			}
			storeInput := input
			store = &storeInput
		case "sessions":
			if sessions != nil {
				return dreamInputSelection{}, errors.New("inputs must include exactly one sessions input")
			}
			sessionsInput := input
			sessions = &sessionsInput
		default:
			return dreamInputSelection{}, fmt.Errorf("inputs[%d].type must be memory_store or sessions", index)
		}
	}
	if store == nil {
		return dreamInputSelection{}, errors.New("inputs must include one memory_store")
	}
	storeID := store.MemoryStoreID
	if storeID == "" {
		return dreamInputSelection{}, errors.New("memory_store.memory_store_id is required")
	}
	var sessionIDs []string
	switch {
	case sessions != nil:
		sessionIDs = sessions.SessionIDs
	case len(inputs) == 1:
		sessionIDs = store.SessionIDs
	default:
		return dreamInputSelection{}, errors.New("inputs must include one sessions input")
	}
	if sessionIDs == nil {
		sessionIDs = []string{}
	}
	return dreamInputSelection{MemoryStoreID: storeID, SessionIDs: sessionIDs}, nil
}

func parseStoredDreamInputs(raw json.RawMessage) (dreamInputSelection, error) {
	var inputs []dreamInputRequest
	if err := json.Unmarshal(raw, &inputs); err != nil {
		return dreamInputSelection{}, errDreamInputsShape
	}
	parsed, err := parseRequestedDreamInputs(inputs)
	if err != nil {
		return dreamInputSelection{}, errDreamInputsShape
	}
	return parsed, nil
}

func marshalOfficialDreamInputs(parsed dreamInputSelection) (json.RawMessage, error) {
	sessionIDs := parsed.SessionIDs
	if sessionIDs == nil {
		sessionIDs = []string{}
	}
	return json.Marshal([]any{
		dreamMemoryStoreInput{Type: "memory_store", MemoryStoreID: parsed.MemoryStoreID},
		dreamSessionsInput{Type: "sessions", SessionIDs: sessionIDs},
	})
}

func parseDreamModel(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", errors.New("model is required")
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return validatedDreamModelID(asString)
	}
	var asObject struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &asObject); err != nil {
		return "", errors.New("model must be a string or {\"id\": \"...\"}")
	}
	return validatedDreamModelID(asObject.ID)
}

func validatedDreamModelID(model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", errors.New("model is required")
	}
	if utf8.RuneCountInString(model) > maxModelChars {
		return "", errors.New("model must be at most 256 characters")
	}
	return model, nil
}

func dreamSessionCountError(count int) error {
	if count < minDreamSessions || count > maxDreamSessions {
		return fmt.Errorf("sessions.session_ids must contain between %d and %d sessions", minDreamSessions, maxDreamSessions)
	}
	return nil
}

func publicDreamInputs(raw json.RawMessage) json.RawMessage {
	parsed, err := parseStoredDreamInputs(raw)
	if err != nil {
		if len(raw) == 0 {
			return json.RawMessage(`[]`)
		}
		return raw
	}
	encoded, err := marshalOfficialDreamInputs(parsed)
	if err != nil {
		return raw
	}
	return encoded
}

func publicDreamOutputs(status string, raw json.RawMessage) json.RawMessage {
	if status == "pending" {
		return json.RawMessage(`[]`)
	}
	output, err := dreamOutput(raw)
	if err != nil {
		return json.RawMessage(`[]`)
	}
	encoded, err := json.Marshal([]dreamPublicOutput{{Type: "memory_store", MemoryStoreID: output.MemoryStoreID}})
	if err != nil {
		return raw
	}
	return encoded
}

func publicDreamSessionID(status string, raw json.RawMessage) *string {
	if status == "pending" {
		return nil
	}
	output, err := dreamOutput(raw)
	if err != nil || output.InternalSessionID == "" {
		return nil
	}
	id := output.InternalSessionID
	return &id
}

func publicDreamModel(model string) dreamModelResponse {
	return dreamModelResponse{ID: model}
}

func responseFromDream(value db.Dream) dreamResponse {
	return dreamResponse{
		Type:         "dream",
		ID:           value.ExternalID,
		Status:       value.Status,
		Inputs:       publicDreamInputs(value.Inputs),
		Outputs:      publicDreamOutputs(value.Status, value.Outputs),
		Model:        publicDreamModel(value.Model),
		Instructions: value.Instructions,
		SessionID:    publicDreamSessionID(value.Status, value.Outputs),
		CreatedAt:    value.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    value.UpdatedAt.UTC().Format(time.RFC3339),
		EndedAt:      formatOptionalTime(value.EndedAt),
		ArchivedAt:   formatOptionalTime(value.ArchivedAt),
		Usage:        usageResponseFromDream(value.Usage),
		Error:        errorResponseFromDream(value.Error),
	}
}

var errDreamInputsShape = errors.New("Dream inputs must contain one memory_store")

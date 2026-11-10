package evaluations

import (
	"encoding/json"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type sessionTurn struct {
	Index      int
	Query      string
	Answer     string
	Inputs     json.RawMessage
	Importable bool
}

func sessionTurns(events []db.SessionEvent) []sessionTurn {
	var turns []sessionTurn
	var current *sessionTurn
	for _, event := range events {
		text := eventText(event.Payload)
		switch event.EventType {
		case "user.message":
			query, inputs, kind := userTurnInputs(event.Payload)
			if kind == userTurnEmpty {
				continue
			}
			if current != nil {
				turns = append(turns, *current)
				current = nil
			}
			turn := sessionTurn{Index: len(turns) + 1, Query: query, Inputs: inputs, Importable: kind == userTurnReady}
			if !turn.Importable {
				turns = append(turns, turn)
				continue
			}
			current = &turn
		case "agent.message":
			if current == nil || text == "" {
				continue
			}
			if current.Answer != "" {
				current.Answer += "\n"
			}
			current.Answer += text
		}
	}
	if current != nil {
		turns = append(turns, *current)
	}
	return turns
}

func selectedSessionTurns(turns []sessionTurn, indexes []int) ([]sessionTurn, error) {
	if len(indexes) == 0 {
		return nil, invalidArgument(turnsRequiredMessage, nil)
	}
	if len(indexes) > maxImportCases {
		return nil, invalidArgument(importTooLargeMessage, nil)
	}
	byIndex := make(map[int]sessionTurn, len(turns))
	for _, turn := range turns {
		byIndex[turn.Index] = turn
	}
	wanted := make(map[int]struct{}, len(indexes))
	for _, index := range indexes {
		if _, ok := byIndex[index]; !ok {
			return nil, invalidArgument(turnInvalidMessage, nil)
		}
		wanted[index] = struct{}{}
	}
	selected := make([]sessionTurn, 0, len(wanted))
	for _, turn := range turns {
		if _, ok := wanted[turn.Index]; ok {
			selected = append(selected, turn)
		}
	}
	return selected, nil
}

func sessionCaseDrafts(turns []sessionTurn, indexes []int, sessionID, expectedAnswer, polarity, acceptance string) ([]caseDraft, int, error) {
	selected, err := selectedSessionTurns(turns, indexes)
	if err != nil {
		return nil, 0, err
	}
	drafts := make([]caseDraft, 0, len(selected))
	skipped := 0
	for _, turn := range selected {
		if !turn.Importable {
			skipped++
			continue
		}
		draft, err := normalizeCase(rawCase{
			Query: turn.Query, Inputs: turn.Inputs, ExpectedAnswer: expectedAnswer, SourceSessionID: sessionID, Polarity: polarity, Acceptance: acceptance,
		})
		if err != nil {
			return nil, 0, err
		}
		drafts = append(drafts, draft)
	}
	return drafts, skipped, nil
}

func eventText(payload json.RawMessage) string {
	var body struct {
		Content json.RawMessage `json:"content"`
		Text    string          `json:"text"`
		Name    string          `json:"name"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return ""
	}
	if strings.TrimSpace(body.Text) != "" {
		return strings.TrimSpace(body.Text)
	}
	trimmed := strings.TrimSpace(string(body.Content))
	if trimmed == "" || trimmed == "null" {
		if body.Name != "" {
			return body.Name
		}
		return ""
	}
	if strings.HasPrefix(trimmed, "\"") {
		var text string
		if err := json.Unmarshal(body.Content, &text); err == nil {
			return strings.TrimSpace(text)
		}
	}
	var blocks []struct {
		Text string `json:"text"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body.Content, &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, block := range blocks {
		if strings.TrimSpace(block.Text) != "" {
			parts = append(parts, strings.TrimSpace(block.Text))
		} else if block.Name != "" {
			parts = append(parts, block.Name)
		}
	}
	return strings.Join(parts, "\n")
}

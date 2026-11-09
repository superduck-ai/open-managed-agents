package evaluations

import (
	"encoding/json"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type sessionTurn struct {
	Index  int
	Query  string
	Answer string
}

func sessionTurns(events []db.SessionEvent) []sessionTurn {
	var turns []sessionTurn
	var current *sessionTurn
	for _, event := range events {
		text := eventText(event.Payload)
		switch event.EventType {
		case "user.message":
			if text == "" {
				continue
			}
			if current != nil {
				turns = append(turns, *current)
			}
			current = &sessionTurn{Index: len(turns) + 1, Query: text}
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

func transcriptFromEvents(events []db.SessionEvent) (output, trajectory string) {
	var answers []string
	var steps []string
	for _, event := range events {
		text := eventText(event.Payload)
		switch event.EventType {
		case "agent.message":
			if text != "" {
				answers = append(answers, text)
			}
		case "agent.tool_use", "agent.tool_result":
			if text != "" {
				steps = append(steps, event.EventType+": "+text)
			}
		}
	}
	return clip(strings.Join(answers, "\n"), 8000), clip(strings.Join(steps, "\n"), 8000)
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
	if err := json.Unmarshal(body.Content, &blocks); err == nil {
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
	if body.Name != "" {
		return body.Name
	}
	return clip(trimmed, 500)
}

func clip(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func terminalStatus(events []db.SessionEvent) (string, string, bool) {
	failed := false
	completed := false
	for _, event := range events {
		switch event.EventType {
		case "session.status_terminated", "session.deleted":
			failed = true
		case "session.status_idle":
			reason := stopReason(event.Payload)
			if reason == "retries_exhausted" {
				failed = true
			}
			if reason == "end_turn" {
				completed = true
			}
		}
	}
	if failed {
		return "failed", "Session terminated", true
	}
	if completed {
		return "completed", "", true
	}
	return "", "", false
}

func stopReason(payload json.RawMessage) string {
	var body struct {
		StopReason *struct {
			Type string `json:"type"`
		} `json:"stop_reason"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || body.StopReason == nil {
		return ""
	}
	return body.StopReason.Type
}

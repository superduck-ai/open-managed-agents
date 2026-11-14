package channels

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"net/url"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessions"
)

const channelForeignInputMessage = "OMA 会话出现外部输入，已暂停渠道派发；请在 OMA 中核对，并使用 /new 或 /reset 新建会话。"

func channelOwnsInput(record db.ChannelRecord, chatID string, event omaEvent) bool {
	var source channelInputMetadata
	if err := jsonv2.Unmarshal(event.Metadata, &source); err != nil {
		return false
	}
	return source.ChannelID == record.ExternalID && source.Type == record.ChannelType && source.AccountID == record.AppID && source.ChatID == chatID && source.MessageID != "" && source.ActorID != ""
}

func channelHistoryHasForeignInput(record db.ChannelRecord, chatID string, events []omaEvent, owned bool) bool {
	for _, event := range events {
		if event.Type != "user.message" {
			continue
		}
		if channelOwnsInput(record, chatID, event) {
			owned = true
			continue
		}
		if owned || len(event.Metadata) > 0 && string(event.Metadata) != "null" {
			return true
		}
	}
	return false
}

func (w *channelWorker) checkInputHistory(ctx context.Context, apiKey, sessionID, chatID string) (bool, error) {
	filter := url.Values{"types[]": {"user.message"}}
	w.mu.Lock()
	after := w.checkedInputs[sessionID]
	w.mu.Unlock()
	if after != "" {
		filter.Set("page", sessions.EncodeEventCursor(after, "asc"))
	}
	owned, foreign, lastID := after != "", false, ""
	err := visitOMAEventPages(ctx, w.supervisor.oma, apiKey, sessionID, filter, func(events []struct {
		ID          string          `json:"id"`
		Type        string          `json:"type"`
		Metadata    json.RawMessage `json:"metadata"`
		ProcessedAt json.RawMessage `json:"processed_at"`
	}) error {
		for _, event := range events {
			input := omaEvent{ID: event.ID, Type: event.Type, Metadata: event.Metadata}
			foreign = foreign || channelHistoryHasForeignInput(w.record, chatID, []omaEvent{input}, owned)
			if event.Type == "user.message" && channelOwnsInput(w.record, chatID, input) {
				owned = true
				if len(event.ProcessedAt) > 0 && string(event.ProcessedAt) != "null" {
					lastID = event.ID
				}
			}
		}
		return nil
	})
	if err != nil || foreign {
		return foreign, err
	}
	if lastID != "" {
		w.mu.Lock()
		w.checkedInputs[sessionID] = lastID
		w.mu.Unlock()
	}
	return false, nil
}

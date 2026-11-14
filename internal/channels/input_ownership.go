package channels

import (
	"context"
	"encoding/json/v2"
	"github.com/superduck-ai/open-managed-agents/internal/sessions"
	"net/url"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const channelForeignInputMessage = "OMA 会话出现外部输入，已暂停渠道派发；请在 OMA 中核对，并使用 /new 或 /reset 新建会话。"

func channelOwnsInput(record db.ChannelRecord, chatID string, event omaEvent) bool {
	var source channelInputMetadata
	if err := json.Unmarshal(event.Metadata, &source); err != nil {
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
	events, err := w.supervisor.oma.listEventPages(ctx, apiKey, sessionID, filter)
	if err != nil {
		return false, err
	}
	if channelHistoryHasForeignInput(w.record, chatID, events, after != "") {
		return true, nil
	}
	if len(events) > 0 {
		last := events[len(events)-1]
		if last.ID != "" && len(last.ProcessedAt) > 0 && string(last.ProcessedAt) != "null" && channelOwnsInput(w.record, chatID, last) {
			w.mu.Lock()
			w.checkedInputs[sessionID] = last.ID
			w.mu.Unlock()
		}
	}
	return false, nil
}

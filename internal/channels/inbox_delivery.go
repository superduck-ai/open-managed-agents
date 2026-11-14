package channels

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type channelPreparedInput struct {
	ArtifactBaseline artifactBaseline           `json:"artifact_baseline"`
	Preparing        bool                       `json:"preparing,omitzero"`
	Attachments      []channelAttachmentReceipt `json:"attachments,omitempty"`
	Content          []map[string]any           `json:"content"`
	Skipped          []string                   `json:"skipped,omitempty"`
	Mounted          map[string]struct{}        `json:"mounted"`
}

func (w *channelWorker) inputSession(ctx context.Context, msg *channelMessage, scopeKey string) (string, error) {
	if msg.input == nil || msg.input.SessionExternalID == "" {
		return w.sessionFor(ctx, msg, scopeKey)
	}
	if w.currentSession(ctx, scopeKey) != msg.input.SessionExternalID {
		return "", errChannelSessionUnconfirmed
	}
	return msg.input.SessionExternalID, nil
}

func (w *channelWorker) inputContent(ctx context.Context, apiKey, sessionID string, msg *channelMessage) ([]map[string]any, map[string]struct{}, error) {
	var prepared channelPreparedInput
	if msg.input != nil && msg.input.PreparedContent != "" {
		err := json.Unmarshal([]byte(msg.input.PreparedContent), &prepared)
		if err != nil {
			return nil, nil, err
		}
		if len(prepared.Skipped) > 0 {
			return nil, nil, fmt.Errorf("原批次存在跳过的附件，本轮未提交：%s", strings.Join(prepared.Skipped, "、"))
		}
		if !prepared.Preparing {
			return prepared.Content, prepared.Mounted, nil
		}
	} else {
		prepared.Preparing = true
	}
	content, mounted, err := w.messageContent(ctx, apiKey, sessionID, msg, msg.Content, &prepared)
	if err != nil || msg.input == nil {
		return content, mounted, err
	}
	err = w.savePreparedInput(ctx, msg, sessionID, channelPreparedInput{Content: content, Mounted: mounted, ArtifactBaseline: prepared.ArtifactBaseline})
	return content, mounted, err
}

func (w *channelWorker) savePreparedInput(ctx context.Context, msg *channelMessage, sessionID string, prepared channelPreparedInput) error {
	if msg.input == nil {
		return nil
	}
	payload, err := json.Marshal(prepared, json.FormatNilMapAsNull(true))
	if err != nil {
		return err
	}
	return w.advanceInput(ctx, msg, db.ChannelInputAdvance{State: "prepared", SessionExternalID: sessionID, PreparedContent: string(payload)})
}

func (w *channelWorker) resumeAcceptedInput(task *channelReplyTask, msg *channelMessage, apiKey string) error {
	task.inputEventID = msg.input.InputEventID
	row, err := w.prepareReplyDelivery(task.ctx, task, msg)
	if err != nil {
		return err
	}
	if row.ReplyMessageID != "" || row.State == "completed" {
		return w.completeInput(task.ctx, msg)
	}
	return w.streamReply(task.ctx, task, msg, apiKey)
}

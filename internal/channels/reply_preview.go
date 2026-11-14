package channels

import (
	"context"
	"encoding/json/v2"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/sessions"
)

const channelPreviewUpdateInterval = 250 * time.Millisecond

type channelPreview struct {
	EventID string `json:"event_id"`
	Event   struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"event"`
	Delta struct {
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"delta"`
}

func (r *replyRunner) consumePreview(ctx context.Context, event sessions.StreamEvent) error {
	var preview channelPreview
	if err := json.Unmarshal(event.Payload, &preview); err != nil {
		return err
	}
	if r.reply.ended {
		return nil
	}
	if event.EventType == "event_start" {
		if preview.Event.Type == "agent.thinking" {
			if r.presentation.IncludeThinking {
				r.reply.thinking = true
				r.reply.status = "思考中"
				r.previewDirty = true
			}
		} else {
			for _, block := range r.reply.blocks {
				if block.eventID == preview.Event.ID {
					return nil
				}
			}
			r.reply.blocks = append(r.reply.blocks, channelReplyBlock{eventID: preview.Event.ID, preview: new(strings.Builder)})
		}
	} else {
		for index := range r.reply.blocks {
			block := &r.reply.blocks[index]
			if block.eventID == preview.EventID && block.preview != nil {
				block.preview.WriteString(preview.Delta.Content.Text)
				r.reply.thinking = false
				r.reply.status = "正在输出"
				r.previewDirty = true
				break
			}
		}
	}
	if !r.previewDirty {
		return nil
	}
	if r.stream == nil {
		return r.open(ctx)
	}
	if time.Since(r.lastUpdate) >= channelPreviewUpdateInterval {
		return r.update(ctx)
	}
	return nil
}

func (r *replyRunner) resetPreviews() {
	blocks := r.reply.blocks[:0]
	for _, block := range r.reply.blocks {
		if block.preview == nil {
			blocks = append(blocks, block)
		}
	}
	clear(r.reply.blocks[len(blocks):])
	r.reply.blocks = blocks
	r.previewDirty = true
}

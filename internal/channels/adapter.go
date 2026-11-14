package channels

import (
	"context"
	"io"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type channelAttachment struct {
	Type     string `json:"type"`
	FileKey  string `json:"file_key"`
	FileName string `json:"file_name"`
}

type channelMessage struct {
	MessageID      string              `json:"message_id"`
	MessageIDs     []string            `json:"message_ids"`
	ChatID         string              `json:"chat_id"`
	ChatType       string              `json:"chat_type"`
	UserID         string              `json:"user_id"`
	Content        string              `json:"content"`
	RawContentType string              `json:"raw_content_type"`
	RootID         string              `json:"root_id"`
	ThreadID       string              `json:"thread_id"`
	Resources      []channelAttachment `json:"resources"`
	input          *db.ChannelInput
}

type channelReplyStream interface {
	receipt() string
	update(context.Context, *channelReply) error
	close(context.Context) error
}

type channelAdapter interface {
	start(context.Context) error
	close(context.Context) error
	replyText(context.Context, *channelMessage, string) error
	openReply(context.Context, *channelMessage) (channelReplyStream, error)
	resumeReply(string) channelReplyStream
	downloadAttachment(context.Context, string, channelAttachment, io.Writer) (string, error)
	sendMedia(context.Context, *channelMessage, omaResource, string, []byte) (string, error)
}

type channelAdapterFactory func(*channelWorker, string) (channelAdapter, error)

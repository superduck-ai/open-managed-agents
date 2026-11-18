package contracts

import (
	"context"
	"io"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

type Attachment struct {
	Type     string `json:"type"`
	FileKey  string `json:"file_key"`
	FileName string `json:"file_name"`
}

type Message struct {
	MessageID      string           `json:"message_id"`
	MessageIDs     []string         `json:"message_ids"`
	ChatID         string           `json:"chat_id"`
	ChatType       string           `json:"chat_type"`
	UserID         string           `json:"user_id"`
	Content        string           `json:"content"`
	RawContentType string           `json:"raw_content_type"`
	RootID         string           `json:"root_id"`
	ThreadID       string           `json:"thread_id"`
	Resources      []Attachment     `json:"resources"`
	Input          *db.ChannelInput `json:"-"`
}

type Resource struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	FileID    string `json:"file_id"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	MountPath string `json:"mount_path"`
	UpdatedAt string `json:"updated_at"`
}

type ActionValue struct {
	RequestKey string `json:"-"`
	Kind       string `json:"kind"`
	ChannelID  string `json:"channel_id"`
	ScopeKey   string `json:"scope_key"`
	SessionID  string `json:"session_id"`
	ToolUseID  string `json:"tool_use_id,omitempty"`
	PairingID  string `json:"pairing_id,omitempty"`
	SourceID   string `json:"source_message_id,omitempty"`
	Result     string `json:"result,omitempty"`
	OpenID     string `json:"open_id"`
	ChatType   string `json:"chat_type,omitempty"`
	ThreadID   string `json:"session_thread_id,omitempty"`
}

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type Question struct {
	ID          string           `json:"id"`
	Title       string           `json:"question"`
	Options     []QuestionOption `json:"options"`
	MultiSelect bool             `json:"multi_select"`
}

type ReplyStream interface {
	Receipt() string
	Update(context.Context, Reply) error
	Close(context.Context) error
}
type Adapter interface {
	OAuthProvider() vaults.EnvOAuthProviderSnapshot
	OAuthUserID(context.Context, string) (string, error)
	Start(context.Context) error
	Close(context.Context) error
	ReplyText(context.Context, *Message, string) error
	OpenReply(context.Context, *Message, ...Reply) (ReplyStream, error)
	ResumeReply(string) ReplyStream
	DownloadAttachment(context.Context, string, Attachment, io.Writer) (string, error)
	SendMedia(context.Context, *Message, Resource, string, []byte) (string, error)
}
type ActionEvent struct {
	Value     map[string]any
	FormValue map[string]any
	OpenID    string
	ChatID    string
	MessageID string
}
type ReplyTool struct{ ID, Name, Input, Output, Status string }
type ReplyBlock struct {
	Text string
	Tool *ReplyTool
}
type Interaction struct {
	Kind      string
	Value     ActionValue
	Name      string
	Input     string
	Questions []Question
}
type Reply struct {
	OAuthOrigin                  string
	Interrupted, Thinking, Ended bool
	Status, Text                 string
	Blocks                       []ReplyBlock
	StopValue                    *ActionValue
	PairingValue                 *ActionValue
	Interactions                 []Interaction
}

type AdapterConfig struct {
	AppID, Secret                  string
	IncludeThinking, ReplyInThread bool
	FileMaxBytes                   int64
	Online                         func(bool)
	Receive                        func(context.Context, *Message) error
	Action                         func(context.Context, *ActionEvent) error
	Prepare                        func(context.Context, *Message, string) error
	Busy                           func(string) bool
	Context                        func() context.Context
}

type AdapterFactory func(AdapterConfig) (Adapter, error)

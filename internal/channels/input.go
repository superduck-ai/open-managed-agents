package channels

import (
	"crypto/sha256"
	"fmt"
	"slices"
)

type channelInputMetadata struct {
	ChannelID  string   `json:"channel_id"`
	Type       string   `json:"channel_type"`
	AccountID  string   `json:"channel_account_id"`
	MessageID  string   `json:"channel_message_id"`
	MessageIDs []string `json:"channel_message_ids,omitempty"`
	ChatID     string   `json:"channel_chat_id"`
	ActorID    string   `json:"channel_actor_id"`
}

func (m channelInputMetadata) matches(other channelInputMetadata) bool {
	return m.ChannelID == other.ChannelID && m.Type == other.Type && m.AccountID == other.AccountID && m.MessageID == other.MessageID && m.ChatID == other.ChatID && m.ActorID == other.ActorID && slices.Equal(m.MessageIDs, other.MessageIDs)
}

func (m channelInputMetadata) requestKey() string {
	return fmt.Sprintf("channel-%x", sha256.Sum256([]byte(m.Type+"\x00"+m.ChannelID+"\x00"+m.AccountID+"\x00"+m.MessageID)))
}

package channels

import (
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func cardControlRequestKey(receipt db.ChannelReplyDelivery, targetID string) string {
	identity := struct {
		Type, Channel, Account, Session, Input, Message, Scope, Chat, Actor, Target string
	}{
		Type: receipt.ChannelType, Channel: receipt.ChannelExternalID, Account: receipt.AccountID,
		Session: receipt.SessionExternalID, Input: receipt.InputEventID, Message: receipt.ReplyMessageID,
		Scope: receipt.ScopeKey, Chat: receipt.ChatID, Actor: receipt.ActorID, Target: targetID,
	}
	encoded, _ := json.Marshal(identity)
	return fmt.Sprintf("channel-control-v1-%x", sha256.Sum256(encoded))
}

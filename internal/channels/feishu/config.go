package feishu

import (
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
)

type Config = contracts.AdapterConfig

func NormalizeAction(event *channeltypes.CardActionEvent) *contracts.ActionEvent {
	if event == nil {
		return nil
	}
	return &contracts.ActionEvent{Value: event.Action.Value, FormValue: event.Action.FormValue, OpenID: event.Operator.OpenID, ChatID: event.ChatID, MessageID: event.MessageID}
}

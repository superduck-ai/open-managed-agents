package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
)

type ReplyStream struct {
	Client    *lark.Client
	messageID string
}

func (s *ReplyStream) Update(ctx context.Context, reply contracts.Reply) error {
	request := larkim.NewPatchMessageReqBuilder().MessageId(s.messageID).Body(larkim.NewPatchMessageReqBodyBuilder().Content(Render(reply)).Build()).Build()
	response, err := s.Client.Im.V1.Message.Patch(ctx, request)
	if err != nil {
		return err
	}
	if !response.Success() {
		return fmt.Errorf("飞书更新卡片失败: %d %s", response.Code, response.Msg)
	}
	return nil
}
func (s *ReplyStream) Receipt() string { return s.messageID }
func (w *Adapter) ResumeReply(receipt string) contracts.ReplyStream {
	return &ReplyStream{Client: w.Client, messageID: receipt}
}

func (s *ReplyStream) Close(context.Context) error { return nil }
func (w *Adapter) OpenReply(ctx context.Context, msg *contracts.Message, initial ...contracts.Reply) (contracts.ReplyStream, error) {
	card := Card("", w.Config.IncludeThinking)
	if len(initial) > 0 {
		card = Render(initial[0])
	}
	var messageID string
	if msg.ChatType == "group" && w.Config.ReplyInThread && msg.MessageID != "" {
		id, err := w.replyMessage(ctx, msg.MessageID, "interactive", card)
		if err != nil {
			return nil, err
		}
		messageID = id
	} else {
		sent, err := w.Channel.Send(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Card: card})
		if err != nil {
			return nil, err
		}
		messageID = sent.MessageID
	}
	return &ReplyStream{Client: w.Client, messageID: messageID}, nil
}

func (w *Adapter) replyMessage(ctx context.Context, messageID, msgType, content string) (string, error) {
	request := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().MsgType(msgType).Content(content).ReplyInThread(true).Build()).
		Build()
	response, err := w.Client.Im.V1.Message.Reply(ctx, request)
	if err != nil {
		return "", err
	}
	if !response.Success() {
		return "", fmt.Errorf("飞书回复消息失败: %d %s", response.Code, response.Msg)
	}
	if response.Data == nil || response.Data.MessageId == nil {
		return "", errors.New("飞书回复消息缺少 message_id")
	}
	return *response.Data.MessageId, nil
}

func (w *Adapter) ReplyText(ctx context.Context, msg *contracts.Message, text string) error {
	if msg.ChatType == "group" && w.Config.ReplyInThread && msg.MessageID != "" {
		content, err := json.Marshal(map[string]string{"text": text})
		if err != nil {
			return err
		}
		_, err = w.replyMessage(ctx, msg.MessageID, "text", string(content))
		return err
	}
	_, err := w.Channel.Send(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Text: text})
	return err
}

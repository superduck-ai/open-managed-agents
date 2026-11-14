package channels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type feishuReplyStream struct {
	client    *lark.Client
	messageID string
}

func (s *feishuReplyStream) update(ctx context.Context, reply *channelReply) error {
	request := larkim.NewPatchMessageReqBuilder().MessageId(s.messageID).Body(larkim.NewPatchMessageReqBodyBuilder().Content(reply.render()).Build()).Build()
	response, err := s.client.Im.V1.Message.Patch(ctx, request)
	if err != nil {
		return err
	}
	if !response.Success() {
		return fmt.Errorf("飞书更新卡片失败: %d %s", response.Code, response.Msg)
	}
	return nil
}
func (s *feishuReplyStream) receipt() string { return s.messageID }
func (w *feishuChannel) resumeReply(receipt string) channelReplyStream {
	return &feishuReplyStream{client: w.client, messageID: receipt}
}

func (s *feishuReplyStream) close(context.Context) error { return nil }
func (w *feishuChannel) openReply(ctx context.Context, msg *channelMessage) (channelReplyStream, error) {
	card := channelCard("", w.worker.record.IncludeThinking)
	var messageID string
	if msg.ChatType == "group" && w.worker.record.ReplyInThread && msg.MessageID != "" {
		id, err := w.replyMessage(ctx, msg.MessageID, "interactive", card)
		if err != nil {
			return nil, err
		}
		messageID = id
	} else {
		sent, err := w.channel.Send(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Card: card})
		if err != nil {
			return nil, err
		}
		messageID = sent.MessageID
	}
	return &feishuReplyStream{client: w.client, messageID: messageID}, nil
}

func (w *feishuChannel) replyMessage(ctx context.Context, messageID, msgType, content string) (string, error) {
	request := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().MsgType(msgType).Content(content).ReplyInThread(true).Build()).
		Build()
	response, err := w.client.Im.V1.Message.Reply(ctx, request)
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

func (w *feishuChannel) replyText(ctx context.Context, msg *channelMessage, text string) error {
	if msg.ChatType == "group" && w.worker.record.ReplyInThread && msg.MessageID != "" {
		content, err := json.Marshal(map[string]string{"text": text})
		if err != nil {
			return err
		}
		_, err = w.replyMessage(ctx, msg.MessageID, "text", string(content))
		return err
	}
	_, err := w.channel.Send(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Text: text})
	return err
}

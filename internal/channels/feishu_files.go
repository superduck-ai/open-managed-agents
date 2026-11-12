package channels

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

const (
	channelAttachmentLimit = 10
	channelAttachmentBytes = 20 << 20
)

func (w *channelWorker) messageContent(ctx context.Context, msg *channeltypes.NormalizedMessage, text string) ([]map[string]any, []string, error) {
	content := make([]map[string]any, 0, 1+len(msg.Resources))
	if text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	skipped := make([]string, 0)
	for index, resource := range msg.Resources {
		if index >= channelAttachmentLimit {
			return nil, nil, fmt.Errorf("一次最多发送 %d 个附件", channelAttachmentLimit)
		}
		block, err := w.attachmentBlock(ctx, resource)
		if err != nil {
			return nil, nil, err
		}
		if block == nil {
			name := resource.FileName
			if name == "" {
				name = resource.Type
			}
			skipped = append(skipped, name)
			continue
		}
		content = append(content, block)
	}
	return content, skipped, nil
}

func (w *channelWorker) attachmentBlock(ctx context.Context, resource channeltypes.Resource) (map[string]any, error) {
	if resource.FileKey == "" {
		return nil, nil
	}
	mediaType := resource.Type
	if mediaType != "image" {
		mediaType = "file"
	}
	data, err := w.channel.DownloadFile(ctx, resource.FileKey, mediaType)
	if err != nil {
		return nil, fmt.Errorf("下载附件失败: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) > channelAttachmentBytes {
		return nil, fmt.Errorf("附件超过 %d MB", channelAttachmentBytes>>20)
	}
	detected := http.DetectContentType(data)
	if resource.Type == "image" {
		switch detected {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
			return map[string]any{
				"type":   "image",
				"source": map[string]string{"type": "base64", "media_type": detected, "data": base64.StdEncoding.EncodeToString(data)},
			}, nil
		default:
			return nil, nil
		}
	}
	switch {
	case detected == "application/pdf":
		return map[string]any{
			"type":   "document",
			"source": map[string]string{"type": "base64", "media_type": "application/pdf", "data": base64.StdEncoding.EncodeToString(data)},
		}, nil
	case strings.HasPrefix(detected, "text/"):
		return map[string]any{
			"type":   "document",
			"source": map[string]string{"type": "text", "media_type": "text/plain", "data": string(data)},
		}, nil
	default:
		return nil, nil
	}
}

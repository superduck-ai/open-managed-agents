package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"io"
	"net/http"
	"slices"
	"strings"
)

func (w *feishuChannel) downloadAttachment(ctx context.Context, messageID string, resource channelAttachment) ([]byte, string, error) {
	if resource.FileKey == "" || messageID == "" {
		return nil, "", fmt.Errorf("附件缺少原消息或文件标识")
	}
	kind := "file"
	if resource.Type == "image" {
		kind = "image"
	}
	request := larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(resource.FileKey).
		Type(kind).
		Build()
	response, err := w.client.Im.V1.MessageResource.Get(ctx, request)
	if err != nil {
		return nil, "", fmt.Errorf("下载附件失败: %w", err)
	}
	if !response.Success() || response.File == nil {
		return nil, "", fmt.Errorf("下载附件失败: %d %s", response.Code, response.Msg)
	}
	data, err := io.ReadAll(io.LimitReader(response.File, w.worker.supervisor.fileMaxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("读取附件失败: %w", err)
	}
	if int64(len(data)) > w.worker.supervisor.fileMaxBytes {
		return nil, "", fmt.Errorf("附件超过 OMA 文件大小限制")
	}
	mediaType := http.DetectContentType(data)
	if resource.Type == "image" && !slices.Contains([]string{"image/png", "image/jpeg", "image/gif", "image/webp"}, mediaType) {
		return nil, "", fmt.Errorf("图片格式暂不支持，仅支持 PNG、JPEG、GIF 和 WebP")
	}
	return data, mediaType, nil
}

func (w *feishuChannel) sendMedia(ctx context.Context, msg *channelMessage, resource omaResource, contentType string, data []byte) (string, error) {
	name := strings.TrimSpace(resource.Filename)
	if name == "" {
		name = resource.FileID
	}
	image := strings.HasPrefix(contentType, "image/") || strings.HasPrefix(resource.MimeType, "image/")
	if image {
		key, err := w.uploadImage(ctx, data)
		if err == nil {
			return w.sendMediaKey(ctx, msg, "image", "image_key", key)
		}
		if !errors.Is(err, errChannelImageUploadRejected) {
			return "", err
		}
	}
	key, err := w.uploadFile(ctx, name, data)
	if err != nil {
		return "", err
	}
	return w.sendMediaKey(ctx, msg, "file", "file_key", key)
}

func (w *feishuChannel) uploadImage(ctx context.Context, data []byte) (string, error) {
	response, err := w.client.Im.V1.Image.Create(ctx, larkim.NewCreateImageReqBuilder().
		Body(larkim.NewCreateImageReqBodyBuilder().ImageType("message").Image(bytes.NewReader(data)).Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !response.Success() {
		return "", feishuMediaUploadError(response.StatusCode, response.Code, true, response.Err != nil && len(response.Err.PermissionViolations) > 0)
	}
	if response.Data == nil || response.Data.ImageKey == nil || *response.Data.ImageKey == "" {
		return "", fmt.Errorf("飞书图片上传回执缺少image_key")
	}
	return *response.Data.ImageKey, nil
}

func (w *feishuChannel) uploadFile(ctx context.Context, name string, data []byte) (string, error) {
	response, err := w.client.Im.V1.File.Create(ctx, larkim.NewCreateFileReqBuilder().
		Body(larkim.NewCreateFileReqBodyBuilder().FileType("stream").FileName(name).File(bytes.NewReader(data)).Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !response.Success() {
		return "", feishuMediaUploadError(response.StatusCode, response.Code, false, response.Err != nil && len(response.Err.PermissionViolations) > 0)
	}
	if response.Data == nil || response.Data.FileKey == nil || *response.Data.FileKey == "" {
		return "", fmt.Errorf("飞书文件上传回执缺少file_key")
	}
	return *response.Data.FileKey, nil
}

func (w *feishuChannel) sendMediaKey(ctx context.Context, msg *channelMessage, msgType, keyField, key string) (string, error) {
	content, err := json.Marshal(map[string]string{keyField: key})
	if err != nil {
		return "", err
	}
	if msg.ChatType == "group" && w.worker.record.ReplyInThread && msg.MessageID != "" {
		return w.replyMessage(ctx, msg.MessageID, msgType, string(content))
	}
	input := &channeltypes.SendInput{ChatID: msg.ChatID}
	if msgType == "image" {
		input.ImageKey = key
	} else {
		input.FileKey = key
	}
	result, err := w.channel.Send(ctx, input)
	if err != nil {
		return "", err
	}
	return result.MessageID, nil
}

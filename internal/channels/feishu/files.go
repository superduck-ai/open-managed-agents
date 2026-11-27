package feishu

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
)

func (w *Adapter) DownloadAttachment(ctx context.Context, messageID string, resource contracts.Attachment, destination io.Writer) (string, error) {
	if resource.FileKey == "" || messageID == "" {
		return "", fmt.Errorf("附件缺少原消息或文件标识")
	}
	kind := "file"
	if resource.Type == "image" {
		kind = "image"
	}
	response, err := w.Client.Im.V1.MessageResource.Get(ctx, larkim.NewGetMessageResourceReqBuilder().MessageId(messageID).FileKey(resource.FileKey).Type(kind).Build())
	if err != nil {
		return "", fmt.Errorf("下载附件失败: %w", err)
	}
	if !response.Success() || response.File == nil {
		return "", fmt.Errorf("下载附件失败: %d %s", response.Code, response.Msg)
	}
	if closer, ok := response.File.(io.Closer); ok {
		defer closer.Close()
	}
	reader := bufio.NewReader(response.File)
	header, err := reader.Peek(512)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("读取附件失败: %w", err)
	}
	mediaType := http.DetectContentType(header)
	if resource.Type == "image" && !slices.Contains([]string{"image/png", "image/jpeg", "image/gif", "image/webp"}, mediaType) {
		return "", fmt.Errorf("图片格式暂不支持，仅支持 PNG、JPEG、GIF 和 WebP")
	}
	size, err := io.Copy(destination, io.LimitReader(reader, w.Config.FileMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("读取附件失败: %w", err)
	}
	if size > w.Config.FileMaxBytes {
		return "", fmt.Errorf("附件超过 OMA 文件大小限制")
	}
	return mediaType, nil
}

func (w *Adapter) SendMedia(ctx context.Context, msg *contracts.Message, resource contracts.Resource, contentType string, data []byte) (string, error) {
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

func (w *Adapter) uploadImage(ctx context.Context, data []byte) (string, error) {
	response, err := w.Client.Im.V1.Image.Create(ctx, larkim.NewCreateImageReqBuilder().
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

func (w *Adapter) uploadFile(ctx context.Context, name string, data []byte) (string, error) {
	response, err := w.Client.Im.V1.File.Create(ctx, larkim.NewCreateFileReqBuilder().
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

func (w *Adapter) sendMediaKey(ctx context.Context, msg *contracts.Message, msgType, keyField, key string) (string, error) {
	content, err := json.Marshal(map[string]string{keyField: key})
	if err != nil {
		return "", err
	}
	if target := w.threadReplyTarget(msg); target != "" {
		return w.replyMessage(ctx, target, msgType, string(content), "")
	}
	input := &channeltypes.SendInput{ChatID: msg.ChatID}
	if msgType == "image" {
		input.ImageKey = key
	} else {
		input.FileKey = key
	}
	result, err := w.Channel.Send(ctx, input)
	if err != nil {
		return "", err
	}
	return result.MessageID, nil
}

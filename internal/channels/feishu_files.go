package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	channelAttachmentLimit = 10
	channelAttachmentBytes = 20 << 20
	channelArtifactLimit   = 10
	channelArtifactBytes   = 30 << 20
)

func (w *channelWorker) messageContent(ctx context.Context, apiKey, sessionID string, msg *channeltypes.NormalizedMessage, text string) ([]map[string]any, []string, map[string]struct{}, error) {
	content := make([]map[string]any, 0, 2)
	if text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	skipped := make([]string, 0)
	mounted := make(map[string]struct{})
	hints := make([]string, 0)
	existing := w.existingFileMounts(ctx, apiKey, sessionID)
	for index, resource := range msg.Resources {
		if index >= channelAttachmentLimit {
			return nil, nil, nil, fmt.Errorf("一次最多发送 %d 个附件", channelAttachmentLimit)
		}
		name := attachmentName(resource)
		data, mediaType, err := w.downloadAttachment(ctx, resource)
		if err != nil {
			return nil, nil, nil, err
		}
		if data == nil {
			skipped = append(skipped, name)
			continue
		}
		file, err := w.supervisor.oma.uploadFile(ctx, apiKey, name, mediaType, data)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("上传附件 %s 失败: %w", name, err)
		}
		mountPath := existing[file.ID]
		if mountPath == "" {
			attached, err := w.supervisor.oma.attachFileResource(ctx, apiKey, sessionID, file.ID)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("挂载附件 %s 失败: %w", name, err)
			}
			mountPath = attached.MountPath
		}
		mounted[file.ID] = struct{}{}
		if mountPath == "" {
			mountPath = file.ID
		}
		hints = append(hints, "📎 "+name+" → `"+mountPath+"`")
	}
	if len(hints) > 0 {
		content = append(content, map[string]any{"type": "text", "text": strings.Join(hints, "\n")})
	}
	return content, skipped, mounted, nil
}

func attachmentName(resource channeltypes.Resource) string {
	if name := strings.TrimSpace(resource.FileName); name != "" {
		return name
	}
	if resource.Type == "image" {
		return "image-" + resource.FileKey
	}
	return "file-" + resource.FileKey
}

func (w *channelWorker) downloadAttachment(ctx context.Context, resource channeltypes.Resource) ([]byte, string, error) {
	if resource.FileKey == "" {
		return nil, "", nil
	}
	mediaType := resource.Type
	if mediaType != "image" {
		mediaType = "file"
	}
	data, err := w.channel.DownloadFile(ctx, resource.FileKey, mediaType)
	if err != nil {
		return nil, "", fmt.Errorf("下载附件失败: %w", err)
	}
	if len(data) == 0 {
		return nil, "", nil
	}
	if len(data) > channelAttachmentBytes {
		return nil, "", fmt.Errorf("附件超过 %d MB", channelAttachmentBytes>>20)
	}
	detected := http.DetectContentType(data)
	switch {
	case strings.HasPrefix(detected, "image/"), detected == "application/pdf", strings.HasPrefix(detected, "text/"):
		return data, detected, nil
	default:
		return nil, "", nil
	}
}

func (w *channelWorker) existingFileMounts(ctx context.Context, apiKey, sessionID string) map[string]string {
	mounts := make(map[string]string)
	resources, err := w.supervisor.oma.listResources(ctx, apiKey, sessionID)
	if err != nil {
		return mounts
	}
	for _, resource := range resources {
		if resource.Type == "file" && resource.FileID != "" {
			mounts[resource.FileID] = resource.MountPath
		}
	}
	return mounts
}

type artifactBaseline map[string]struct{}

func (w *channelWorker) resourceBaseline(ctx context.Context, apiKey, sessionID string) artifactBaseline {
	baseline := make(artifactBaseline)
	resources, err := w.supervisor.oma.listResources(ctx, apiKey, sessionID)
	if err != nil {
		return baseline
	}
	for _, resource := range resources {
		if resource.ID != "" {
			baseline[resource.ID] = struct{}{}
		}
	}
	return baseline
}

func (w *channelWorker) sendArtifacts(ctx context.Context, task *channelReplyTask, msg *channeltypes.NormalizedMessage, apiKey string, baseline artifactBaseline) {
	resources, err := w.supervisor.oma.listResources(ctx, apiKey, task.sessionID)
	if err != nil {
		return
	}
	sent := 0
	for _, resource := range resources {
		if resource.Type != "file" || resource.FileID == "" {
			continue
		}
		if _, seen := baseline[resource.ID]; seen {
			continue
		}
		if _, inbound := task.inbound[resource.FileID]; inbound {
			continue
		}
		if w.artifactSent(task.sessionID, resource.ID) {
			continue
		}
		if sent >= channelArtifactLimit {
			w.notify(ctx, msg, fmt.Sprintf("本轮产物超过 %d 个，剩余文件请在 OMA 会话中查看。", channelArtifactLimit))
			return
		}
		data, contentType, err := w.supervisor.oma.downloadFile(ctx, apiKey, resource.FileID)
		if err != nil || len(data) == 0 {
			w.supervisor.logger.WarnContext(ctx, "download channel artifact", "channel_id", w.record.ExternalID, "file_id", resource.FileID, "error", err)
			w.notify(ctx, msg, "有产物下载失败，请在 OMA 会话中查看。")
			continue
		}
		if resource.Filename == "" || resource.MimeType == "" {
			if meta, metaErr := w.supervisor.oma.fileMetadata(ctx, apiKey, resource.FileID); metaErr == nil {
				resource.Filename = meta.Filename
				resource.MimeType = meta.MimeType
			}
		}
		if len(data) > channelArtifactBytes {
			w.notify(ctx, msg, fmt.Sprintf("产物 %s 超过 %d MB，请在 OMA 会话中查看。", resource.Filename, channelArtifactBytes>>20))
			continue
		}
		if err := w.sendMedia(ctx, msg, resource, contentType, data); err != nil {
			w.supervisor.logger.WarnContext(ctx, "send channel artifact", "channel_id", w.record.ExternalID, "file_id", resource.FileID, "error", err)
			w.notify(ctx, msg, fmt.Sprintf("产物 %s 发送失败，请在 OMA 会话中查看。", resource.Filename))
			continue
		}
		w.markArtifact(task.sessionID, resource.ID)
		sent++
	}
}

func (w *channelWorker) artifactSent(sessionID, resourceID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.artifacts[sessionID+":"+resourceID]
	return ok
}

func (w *channelWorker) markArtifact(sessionID, resourceID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.artifacts[sessionID+":"+resourceID] = struct{}{}
}

func (w *channelWorker) sendMedia(ctx context.Context, msg *channeltypes.NormalizedMessage, resource omaResource, contentType string, data []byte) error {
	name := strings.TrimSpace(resource.Filename)
	if name == "" {
		name = resource.FileID
	}
	image := strings.HasPrefix(contentType, "image/") || strings.HasPrefix(resource.MimeType, "image/")
	if image {
		key, err := w.uploadImage(ctx, data)
		if err != nil {
			return err
		}
		return w.sendMediaKey(ctx, msg, "image", "image_key", key)
	}
	key, err := w.uploadFile(ctx, name, data)
	if err != nil {
		return err
	}
	return w.sendMediaKey(ctx, msg, "file", "file_key", key)
}

func (w *channelWorker) uploadImage(ctx context.Context, data []byte) (string, error) {
	response, err := w.client.Im.V1.Image.Create(ctx, larkim.NewCreateImageReqBuilder().
		Body(larkim.NewCreateImageReqBodyBuilder().ImageType("message").Image(bytes.NewReader(data)).Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !response.Success() || response.Data == nil || response.Data.ImageKey == nil {
		return "", fmt.Errorf("飞书图片上传失败: %d %s", response.Code, response.Msg)
	}
	return *response.Data.ImageKey, nil
}

func (w *channelWorker) uploadFile(ctx context.Context, name string, data []byte) (string, error) {
	response, err := w.client.Im.V1.File.Create(ctx, larkim.NewCreateFileReqBuilder().
		Body(larkim.NewCreateFileReqBodyBuilder().FileType("stream").FileName(name).File(bytes.NewReader(data)).Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !response.Success() || response.Data == nil || response.Data.FileKey == nil {
		return "", fmt.Errorf("飞书文件上传失败: %d %s", response.Code, response.Msg)
	}
	return *response.Data.FileKey, nil
}

func (w *channelWorker) sendMediaKey(ctx context.Context, msg *channeltypes.NormalizedMessage, msgType, keyField, key string) error {
	content, err := json.Marshal(map[string]string{keyField: key})
	if err != nil {
		return err
	}
	if w.record.ReplyInThread && msg.MessageID != "" {
		_, err := w.replyMessage(ctx, msg.MessageID, msgType, string(content))
		return err
	}
	input := &channeltypes.SendInput{ChatID: msg.ChatID}
	if msgType == "image" {
		input.ImageKey = key
	} else {
		input.FileKey = key
	}
	_, err = w.channel.Send(ctx, input)
	return err
}

func (w *channelWorker) notify(ctx context.Context, msg *channeltypes.NormalizedMessage, text string) {
	if err := w.replyText(ctx, msg, text); err != nil {
		w.supervisor.logger.DebugContext(ctx, "send channel notice", "channel_id", w.record.ExternalID, "error", err)
	}
}

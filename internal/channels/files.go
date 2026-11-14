package channels

import (
	"context"
	"encoding/base64"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
)

func (w *channelWorker) messageContent(ctx context.Context, apiKey, sessionID string, msg *channelMessage, text string, prepared *channelPreparedInput) ([]map[string]any, map[string]struct{}, error) {
	switch msg.RawContentType {
	case "image", "file":
		text = ""
	case "post":
		for _, resource := range msg.Resources {
			switch resource.Type {
			case "image":
				text = strings.ReplaceAll(text, "![image]("+resource.FileKey+")", "")
			case "file":
				text = strings.ReplaceAll(text, `<file key="`+resource.FileKey+`"/>`, "")
			}
		}
		text = strings.TrimSpace(text)
	}
	content := make([]map[string]any, 0, 2)
	if text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	attachments, err := w.downloadMessageAttachments(ctx, msg)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		for _, attachment := range attachments {
			attachment.close()
		}
	}()
	mounted := make(map[string]struct{})
	paths := make([]string, 0)
	existing := map[string]omaResource{}
	if len(msg.Resources) > 0 {
		var err error
		existing, err = w.existingFileMounts(ctx, apiKey, sessionID)
		if err != nil {
			return nil, nil, err
		}
	}
	encodedText, _ := json.Marshal(text)
	imageBudget := (4 << 20) - len(encodedText) - (64 << 10)
	if prepared == nil {
		prepared = &channelPreparedInput{Preparing: true}
	}
	if prepared.Attachments == nil {
		prepared.Attachments = make([]channelAttachmentReceipt, len(attachments))
	}
	save := func() error { return w.savePreparedInput(ctx, msg, sessionID, *prepared) }
	for i, attachment := range attachments {
		name, mediaType := attachment.name, attachment.mediaType
		encodedSize := base64.StdEncoding.EncodedLen(int(attachment.size))
		if attachment.kind == "image" && encodedSize <= imageBudget {
			data, err := io.ReadAll(attachment.file)
			if err != nil {
				return nil, nil, err
			}
			content = append(content, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type": "base64", "media_type": mediaType, "data": base64.StdEncoding.EncodeToString(data),
				},
			})
			imageBudget -= encodedSize
			continue
		}
		resource, err := w.prepareAttachment(ctx, apiKey, sessionID, attachment, &prepared.Attachments[i], existing, save)
		if err != nil {
			return nil, nil, err
		}
		mounted[resource.FileID] = struct{}{}
		paths = append(paths, fmt.Sprintf("%q (%s)：%s", name, mediaType, resource.MountPath))
	}
	if len(paths) > 0 {
		content = append(content, map[string]any{"type": "text", "text": "本轮附件（沙箱路径）：\n" + strings.Join(paths, "\n")})
	}
	return content, mounted, nil
}

type channelDownloadedAttachment struct {
	kind      string
	name      string
	file      *os.File
	size      int64
	mediaType string
}

func (a channelDownloadedAttachment) close() { _ = a.file.Close(); _ = os.Remove(a.file.Name()) }

func (w *channelWorker) downloadMessageAttachments(ctx context.Context, msg *channelMessage) (attachments []channelDownloadedAttachment, resultErr error) {
	defer func() {
		if resultErr != nil {
			for _, attachment := range attachments {
				attachment.close()
			}
			attachments = nil
		}
	}()
	for _, resource := range msg.Resources {
		if resource.Type != "file" && resource.Type != "image" {
			return attachments, fmt.Errorf("附件 %s 的类型暂不支持，本轮未提交", attachmentName(resource))
		}
	}
	for _, resource := range msg.Resources {
		file, err := os.CreateTemp("", "oma-channel-attachment-*")
		if err != nil {
			return attachments, err
		}
		attachment := channelDownloadedAttachment{kind: resource.Type, name: attachmentName(resource), file: file}
		attachments = append(attachments, attachment)
		mediaType, err := w.adapter.downloadAttachment(ctx, msg.MessageID, resource, file)
		if err != nil {
			return attachments, fmt.Errorf("下载附件 %s 失败: %w", attachment.name, err)
		}
		size, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return attachments, err
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return attachments, err
		}
		attachments[len(attachments)-1].size, attachments[len(attachments)-1].mediaType = size, mediaType
	}
	return attachments, nil
}

func attachmentName(resource channelAttachment) string {
	if name := strings.TrimSpace(resource.FileName); name != "" {
		return name
	}
	if resource.Type == "image" {
		return "image-" + resource.FileKey
	}
	return "file-" + resource.FileKey
}

func (w *channelWorker) existingFileMounts(ctx context.Context, apiKey, sessionID string) (map[string]omaResource, error) {
	mounts := make(map[string]omaResource)
	resources, err := w.supervisor.oma.listResources(ctx, apiKey, sessionID)
	if err != nil {
		return nil, fmt.Errorf("读取会话附件失败: %w", err)
	}
	for _, resource := range resources {
		if resource.Type == "file" && resource.FileID != "" && strings.HasPrefix(resource.MountPath, sandboxmount.SandboxUploadsMount+"/") {
			mounts[resource.FileID] = resource
		}
	}
	return mounts, nil
}

type artifactBaseline map[string]string

func artifactVersion(resource omaResource) string {
	return resource.FileID + ":" + resource.UpdatedAt
}

func (w *channelWorker) resourceBaseline(ctx context.Context, apiKey, sessionID string) (artifactBaseline, error) {
	resources, err := w.supervisor.oma.listResources(ctx, apiKey, sessionID)
	if err != nil {
		return nil, err
	}
	return artifactSnapshot(resources), nil
}

func artifactSnapshot(resources []omaResource) artifactBaseline {
	baseline := make(artifactBaseline)
	for _, resource := range resources {
		if resource.Type == "file" && resource.ID != "" {
			baseline[resource.ID] = artifactVersion(resource)
		}
	}
	return baseline
}

func (w *channelWorker) stableArtifactResources(ctx context.Context, apiKey, sessionID string, interval, timeout time.Duration) ([]omaResource, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var previous artifactBaseline
	unchanged := 0
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		resources, err := w.supervisor.oma.listResources(ctx, apiKey, sessionID)
		if err != nil {
			return nil, err
		}
		next := artifactSnapshot(resources)
		if previous != nil && maps.Equal(previous, next) {
			unchanged++
		} else {
			unchanged = 0
		}
		if unchanged >= 2 {
			return resources, nil
		}
		previous = next
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *channelWorker) sendArtifacts(ctx context.Context, task *channelReplyTask, msg *channelMessage, apiKey string) {
	allowed, err := w.cardAuthorized(ctx, channelActionValue{ChatType: msg.ChatType}, msg.UserID, msg.ChatID)
	if err != nil || !allowed {
		return
	}
	ctx, finish := w.keepArtifactLease(ctx, task)
	defer finish()
	if task.delivery == nil || task.delivery.ArtifactDelivery == "" {
		return
	}
	var state channelArtifactDelivery
	if err := jsonv2.Unmarshal([]byte(task.delivery.ArtifactDelivery), &state); err != nil {
		w.supervisor.logger.WarnContext(ctx, "decode channel artifact delivery", "channel_id", w.record.ExternalID, "error", err)
		return
	}
	if state.Baseline == nil {
		return
	}
	if !state.Planned {
		if err := w.planArtifactDelivery(ctx, task, apiKey, &state); err != nil {
			w.supervisor.logger.WarnContext(ctx, "plan channel artifact delivery", "channel_id", w.record.ExternalID, "error", err)
			return
		}
	}
	if err := w.deliverArtifactPlan(ctx, task, msg, apiKey, &state); err != nil {
		w.supervisor.logger.WarnContext(ctx, "deliver channel artifacts", "channel_id", w.record.ExternalID, "error", err)
	}
}

func (w *channelWorker) notify(ctx context.Context, msg *channelMessage, text string) {
	if err := w.replyText(ctx, msg, text); err != nil {
		w.supervisor.logger.DebugContext(ctx, "send channel notice", "channel_id", w.record.ExternalID, "error", err)
	}
}

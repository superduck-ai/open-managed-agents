package channels

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/messagemedia"
	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
)

type channelAttachmentReceipt struct {
	Uploading bool        `json:"uploading,omitzero"`
	Mounting  bool        `json:"mounting,omitzero"`
	File      omaFile     `json:"file"`
	Resource  omaResource `json:"resource"`
}

func (w *channelWorker) prepareAttachment(ctx context.Context, apiKey, sessionID, messageID string, index int, attachment channelDownloadedAttachment, receipt *channelAttachmentReceipt, save func() error) (omaResource, error) {
	if receipt.Resource.FileID != "" {
		return receipt.Resource, nil
	}
	if receipt.Uploading || receipt.Mounting {
		return omaResource{}, fmt.Errorf("附件 %s 的旧上传或挂载结果尚未确认，本轮未提交", attachment.name)
	}
	key := sha256.Sum256([]byte(w.record.ExternalID + "\x00" + messageID))
	attachmentPath := path.Join(messagemedia.AttachmentPath(fmt.Sprintf("%x", key), index, messagemedia.BlockDocument, attachment.mediaType), path.Base(strings.ReplaceAll(attachment.name, "\\", "/")))
	file, err := w.supervisor.putAttachment(ctx, apiKey, sessionID, attachmentPath, attachment.mediaType, attachment.file)
	if err != nil {
		return omaResource{}, fmt.Errorf("保存附件 %s 失败: %w", attachment.name, err)
	}
	receipt.Resource = omaResource{Type: "file", FileID: file.ExternalID, MountPath: sandboxmount.SandboxPath(attachmentPath)}
	if err := save(); err != nil {
		return omaResource{}, err
	}
	return receipt.Resource, nil
}

package channels

import (
	"context"
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
)

type channelAttachmentReceipt struct {
	Uploading bool        `json:"uploading,omitzero"`
	Mounting  bool        `json:"mounting,omitzero"`
	File      omaFile     `json:"file"`
	Resource  omaResource `json:"resource"`
}

func (w *channelWorker) prepareAttachment(ctx context.Context, apiKey, sessionID string, attachment channelDownloadedAttachment, receipt *channelAttachmentReceipt, existing map[string]omaResource, save func() error) (omaResource, error) {
	if receipt.Resource.FileID != "" {
		return receipt.Resource, nil
	}
	if receipt.File.ID == "" {
		if receipt.Uploading {
			return omaResource{}, fmt.Errorf("附件 %s 上传结果未确认，本轮未提交，禁止重复上传", attachment.name)
		}
		receipt.Uploading = true
		if err := save(); err != nil {
			return omaResource{}, err
		}
		file, err := w.supervisor.oma.uploadFile(ctx, apiKey, attachment.name, attachment.mediaType, attachment.file)
		if err != nil {
			if errors.Is(err, errOMARequestRejected) {
				receipt.Uploading = false
				if saveErr := save(); saveErr != nil {
					return omaResource{}, saveErr
				}
			}
			return omaResource{}, fmt.Errorf("上传附件 %s 失败: %w", attachment.name, err)
		}
		receipt.File, receipt.Uploading = file, false
		if err := save(); err != nil {
			return omaResource{}, err
		}
	}
	file := receipt.File
	resource, found := existing[file.ID]
	if !found {
		mountPath := sandboxmount.SandboxPath("/uploads/" + file.ID + "/" + file.Filename)
		for _, candidate := range existing {
			if candidate.MountPath == mountPath {
				resource, found = candidate, true
				break
			}
		}
	}
	if !found {
		if receipt.Mounting {
			return omaResource{}, fmt.Errorf("附件 %s 挂载结果未确认，本轮未提交", attachment.name)
		}
		receipt.Mounting = true
		if err := save(); err != nil {
			return omaResource{}, err
		}
		var err error
		resource, err = w.supervisor.oma.attachFileResource(ctx, apiKey, sessionID, file.ID, file.Filename)
		if err != nil {
			if errors.Is(err, errOMARequestRejected) {
				receipt.Mounting = false
				if saveErr := save(); saveErr != nil {
					return omaResource{}, saveErr
				}
			}
			return omaResource{}, fmt.Errorf("挂载附件 %s 失败: %w", attachment.name, err)
		}
	}
	if resource.FileID == "" || resource.MountPath == "" {
		return omaResource{}, fmt.Errorf("附件 %s 的挂载回执不完整，本轮未提交", attachment.name)
	}
	receipt.Resource, receipt.Mounting = resource, false
	if err := save(); err != nil {
		return omaResource{}, err
	}
	return resource, nil
}

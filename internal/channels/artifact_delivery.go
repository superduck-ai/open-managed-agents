package channels

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
)

type channelArtifactDelivery struct {
	Baseline artifactBaseline      `json:"baseline"`
	Planned  bool                  `json:"planned,omitzero"`
	Files    []channelArtifactFile `json:"files"`
}

type channelArtifactFile struct {
	Resource  omaResource `json:"resource"`
	Metadata  *omaFile    `json:"metadata,omitzero"`
	State     string      `json:"state"`
	MessageID string      `json:"message_id,omitempty"`
	LastError string      `json:"last_error,omitempty"`
	RetryAt   time.Time   `json:"retry_at,omitzero"`
}

func (w *channelWorker) keepArtifactLease(ctx context.Context, task *channelReplyTask) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := w.supervisor.store.RenewChannelReplyDelivery(ctx, w.deliveryScope(), task.inputEventID, w.deliveryOwner); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() { cancel(); <-done }
}

func (w *channelWorker) captureArtifactBaseline(ctx context.Context, msg *channelMessage, sessionID string, current artifactBaseline) (artifactBaseline, error) {
	if msg.Input == nil {
		return current, nil
	}
	if msg.Input.PreparedContent != "" {
		var prepared channelPreparedInput
		if err := json.Unmarshal([]byte(msg.Input.PreparedContent), &prepared); err != nil {
			return nil, err
		}
		return prepared.ArtifactBaseline, nil
	}
	err := w.savePreparedInput(ctx, msg, sessionID, channelPreparedInput{Preparing: true, ArtifactBaseline: current})
	return current, err
}

func (w *channelWorker) saveArtifactDelivery(ctx context.Context, task *channelReplyTask, state *channelArtifactDelivery) error {
	payload, err := json.Marshal(state, json.FormatNilMapAsNull(true))
	if err != nil {
		return err
	}
	if err := w.supervisor.store.SaveChannelArtifactDelivery(ctx, w.deliveryScope(), task.inputEventID, w.deliveryOwner, string(payload)); err != nil {
		return err
	}
	task.delivery.ArtifactDelivery = string(payload)
	task.deliveryRenewedAt = time.Now()
	return nil
}

func (w *channelWorker) planArtifactDelivery(ctx context.Context, task *channelReplyTask, apiKey string, state *channelArtifactDelivery) error {
	var resources []omaResource
	var err error
	if task.usedTools {
		resources, err = w.stableArtifactResources(ctx, apiKey, task.sessionID, 2*time.Second, time.Minute)
	} else {
		resources, err = w.supervisor.oma.listResources(ctx, apiKey, task.sessionID)
	}
	if err != nil {
		return err
	}
	state.Files = make([]channelArtifactFile, 0)
	for _, resource := range resources {
		if resource.Type != "file" || resource.FileID == "" || !strings.HasPrefix(resource.MountPath, sandboxmount.SandboxOutputsMount+"/") {
			continue
		}
		if previous, seen := state.Baseline[resource.ID]; seen && previous == artifactVersion(resource) {
			continue
		}
		if _, inbound := task.inbound[resource.FileID]; inbound {
			continue
		}
		file := channelArtifactFile{Resource: resource, State: "pending"}
		state.Files = append(state.Files, file)
	}
	state.Planned = true
	return w.saveArtifactDelivery(ctx, task, state)
}

func (w *channelWorker) artifactFailure(ctx context.Context, task *channelReplyTask, msg *channelMessage, state *channelArtifactDelivery, index int, next, reason string) error {
	file := &state.Files[index]
	notify := file.LastError != reason
	file.State, file.LastError = next, reason
	if next == "pending" {
		file.RetryAt = time.Now().Add(time.Minute)
	}
	if err := w.saveArtifactDelivery(ctx, task, state); err != nil {
		return err
	}
	if notify {
		w.notify(ctx, msg, fmt.Sprintf("产物 %s：%s；可在 OMA 会话中查看。", file.Resource.Filename, reason))
	}
	return nil
}

func (w *channelWorker) deliverArtifactFile(ctx context.Context, task *channelReplyTask, msg *channelMessage, apiKey string, state *channelArtifactDelivery, index int) error {
	file := &state.Files[index]
	if file.State == "sent" || file.State == "unknown" || file.State == "skipped" {
		return nil
	}
	if file.State == "sending" {
		return w.artifactFailure(ctx, task, msg, state, index, "unknown", "发送结果未确认，未自动重发")
	}
	if time.Now().Before(file.RetryAt) {
		return nil
	}
	data, contentType, err := w.supervisor.oma.downloadFile(ctx, apiKey, file.Resource.FileID, file.Metadata.Size)
	if err != nil {
		return w.artifactFailure(ctx, task, msg, state, index, "pending", "下载失败，等待重试")
	}
	if int64(len(data)) != file.Metadata.Size {
		return w.artifactFailure(ctx, task, msg, state, index, "pending", "下载长度与元数据不符，等待重试")
	}
	file.State = "sending"
	if err := w.saveArtifactDelivery(ctx, task, state); err != nil {
		return err
	}
	receipt, err := w.adapter.SendMedia(ctx, msg, file.Resource, contentType, data)
	if err != nil {
		if errors.Is(err, errChannelMediaUploadRejected) {
			return w.artifactFailure(ctx, task, msg, state, index, "pending", "平台上传被拒绝，等待重试")
		}
		return w.artifactFailure(ctx, task, msg, state, index, "unknown", "发送结果未确认，未自动重发")
	}
	if receipt == "" {
		return w.artifactFailure(ctx, task, msg, state, index, "unknown", "发送回执缺少消息标识，未自动重发")
	}
	file.State, file.MessageID, file.LastError, file.RetryAt = "sent", receipt, "", time.Time{}
	if err := w.saveArtifactDelivery(ctx, task, state); err != nil {
		return err
	}
	return nil
}

func (w *channelWorker) deliverArtifactPlan(ctx context.Context, task *channelReplyTask, msg *channelMessage, apiKey string, state *channelArtifactDelivery) error {
	for index := range state.Files {
		file := &state.Files[index]
		if file.State == "skipped" {
			continue
		}
		if file.Metadata == nil {
			metadata, err := w.supervisor.oma.fileMetadata(ctx, apiKey, file.Resource.FileID)
			if err != nil {
				if err := w.artifactFailure(ctx, task, msg, state, index, "pending", "元数据读取失败，等待重试"); err != nil {
					return err
				}
				continue
			}
			file.Metadata = &metadata
			file.Resource.Filename, file.Resource.MimeType = metadata.Filename, metadata.MimeType
			if err := w.saveArtifactDelivery(ctx, task, state); err != nil {
				return err
			}
		}
		metadata := file.Metadata
		if metadata.ID != file.Resource.FileID || metadata.Size < 0 {
			if err := w.artifactFailure(ctx, task, msg, state, index, "skipped", "文件元数据无效"); err != nil {
				return err
			}
			continue
		}
		if err := w.deliverArtifactFile(ctx, task, msg, apiKey, state, index); err != nil {
			return err
		}
	}
	for _, file := range state.Files {
		if file.State == "pending" || file.State == "sending" {
			return nil
		}
	}
	return w.supervisor.store.CompleteChannelReplyDelivery(ctx, w.deliveryScope(), task.inputEventID, w.deliveryOwner)
}

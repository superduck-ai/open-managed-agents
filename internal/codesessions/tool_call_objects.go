package codesessions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

const toolCallCleanupTimeout = 5 * time.Second

func validateToolCallResult(reader io.Reader, status string) error {
	var result struct {
		Content []struct {
			Type     string  `json:"type"`
			Text     *string `json:"text"`
			Data     *string `json:"data"`
			MimeType string  `json:"mimeType"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&result); err != nil {
		return err
	}
	if result.Content == nil || (status == "succeeded" && result.IsError) {
		return errToolCallResult
	}
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			if block.Text == nil {
				return errToolCallResult
			}
		case "image":
			if block.Data == nil || *block.Data == "" {
				return errToolCallResult
			}
			switch block.MimeType {
			case "image/png", "image/jpeg", "image/gif", "image/webp":
			default:
				return errToolCallResult
			}
		default:
			return errToolCallResult
		}
	}
	var trailing json.RawMessage
	if decoder.Decode(&trailing) != io.EOF {
		return errToolCallResult
	}
	return nil
}

func (h *Handler) readToolCallResult(r *http.Request, report *db.SandboxToolCallReport) error {
	digest := r.Header.Get("Oma-Result-Sha256")
	size := r.ContentLength
	report.ResultBytes, report.ResultSHA256 = &size, &digest
	if size <= 32<<10 {
		body, err := io.ReadAll(io.LimitReader(r.Body, size+1))
		if err != nil {
			return err
		}
		hash := sha256.Sum256(body)
		if int64(len(body)) != size || hex.EncodeToString(hash[:]) != digest {
			return errToolCallResult
		}
		if err := validateToolCallResult(bytes.NewReader(body), report.Event); err != nil {
			return err
		}
		if !bytes.Contains(body, []byte(`\u0000`)) {
			report.Result = body
			return nil
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	objects := h.service.workerEventObjects
	if objects == nil {
		return errSandboxUnavailable
	}
	blobUUID := uuid.NewString()
	key := "tool-calls/" + report.Scope.OrganizationUUID + "/" + report.Scope.WorkspaceUUID + "/" + blobUUID + ".json"
	blob := db.EventPayloadBlob{UUID: blobUUID, ExternalID: "epb_" + blobUUID, OrganizationUUID: report.Scope.OrganizationUUID, WorkspaceUUID: report.Scope.WorkspaceUUID, Bucket: objects.Name(), Key: key, Size: size, SHA256: digest}
	if err := h.db.RegisterEventPayloadBlob(r.Context(), blob); err != nil {
		return err
	}
	report.ResultBlobUUID, report.ResultObjectKey = &blobUUID, &key
	hash := sha256.New()
	uploaded, err := objects.Upload(r.Context(), key, io.TeeReader(io.LimitReader(r.Body, size+1), hash), storage.UploadOptions{Size: size, ContentType: "application/json"})
	if err != nil {
		return err
	}
	if uploaded.Size != size || hex.EncodeToString(hash.Sum(nil)) != digest {
		return errToolCallResult
	}
	object, err := objects.Open(r.Context(), key, nil)
	if err != nil {
		return err
	}
	defer object.Body.Close()
	return validateToolCallResult(object.Body, report.Event)
}

func (h *Handler) restoreToolCallResult(ctx context.Context, call *db.SandboxToolCall) error {
	if call.ResultObjectKey == nil {
		return nil
	}
	objects := h.service.workerEventObjects
	if objects == nil {
		return errSandboxUnavailable
	}
	object, err := objects.Open(ctx, *call.ResultObjectKey, nil)
	if err != nil {
		return err
	}
	defer object.Body.Close()
	body, err := io.ReadAll(io.LimitReader(object.Body, 8<<20+1))
	if err != nil {
		return err
	}
	hash := sha256.Sum256(body)
	if call.ResultBytes == nil || call.ResultSHA256 == nil || int64(len(body)) != *call.ResultBytes || hex.EncodeToString(hash[:]) != *call.ResultSHA256 {
		return errToolCallResult
	}
	call.Result = body
	return nil
}

func (h *Handler) discardToolCallObject(ctx context.Context, key *string) {
	if key == nil || h.service.workerEventObjects == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), toolCallCleanupTimeout)
	defer cancel()
	if err := h.service.workerEventObjects.Delete(ctx, *key, storage.DeleteOptions{}); err != nil {
		h.logger.WarnContext(ctx, "tool call object cleanup deferred", "error", err)
	}
}

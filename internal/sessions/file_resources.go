package sessions

import (
	"encoding/json"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
)

// fileResourcePayload 是写入 session_resources.payload 的文件资源边界结构。
// 这里保留外部合同字段名，进入业务逻辑后再按需要做校验和映射。
type fileResourcePayload struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	FileID    string `json:"file_id"`
	Source    string `json:"source"`
	MountPath string `json:"mount_path"`
}

// validateSessionResourceMounts 聚合校验当前 Session 的所有 file mount path，
// 统一阻止重复挂载、祖先/子孙路径冲突等会破坏 sandbox 目录语义的输入。
func validateSessionResourceMounts(resources []db.SessionResource) error {
	mounts := make([]string, 0, len(resources))
	for _, resource := range resources {
		if resource.ResourceType != "file" {
			continue
		}
		var payload fileResourcePayload
		if err := json.Unmarshal(resource.Payload, &payload); err != nil {
			return errors.New("file resource payload is invalid")
		}
		if payload.Source != sandboxmount.FileSource {
			return errors.New("file resource source is invalid")
		}
		mounts = append(mounts, payload.MountPath)
	}
	return sandboxmount.ValidateFileMountPaths(mounts)
}

func sessionFileMounts(resources []db.SessionResource) ([]db.SessionFileMount, error) {
	mounts := make([]db.SessionFileMount, 0, len(resources))
	for _, resource := range resources {
		if resource.ResourceType != "file" {
			continue
		}
		var payload fileResourcePayload
		if err := json.Unmarshal(resource.Payload, &payload); err != nil {
			return nil, errors.New("file resource payload is invalid")
		}
		if payload.ID != resource.ExternalID || payload.FileID == "" ||
			payload.Source != sandboxmount.FileSource {
			return nil, errors.New("file resource payload is invalid")
		}
		backingPath, err := sandboxmount.FileBackingPath(payload.MountPath)
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, db.SessionFileMount{
			ResourceExternalID: resource.ExternalID,
			FileExternalID:     payload.FileID,
			Path:               backingPath,
		})
	}
	return mounts, nil
}

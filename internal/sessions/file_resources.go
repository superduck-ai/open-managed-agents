package sessions

import (
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

// validateSessionResourceMounts 聚合校验当前 Session 的所有 file mount path，
// 统一阻止重复挂载、祖先/子孙路径冲突等会破坏 sandbox 目录语义的输入。
func validateSessionResourceMounts(resources []db.SessionResource) error {
	specs := make([]sessionresource.FileSpec, 0, len(resources))
	for _, resource := range resources {
		if resource.ResourceType != sessionresource.FileType {
			continue
		}
		spec, err := sessionresource.ParseFilePayload(resource.Payload, resource.ExternalID)
		if err != nil {
			return err
		}
		specs = append(specs, spec)
	}
	return sessionresource.ValidateFileSpecs(specs)
}

func sessionFileMounts(resources []db.SessionResource) ([]db.SessionFileMount, error) {
	mounts := make([]db.SessionFileMount, 0, len(resources))
	for _, resource := range resources {
		if resource.ResourceType != sessionresource.FileType {
			continue
		}
		spec, err := sessionresource.ParseFilePayload(resource.Payload, resource.ExternalID)
		if err != nil {
			return nil, err
		}
		mount, err := spec.SessionFileMount(resource.ExternalID)
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, mount)
	}
	return mounts, nil
}

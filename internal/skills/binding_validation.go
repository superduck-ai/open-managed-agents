package skills

import (
	"context"
	"encoding/json"
	"strings"
)

func (r *RuntimeResolver) ValidateAgentSnapshotBindings(ctx context.Context, workspaceUUID string, snapshot json.RawMessage) error {
	refs, err := runtimeSkillRefs(snapshot)
	if err != nil {
		return bindingReferenceError(runtimeSkillRef{}, err)
	}
	if len(refs) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return errRuntimeSkillResolverUnavailable
	}
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		ref.Type = strings.TrimSpace(ref.Type)
		ref.SkillID = strings.TrimSpace(ref.SkillID)
		ref.Version = firstNonEmpty(ref.Version, "latest")
		key := ref.Type + "\x00" + ref.SkillID + "\x00" + ref.Version
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, err := r.resolveRef(ctx, workspaceUUID, ref); err != nil {
			return err
		}
	}
	return nil
}

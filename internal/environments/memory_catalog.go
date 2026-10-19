package environments

import (
	"encoding/xml"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

type memoryStoreDefinition struct {
	Name         string `json:"name" xml:"name"`
	MountPath    string `json:"mount_path" xml:"mount_path"`
	Access       string `json:"access" xml:"access"`
	Description  string `json:"description" xml:"description"`
	Instructions string `json:"instructions" xml:"instructions"`
}

func memoryStoreDefinitions(mounts []memoryRuntimeMount) []memoryStoreDefinition {
	stores := make([]memoryStoreDefinition, 0, len(mounts))
	for _, mount := range mounts {
		stores = append(stores, memoryStoreDefinition{
			Name: mount.Name, MountPath: mount.MountPath, Access: string(mount.Access),
			Description: mount.Description, Instructions: mount.Instructions,
		})
	}
	return stores
}

func memoryCatalogPrompt(stores []memoryStoreDefinition) (string, error) {
	if len(stores) == 0 {
		return "", nil
	}
	if len(stores) > sessionresource.MaxMemoryStores {
		return "", errMemorySnapshotInvalid
	}
	seen := make(map[string]bool, len(stores))
	for _, store := range stores {
		slug := sessionresource.MemorySlugFromMountPath(store.MountPath)
		if store.Name == "" || slug == "" || strings.ContainsAny(slug, "/\\") || path.Clean(store.MountPath) != store.MountPath || seen[store.MountPath] || utf8.RuneCountInString(store.Instructions) > sessionresource.MaxMemoryInstructionsRunes {
			return "", errMemorySnapshotInvalid
		}
		if store.Access != string(sessionresource.MemoryAccessReadOnly) && store.Access != string(sessionresource.MemoryAccessReadWrite) {
			return "", errMemorySnapshotInvalid
		}
		seen[store.MountPath] = true
	}
	catalog, err := xml.MarshalIndent(struct {
		XMLName xml.Name                `xml:"attached_memory_stores"`
		Stores  []memoryStoreDefinition `xml:"store"`
	}{Stores: stores}, "", "  ")
	if err != nil {
		return "", errMemorySnapshotInvalid
	}
	return "\n\nUse the attached memory stores according to their descriptions and instructions. Read relevant documents on demand with Read, and use Glob or Grep to find them when needed. Do not load all memory documents. Use Write or Edit to update a suitable read_write store; read_only stores cannot be modified. Files inside attached stores are persistent across Sessions that attach the same store. Files at /mnt/memory/ outside those stores, including MEMORY.md, are sandbox-local and do not persist across sandbox replacement. No default store is selected for you.\n" + string(catalog), nil
}

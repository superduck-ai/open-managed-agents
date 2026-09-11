package environments

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

const (
	memoryMarkdownSandboxPath         = sessionresource.MemoryMountRoot + "/MEMORY.md"
	claudeCodeRemoteMemoryDirEnv      = "CLAUDE_CODE_REMOTE_MEMORY_DIR"
	claudeCoworkMemoryPathOverrideEnv = "CLAUDE_COWORK_MEMORY_PATH_OVERRIDE"
	memoryFilestoreNamespace          = "/memory"
	memoryRcloneCacheSeconds          = 1
)

type memoryRuntimeMount struct {
	Name         string
	Description  string
	Instructions string
	Access       string
	MountPath    string
	Slug         string
}

type memorySnapshotPayload struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	Access       string `json:"access"`
	MountPath    string `json:"mount_path"`
}

func parseMemoryRuntimeMount(payload json.RawMessage) (memoryRuntimeMount, bool) {
	var snapshot memorySnapshotPayload
	if json.Unmarshal(payload, &snapshot) != nil {
		return memoryRuntimeMount{}, false
	}
	slug := sessionresource.MemorySlugFromMountPath(snapshot.MountPath)
	if slug == "" || strings.Contains(slug, "/") {
		return memoryRuntimeMount{}, false
	}
	access, err := sessionresource.NormalizeMemoryAccess(snapshot.Access)
	if err != nil {
		return memoryRuntimeMount{}, false
	}
	return memoryRuntimeMount{
		Name:         snapshot.Name,
		Description:  snapshot.Description,
		Instructions: snapshot.Instructions,
		Access:       access,
		MountPath:    snapshot.MountPath,
		Slug:         slug,
	}, true
}

func renderMemoryMarkdown(mounts []memoryRuntimeMount) string {
	var builder strings.Builder
	// 固定引导先不写入 MEMORY.md；只注入 store 目录段。
	builder.WriteString("<!-- oma-stores -->\n")
	for _, mount := range mounts {
		fmt.Fprintf(
			&builder,
			"- [%s](%s) %s%s\n",
			escapeMemoryMarkdownLinkText(mount.Name),
			mount.MountPath,
			memoryMarkdownAccess(mount.Access),
			memoryMarkdownNarrative(mount.Description, mount.Instructions),
		)
	}
	return builder.String()
}

// memoryMarkdownNarrative renders the trailing " — description。instructions"
// segment. Both fields are optional, so each separator is dropped when the text
// around it is empty; otherwise a store without a description would render as
// "rw — 。instructions".
func memoryMarkdownNarrative(description, instructions string) string {
	description = flattenMemoryMarkdownField(description)
	instructions = flattenMemoryMarkdownField(instructions)
	switch {
	case description == "" && instructions == "":
		return ""
	case description == "":
		return " — " + instructions
	case instructions == "":
		return " — " + description + "。"
	default:
		return " — " + description + "。" + instructions
	}
}

// flattenMemoryMarkdownField collapses every line break so that one store stays
// on one line. Only the store name is validated against control characters, so
// descriptions and instructions can legitimately carry newlines.
func flattenMemoryMarkdownField(value string) string {
	for _, lineBreak := range []string{"\r\n", "\n", "\r", "\u2028", "\u2029"} {
		value = strings.ReplaceAll(value, lineBreak, " ")
	}
	return value
}

// escapeMemoryMarkdownLinkText keeps a store name inside its markdown link
// label. Names allow any non-control character, so an unescaped "]" would let a
// name close the label early and point the agent at a forged mount path.
func escapeMemoryMarkdownLinkText(value string) string {
	value = flattenMemoryMarkdownField(value)
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "[", `\[`)
	value = strings.ReplaceAll(value, "]", `\]`)
	return value
}

func memoryMarkdownAccess(access string) string {
	if access == sessionresource.MemoryAccessReadOnly {
		return "ro"
	}
	return "rw"
}

func memoryRootMkdirCommand() string {
	return "mkdir -p " + shellQuote(sessionresource.MemoryMountRoot)
}

func memoryFilestoreSource(slug string) string {
	return memoryFilestoreNamespace + "/" + slug
}

func memorySessionEnvironment(mounts []memoryRuntimeMount) map[string]string {
	if len(mounts) == 0 {
		return nil
	}
	return map[string]string{
		claudeCodeRemoteMemoryDirEnv:      sessionresource.MemoryMountRoot,
		claudeCoworkMemoryPathOverrideEnv: sessionresource.MemoryMountRoot,
	}
}

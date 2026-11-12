package skills

import (
	"slices"
	"strings"
)

var skillIconKeys = []string{"blocks", "box", "sparkles", "bot", "wrench", "workflow", "file-text", "file-type-2", "book-open", "folder", "pen-line", "search", "image", "wand-sparkles", "scan-eye", "clapperboard", "film", "mic", "music-2", "chart-no-axes-combined", "landmark", "code-2", "git-fork", "git-branch"}

func skillIconKey(value string) (string, error) {
	key := strings.TrimSpace(value)
	if key == "" {
		return "blocks", nil
	}
	if !slices.Contains(skillIconKeys, key) {
		return "", invalidSkillIcon()
	}
	return key, nil
}

func requireSkillIconKey(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", invalidSkillIcon()
	}
	return skillIconKey(value)
}

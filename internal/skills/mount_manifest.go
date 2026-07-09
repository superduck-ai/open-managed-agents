package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const MountManifestVersion = 1

type MountManifest struct {
	Version int                  `json:"version"`
	Skills  []MountManifestSkill `json:"skills"`
}

type MountManifestSkill struct {
	Source      string `json:"source"`
	SkillID     string `json:"skill_id"`
	Version     string `json:"version"`
	Directory   string `json:"directory"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Filename    string `json:"filename"`
	SHA256      string `json:"sha256"`
	SizeBytes   int64  `json:"size_bytes"`
}

func BuildMountManifest(runtimeSkills []RuntimeSkill) (MountManifest, []byte, string, error) {
	ordered := append([]RuntimeSkill(nil), runtimeSkills...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left := ordered[i]
		right := ordered[j]
		return strings.Join([]string{left.Source, left.SkillID, left.Version, left.Directory}, "\x00") <
			strings.Join([]string{right.Source, right.SkillID, right.Version, right.Directory}, "\x00")
	})

	manifest := MountManifest{
		Version: MountManifestVersion,
		Skills:  make([]MountManifestSkill, 0, len(ordered)),
	}
	filenames := map[string]struct{}{}
	for _, skill := range ordered {
		entry, err := MountManifestEntry(skill)
		if err != nil {
			return MountManifest{}, nil, "", err
		}
		if _, ok := filenames[entry.Filename]; ok {
			return MountManifest{}, nil, "", fmt.Errorf("duplicate skill mount filename %q", entry.Filename)
		}
		filenames[entry.Filename] = struct{}{}
		manifest.Skills = append(manifest.Skills, entry)
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return MountManifest{}, nil, "", err
	}
	sum := sha256.Sum256(data)
	return manifest, data, hex.EncodeToString(sum[:]), nil
}

func MountManifestEntry(skill RuntimeSkill) (MountManifestSkill, error) {
	sha := strings.TrimSpace(skill.SHA256)
	if len(skill.Archive) > 0 {
		got := sha256Hex(skill.Archive)
		if sha == "" {
			sha = got
		} else if got != sha {
			return MountManifestSkill{}, fmt.Errorf("skill archive checksum mismatch for %s/%s@%s", skill.Source, skill.SkillID, skill.Version)
		}
	}
	if sha == "" {
		return MountManifestSkill{}, fmt.Errorf("skill checksum is required for %s/%s@%s", skill.Source, skill.SkillID, skill.Version)
	}
	sizeBytes := skill.SizeBytes
	if sizeBytes == 0 && len(skill.Archive) > 0 {
		sizeBytes = int64(len(skill.Archive))
	}
	return MountManifestSkill{
		Source:      strings.TrimSpace(skill.Source),
		SkillID:     strings.TrimSpace(skill.SkillID),
		Version:     strings.TrimSpace(skill.Version),
		Directory:   strings.TrimSpace(skill.Directory),
		Name:        strings.TrimSpace(skill.Name),
		Description: strings.TrimSpace(skill.Description),
		Filename:    MountArchiveFilename(skill),
		SHA256:      sha,
		SizeBytes:   sizeBytes,
	}, nil
}

func MountArchiveFilename(skill RuntimeSkill) string {
	sha := strings.TrimSpace(skill.SHA256)
	if sha == "" && len(skill.Archive) > 0 {
		sha = sha256Hex(skill.Archive)
	}
	if len(sha) > 12 {
		sha = sha[:12]
	}
	if sha == "" {
		sha = "unknown"
	}
	return strings.Join([]string{
		safeMountFilenamePart(skill.Source),
		safeMountFilenamePart(skill.SkillID),
		safeMountFilenamePart(firstNonEmpty(skill.Version, "latest")),
		safeMountFilenamePart(sha),
	}, "__") + ".zip"
}

func safeMountFilenamePart(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	out := strings.Trim(builder.String(), "._-")
	if out == "" {
		return "unknown"
	}
	if len(out) > 80 {
		return out[:80]
	}
	return out
}

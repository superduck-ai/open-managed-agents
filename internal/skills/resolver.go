package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type RuntimeResolver struct {
	db      *db.DB
	store   storage.ObjectStore
	catalog *builtinCatalog
}

type RuntimeSkill struct {
	Source           string
	SkillID          string
	RequestedVersion string
	Version          string
	Directory        string
	Name             string
	Description      string
	SHA256           string
	Archive          []byte
	SizeBytes        int64
	archiveLoader    func(context.Context) ([]byte, error)
}

func (s RuntimeSkill) LoadArchive(ctx context.Context) ([]byte, error) {
	if len(s.Archive) > 0 {
		return append([]byte(nil), s.Archive...), nil
	}
	if s.archiveLoader == nil {
		return nil, fmt.Errorf("skill archive loader is unavailable for %s/%s@%s", s.Source, s.SkillID, s.Version)
	}
	data, err := s.archiveLoader(ctx)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), data...), nil
}

type runtimeSkillRef struct {
	Type    string `json:"type"`
	SkillID string `json:"skill_id"`
	Version string `json:"version"`
}

func NewRuntimeResolver(cfg config.Config, database *db.DB, store storage.ObjectStore) *RuntimeResolver {
	return &RuntimeResolver{
		db:      database,
		store:   store,
		catalog: loadBuiltinCatalog(cfg),
	}
}

func (r *RuntimeResolver) ResolveAgentSnapshot(ctx context.Context, workspaceID int64, snapshot json.RawMessage) ([]RuntimeSkill, error) {
	if r == nil {
		return nil, nil
	}
	refs, err := runtimeSkillRefs(snapshot)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, nil
	}

	resolved := make([]RuntimeSkill, 0, len(refs))
	seenRefs := map[string]struct{}{}
	seenResolved := map[string]struct{}{}
	dirs := map[string]string{}
	for _, ref := range refs {
		ref.Type = strings.TrimSpace(ref.Type)
		ref.SkillID = strings.TrimSpace(ref.SkillID)
		ref.Version = firstNonEmpty(ref.Version, "latest")
		refKey := ref.Type + "\x00" + ref.SkillID + "\x00" + ref.Version
		if _, ok := seenRefs[refKey]; ok {
			continue
		}
		seenRefs[refKey] = struct{}{}

		skill, err := r.resolveRef(ctx, workspaceID, ref)
		if err != nil {
			return nil, err
		}
		resolvedKey := skill.Source + "\x00" + skill.SkillID + "\x00" + skill.Version
		if _, ok := seenResolved[resolvedKey]; ok {
			continue
		}
		dirKey := strings.TrimSpace(skill.Directory)
		if previous, ok := dirs[dirKey]; ok && previous != resolvedKey {
			return nil, fmt.Errorf("skill install directory %q is used by multiple skills", dirKey)
		}
		dirs[dirKey] = resolvedKey
		seenResolved[resolvedKey] = struct{}{}
		resolved = append(resolved, skill)
	}
	return resolved, nil
}

func runtimeSkillRefs(snapshot json.RawMessage) ([]runtimeSkillRef, error) {
	object := map[string]json.RawMessage{}
	if len(snapshot) == 0 || strings.TrimSpace(string(snapshot)) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(snapshot, &object); err != nil {
		return nil, fmt.Errorf("decode agent snapshot: %w", err)
	}
	raw, ok := object["skills"]
	if !ok || len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil, nil
	}
	var refs []runtimeSkillRef
	if err := json.Unmarshal(raw, &refs); err != nil {
		return nil, fmt.Errorf("decode agent skills: %w", err)
	}
	for i, ref := range refs {
		if strings.TrimSpace(ref.Type) != "anthropic" && strings.TrimSpace(ref.Type) != "custom" {
			return nil, fmt.Errorf("skill %d type must be anthropic or custom", i)
		}
		if strings.TrimSpace(ref.SkillID) == "" {
			return nil, fmt.Errorf("skill %d id must be non-empty", i)
		}
	}
	return refs, nil
}

func (r *RuntimeResolver) resolveRef(ctx context.Context, workspaceID int64, ref runtimeSkillRef) (RuntimeSkill, error) {
	switch ref.Type {
	case "anthropic":
		return r.resolveBuiltin(ref)
	case "custom":
		return r.resolveCustom(ctx, workspaceID, ref)
	default:
		return RuntimeSkill{}, fmt.Errorf("unsupported skill type %q", ref.Type)
	}
}

func (r *RuntimeResolver) resolveBuiltin(ref runtimeSkillRef) (RuntimeSkill, error) {
	if r.catalog == nil {
		return RuntimeSkill{}, errors.New("built-in skills catalog is unavailable")
	}
	skill, ok := r.catalog.get(ref.SkillID)
	if !ok {
		return RuntimeSkill{}, fmt.Errorf("built-in skill not found: %s", ref.SkillID)
	}
	if ref.Version != "latest" && ref.Version != builtinVersion {
		return RuntimeSkill{}, fmt.Errorf("built-in skill version not found: %s", ref.Version)
	}
	catalogSkill := skill
	return RuntimeSkill{
		Source:           "anthropic",
		SkillID:          skill.ID,
		RequestedVersion: ref.Version,
		Version:          builtinVersion,
		Directory:        skill.Directory,
		Name:             firstNonEmpty(skill.Name, skill.ID),
		Description:      skill.Description,
		SHA256:           skill.SHA256,
		SizeBytes:        skill.ArchiveSize,
		archiveLoader: func(ctx context.Context) ([]byte, error) {
			return loadBuiltinArchive(ctx, catalogSkill)
		},
	}, nil
}

func (r *RuntimeResolver) resolveCustom(ctx context.Context, workspaceID int64, ref runtimeSkillRef) (RuntimeSkill, error) {
	if r.db == nil || r.store == nil {
		return RuntimeSkill{}, errors.New("custom skill resolver is unavailable")
	}
	version := ref.Version
	if version == "latest" {
		skill, err := r.db.GetSkill(ctx, workspaceID, ref.SkillID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return RuntimeSkill{}, fmt.Errorf("custom skill not found: %s", ref.SkillID)
			}
			return RuntimeSkill{}, err
		}
		if skill.LatestVersion == nil || strings.TrimSpace(*skill.LatestVersion) == "" {
			return RuntimeSkill{}, fmt.Errorf("custom skill has no active versions: %s", ref.SkillID)
		}
		version = strings.TrimSpace(*skill.LatestVersion)
	}
	record, err := r.db.GetSkillVersion(ctx, workspaceID, ref.SkillID, version)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return RuntimeSkill{}, fmt.Errorf("custom skill version not found: %s@%s", ref.SkillID, ref.Version)
		}
		return RuntimeSkill{}, err
	}
	versionRecord := record
	return RuntimeSkill{
		Source:           "custom",
		SkillID:          ref.SkillID,
		RequestedVersion: ref.Version,
		Version:          record.Version,
		Directory:        record.Directory,
		Name:             record.Name,
		Description:      record.Description,
		SHA256:           record.SHA256,
		SizeBytes:        record.SizeBytes,
		archiveLoader: func(ctx context.Context) ([]byte, error) {
			return r.loadCustomArchive(ctx, versionRecord)
		},
	}, nil
}

func loadBuiltinArchive(ctx context.Context, skill builtinSkill) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(skill.ArchivePath)
	if err != nil {
		return nil, fmt.Errorf("read built-in skill %s: %w", skill.ID, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateRuntimeSkillArchive(data, "built-in skill", skill.ID, builtinVersion, skill.Directory, skill.SHA256, skill.ArchiveSize); err != nil {
		return nil, err
	}
	return data, nil
}

func (r *RuntimeResolver) loadCustomArchive(ctx context.Context, record db.SkillVersion) ([]byte, error) {
	object, err := r.store.Get(ctx, record.S3Key)
	if err != nil {
		return nil, fmt.Errorf("read custom skill object %s@%s: %w", record.SkillExternalID, record.Version, err)
	}
	defer object.Body.Close()
	data, err := io.ReadAll(object.Body)
	if err != nil {
		return nil, fmt.Errorf("read custom skill archive %s@%s: %w", record.SkillExternalID, record.Version, err)
	}
	if err := validateRuntimeSkillArchive(data, "custom skill", record.SkillExternalID, record.Version, record.Directory, record.SHA256, record.SizeBytes); err != nil {
		return nil, err
	}
	return data, nil
}

func validateRuntimeSkillArchive(data []byte, label string, skillID string, version string, directory string, sha string, sizeBytes int64) error {
	if int64(len(data)) != sizeBytes {
		return fmt.Errorf("%s archive size mismatch %s@%s: got %d want %d", label, skillID, version, len(data), sizeBytes)
	}
	if got := sha256Hex(data); got != sha {
		return fmt.Errorf("%s archive checksum mismatch %s@%s", label, skillID, version)
	}
	archiveDirectory, _, err := inspectSkillArchiveBytes(data)
	if err != nil {
		return fmt.Errorf("inspect %s %s@%s: %w", label, skillID, version, err)
	}
	if archiveDirectory != directory {
		return fmt.Errorf("%s %s@%s directory changed from %q to %q", label, skillID, version, directory, archiveDirectory)
	}
	return nil
}

func inspectSkillArchiveBytes(data []byte) (string, []byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, err
	}
	var top string
	var skillMD []byte
	var uncompressedSize uint64
	for _, file := range reader.File {
		name := strings.ReplaceAll(file.Name, "\\", "/")
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, "/") || strings.Contains(name, "\x00") {
			return "", nil, fmt.Errorf("invalid zip path %q", file.Name)
		}
		cleanName := strings.TrimSuffix(name, "/")
		if cleanName == "" {
			return "", nil, fmt.Errorf("invalid zip path %q", file.Name)
		}
		parts := strings.Split(cleanName, "/")
		if len(parts) == 0 || parts[0] == "" || parts[0] == "." || parts[0] == ".." {
			return "", nil, fmt.Errorf("invalid zip top-level path %q", file.Name)
		}
		for _, part := range parts {
			if part == "" || part == "." || part == ".." {
				return "", nil, fmt.Errorf("invalid zip path %q", file.Name)
			}
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return "", nil, fmt.Errorf("zip contains symlink: %s", file.Name)
		}
		if top == "" {
			top = parts[0]
		} else if top != parts[0] {
			return "", nil, fmt.Errorf("multiple top-level directories: %s and %s", top, parts[0])
		}
		if file.FileInfo().IsDir() {
			continue
		}
		var err error
		uncompressedSize, err = addZipFileUncompressedSize(uncompressedSize, file)
		if err != nil {
			return "", nil, err
		}
		if cleanName == top+"/SKILL.md" {
			data, err := readZipFile(file)
			if err != nil {
				return "", nil, err
			}
			skillMD = data
		}
	}
	if top == "" {
		return "", nil, errors.New("archive is empty")
	}
	if len(skillMD) == 0 {
		return "", nil, fmt.Errorf("%s/SKILL.md not found", top)
	}
	return top, skillMD, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

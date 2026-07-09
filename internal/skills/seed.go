package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type BuiltinSeedOptions struct {
	Dir          string
	VersionsPath string
	Prune        bool
}

type BuiltinSeedResult struct {
	Imported int
	Pruned   int
	Skills   []string
}

func SeedBuiltinSkills(ctx context.Context, database *db.DB, store storage.ObjectStore, opts BuiltinSeedOptions) (BuiltinSeedResult, error) {
	dir := strings.TrimSpace(opts.Dir)
	if dir == "" {
		return BuiltinSeedResult{}, errors.New("--dir is required")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return BuiltinSeedResult{}, err
	}
	if !info.IsDir() {
		return BuiltinSeedResult{}, fmt.Errorf("%s is not a directory", dir)
	}

	versionMap, err := loadBuiltinSeedVersions(opts.VersionsPath)
	if err != nil {
		return BuiltinSeedResult{}, err
	}

	entries, err := filepath.Glob(filepath.Join(dir, "*.skill"))
	if err != nil {
		return BuiltinSeedResult{}, err
	}
	sort.Strings(entries)
	if len(entries) == 0 {
		return BuiltinSeedResult{}, fmt.Errorf("no .skill archives found in %s", dir)
	}

	now := time.Now().UTC()
	result := BuiltinSeedResult{Skills: make([]string, 0, len(entries))}
	for _, archivePath := range entries {
		skillID := strings.TrimSuffix(filepath.Base(archivePath), filepath.Ext(archivePath))
		if strings.TrimSpace(skillID) == "" {
			return BuiltinSeedResult{}, fmt.Errorf("invalid skill archive name: %s", archivePath)
		}
		data, err := os.ReadFile(archivePath)
		if err != nil {
			return BuiltinSeedResult{}, err
		}
		pkg, err := skillPackageFromArchive(data, MaxSkillPackageBytes)
		if err != nil {
			return BuiltinSeedResult{}, fmt.Errorf("%s: %w", archivePath, err)
		}
		info, err := os.Stat(archivePath)
		if err != nil {
			return BuiltinSeedResult{}, err
		}
		version := strings.TrimSpace(versionMap[skillID])
		if version == "" {
			version = defaultBuiltinSeedVersion(info.ModTime(), pkg.SHA256)
		}
		objectKey := fmt.Sprintf("builtin-skills/%s/versions/%s/%s.skill", sanitizeForKey(skillID), sanitizeForKey(version), pkg.SHA256)
		if err := store.Put(ctx, objectKey, bytes.NewReader(pkg.Zip), pkg.Size, skillArchiveContentType); err != nil {
			return BuiltinSeedResult{}, fmt.Errorf("upload %s: %w", archivePath, err)
		}

		_, _, err = database.UpsertBuiltinSkillWithVersion(ctx, db.BuiltinSkill{
			ExternalID:   skillID,
			DisplayTitle: firstNonEmpty(pkg.Name, skillID),
			CreatedAt:    now,
		}, db.BuiltinSkillVersion{
			ExternalID:  builtinVersionExternalID(skillID, version),
			Version:     version,
			Name:        firstNonEmpty(pkg.Name, skillID),
			Description: pkg.Description,
			Directory:   pkg.Directory,
			S3Bucket:    store.Bucket(),
			S3Key:       objectKey,
			SizeBytes:   pkg.Size,
			SHA256:      pkg.SHA256,
			CreatedAt:   now,
		})
		if err != nil {
			if deleteErr := store.Delete(ctx, objectKey); deleteErr != nil {
				log.Printf("seed builtin skills: cleanup failed for %s: %v", objectKey, deleteErr)
			}
			if errors.Is(err, db.ErrVersionConflict) {
				return BuiltinSeedResult{}, fmt.Errorf("%s version %s already exists with different content; choose a new version", skillID, version)
			}
			return BuiltinSeedResult{}, err
		}
		result.Imported++
		result.Skills = append(result.Skills, skillID)
	}

	if opts.Prune {
		prunedVersions, err := database.SoftDeleteMissingBuiltinSkills(ctx, result.Skills, now)
		if err != nil {
			return BuiltinSeedResult{}, err
		}
		for _, version := range prunedVersions {
			if err := store.Delete(ctx, version.S3Key); err != nil {
				log.Printf("seed builtin skills: delete pruned object failed for %s version %s (%s): %v", version.SkillExternalID, version.Version, version.S3Key, err)
			}
		}
		result.Pruned = len(prunedVersions)
	}
	return result, nil
}

func loadBuiltinSeedVersions(path string) (map[string]string, error) {
	if strings.TrimSpace(path) == "" {
		return map[string]string{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, errors.New("versions file must not be empty")
	}
	var versions map[string]string
	if strings.HasPrefix(trimmed, "{") {
		if err := json.Unmarshal(data, &versions); err != nil {
			return nil, fmt.Errorf("parse versions file: %w", err)
		}
	} else {
		versions = map[string]string{}
		for lineNumber, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			skillID, version, ok := strings.Cut(line, "=")
			if !ok {
				return nil, fmt.Errorf("parse versions file line %d: expected skill_id=version", lineNumber+1)
			}
			versions[strings.TrimSpace(skillID)] = strings.TrimSpace(version)
		}
	}
	for skillID, version := range versions {
		if strings.TrimSpace(skillID) == "" || strings.TrimSpace(version) == "" {
			return nil, errors.New("versions file must map non-empty skill ids to non-empty versions")
		}
	}
	return versions, nil
}

func defaultBuiltinSeedVersion(modTime time.Time, sha string) string {
	if len(sha) >= 12 {
		return sha[:12]
	}
	if strings.TrimSpace(sha) != "" {
		return sha
	}
	if !modTime.IsZero() {
		return modTime.UTC().Format("20060102")
	}
	return sha
}

func builtinVersionExternalID(skillID, version string) string {
	return "skillver_" + sanitizeForKey(skillID) + "_" + sanitizeForKey(version)
}

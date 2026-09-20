package skills

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

//go:embed product
var productSkillFS embed.FS

type productSkillSource struct {
	ID       string
	Markdown []byte
}

func EnsureProductBuiltinSkills(
	ctx context.Context,
	database *db.DB,
	store storage.ObjectStore,
	logger *slog.Logger,
) (BuiltinSeedResult, error) {
	logger = logging.LoggerOrDefault(logger)
	sources, err := loadProductSkillSources(productSkillFS)
	if err != nil {
		return BuiltinSeedResult{}, err
	}
	return seedProductBuiltinSkills(ctx, database, store, sources, logger)
}

func seedProductBuiltinSkills(
	ctx context.Context,
	database *db.DB,
	store storage.ObjectStore,
	sources []productSkillSource,
	logger *slog.Logger,
) (BuiltinSeedResult, error) {
	now := time.Now().UTC()
	result := BuiltinSeedResult{Skills: make([]string, 0, len(sources))}
	for _, source := range sources {
		version, err := productSkillVersion(source.ID, source.Markdown)
		if err != nil {
			return BuiltinSeedResult{}, err
		}
		pkg, err := packProductSkill(source.ID, source.Markdown)
		if err != nil {
			return BuiltinSeedResult{}, fmt.Errorf("%s: %w", source.ID, err)
		}
		if err := publishBuiltinSkill(ctx, database, store, logger, source.ID, version, pkg, now); err != nil {
			return BuiltinSeedResult{}, err
		}
		result.Imported++
		result.Skills = append(result.Skills, source.ID)
	}
	return result, nil
}

func loadProductSkillSources(fsys fs.FS) ([]productSkillSource, error) {
	matches, err := fs.Glob(fsys, "product/*/SKILL.md")
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	sources := make([]productSkillSource, 0, len(matches))
	for _, match := range matches {
		skillID := path.Base(path.Dir(match))
		if skillID == "" || skillID == "." || skillID == "product" {
			return nil, fmt.Errorf("invalid product skill path: %s", match)
		}
		if skillID == "ccOri" {
			// Reference snapshot of the upstream Auto-dream skill; not a catalog builtin.
			continue
		}
		data, err := fs.ReadFile(fsys, match)
		if err != nil {
			return nil, err
		}
		sources = append(sources, productSkillSource{ID: skillID, Markdown: data})
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no product skills embedded")
	}
	return sources, nil
}

func keepBuiltinSkillsForPrune(imported []string) ([]string, error) {
	sources, err := loadProductSkillSources(productSkillFS)
	if err != nil {
		return nil, err
	}
	keep := append([]string{}, imported...)
	seen := map[string]struct{}{}
	for _, skillID := range keep {
		seen[skillID] = struct{}{}
	}
	for _, source := range sources {
		if _, ok := seen[source.ID]; ok {
			continue
		}
		keep = append(keep, source.ID)
	}
	return keep, nil
}

func productSkillVersion(skillID string, skillMD []byte) (string, error) {
	meta := parseSkillMetadata(skillMD, skillID)
	if meta.Version == "" {
		return "", fmt.Errorf("%s: %w", skillID, errProductSkillVersionMissing)
	}
	return meta.Version, nil
}

func packProductSkill(skillID string, skillMD []byte) (skillPackage, error) {
	return skillPackageFromFiles([]normalizedSkillFile{{
		Name: skillID + "/SKILL.md",
		Data: skillMD,
	}}, MaxSkillPackageBytes)
}

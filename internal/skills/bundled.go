package skills

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type bundledSkill struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	IconKey   string `json:"icon_key"`
	Directory string `json:"directory"`
}

func SyncBundledSkills(ctx context.Context, database *db.DB, store storage.ObjectStore, files fs.FS) (int, error) {
	data, err := fs.ReadFile(files, "catalog.json")
	if err != nil {
		return 0, err
	}
	var catalog struct {
		Skills []bundledSkill `json:"skills"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return 0, err
	}
	if len(catalog.Skills) == 0 {
		return 0, fmt.Errorf("bundled skills catalog is empty")
	}
	seen := make(map[string]bool)
	for i, entry := range catalog.Skills {
		if seen[entry.ID] || entry.ID == "" || entry.Title == "" || !fs.ValidPath(entry.Directory) {
			return i, fmt.Errorf("invalid bundled skill entry %q", entry.ID)
		}
		seen[entry.ID] = true
		if err := syncBundledSkill(ctx, database, store, files, entry); err != nil {
			return i, fmt.Errorf("bundled skill %s: %w", entry.ID, err)
		}
	}
	return len(catalog.Skills), nil
}

func syncBundledSkill(ctx context.Context, database *db.DB, store storage.ObjectStore, files fs.FS, entry bundledSkill) error {
	icon, err := skillIconKey(entry.IconKey)
	if err != nil {
		return err
	}
	pkg, err := readBundledPackage(files, entry)
	if err != nil {
		return err
	}
	version := "bundled-" + pkg.SHA256[:32]
	key := "builtin-skills/bundled/" + entry.ID + "/" + pkg.SHA256 + ".zip"
	if _, err := store.Upload(ctx, key, bytes.NewReader(pkg.Zip), storage.UploadOptions{Size: pkg.Size, ContentType: skillArchiveContentType}); err != nil {
		return err
	}
	return database.SyncBundledSkill(ctx,
		db.SystemSkill{UUID: uuid.New().String(), ExternalID: entry.ID, DisplayTitle: entry.Title, IconKey: icon},
		db.SystemSkillVersion{
			UUID: uuid.New().String(), ExternalID: "skver_" + uuid.New().String(), Version: version,
			Name: pkg.Name, Description: pkg.Description, Directory: pkg.Directory,
			S3Bucket: store.Name(), S3Key: key, SizeBytes: pkg.Size, SHA256: pkg.SHA256, CreatedAt: time.Now().UTC(),
		},
	)
}

func readBundledPackage(files fs.FS, entry bundledSkill) (skillPackage, error) {
	var content []normalizedSkillFile
	err := fs.WalkDir(files, entry.Directory, func(name string, item fs.DirEntry, err error) error {
		if err != nil || item.IsDir() {
			return err
		}
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		relative := strings.TrimPrefix(name, entry.Directory+"/")
		content = append(content, normalizedSkillFile{Name: path.Join(entry.ID, relative), Data: data})
		return nil
	})
	if err != nil {
		return skillPackage{}, err
	}
	pkg, err := skillPackageFromFiles(content, MaxSkillPackageBytes, false)
	if err != nil {
		return skillPackage{}, err
	}
	if pkg.Name != entry.ID {
		return skillPackage{}, fmt.Errorf("package name %q does not match bundled ID %q", pkg.Name, entry.ID)
	}
	return pkg, nil
}

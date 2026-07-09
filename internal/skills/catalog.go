package skills

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const builtinVersion = "1"

type builtinCatalog struct {
	root   string
	skills map[string]builtinSkill
	order  []string
}

type builtinSkill struct {
	ID            string
	Name          string
	Description   string
	License       string
	Directory     string
	ArchivePath   string
	ArchiveSize   int64
	SHA256        string
	LatestVersion *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func loadBuiltinCatalog(cfg config.Config) *builtinCatalog {
	root, err := resolveBuiltinRoot(cfg.SkillsBuiltinDir)
	if err != nil {
		log.Printf("skills builtin catalog disabled: %v", err)
		return &builtinCatalog{root: cfg.SkillsBuiltinDir, skills: map[string]builtinSkill{}}
	}

	entries, err := filepath.Glob(filepath.Join(root, "*.skill"))
	if err != nil {
		log.Printf("skills builtin catalog glob root=%s: %v", root, err)
		return &builtinCatalog{root: root, skills: map[string]builtinSkill{}}
	}

	catalog := &builtinCatalog{root: root, skills: make(map[string]builtinSkill, len(entries))}
	for _, archivePath := range entries {
		skill, err := loadBuiltinSkill(root, archivePath)
		if err != nil {
			log.Printf("skip builtin skill archive=%s: %v", archivePath, err)
			continue
		}
		catalog.skills[skill.ID] = skill
		catalog.order = append(catalog.order, skill.ID)
	}
	sort.Slice(catalog.order, func(i, j int) bool {
		left := catalog.skills[catalog.order[i]]
		right := catalog.skills[catalog.order[j]]
		if left.CreatedAt.Equal(right.CreatedAt) {
			return left.ID < right.ID
		}
		return left.CreatedAt.After(right.CreatedAt)
	})
	return catalog
}

func resolveBuiltinRoot(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("SKILLS_BUILTIN_DIR is empty")
	}
	if filepath.IsAbs(raw) {
		if info, err := os.Stat(raw); err != nil {
			return "", err
		} else if !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", raw)
		}
		return raw, nil
	}

	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, raw)
		if info, err := os.Stat(candidate); err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("%s is not a directory", candidate)
			}
			return candidate, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return "", fmt.Errorf("%s not found from repository root %s", raw, dir)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%s not found", raw)
		}
		dir = parent
	}
}

func loadBuiltinSkill(root, archivePath string) (builtinSkill, error) {
	info, err := os.Stat(archivePath)
	if err != nil {
		return builtinSkill{}, err
	}
	if info.IsDir() {
		return builtinSkill{}, fmt.Errorf("%s is a directory", archivePath)
	}

	directory, skillMD, err := inspectSkillArchive(archivePath)
	if err != nil {
		return builtinSkill{}, err
	}
	id := strings.TrimSuffix(filepath.Base(archivePath), filepath.Ext(archivePath))

	metadataPath := filepath.Join(root, id, "SKILL.md")
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		metadataBytes = skillMD
	}
	sha, err := fileSHA256(archivePath)
	if err != nil {
		return builtinSkill{}, err
	}
	meta := parseSkillMetadata(metadataBytes, directory)
	version := builtinVersion
	return builtinSkill{
		ID:            id,
		Name:          meta.Name,
		Description:   meta.Description,
		License:       meta.License,
		Directory:     directory,
		ArchivePath:   archivePath,
		ArchiveSize:   info.Size(),
		SHA256:        sha,
		LatestVersion: &version,
		CreatedAt:     info.ModTime().UTC(),
		UpdatedAt:     info.ModTime().UTC(),
	}, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func inspectSkillArchive(path string) (string, []byte, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return "", nil, err
	}
	defer reader.Close()

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
		parts := strings.Split(name, "/")
		if len(parts) == 0 || parts[0] == "" || parts[0] == "." || parts[0] == ".." {
			return "", nil, fmt.Errorf("invalid zip top-level path %q", file.Name)
		}
		for _, part := range parts {
			if part == ".." {
				return "", nil, fmt.Errorf("zip path traverses parent: %q", file.Name)
			}
		}
		if top == "" {
			top = parts[0]
		} else if top != parts[0] {
			return "", nil, fmt.Errorf("multiple top-level directories: %s and %s", top, parts[0])
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return "", nil, fmt.Errorf("zip contains symlink: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			continue
		}
		var err error
		uncompressedSize, err = addZipFileUncompressedSize(uncompressedSize, file)
		if err != nil {
			return "", nil, err
		}
		if name == top+"/SKILL.md" {
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

func readZipFile(file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, rc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *builtinCatalog) get(id string) (builtinSkill, bool) {
	if c == nil {
		return builtinSkill{}, false
	}
	skill, ok := c.skills[id]
	return skill, ok
}

func (c *builtinCatalog) list(offset, limit int) ([]builtinSkill, bool) {
	if c == nil {
		return nil, false
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = defaultSkillsLimit
	}
	if offset >= len(c.order) {
		return []builtinSkill{}, false
	}
	end := offset + limit
	if end > len(c.order) {
		end = len(c.order)
	}
	out := make([]builtinSkill, 0, end-offset)
	for _, id := range c.order[offset:end] {
		out = append(out, c.skills[id])
	}
	return out, end < len(c.order)
}

func (c *builtinCatalog) len() int {
	if c == nil {
		return 0
	}
	return len(c.order)
}

type skillMetadata struct {
	Name        string
	Description string
	License     string
}

func parseSkillMetadata(data []byte, fallbackName string) skillMetadata {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	meta := parseFrontmatter(text)
	name := firstNonEmpty(meta["name"], firstMarkdownHeading(text), fallbackName)
	description := firstNonEmpty(meta["description"], firstMarkdownParagraph(text))
	return skillMetadata{
		Name:        name,
		Description: description,
		License:     meta["license"],
	}
}

func parseFrontmatter(text string) map[string]string {
	values := map[string]string{}
	if !strings.HasPrefix(text, "---\n") {
		return values
	}
	end := strings.Index(text[len("---\n"):], "\n---")
	if end < 0 {
		return values
	}
	block := text[len("---\n") : len("---\n")+end]
	for _, line := range strings.Split(block, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.ToLower(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		} else {
			value = strings.Trim(value, `"'`)
		}
		values[key] = value
	}
	return values
}

func firstMarkdownHeading(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}
	return ""
}

func firstMarkdownParagraph(text string) string {
	if strings.HasPrefix(text, "---\n") {
		if end := strings.Index(text[len("---\n"):], "\n---"); end >= 0 {
			text = text[len("---\n")+end+len("\n---"):]
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

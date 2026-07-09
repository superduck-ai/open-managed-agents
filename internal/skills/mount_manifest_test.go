package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestBuildMountManifestIsDeterministic(t *testing.T) {
	first := []RuntimeSkill{
		{Source: "custom", SkillID: "skill_b", Version: "2", Directory: "b", SHA256: "bbbb", SizeBytes: 10},
		{Source: "custom", SkillID: "skill_a", Version: "1", Directory: "a", SHA256: "aaaa", SizeBytes: 20},
	}
	second := []RuntimeSkill{first[1], first[0]}

	_, firstData, firstSHA, err := BuildMountManifest(first)
	if err != nil {
		t.Fatalf("BuildMountManifest(first) error = %v", err)
	}
	_, secondData, secondSHA, err := BuildMountManifest(second)
	if err != nil {
		t.Fatalf("BuildMountManifest(second) error = %v", err)
	}
	if firstSHA != secondSHA || string(firstData) != string(secondData) {
		t.Fatalf("manifest is not deterministic:\n%s\n%s", firstData, secondData)
	}
}

func TestBuildMountManifestUsesResolvedVersionForCacheKey(t *testing.T) {
	latest := []RuntimeSkill{{
		Source:           "custom",
		SkillID:          "skill_a",
		RequestedVersion: "latest",
		Version:          "20260708",
		Directory:        "runtime-skill",
		SHA256:           "aaaa",
		SizeBytes:        20,
	}}
	pinned := []RuntimeSkill{{
		Source:           "custom",
		SkillID:          "skill_a",
		RequestedVersion: "20260708",
		Version:          "20260708",
		Directory:        "runtime-skill",
		SHA256:           "aaaa",
		SizeBytes:        20,
	}}

	_, latestData, latestSHA, err := BuildMountManifest(latest)
	if err != nil {
		t.Fatalf("BuildMountManifest(latest) error = %v", err)
	}
	_, pinnedData, pinnedSHA, err := BuildMountManifest(pinned)
	if err != nil {
		t.Fatalf("BuildMountManifest(pinned) error = %v", err)
	}
	if latestSHA != pinnedSHA || string(latestData) != string(pinnedData) {
		t.Fatalf("requested version changed cache manifest:\n%s\n%s", latestData, pinnedData)
	}
	if bytes.Contains(latestData, []byte("requested_version")) {
		t.Fatalf("cache manifest contains requested_version: %s", latestData)
	}
}

func TestBuildMountManifestDoesNotLoadArchive(t *testing.T) {
	_, _, _, err := BuildMountManifest([]RuntimeSkill{{
		Source:    "custom",
		SkillID:   "skill_a",
		Version:   "20260708",
		Directory: "runtime-skill",
		SHA256:    "aaaa",
		SizeBytes: 20,
		archiveLoader: func(context.Context) ([]byte, error) {
			return nil, errors.New("archive loader should not be called")
		},
	}})
	if err != nil {
		t.Fatalf("BuildMountManifest() error = %v", err)
	}
}

func TestAddZipFileUncompressedSizeRejectsOversize(t *testing.T) {
	_, err := addZipFileUncompressedSize(0, &zip.File{
		FileHeader: zip.FileHeader{
			Name:               "runtime-skill/huge.bin",
			UncompressedSize64: maxSkillArchiveUncompressedBytes + 1,
		},
	})
	if err == nil {
		t.Fatal("addZipFileUncompressedSize error = nil, want oversize error")
	}
}

func TestInspectSkillArchiveAcceptsDirectoryEntries(t *testing.T) {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	dirHeader := &zip.FileHeader{Name: "runtime-skill/"}
	dirHeader.SetMode(0755)
	if _, err := writer.CreateHeader(dirHeader); err != nil {
		t.Fatalf("CreateHeader(dir) error = %v", err)
	}
	fileHeader := &zip.FileHeader{Name: "runtime-skill/SKILL.md"}
	fileHeader.SetMode(0644)
	file, err := writer.CreateHeader(fileHeader)
	if err != nil {
		t.Fatalf("CreateHeader(SKILL.md) error = %v", err)
	}
	if _, err := file.Write([]byte("# Runtime Skill\n")); err != nil {
		t.Fatalf("Write(SKILL.md) error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	dir, skillMD, err := inspectSkillArchiveBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("inspectSkillArchiveBytes() error = %v", err)
	}
	if dir != "runtime-skill" || string(skillMD) != "# Runtime Skill\n" {
		t.Fatalf("inspectSkillArchiveBytes() = %q, %q", dir, skillMD)
	}
}

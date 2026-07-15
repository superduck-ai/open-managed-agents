package e2bruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"

	e2b "github.com/superduck-ai/e2b-go-sdk"
)

const (
	SandboxSkillsMountPath = "/mnt/skills"
	SkillMountMetadataKey  = "managed_agent_skills_mount"
	skillMountManifestPath = "manifest.json"
	skillMountReadyPath    = ".ready"
	skillMountVolumePrefix = "managed-agent-skills-"
)

type SkillMount struct {
	MountPath      string                         `json:"mount_path"`
	VolumeName     string                         `json:"volume_name"`
	ManifestSHA256 string                         `json:"manifest_sha256"`
	Skills         []skillsapi.MountManifestSkill `json:"skills,omitempty"`
}

func (p *E2BProvider) PrepareSkillMount(ctx context.Context, runtimeSkills []skillsapi.RuntimeSkill) (*SkillMount, error) {
	if len(runtimeSkills) == 0 {
		return nil, nil
	}
	manifest, manifestBytes, manifestSHA256, err := skillsapi.BuildMountManifest(runtimeSkills)
	if err != nil {
		return nil, err
	}
	volumeName := skillMountVolumeName(manifestSHA256)
	volume, created, err := p.connectOrCreateSkillVolume(ctx, volumeName)
	if err != nil {
		return nil, err
	}
	if !created {
		ready, err := p.skillVolumeReady(ctx, volume, volumeName, manifestSHA256)
		if err != nil {
			return nil, err
		}
		if ready {
			return &SkillMount{
				MountPath:      SandboxSkillsMountPath,
				VolumeName:     volumeName,
				ManifestSHA256: manifestSHA256,
				Skills:         manifest.Skills,
			}, nil
		}
	}
	if err := p.writeSkillVolume(ctx, volume, manifestBytes, manifestSHA256, manifest, runtimeSkills); err != nil {
		return nil, err
	}
	return &SkillMount{
		MountPath:      SandboxSkillsMountPath,
		VolumeName:     volumeName,
		ManifestSHA256: manifestSHA256,
		Skills:         manifest.Skills,
	}, nil
}

func (p *E2BProvider) skillVolumeReady(ctx context.Context, volume *e2b.Volume, volumeName string, manifestSHA256 string) (bool, error) {
	exists, err := volume.Exists(ctx, skillMountReadyPath, p.volumeAPIOpts())
	if err != nil {
		return false, fmt.Errorf("check managed agent skill volume readiness %s: %w", volumeName, err)
	}
	if !exists {
		return false, nil
	}
	value, err := volume.ReadFile(ctx, skillMountReadyPath, p.volumeReadOpts())
	if err != nil {
		return false, fmt.Errorf("read managed agent skill volume ready marker %s: %w", volumeName, err)
	}
	var marker string
	switch typed := value.(type) {
	case string:
		marker = typed
	case []byte:
		marker = string(typed)
	default:
		return false, fmt.Errorf("managed agent skill volume ready marker %s has unsupported type %T", volumeName, value)
	}
	return strings.TrimSpace(marker) == strings.TrimSpace(manifestSHA256), nil
}

func (p *E2BProvider) connectOrCreateSkillVolume(ctx context.Context, volumeName string) (*e2b.Volume, bool, error) {
	opts := p.volumeConnectionOpts()
	volumes, err := e2b.ListVolumes(ctx, opts)
	if err != nil {
		return nil, false, fmt.Errorf("list E2B volumes for managed agent skills: %w", err)
	}
	for _, volume := range volumes {
		if volume.Name != volumeName {
			continue
		}
		connected, err := e2b.ConnectVolume(ctx, volume.VolumeID, opts)
		if err != nil {
			return nil, false, fmt.Errorf("connect managed agent skill volume %s: %w", volumeName, err)
		}
		return connected, false, nil
	}
	created, err := e2b.CreateVolume(ctx, volumeName, opts)
	if err == nil {
		return created, true, nil
	}
	volumes, listErr := e2b.ListVolumes(ctx, opts)
	if listErr == nil {
		for _, volume := range volumes {
			if volume.Name != volumeName {
				continue
			}
			connected, connectErr := e2b.ConnectVolume(ctx, volume.VolumeID, opts)
			if connectErr != nil {
				return nil, false, fmt.Errorf("connect concurrently created managed agent skill volume %s: %w", volumeName, connectErr)
			}
			return connected, false, nil
		}
	}
	return nil, false, fmt.Errorf("create managed agent skill volume %s: %w", volumeName, err)
}

func (p *E2BProvider) writeSkillVolume(ctx context.Context, volume *e2b.Volume, manifestBytes []byte, manifestSHA256 string, manifest skillsapi.MountManifest, runtimeSkills []skillsapi.RuntimeSkill) error {
	skillsByFilename := make(map[string]skillsapi.RuntimeSkill, len(runtimeSkills))
	for _, skill := range runtimeSkills {
		skillsByFilename[skillsapi.MountArchiveFilename(skill)] = skill
	}
	opts := p.volumeWriteOpts()
	if _, err := volume.WriteFile(ctx, skillMountManifestPath, manifestBytes, opts); err != nil {
		return fmt.Errorf("write managed agent skill manifest: %w", err)
	}
	for _, entry := range manifest.Skills {
		skill, ok := skillsByFilename[entry.Filename]
		if !ok {
			return fmt.Errorf("managed agent skill archive %s is missing", entry.Filename)
		}
		archive, err := skill.LoadArchive(ctx)
		if err != nil {
			return err
		}
		if len(archive) == 0 {
			return fmt.Errorf("managed agent skill archive %s is empty", entry.Filename)
		}
		if _, err := volume.WriteFile(ctx, entry.Filename, archive, opts); err != nil {
			return fmt.Errorf("write managed agent skill archive %s: %w", entry.Filename, err)
		}
	}
	if _, err := volume.WriteFile(ctx, skillMountReadyPath, []byte(manifestSHA256+"\n"), opts); err != nil {
		return fmt.Errorf("write managed agent skill volume ready marker: %w", err)
	}
	return nil
}

func (p *E2BProvider) volumeConnectionOpts() *e2b.VolumeConnectionOpts {
	requestTimeoutMs := int(p.cfg.E2BRequestTimeout / time.Millisecond)
	debug := p.cfg.E2BDebug
	return &e2b.VolumeConnectionOpts{
		ApiKey:           p.cfg.E2BAPIKey,
		AccessToken:      p.cfg.E2BAccessToken,
		Domain:           p.cfg.E2BDomain,
		ApiUrl:           p.cfg.E2BAPIURL,
		SandboxUrl:       p.cfg.E2BSandboxURL,
		Debug:            &debug,
		RequestTimeoutMs: &requestTimeoutMs,
	}
}

func (p *E2BProvider) volumeAPIOpts() *e2b.VolumeApiOpts {
	requestTimeoutMs := int(p.cfg.E2BRequestTimeout / time.Millisecond)
	debug := p.cfg.E2BDebug
	return &e2b.VolumeApiOpts{
		Domain:           p.cfg.E2BDomain,
		Debug:            &debug,
		ApiUrl:           p.cfg.E2BAPIURL,
		RequestTimeoutMs: &requestTimeoutMs,
	}
}

func (p *E2BProvider) volumeReadOpts() *e2b.VolumeReadOpts {
	apiOpts := p.volumeAPIOpts()
	return &e2b.VolumeReadOpts{
		VolumeApiOpts: *apiOpts,
	}
}

func (p *E2BProvider) volumeWriteOpts() *e2b.VolumeWriteOptions {
	force := true
	mode := 0o644
	apiOpts := p.volumeAPIOpts()
	return &e2b.VolumeWriteOptions{
		VolumeMetadataOptions: e2b.VolumeMetadataOptions{Mode: &mode},
		Force:                 &force,
		Domain:                apiOpts.Domain,
		Debug:                 apiOpts.Debug,
		ApiUrl:                apiOpts.ApiUrl,
		RequestTimeoutMs:      apiOpts.RequestTimeoutMs,
	}
}

func skillMountVolumeName(manifestSHA256 string) string {
	sha := strings.TrimSpace(manifestSHA256)
	if sha == "" {
		sha = "unknown"
	}
	return skillMountVolumePrefix + sha
}

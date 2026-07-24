package environments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
)

const (
	rcloneFilestorePath       = "/opt/rclone/rclone-filestore"
	rcloneConfigPath          = "/tmp/rclone-mount-config.json"
	rcloneStateDirectory      = "/tmp/rclone-mounts"
	rcloneReadyPath           = rcloneStateDirectory + "/ready"
	rcloneUploadsDestination  = "/mnt/session/uploads"
	rcloneReadyPollInterval   = 200 * time.Millisecond
	rcloneReadyTimeout        = 20 * time.Second
	rcloneCommandGraceTimeout = 5 * time.Second
	rcloneConfigCleanupTries  = 3
)

type rcloneMountConfig struct {
	CacheDurationSeconds float64 `json:"cache_duration_s"`
	AuthToken            string  `json:"auth_token"`
	Destination          string  `json:"destination"`
	DirectoryPermissions string  `json:"dir_perms"`
	FilePermissions      string  `json:"file_perms"`
	FilesystemID         string  `json:"filesystem_id"`
	GID                  int     `json:"gid"`
	Readonly             bool    `json:"readonly"`
	Source               string  `json:"source"`
	UID                  int     `json:"uid"`
	VFSCacheMaxSize      string  `json:"vfs_cache_max_size"`
	VFSCacheMode         string  `json:"vfs_cache_mode"`
}

type rcloneMultimountConfig struct {
	Mounts     []rcloneMountConfig `json:"mounts"`
	ReadyFile  string              `json:"ready_file"`
	ServiceURL string              `json:"service_url"`
	StateDir   string              `json:"state_dir"`
}

type rcloneFilestoreLaunch struct {
	ConfigPayload []byte
}

// prepareRcloneFilestoreLaunch resolves the Session filesystem authority and
// builds the fixed rclone multimount configuration. Session resource writes
// maintain the filesystem namespace; sandbox startup does not reconcile it.
func (r *Runner) prepareRcloneFilestoreLaunch(
	ctx context.Context,
	session db.Session,
) (rcloneFilestoreLaunch, error) {
	if r.filestoreCredentials == nil {
		return rcloneFilestoreLaunch{}, errors.New("filestore signer is not configured for managed-agent sandbox")
	}
	serviceURL := codeSessionSandboxAPIBaseURL(r.cfg)
	if serviceURL == "" {
		return rcloneFilestoreLaunch{}, errors.New("code_session.sandbox_api_base_url is required for managed-agent filestore")
	}
	scope, err := r.db.GetFilestoreTokenScopeForSessionIssue(ctx, session.WorkspaceID, session.ExternalID)
	if err != nil {
		return rcloneFilestoreLaunch{}, fmt.Errorf("resolve managed-agent filestore identity: %w", err)
	}
	identity := filestoreTokenIdentityFromScope(scope)
	readWriteIdentity := identity
	readWriteIdentity.WritePrefixes = []string{"/outputs"}
	readWriteToken, err := r.filestoreCredentials.Issue(readWriteIdentity)
	if err != nil {
		return rcloneFilestoreLaunch{}, fmt.Errorf("issue managed-agent filestore read-write token: %w", err)
	}
	readonlyToken, err := r.filestoreCredentials.IssueReadonly(identity)
	if err != nil {
		return rcloneFilestoreLaunch{}, fmt.Errorf("issue managed-agent filestore readonly token: %w", err)
	}
	configPayload, err := json.Marshal(buildRcloneMultimountConfig(scope.FilesystemExternalID, serviceURL, readWriteToken, readonlyToken))
	if err != nil {
		return rcloneFilestoreLaunch{}, fmt.Errorf("encode managed-agent filestore config: %w", err)
	}
	return rcloneFilestoreLaunch{ConfigPayload: configPayload}, nil
}

func filestoreTokenIdentityFromScope(scope db.FilestoreTokenScope) filestore.TokenIdentity {
	return filestore.TokenIdentity{
		Subject:                   scope.AccountExternalID,
		OrgUUID:                   scope.OrganizationUUID,
		AccountUUID:               scope.AccountUUID,
		WorkspaceUUID:             scope.WorkspaceUUID,
		WorkspaceTaggedID:         scope.WorkspaceExternalID,
		ResolvedWorkspaceTaggedID: scope.WorkspaceExternalID,
		FilesystemID:              scope.FilesystemExternalID,
		OrgTaints:                 append([]string(nil), scope.OrgTaints...),
		WorkspaceCMEKEnabled:      scope.WorkspaceCMEKEnabled,
	}
}

// buildRcloneMultimountConfig 把 sandbox 内几个固定挂载点映射到同一个
// filestore filesystem：outputs 读写，其余目录按最小权限原则只读挂载。
func buildRcloneMultimountConfig(filesystemID, serviceURL, readWriteToken, readonlyToken string) rcloneMultimountConfig {
	mount := func(source, destination string, cacheSeconds float64, readonly bool, token string) rcloneMountConfig {
		return rcloneMountConfig{
			CacheDurationSeconds: cacheSeconds,
			AuthToken:            token,
			Destination:          destination,
			DirectoryPermissions: "0755",
			FilePermissions:      "0644",
			FilesystemID:         filesystemID,
			GID:                  1000,
			Readonly:             readonly,
			Source:               source,
			UID:                  999,
			VFSCacheMaxSize:      "1G",
			VFSCacheMode:         "full",
		}
	}
	return rcloneMultimountConfig{
		Mounts: []rcloneMountConfig{
			mount("/outputs", "/mnt/user-data/outputs", 3600, false, readWriteToken),
			mount("/uploads", rcloneUploadsDestination, 1, true, readonlyToken),
			mount("/transcripts", "/mnt/transcripts", 10, true, readonlyToken),
			mount("/tool_results", "/mnt/user-data/tool_results", 3, true, readonlyToken),
		},
		ReadyFile:  rcloneReadyPath,
		ServiceURL: strings.TrimRight(strings.TrimSpace(serviceURL), "/"),
		StateDir:   rcloneStateDirectory,
	}
}

func rcloneStartCommand() string {
	return shellQuote(rcloneFilestorePath) + " multimount --config " + shellQuote(rcloneConfigPath)
}

func rcloneConfigPermissionsCommand() string {
	return "chmod 0600 " + shellQuote(rcloneConfigPath)
}

func rcloneConfigCleanupCommand() string {
	return "rm -f " + shellQuote(rcloneConfigPath)
}

package db

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/filestorepath"
)

const (
	SessionResourceFileKindFile      = "file"
	SessionResourceFileKindDirectory = "directory"
	SessionResourceFileKindArchive   = "archive"

	filestoreMaxPathBytes             = filestorepath.MaxBytes
	filestoreCleanupJobType           = "filestore_object_cleanup"
	filestoreFilesystemCleanupJobType = "filestore_filesystem_cleanup"
)

// SessionResourceFileOwnership 明确普通 namespace File 的对象生命周期责任。
// 空值只允许用于 Directory、Skill Archive 或尚未绑定 namespace 的 Resource。
type SessionResourceFileOwnership string

const (
	SessionResourceFileOwnershipReferenced SessionResourceFileOwnership = "referenced"
	SessionResourceFileOwnershipOwned      SessionResourceFileOwnership = "owned"
)

var (
	ErrFilestorePathExists              = errors.New("filestore path already exists")
	ErrFilestoreParentMissing           = errors.New("filestore parent directory does not exist")
	ErrFilestoreNotFile                 = errors.New("filestore resource is not a file")
	ErrFilestoreNotDirectory            = errors.New("filestore resource is not a directory")
	ErrFilestoreDirectoryNotEmpty       = errors.New("filestore directory is not empty")
	ErrFilestoreInvalidMove             = errors.New("invalid filestore move")
	ErrFilestoreCleanupJobNotCancelable = errors.New("filestore cleanup job is not cancelable")
)

// FilestoreFilesystem 是文件系统命名空间及其稳定租户、会话归属。
// 组织、工作区与会话都使用 UUID 持久化，不能仅凭 ExternalID 全局查找。
type FilestoreFilesystem struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	SessionUUID         string
	CodeSessionUUID     *string
	CreatedByAPIKeyUUID *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletedAt           *time.Time
}

// FilestoreTokenScope 是 Filestore JWT 通过数据库回查后得到的完整授权边界。
// UUID 与 external ID 同时保留，以便 API 层既能校验 claim，又能向下游传递稳定标识。
type FilestoreTokenScope struct {
	OrganizationUUID     string
	WorkspaceUUID        string
	WorkspaceExternalID  string
	AccountUUID          string
	AccountExternalID    string
	FilesystemUUID       string
	FilesystemExternalID string
	// OrgTaints 是 organizations.settings 中的当前组织策略标签。
	OrgTaints []string
	// WorkspaceCMEKEnabled 由 workspace.external_key_id 是否非空推导，
	// 表示工作区的 CMEK 配置状态，不负责对对象存储执行加密。
	WorkspaceCMEKEnabled bool
}

// ProvisionFilestoreFilesystemInput 描述一次幂等建档请求；租户与会话 UUID 必须彼此一致。
type ProvisionFilestoreFilesystemInput struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	SessionUUID         string
	CodeSessionUUID     *string
	CreatedByAPIKeyUUID *string
	Now                 time.Time
}

// SessionResourceFile 是由 Session Resource 与可选真实 File 组合出的资源文件。
// 目录不关联 File；Input Resource 引用 Source File；Owned File 与 Skill Archive
// 都通过 file_uuid 引用承载各自快照的 File。
type SessionResourceFile struct {
	ID                    int64
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	SessionUUID           string
	Kind                  string
	Path                  string
	ParentPath            *string
	FileUUID              *string
	FileOwnership         SessionResourceFileOwnership
	SizeBytes             *int64
	MediaType             *string
	DetectedMimeType      *string
	Metadata              json.RawMessage
	AuthorizationMetadata json.RawMessage
	Tags                  []string
	Downloadable          bool
	MD5                   *string
	SHA256                *string
	S3Bucket              *string
	S3Key                 *string
	S3ETag                *string
	S3VersionID           *string
	ExpiresAt             *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
	DeletedAt             *time.Time
}

// FilestoreFileBlob 汇集写入文件节点所需的内容元数据与对象定位信息。
type FilestoreFileBlob struct {
	SizeBytes             int64
	MediaType             string
	DetectedMimeType      string
	Metadata              json.RawMessage
	AuthorizationMetadata json.RawMessage
	Tags                  []string
	Downloadable          bool
	MD5                   string
	SHA256                string
	S3Bucket              string
	S3Key                 string
	S3ETag                string
	S3VersionID           string
	ExpiresAt             *time.Time
}

// SessionSkillArchiveResourceInput 描述一个已解析的不可变 skill ZIP。
// SkillVersionUUID 只用于写入时验证来源，不会持久化到 Session Resource 或 File。
type SessionSkillArchiveResourceInput struct {
	Source           string
	SkillVersionUUID string
	Directory        string
	S3Bucket         string
	S3Key            string
	SizeBytes        int64
	SHA256           string
}

// SessionResourceFilePageCursor 保存键集分页的最后一个 (Path, UUID) 排序键。
type SessionResourceFilePageCursor struct {
	Path string
	UUID string
}

// SessionResourceFilePage 表示一页目录节点及其后续页状态。
type SessionResourceFilePage struct {
	Entries []SessionResourceFile
	HasMore bool
}

// ListSessionResourceFilesPageParams 定义一次有界的目录枚举。
type ListSessionResourceFilesPageParams struct {
	WorkspaceUUID  string
	FilesystemUUID string
	DirectoryPath  string
	Recursive      bool
	Limit          int
	Cursor         *SessionResourceFilePageCursor
}

// MakeFilestoreDirectoryInput 描述目录创建及可选的父目录补齐行为。
type MakeFilestoreDirectoryInput struct {
	WorkspaceUUID  string
	FilesystemUUID string
	Path           string
	MakeParents    bool
	Now            time.Time
}

// PutFilestoreFileInput 将已上传对象绑定到文件路径，并在同一事务中核算工作区配额。
type PutFilestoreFileInput struct {
	WorkspaceUUID              string
	FilesystemUUID             string
	Path                       string
	Blob                       FilestoreFileBlob
	OverwriteExisting          bool
	OrphanCleanupJobExternalID string
	WorkspaceStorageLimitBytes int64
	Now                        time.Time
}

// CopyFilestoreFileInput 将服务端复制所得的新对象绑定到目标路径。
// ExpectedSource 字段用于确认复制期间源文件未被并发替换。
type CopyFilestoreFileInput struct {
	WorkspaceUUID              string
	FilesystemUUID             string
	SourcePath                 string
	DestinationPath            string
	ExpectedSourceS3Key        string
	ExpectedSourceS3VersionID  string
	DestinationS3Bucket        string
	DestinationS3Key           string
	DestinationS3ETag          string
	DestinationS3VersionID     string
	OverwriteExisting          bool
	OrphanCleanupJobExternalID string
	WorkspaceStorageLimitBytes int64
	Now                        time.Time
}

// MoveFilestoreFileInput 描述文件路径移动；底层对象本身不迁移。
type MoveFilestoreFileInput struct {
	WorkspaceUUID     string
	FilesystemUUID    string
	SourcePath        string
	DestinationPath   string
	OverwriteExisting bool
	Now               time.Time
}

// MoveFilestoreDirectoryInput 描述目录及其整棵子树的原子改名。
type MoveFilestoreDirectoryInput struct {
	WorkspaceUUID   string
	FilesystemUUID  string
	SourcePath      string
	DestinationPath string
	Now             time.Time
}

// RemoveSessionResourceFileInput 描述单个文件的软删除。
type RemoveSessionResourceFileInput struct {
	WorkspaceUUID  string
	FilesystemUUID string
	Path           string
	Now            time.Time
}

// RemoveFilestoreDirectoryInput 描述目录软删除，Recursive 控制是否允许删除非空子树。
type RemoveFilestoreDirectoryInput struct {
	WorkspaceUUID  string
	FilesystemUUID string
	Path           string
	Recursive      bool
	Now            time.Time
}

// FilestoreMutationResult 返回变更后的 namespace 节点及随事务创建的对象清理任务。
type FilestoreMutationResult struct {
	Node        SessionResourceFile
	CleanupJobs []FilestoreObjectCleanupJob
}

// FilestoreObjectCleanupJob 描述一个可租约、可重试的对象版本删除任务。
// UUID 是任务持久化和执行时的权威归属。
type FilestoreObjectCleanupJob struct {
	UUID                 string    `db:"uuid"`
	ExternalID           string    `db:"external_id"`
	WorkspaceUUID        string    `db:"workspace_uuid"`
	FilesystemUUID       string    `db:"filesystem_uuid"`
	FilesystemExternalID string    `db:"filesystem_external_id"`
	EntryExternalID      string    `db:"entry_external_id"`
	Bucket               string    `db:"bucket"`
	Key                  string    `db:"key"`
	ETag                 string    `db:"etag"`
	VersionID            string    `db:"version_id"`
	Reason               string    `db:"reason"`
	Attempts             int       `db:"attempts"`
	RunAfter             time.Time `db:"run_after"`
}

// FilestoreCleanupAnomaly 表示清理时发现无法定位底层对象的 Owned File。
// 数据库仍会退休逻辑节点并修正账本；worker 负责记录该异常供运维追踪。
type FilestoreCleanupAnomaly struct {
	WorkspaceUUID   string
	FilesystemUUID  string
	EntryExternalID string
	Reason          string
}

// FilestoreFilesystemCleanupJob 将已删除 Session 的整个文件系统拆成有界批次回收。
// 它只负责退休元数据并投递对象任务，不在数据库事务中直接访问 S3。
// UUID 是持久化引用和 worker 执行时的租户边界。
type FilestoreFilesystemCleanupJob struct {
	UUID                 string    `db:"uuid"`
	ExternalID           string    `db:"external_id"`
	WorkspaceUUID        string    `db:"workspace_uuid"`
	FilesystemUUID       string    `db:"filesystem_uuid"`
	FilesystemExternalID string    `db:"filesystem_external_id"`
	Attempts             int       `db:"attempts"`
	RunAfter             time.Time `db:"run_after"`
}

// EnqueueFilestoreObjectCleanupJobInput 描述对象清理任务的创建参数。
type EnqueueFilestoreObjectCleanupJobInput struct {
	WorkspaceUUID   string
	FilesystemUUID  string
	EntryExternalID string
	Bucket          string
	Key             string
	ETag            string
	VersionID       string
	Reason          string
	RunAfter        time.Time
}

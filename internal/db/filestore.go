package db

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/filestorepath"
)

const (
	FilestoreEntryKindFile      = "file"
	FilestoreEntryKindDirectory = "directory"

	filestoreMaxPathBytes             = filestorepath.MaxBytes
	filestoreCleanupJobType           = "filestore_object_cleanup"
	filestoreFilesystemCleanupJobType = "filestore_filesystem_cleanup"
)

var (
	ErrFilestorePathExists              = errors.New("filestore path already exists")
	ErrFilestoreParentMissing           = errors.New("filestore parent directory does not exist")
	ErrFilestoreNotFile                 = errors.New("filestore entry is not a file")
	ErrFilestoreNotDirectory            = errors.New("filestore entry is not a directory")
	ErrFilestoreDirectoryNotEmpty       = errors.New("filestore directory is not empty")
	ErrFilestoreInvalidMove             = errors.New("invalid filestore move")
	ErrFilestoreCleanupJobNotCancelable = errors.New("filestore cleanup job is not cancelable")
)

// FilestoreFilesystem 是文件系统命名空间及其稳定租户、会话归属。
// 组织、工作区与会话都使用 UUID 持久化，不能仅凭 ExternalID 全局查找。
type FilestoreFilesystem struct {
	ID                  int64
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
// UUID 与 external ID 同时保留，以便 API 层既能校验 claim，又能向下游传递内部主键。
type FilestoreTokenScope struct {
	OrganizationID         int64
	OrganizationUUID       string
	OrganizationExternalID string
	WorkspaceID            int64
	WorkspaceUUID          string
	WorkspaceExternalID    string
	AccountID              int64
	AccountUUID            string
	AccountExternalID      string
	FilesystemID           int64
	FilesystemUUID         string
	FilesystemExternalID   string
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

// FilestoreEntry 是目录树中的一个持久化节点。
// 目录字段保持为空；文件字段指向对象存储中的一个不可变对象版本。
// Managed 字段标识由 Session File resource 管理且借用 Files API 对象的条目。
type FilestoreEntry struct {
	ID                        int64
	UUID                      string
	ExternalID                string
	OrganizationUUID          string
	WorkspaceUUID             string
	FilesystemUUID            string
	Kind                      string
	Path                      string
	ParentPath                *string
	SizeBytes                 *int64
	MediaType                 *string
	DetectedMimeType          *string
	Metadata                  json.RawMessage
	AuthorizationMetadata     json.RawMessage
	Tags                      []string
	Downloadable              bool
	MD5                       *string
	SHA256                    *string
	S3Bucket                  *string
	S3Key                     *string
	S3ETag                    *string
	S3VersionID               *string
	ExpiresAt                 *time.Time
	ManagedBy                 *string
	ManagedResourceExternalID *string
	SourceFileUUID            *string
	CreatedByAPIKeyUUID       *string
	CreatedBySessionUUID      *string
	CreatedByCodeSessionUUID  *string
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
	DeletedAt                 *time.Time
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

// FilestoreEntryPageCursor 保存键集分页的最后一个 (Path, ID) 排序键。
type FilestoreEntryPageCursor struct {
	Path string
	ID   int64
}

// FilestoreEntryPage 表示一页目录节点及其后续页状态。
type FilestoreEntryPage struct {
	Entries []FilestoreEntry
	HasMore bool
}

// ListFilestoreEntriesPageParams 定义一次有界的目录枚举。
type ListFilestoreEntriesPageParams struct {
	WorkspaceID   int64
	FilesystemID  int64
	DirectoryPath string
	Recursive     bool
	Limit         int
	Cursor        *FilestoreEntryPageCursor
}

// MakeFilestoreDirectoryInput 描述目录创建及可选的父目录补齐行为。
type MakeFilestoreDirectoryInput struct {
	WorkspaceID  int64
	FilesystemID int64
	Path         string
	MakeParents  bool
	Now          time.Time
}

// PutFilestoreFileInput 将已上传对象绑定到文件路径，并在同一事务中核算工作区配额。
type PutFilestoreFileInput struct {
	WorkspaceID                int64
	FilesystemID               int64
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
	WorkspaceID                int64
	FilesystemID               int64
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
	WorkspaceID       int64
	FilesystemID      int64
	SourcePath        string
	DestinationPath   string
	OverwriteExisting bool
	Now               time.Time
}

// MoveFilestoreDirectoryInput 描述目录及其整棵子树的原子改名。
type MoveFilestoreDirectoryInput struct {
	WorkspaceID     int64
	FilesystemID    int64
	SourcePath      string
	DestinationPath string
	Now             time.Time
}

// RemoveFilestoreEntryInput 描述单个文件的软删除。
type RemoveFilestoreEntryInput struct {
	WorkspaceID  int64
	FilesystemID int64
	Path         string
	Now          time.Time
}

// RemoveFilestoreDirectoryInput 描述目录软删除，Recursive 控制是否允许删除非空子树。
type RemoveFilestoreDirectoryInput struct {
	WorkspaceID  int64
	FilesystemID int64
	Path         string
	Recursive    bool
	Now          time.Time
}

// FilestoreMutationResult 返回变更后的主条目及随事务创建的对象清理任务。
type FilestoreMutationResult struct {
	Entry       FilestoreEntry
	CleanupJobs []FilestoreObjectCleanupJob
}

// FilestoreObjectCleanupJob 描述一个可租约、可重试的对象版本删除任务。
// UUID 是任务持久化的权威归属；bigint ID 仅在当前数据库租约任务时重新解析，
// 不能用于跨库恢复、租户迁移或合库后的身份判断。
type FilestoreObjectCleanupJob struct {
	ID                   int64     `db:"id"`
	ExternalID           string    `db:"external_id"`
	WorkspaceUUID        string    `db:"workspace_uuid"`
	FilesystemUUID       string    `db:"filesystem_uuid"`
	WorkspaceID          int64     `db:"workspace_id"`
	FilesystemID         int64     `db:"filesystem_id"`
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

// FilestoreFilesystemCleanupJob 将已删除 Session 的整个文件系统拆成有界批次回收。
// 它只负责退休元数据并投递对象任务，不在数据库事务中直接访问 S3。
// UUID 是持久化引用，bigint ID 是 worker 在当前数据库中解析出的短期执行上下文。
type FilestoreFilesystemCleanupJob struct {
	ID                   int64     `db:"id"`
	ExternalID           string    `db:"external_id"`
	WorkspaceUUID        string    `db:"workspace_uuid"`
	FilesystemUUID       string    `db:"filesystem_uuid"`
	WorkspaceID          int64     `db:"workspace_id"`
	FilesystemID         int64     `db:"filesystem_id"`
	FilesystemExternalID string    `db:"filesystem_external_id"`
	Attempts             int       `db:"attempts"`
	RunAfter             time.Time `db:"run_after"`
}

// EnqueueFilestoreObjectCleanupJobInput 描述对象清理任务的创建参数。
// 当前库 ID 仅用于在插入时校验归属并解析 UUID，不会写入任务 payload。
type EnqueueFilestoreObjectCleanupJobInput struct {
	WorkspaceID     int64
	FilesystemID    int64
	EntryExternalID string
	Bucket          string
	Key             string
	ETag            string
	VersionID       string
	Reason          string
	RunAfter        time.Time
}

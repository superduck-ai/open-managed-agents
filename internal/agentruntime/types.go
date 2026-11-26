package agentruntime

import (
	"encoding/json"
	"time"
)

type ProviderKind string

const (
	ProviderOMANative     ProviderKind = "oma_native"
	ProviderQoderCloud    ProviderKind = "qoder_cloud"
	ProviderClaudeManaged ProviderKind = "claude_managed"
	ProviderOpenAIAgents  ProviderKind = "openai_agents"
)

func (k ProviderKind) IsNative() bool {
	return k == ProviderOMANative
}

func (k ProviderKind) String() string {
	return string(k)
}

type BudgetMode string

const (
	BudgetModeNone    BudgetMode = "none"
	BudgetModeUSD     BudgetMode = "usd"
	BudgetModeCredits BudgetMode = "credits"
)

type UsageMeteringMode string

const (
	UsageMeteringTokenAndCost    UsageMeteringMode = "token_and_cost"
	UsageMeteringProviderCredits UsageMeteringMode = "provider_credits"
)

type ArtifactDiscoveryMode string

const (
	ArtifactDiscoveryFilesystemOutputsDir ArtifactDiscoveryMode = "filesystem_outputs_dir"
	ArtifactDiscoveryExplicitDeliverTool  ArtifactDiscoveryMode = "explicit_deliver_tool"
)

type FileUploadConstraints struct {
	MaxSizeBytes        int64    `json:"max_size_bytes"`
	AllowedMIMEPrefixes []string `json:"allowed_mime_prefixes"`
	AllowBinary         bool     `json:"allow_binary"`
}

type ResourceType string

const (
	ResourceTypeAgent         ResourceType = "agent"
	ResourceTypeEnvironment   ResourceType = "environment"
	ResourceTypeSession       ResourceType = "session"
	ResourceTypeFile          ResourceType = "file"
	ResourceTypeSkill         ResourceType = "skill"
	ResourceTypeVault         ResourceType = "vault"
	ResourceTypeMemoryStore   ResourceType = "memory_store"
	ResourceTypeDeployment    ResourceType = "deployment"
	ResourceTypeDeploymentRun ResourceType = "deployment_run"
)

type Scope struct {
	OrganizationUUID   string
	WorkspaceUUID      string
	RuntimeProfileUUID string
	ProviderKind       ProviderKind
	UserUUID           string
}

type ResourceRef struct {
	Type                ResourceType
	OMAExternalID       string
	RemoteID            string
	ParentOMAExternalID string
	Role                string
}

type Page[T any] struct {
	Items      []T
	FirstID    string
	LastID     string
	HasMore    bool
	NextCursor string
}

type Model struct {
	ID                   string
	DisplayName          string
	DefaultContextWindow int64
	IsVL                 bool
	Enabled              bool
	ProviderKind         ProviderKind
}

type Agent struct {
	ID          string
	Version     int64
	Name        string
	Description string
	Model       string
	System      string
	Tools       json.RawMessage
	MCPServers  json.RawMessage
	Skills      json.RawMessage
	Metadata    map[string]string
	ArchivedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateAgentInput struct {
	Name        string
	Description string
	Model       string
	System      string
	Tools       json.RawMessage
	MCPServers  json.RawMessage
	Skills      json.RawMessage
	Metadata    map[string]string
}

type UpdateAgentInput struct {
	Version     int64
	Name        *string
	Description *string
	Model       *string
	System      *string
	Tools       json.RawMessage
	MCPServers  json.RawMessage
	Skills      json.RawMessage
	Metadata    map[string]string
}

type ListAgentsQuery struct {
	IncludeArchived bool
	Limit           int
	Offset          int
}

type SearchAgentsQuery struct {
	Query           string
	IncludeArchived bool
	Limit           int
	Offset          int
}

type ListAgentVersionsQuery struct {
	Limit int
	Page  string
}

type Environment struct {
	ID          string
	Name        string
	Description string
	Config      json.RawMessage
	Metadata    map[string]string
	ArchivedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateEnvironmentInput struct {
	Name        string
	Description string
	Config      json.RawMessage
	Metadata    map[string]string
}

type UpdateEnvironmentInput struct {
	Name        *string
	Description *string
	Config      json.RawMessage
	Metadata    map[string]string
}

type ListEnvironmentsQuery struct {
	IncludeArchived bool
	Limit           int
	Offset          int
}

type File struct {
	ID           string
	Filename     string
	MimeType     string
	SizeBytes    int64
	Downloadable bool
	ScopeID      string
	Role         string
	CreatedAt    time.Time
}

type UploadFileInput struct {
	Filename string
	MimeType string
	Content  []byte
	ScopeID  string
}

type ListFilesQuery struct {
	ScopeID string
	Limit   int
	Offset  int
}

type SessionBudget struct {
	Mode          BudgetMode
	MaxCostUSD    string
	MaxCreditCost string
}

type SessionUsage struct {
	ModelCredits          float64
	SandboxRuntimeCredits float64
	TotalCredits          float64
	InputTokens           int64
	OutputTokens          int64
	CostUSD               float64
}

type Session struct {
	ID            string
	AgentID       string
	AgentVersion  int64
	EnvironmentID string
	Title         string
	Status        string
	VaultIDs      []string
	Metadata      map[string]string
	Resources     []SessionResource
	Budget        *SessionBudget
	Usage         *SessionUsage
	ArchivedAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreateSessionInput struct {
	AgentID       string
	AgentVersion  int64
	EnvironmentID string
	Title         string
	VaultIDs      []string
	Metadata      map[string]string
	Resources     []AddSessionResourceInput
	InitialEvents []json.RawMessage
	Budget        *SessionBudget
}

type UpdateSessionInput struct {
	Title    *string
	Metadata map[string]string
}

type ListSessionsQuery struct {
	IncludeArchived bool
	Limit           int
	Offset          int
}

type Event struct {
	ID          string
	Type        string
	ThreadID    string
	ProcessedAt *time.Time
	Payload     json.RawMessage
}

type SendEventsInput struct {
	Events []json.RawMessage
}

type ListEventsQuery struct {
	ThreadID string
	BeforeID string
	AfterID  string
	Limit    int
	Order    string
}

type StreamOptions struct {
	LastEventID string
	ThreadID    string
	DeltaTypes  []string
}

type SessionResource struct {
	ID            string
	SessionID     string
	Type          string
	FileID        string
	MemoryStoreID string
	MountPath     string
	Payload       json.RawMessage
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type AddSessionResourceInput struct {
	Type          string
	FileID        string
	MemoryStoreID string
	MountPath     string
	Payload       json.RawMessage
}

type UpdateSessionResourceInput struct {
	MountPath *string
	Payload   json.RawMessage
}

type Thread struct {
	ID        string
	SessionID string
	Title     string
	Status    string
	Metadata  map[string]string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type HealthCheckResult struct {
	Status    string
	Latency   time.Duration
	Message   string
	CheckedAt time.Time
}

type MemoryStore struct {
	ID          string
	Name        string
	Description string
	Metadata    map[string]string
	ArchivedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateMemoryStoreInput struct {
	Name        string
	Description string
	Metadata    map[string]string
}

type UpdateMemoryStoreInput struct {
	Name        *string
	Description *string
	Metadata    map[string]string
}

type ListMemoryStoresQuery struct {
	IncludeArchived bool
	Limit           int
	Offset          int
	CreatedAtGTE    *time.Time
	CreatedAtLTE    *time.Time
}

type MemoryPrecondition struct {
	Type          string
	ContentSHA256 string
}

type Memory struct {
	ID               string
	MemoryStoreID    string
	MemoryVersionID  string
	Path             string
	Content          *string
	ContentSizeBytes int64
	ContentSHA256    string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateMemoryInput struct {
	Path         string
	Content      string
	View         string
	Precondition *MemoryPrecondition
}

type UpdateMemoryInput struct {
	Path         *string
	Content      *string
	View         string
	Precondition *MemoryPrecondition
}

type DeleteMemoryInput struct {
	ExpectedContentSHA256 *string
}

type GetMemoryQuery struct {
	View string
}

type ListMemoriesQuery struct {
	PathPrefix string
	Depth      *int
	View       string
	Order      string
	OrderBy    string
	Limit      int
	Page       string
}

type MemoryListResult struct {
	Items    []Memory
	Prefixes []string
	NextPage string
	HasMore  bool
}

type MemoryActor struct {
	Type      string
	APIKeyID  string
	SessionID string
	UserID    string
}

type MemoryVersion struct {
	ID               string
	MemoryID         string
	MemoryStoreID    string
	Operation        string
	Path             *string
	Content          *string
	ContentSizeBytes *int64
	ContentSHA256    *string
	CreatedBy        MemoryActor
	Redacted         bool
	RedactedAt       *time.Time
	RedactedBy       *MemoryActor
	CreatedAt        time.Time
}

type ListMemoryVersionsQuery struct {
	MemoryID     string
	Path         string
	Operation    string
	APIKeyID     string
	SessionID    string
	View         string
	Limit        int
	Page         string
	CreatedAtGTE *time.Time
	CreatedAtLTE *time.Time
}

type GetMemoryVersionQuery struct {
	View string
}

type DeploymentAgentRef struct {
	ID      string
	Type    string
	Version int
}

type DeploymentSchedule struct {
	Type           string   `json:"type"`
	Expression     string   `json:"expression"`
	Timezone       string   `json:"timezone"`
	LastRunAt      *string  `json:"last_run_at,omitempty"`
	UpcomingRunsAt []string `json:"upcoming_runs_at,omitempty"`
}

type DeploymentRunError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type DeploymentPausedReason struct {
	Type  string              `json:"type"`
	Error *DeploymentRunError `json:"error,omitempty"`
}

type DeploymentRunTriggerContext struct {
	Type        string `json:"type"`
	ScheduledAt string `json:"scheduled_at,omitempty"`
}

type Deployment struct {
	ID            string
	Agent         DeploymentAgentRef
	EnvironmentID string
	Name          string
	Description   string
	Metadata      map[string]string
	InitialEvents []json.RawMessage
	Resources     []json.RawMessage
	VaultIDs      []string
	Budget        *SessionBudget
	Schedule      *DeploymentSchedule
	Status        string
	PausedReason  *DeploymentPausedReason
	ArchivedAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreateDeploymentInput struct {
	AgentID       string
	AgentVersion  int
	EnvironmentID string
	Name          string
	Description   string
	Metadata      map[string]string
	InitialEvents []json.RawMessage
	Resources     []AddSessionResourceInput
	VaultIDs      []string
	Budget        *SessionBudget
	Schedule      *DeploymentSchedule
}

type UpdateDeploymentInput struct {
	AgentID       *string
	AgentVersion  int
	EnvironmentID *string
	Name          *string
	Description   *string
	ClearDesc     bool
	Metadata      map[string]string
	MetadataSet   bool
	InitialEvents []json.RawMessage
	EventsSet     bool
	Resources     []AddSessionResourceInput
	ResourcesSet  bool
	VaultIDs      []string
	VaultIDsSet   bool
	Budget        *SessionBudget
	BudgetSet     bool
	Schedule      *DeploymentSchedule
	ScheduleSet   bool
}

type ListDeploymentsQuery struct {
	IncludeArchived bool
	AgentID         string
	Status          string
	CreatedAtGTE    *time.Time
	CreatedAtLTE    *time.Time
	Limit           int
	Offset          int
}

type DeploymentRun struct {
	ID             string
	DeploymentID   string
	Agent          DeploymentAgentRef
	SessionID      *string
	Error          *DeploymentRunError
	TriggerContext DeploymentRunTriggerContext
	CreatedAt      time.Time
}

type ListDeploymentRunsQuery struct {
	DeploymentID string
	TriggerType  string
	HasError     *bool
	CreatedAtGT  *time.Time
	CreatedAtGTE *time.Time
	CreatedAtLT  *time.Time
	CreatedAtLTE *time.Time
	Limit        int
	Offset       int
}

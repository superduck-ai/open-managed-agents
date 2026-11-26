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
	ResourceTypeAgent       ResourceType = "agent"
	ResourceTypeEnvironment ResourceType = "environment"
	ResourceTypeSession     ResourceType = "session"
	ResourceTypeFile        ResourceType = "file"
	ResourceTypeSkill       ResourceType = "skill"
	ResourceTypeVault       ResourceType = "vault"
	ResourceTypeMemoryStore ResourceType = "memory_store"
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
	ID        string
	SessionID string
	Type      string
	FileID    string
	MountPath string
	Payload   json.RawMessage
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AddSessionResourceInput struct {
	Type      string
	FileID    string
	MountPath string
	Payload   json.RawMessage
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

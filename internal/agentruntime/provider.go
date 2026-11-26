package agentruntime

import (
	"context"
	"encoding/json"
	"io"
)

type Runtime interface {
	ProviderKind() ProviderKind
	Capabilities() CapabilitySet
	Close() error
}

type ModelRuntime interface {
	ListModels(ctx context.Context, scope Scope) ([]Model, error)
}

type AgentRuntime interface {
	CreateAgent(ctx context.Context, scope Scope, input CreateAgentInput) (Agent, error)
	GetAgent(ctx context.Context, scope Scope, agentID string) (Agent, error)
	ListAgents(ctx context.Context, scope Scope, query ListAgentsQuery) (Page[Agent], error)
	SearchAgents(ctx context.Context, scope Scope, query SearchAgentsQuery) (Page[Agent], error)
	UpdateAgent(ctx context.Context, scope Scope, agentID string, input UpdateAgentInput) (Agent, error)
	ArchiveAgent(ctx context.Context, scope Scope, agentID string) (Agent, error)
}

type AgentVersionRuntime interface {
	GetAgentVersion(ctx context.Context, scope Scope, agentID string, version int64) (Agent, error)
	ListAgentVersions(ctx context.Context, scope Scope, agentID string, query ListAgentVersionsQuery) (Page[Agent], error)
}

type EnvironmentRuntime interface {
	CreateEnvironment(ctx context.Context, scope Scope, input CreateEnvironmentInput) (Environment, error)
	GetEnvironment(ctx context.Context, scope Scope, environmentID string) (Environment, error)
	ListEnvironments(ctx context.Context, scope Scope, query ListEnvironmentsQuery) (Page[Environment], error)
	UpdateEnvironment(ctx context.Context, scope Scope, environmentID string, input UpdateEnvironmentInput) (Environment, error)
	ArchiveEnvironment(ctx context.Context, scope Scope, environmentID string) (Environment, error)
	DeleteEnvironment(ctx context.Context, scope Scope, environmentID string) error
}

type FileContentStream interface {
	io.ReadCloser
	ContentType() string
	ContentLength() int64
	Filename() string
}

type FileRuntime interface {
	UploadFile(ctx context.Context, scope Scope, input UploadFileInput) (File, error)
	GetFile(ctx context.Context, scope Scope, fileID string) (File, error)
	ListFiles(ctx context.Context, scope Scope, query ListFilesQuery) (Page[File], error)
	DownloadFile(ctx context.Context, scope Scope, fileID string) (FileContentStream, error)
	DeleteFile(ctx context.Context, scope Scope, fileID string) error
}

type StreamFrame struct {
	ID    string
	Event string
	Data  json.RawMessage
}

type EventStream interface {
	Next(ctx context.Context) (StreamFrame, error)
	Close() error
}

type SessionRuntime interface {
	CreateSession(ctx context.Context, scope Scope, input CreateSessionInput) (Session, error)
	GetSession(ctx context.Context, scope Scope, sessionID string) (Session, error)
	ListSessions(ctx context.Context, scope Scope, query ListSessionsQuery) (Page[Session], error)
	UpdateSession(ctx context.Context, scope Scope, sessionID string, input UpdateSessionInput) (Session, error)
	ArchiveSession(ctx context.Context, scope Scope, sessionID string) (Session, error)
	DeleteSession(ctx context.Context, scope Scope, sessionID string) error
	SendEvents(ctx context.Context, scope Scope, sessionID string, input SendEventsInput) ([]Event, error)
	ListEvents(ctx context.Context, scope Scope, sessionID string, query ListEventsQuery) (Page[Event], error)
	StreamEvents(ctx context.Context, scope Scope, sessionID string, opts StreamOptions) (EventStream, error)
	ListSessionResources(ctx context.Context, scope Scope, sessionID string) ([]SessionResource, error)
	AddSessionResource(ctx context.Context, scope Scope, sessionID string, input AddSessionResourceInput) (SessionResource, error)
	UpdateSessionResource(ctx context.Context, scope Scope, sessionID string, resourceID string, input UpdateSessionResourceInput) (SessionResource, error)
	DeleteSessionResource(ctx context.Context, scope Scope, sessionID string, resourceID string) error
	ListThreads(ctx context.Context, scope Scope, sessionID string) ([]Thread, error)
	GetThread(ctx context.Context, scope Scope, sessionID string, threadID string) (Thread, error)
}

type HealthChecker interface {
	HealthCheck(ctx context.Context) (HealthCheckResult, error)
}

type MemoryRuntime interface {
	CreateMemoryStore(ctx context.Context, scope Scope, input CreateMemoryStoreInput) (MemoryStore, error)
	GetMemoryStore(ctx context.Context, scope Scope, storeID string) (MemoryStore, error)
	ListMemoryStores(ctx context.Context, scope Scope, query ListMemoryStoresQuery) (Page[MemoryStore], error)
	UpdateMemoryStore(ctx context.Context, scope Scope, storeID string, input UpdateMemoryStoreInput) (MemoryStore, error)
	ArchiveMemoryStore(ctx context.Context, scope Scope, storeID string) (MemoryStore, error)
	DeleteMemoryStore(ctx context.Context, scope Scope, storeID string) error
	CreateMemory(ctx context.Context, scope Scope, storeID string, input CreateMemoryInput) (Memory, error)
	GetMemory(ctx context.Context, scope Scope, storeID string, memoryID string, query GetMemoryQuery) (Memory, error)
	ListMemories(ctx context.Context, scope Scope, storeID string, query ListMemoriesQuery) (MemoryListResult, error)
	UpdateMemory(ctx context.Context, scope Scope, storeID string, memoryID string, input UpdateMemoryInput) (Memory, error)
	DeleteMemory(ctx context.Context, scope Scope, storeID string, memoryID string, input DeleteMemoryInput) error
	ListMemoryVersions(ctx context.Context, scope Scope, storeID string, query ListMemoryVersionsQuery) (Page[MemoryVersion], error)
	GetMemoryVersion(ctx context.Context, scope Scope, storeID string, versionID string, query GetMemoryVersionQuery) (MemoryVersion, error)
	RedactMemoryVersion(ctx context.Context, scope Scope, storeID string, versionID string) (MemoryVersion, error)
}

type DeploymentRuntime interface {
	CreateDeployment(ctx context.Context, scope Scope, input CreateDeploymentInput) (Deployment, error)
	GetDeployment(ctx context.Context, scope Scope, deploymentID string) (Deployment, error)
	ListDeployments(ctx context.Context, scope Scope, query ListDeploymentsQuery) (Page[Deployment], error)
	UpdateDeployment(ctx context.Context, scope Scope, deploymentID string, input UpdateDeploymentInput) (Deployment, error)
	ArchiveDeployment(ctx context.Context, scope Scope, deploymentID string) (Deployment, error)
	PauseDeployment(ctx context.Context, scope Scope, deploymentID string) (Deployment, error)
	UnpauseDeployment(ctx context.Context, scope Scope, deploymentID string) (Deployment, error)
	RunDeployment(ctx context.Context, scope Scope, deploymentID string) (DeploymentRun, error)
	GetDeploymentRun(ctx context.Context, scope Scope, runID string) (DeploymentRun, error)
	ListDeploymentRuns(ctx context.Context, scope Scope, query ListDeploymentRunsQuery) (Page[DeploymentRun], error)
}

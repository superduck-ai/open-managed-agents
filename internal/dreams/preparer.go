package dreams

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/systemresource"
)

const (
	dreamDefaultEnvironmentName  = "dream_env（Dream 内部环境）"
	dreamDefaultEnvironmentKind  = systemresource.DreamDefaultEnvironmentKind
	dreamDefaultAgentName        = "dream_agent（Dream 内部 Agent）"
	dreamDefaultAgentKind        = systemresource.DreamDefaultAgentKind
	dreamDefaultAgentDescription = "Reusable system agent managed by Dream."
)

var (
	dreamDefaultEnvironmentConfig = json.RawMessage(`{"type":"cloud","packages":{"type":"packages","apt":[],"cargo":[],"gem":[],"go":[],"npm":[],"pip":[]},"networking":{"type":"unrestricted"}}`)
	dreamDefaultAgentSystem       *string
	dreamDefaultAgentSkills       = json.RawMessage(`[{"type":"anthropic","skill_id":"dream","version":"latest"}]`)
	dreamDefaultAgentMCPServers   = json.RawMessage(`[]`)
	dreamDefaultAgentTools        = json.RawMessage(`[]`)
)

// Preparer owns Dream-only infrastructure creation. It is intentionally
// independent of the HTTP Handler so workers do not depend on routing state.
type Preparer struct {
	database      *db.DB
	storageClient storage.Client
	store         storage.ObjectStore
}

func NewPreparer(database *db.DB, storageClient storage.Client, store storage.ObjectStore) *Preparer {
	return &Preparer{database: database, storageClient: storageClient, store: store}
}

func (p *Preparer) ensureDreamDefaultEnvironment(ctx context.Context, principal auth.Principal, now time.Time) (db.Environment, error) {
	environment, err := p.database.GetDreamDefaultEnvironment(ctx, principal.WorkspaceUUID)
	if err == nil {
		if environment.ArchivedAt == nil {
			return environment, nil
		}
		return p.database.RestoreDreamDefaultEnvironment(ctx, principal.WorkspaceUUID, environment.UUID)
	}
	if !errors.Is(err, db.ErrNotFound) {
		return db.Environment{}, err
	}

	scope := "organization"
	environmentID, err := ids.New("env_")
	if err != nil {
		return db.Environment{}, err
	}
	created, err := p.database.CreateEnvironment(ctx, db.Environment{
		UUID: uuid.NewV4().String(), ExternalID: environmentID, OrganizationUUID: principal.OrganizationUUID,
		WorkspaceUUID: principal.WorkspaceUUID, CreatedByAPIKeyUUID: principal.APIKeyUUID,
		Name: dreamDefaultEnvironmentName, Description: "Reusable internal environment managed by Dream.",
		Config:   dreamDefaultEnvironmentConfig,
		Metadata: json.RawMessage(fmt.Sprintf(`{"internal_kind":%q}`, systemresource.DreamDefaultEnvironmentKind)), Scope: &scope,
		Provider: "e2b", CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		return created, nil
	}
	if !errors.Is(err, db.ErrDuplicate) {
		return db.Environment{}, err
	}
	environment, lookupErr := p.database.GetDreamDefaultEnvironment(ctx, principal.WorkspaceUUID)
	if lookupErr != nil {
		return db.Environment{}, fmt.Errorf("load concurrent Dream environment: %w", lookupErr)
	}
	if environment.ArchivedAt == nil {
		return environment, nil
	}
	return p.database.RestoreDreamDefaultEnvironment(ctx, principal.WorkspaceUUID, environment.UUID)
}

func (p *Preparer) ensureDreamDefaultAgent(ctx context.Context, principal auth.Principal, now time.Time) (db.Agent, error) {
	agent, err := p.database.GetDreamDefaultAgent(ctx, principal.WorkspaceUUID)
	if err == nil {
		if agent.ArchivedAt == nil {
			return agent, nil
		}
		return p.database.RestoreDreamDefaultAgent(ctx, principal.WorkspaceUUID, agent.UUID)
	}
	if !errors.Is(err, db.ErrNotFound) {
		return db.Agent{}, err
	}
	models, err := llmproviders.ListModelIDs(ctx, p.database, principal.OrganizationUUID, principal.WorkspaceUUID)
	if err != nil {
		return db.Agent{}, fmt.Errorf("list configured models: %w", err)
	}
	defaultModel, err := firstConfiguredModel(models)
	if err != nil {
		return db.Agent{}, err
	}
	agentID, err := ids.New("agent_")
	if err != nil {
		return db.Agent{}, err
	}
	versionID, err := ids.New("agentver_")
	if err != nil {
		return db.Agent{}, err
	}
	description := dreamDefaultAgentDescription
	created, err := p.database.CreateAgent(ctx, db.Agent{
		UUID: uuid.NewV4().String(), ExternalID: agentID, WorkspaceUUID: principal.WorkspaceUUID,
		CreatedByAPIKeyUUID: principal.APIKeyUUID, CurrentVersion: 1, Name: dreamDefaultAgentName,
		Description: &description, System: dreamDefaultAgentSystem, Model: defaultModel,
		MCPServers: dreamDefaultAgentMCPServers,
		Metadata:   json.RawMessage(fmt.Sprintf(`{"internal_kind":%q}`, systemresource.DreamDefaultAgentKind)),
		Multiagent: json.RawMessage(`null`), Skills: dreamDefaultAgentSkills, Tools: dreamDefaultAgentTools,
		CreatedAt: now, UpdatedAt: now,
	}, versionID)
	if err == nil {
		return created, nil
	}
	if !errors.Is(err, db.ErrDuplicate) {
		return db.Agent{}, err
	}
	agent, lookupErr := p.database.GetDreamDefaultAgent(ctx, principal.WorkspaceUUID)
	if lookupErr != nil {
		return db.Agent{}, fmt.Errorf("load concurrent Dream agent: %w", lookupErr)
	}
	if agent.ArchivedAt == nil {
		return agent, nil
	}
	return p.database.RestoreDreamDefaultAgent(ctx, principal.WorkspaceUUID, agent.UUID)
}

func dreamSessionAgentSnapshot(agent db.Agent, model string) (json.RawMessage, error) {
	agent.Model = json.RawMessage(fmt.Sprintf(`{"id":%q}`, model))
	return agentsnapshot.FromAgent(agent)
}

func firstConfiguredModel(models []string) (json.RawMessage, error) {
	for _, model := range models {
		if model = strings.TrimSpace(model); model != "" {
			return json.RawMessage(fmt.Sprintf(`{"id":%q}`, model)), nil
		}
	}
	return nil, errors.New("workspace has no configured model for Dream default agent")
}

func (p *Preparer) cloneMemoryStore(ctx context.Context, principal auth.Principal, dream db.Dream, source db.MemoryStore, now time.Time) (db.MemoryStore, []string, error) {
	storeID, err := ids.New("memstore_")
	if err != nil {
		return db.MemoryStore{}, nil, err
	}
	output, err := p.database.CreateMemoryStore(ctx, db.MemoryStore{
		UUID: uuid.NewV4().String(), ExternalID: storeID, OrganizationUUID: principal.OrganizationUUID,
		WorkspaceUUID: principal.WorkspaceUUID, CreatedByAPIKeyUUID: principal.APIKeyUUID,
		Name:        "Dream output: " + source.Name,
		Description: "Prepared output store for Dream " + dream.ExternalID,
		Metadata:    json.RawMessage(fmt.Sprintf(`{"dream_id":%q,"source_memory_store_id":%q}`, dream.ExternalID, source.ExternalID)),
		CreatedAt:   now, UpdatedAt: now,
	})
	if err != nil {
		return db.MemoryStore{}, nil, err
	}

	var copiedKeys []string
	var cursor *db.MemoryPageCursor
	for {
		memories, more, listErr := p.database.ListMemoriesPage(ctx, db.ListMemoriesPageParams{
			WorkspaceUUID: principal.WorkspaceUUID, MemoryStoreExternalID: source.ExternalID,
			Limit: 100, Cursor: cursor,
		})
		if listErr != nil {
			return output, copiedKeys, listErr
		}
		for _, memory := range memories {
			if err := p.cloneMemory(ctx, principal, output, memory, now, &copiedKeys); err != nil {
				return output, copiedKeys, err
			}
		}
		if !more || len(memories) == 0 {
			return output, copiedKeys, nil
		}
		last := memories[len(memories)-1]
		cursor = &db.MemoryPageCursor{Path: last.Path, CreatedAt: last.CreatedAt, UpdatedAt: last.UpdatedAt, UUID: last.UUID}
	}
}

func (p *Preparer) cloneMemory(ctx context.Context, principal auth.Principal, output db.MemoryStore, source db.Memory, now time.Time, copiedKeys *[]string) error {
	memoryID, err := ids.New("mem_")
	if err != nil {
		return err
	}
	versionID, err := ids.New("memver_")
	if err != nil {
		return err
	}
	memoryUUID, versionUUID := uuid.NewV4().String(), uuid.NewV4().String()
	destinationKey := db.MemoryContentObjectKey(principal.WorkspaceUUID, output.UUID, memoryUUID, versionUUID)
	if source.S3Bucket == p.store.Name() {
		if _, err := p.store.Copy(ctx, source.S3Key, destinationKey); err != nil {
			return err
		}
	} else {
		if p.storageClient == nil {
			return errors.New("Dream source memory uses a different storage bucket")
		}
		sourceStore, err := p.storageClient.ForBucket(source.S3Bucket)
		if err != nil {
			return err
		}
		object, err := sourceStore.Open(ctx, source.S3Key, nil)
		if err != nil {
			return err
		}
		defer object.Body.Close()
		if _, err := p.store.Upload(ctx, destinationKey, io.LimitReader(object.Body, db.MaxMemoryContentBytes+1), storage.UploadOptions{Size: source.ContentSizeBytes, ContentType: "text/plain; charset=utf-8"}); err != nil {
			return err
		}
	}
	*copiedKeys = append(*copiedKeys, destinationKey)
	path := source.Path
	size, sha, bucket, key := source.ContentSizeBytes, source.ContentSHA256, p.store.Name(), destinationKey
	_, err = p.database.CreateMemory(ctx, db.Memory{
		UUID: memoryUUID, ExternalID: memoryID, OrganizationUUID: principal.OrganizationUUID, WorkspaceUUID: principal.WorkspaceUUID,
		MemoryStoreExternalID: output.ExternalID, Path: path, ContentSizeBytes: size, ContentSHA256: sha,
		S3Bucket: bucket, S3Key: key, CreatedAt: now, UpdatedAt: now,
	}, db.MemoryVersion{
		UUID: versionUUID, ExternalID: versionID, Operation: "created", Path: &path,
		ContentSizeBytes: &size, ContentSHA256: &sha, S3Bucket: &bucket, S3Key: &key,
		CreatedBy: db.MemoryActor{Type: db.MemoryActorTypeAPI, APIKeyUUID: principal.APIKeyUUID}, CreatedAt: now,
	})
	return err
}

func (p *Preparer) createInternalSession(ctx context.Context, principal auth.Principal, dream db.Dream, environment db.Environment, agent db.Agent, snapshot json.RawMessage, output db.MemoryStore, now time.Time) (db.Session, error) {
	sessionID, err := ids.New("sesn_")
	if err != nil {
		return db.Session{}, err
	}
	threadID, err := ids.New("sthr_")
	if err != nil {
		return db.Session{}, err
	}
	workID, err := ids.New("work_")
	if err != nil {
		return db.Session{}, err
	}
	resourceID, err := ids.New("sesrsc_")
	if err != nil {
		return db.Session{}, err
	}
	slug := sessionresource.SlugifyMemoryName(output.Name, output.ExternalID)
	payload, err := httpapi.MarshalRaw(sessionresource.SnapshotMemoryStore(output.ExternalID, sessionresource.MemoryAccessReadWrite, "Dream writes only to this cloned output store.", output.Name, output.Description, slug).PayloadFields(resourceID))
	if err != nil {
		return db.Session{}, err
	}
	title := "Dream " + dream.ExternalID
	created, _, _, _, err := p.database.CreateSession(ctx, db.CreateSessionInput{
		Session: db.Session{UUID: uuid.NewV4().String(), ExternalID: sessionID, OrganizationUUID: principal.OrganizationUUID,
			WorkspaceUUID: principal.WorkspaceUUID, CreatedByAPIKeyUUID: principal.APIKeyUUID, RuntimeUserUUID: principal.UserUUID,
			EnvironmentUUID: environment.UUID, EnvironmentExternalID: environment.ExternalID, AgentUUID: agent.UUID,
			AgentExternalID: agent.ExternalID, AgentVersion: agent.CurrentVersion, AgentSnapshot: snapshot, Title: &title,
			Metadata: json.RawMessage(fmt.Sprintf(`{"internal_kind":"dream","dream_id":%q}`, dream.ExternalID)), VaultIDs: []string{}, Status: "idle",
			Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`), OutcomeEvaluations: json.RawMessage(`[]`), CreatedAt: now, UpdatedAt: now},
		Thread: db.SessionThread{UUID: uuid.NewV4().String(), ExternalID: threadID, OrganizationUUID: principal.OrganizationUUID,
			WorkspaceUUID: principal.WorkspaceUUID, AgentSnapshot: snapshot, Status: "idle", Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now},
		Resources: []db.CreateSessionResourceInput{{Resource: db.SessionResource{UUID: uuid.NewV4().String(), ExternalID: resourceID,
			OrganizationUUID: principal.OrganizationUUID, WorkspaceUUID: principal.WorkspaceUUID, ResourceType: db.SessionResourceTypeMemoryStore,
			Payload: payload, SecretPayload: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now}}},
		Work: db.EnvironmentWork{UUID: uuid.NewV4().String(), ExternalID: workID, OrganizationUUID: principal.OrganizationUUID,
			WorkspaceUUID: principal.WorkspaceUUID, EnvironmentUUID: environment.UUID, EnvironmentExternalID: environment.ExternalID,
			Metadata: json.RawMessage(fmt.Sprintf(`{"internal_kind":"dream","dream_id":%q}`, dream.ExternalID)), State: "queued", CreatedAt: now, UpdatedAt: now},
	})
	return created, err
}

func (p *Preparer) transcriptRelations(ctx context.Context, workspaceUUID string, dream db.Dream, sessionIDs []string, now time.Time) ([]db.DreamSessionTranscript, error) {
	relations := make([]db.DreamSessionTranscript, 0, len(sessionIDs))
	for ordinal, sessionID := range sessionIDs {
		session, found, err := p.database.GetSession(ctx, workspaceUUID, sessionID)
		if err != nil {
			return nil, err
		}
		if !found || session.ArchivedAt != nil {
			return nil, permanentDreamErrorOfType(dreamErrorInputSessionUnavailable, fmt.Errorf("input session %s was deleted or archived after the Dream was created", sessionID))
		}
		relations = append(relations, db.DreamSessionTranscript{UUID: uuid.NewV4().String(), DreamUUID: dream.UUID,
			WorkspaceUUID: workspaceUUID, SourceSessionUUID: session.UUID, SourceSessionExternalID: session.ExternalID,
			Ordinal: ordinal, CreatedAt: now})
	}
	return relations, nil
}

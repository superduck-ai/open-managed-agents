package db

import (
	"bytes"
	"context"

	"github.com/superduck-ai/yourbatis"
)

func insertSessionTx(
	ctx context.Context,
	executor yourbatis.Executor,
	input CreateSessionInput,
) (Session, SessionThread, []SessionResource, EnvironmentWork, error) {
	sessionMapper := NewSessionMapper(executor)
	threadMapper := NewSessionThreadMapper(executor)
	workMapper := NewEnvironmentWorkMapper(executor)

	sessionRow, err := sessionMapper.Insert(ctx, sessionWriteParameters(input.Session))
	if err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	session := sessionRow.session()
	filesystem, err := insertSessionFilesystemTx(ctx, executor, session)
	if err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	if err = ensureFilestoreFixedRootsTx(ctx, executor, filesystem, session.CreatedAt); err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}

	input.Thread.SessionUUID = session.UUID
	input.Thread.SessionExternalID = session.ExternalID
	threadRow, err := threadMapper.Insert(ctx, sessionThreadWriteParameters(input.Thread))
	if err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	thread := threadRow.thread()

	if err = enforceSessionFileResourceCapacityTx(
		ctx,
		executor,
		session.WorkspaceUUID,
		session.ExternalID,
		sessionFileResourceCount(input.Resources),
	); err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	if sessionHasFileMount(input.Resources) {
		lockedFilesystem, lockErr := lockSessionFilestoreMutationTx(ctx, executor, session)
		if lockErr != nil {
			return Session{}, SessionThread{}, nil, EnvironmentWork{}, lockErr
		}
		if lockedFilesystem.UUID != filesystem.UUID {
			return Session{}, SessionThread{}, nil, EnvironmentWork{}, ErrPreconditionFailed
		}
		filesystem = lockedFilesystem
	}

	resources := make([]SessionResource, 0, len(input.Resources))
	for _, resourceInput := range input.Resources {
		resource := resourceInput.Resource
		resource.SessionExternalID = session.ExternalID
		created, createErr := createSessionResource(ctx, executor, resource)
		if createErr != nil {
			return Session{}, SessionThread{}, nil, EnvironmentWork{}, createErr
		}
		if _, createErr = bindSessionFileResourceWithLockedFilesystemTx(
			ctx,
			executor,
			session,
			filesystem,
			created,
			resourceInput.FileMount,
		); createErr != nil {
			return Session{}, SessionThread{}, nil, EnvironmentWork{}, createErr
		}
		resources = append(resources, created)
	}

	input.Work.SessionUUID = session.UUID
	workRow, err := workMapper.Insert(ctx, environmentWorkWriteParamsFrom(input.Work))
	if err != nil {
		return Session{}, SessionThread{}, nil, EnvironmentWork{}, err
	}
	return session, thread, resources, workRow.work(), nil
}

func createSessionResource(
	ctx context.Context,
	executor yourbatis.Executor,
	resource SessionResource,
) (SessionResource, error) {
	mapper := NewSessionResourceMapper(executor)
	row, err := mapper.Insert(ctx, sessionResourceWriteParameters(resource))
	if err != nil {
		return SessionResource{}, mapNoRows(err)
	}
	return row.resource(), nil
}

func insertSessionEventsTx(
	ctx context.Context,
	executor yourbatis.Executor,
	session Session,
	events []SessionEvent,
	ignoreExisting bool,
) ([]SessionEvent, error) {
	threadMapper := NewSessionThreadMapper(executor)
	eventMapper := NewSessionEventMapper(executor)
	primaryRow, err := threadMapper.FindPrimary(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return nil, mapNoRows(err)
	}
	primary := primaryRow.thread()

	created := make([]SessionEvent, 0, len(events))
	for _, event := range events {
		event.OrganizationUUID = session.OrganizationUUID
		event.WorkspaceUUID = session.WorkspaceUUID
		event.SessionUUID = session.UUID
		event.SessionExternalID = session.ExternalID
		if event.ThreadExternalID == nil {
			event.ThreadUUID = &primary.UUID
			threadExternalID := primary.ExternalID
			event.ThreadExternalID = &threadExternalID
		} else {
			threadRow, findErr := threadMapper.FindByExternalID(
				ctx,
				session.WorkspaceUUID,
				session.ExternalID,
				*event.ThreadExternalID,
			)
			if findErr != nil {
				return nil, mapNoRows(findErr)
			}
			thread := threadRow.thread()
			event.ThreadUUID = &thread.UUID
		}

		params := sessionEventWriteParameters(event)
		if ignoreExisting {
			row, found, insertErr := eventMapper.InsertIfAbsent(ctx, params)
			if insertErr != nil {
				return nil, insertErr
			}
			if found {
				created = append(created, row.event())
			}
			continue
		}
		row, insertErr := eventMapper.Insert(ctx, params)
		if insertErr != nil {
			return nil, insertErr
		}
		created = append(created, row.event())
	}
	return created, nil
}

func sessionWriteParameters(session Session) sessionWriteParams {
	return sessionWriteParams{
		UUID: session.UUID, ExternalID: session.ExternalID,
		OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID,
		CreatedByAPIKeyUUID: session.CreatedByAPIKeyUUID, EnvironmentUUID: session.EnvironmentUUID,
		EnvironmentExternalID: session.EnvironmentExternalID, AgentUUID: session.AgentUUID,
		AgentExternalID: session.AgentExternalID, AgentVersion: session.AgentVersion,
		AgentSnapshot: agentJSONArg(session.AgentSnapshot), DeploymentUUID: session.DeploymentUUID,
		DeploymentID: session.DeploymentID, Title: session.Title, Metadata: agentJSONArg(session.Metadata),
		VaultIDs: append(sessionVaultIDs{}, session.VaultIDs...), Status: session.Status,
		Usage: agentJSONArg(session.Usage),
		Stats: agentJSONArg(session.Stats), OutcomeEvaluations: agentJSONArg(session.OutcomeEvaluations),
		Budget: agentJSONArg(session.Budget), CreatedAt: session.CreatedAt,
	}
}

func sessionThreadWriteParameters(thread SessionThread) sessionThreadWriteParams {
	return sessionThreadWriteParams{
		UUID: thread.UUID, ExternalID: thread.ExternalID, OrganizationUUID: thread.OrganizationUUID,
		WorkspaceUUID: thread.WorkspaceUUID, SessionUUID: thread.SessionUUID,
		SessionExternalID: thread.SessionExternalID, ParentThreadUUID: thread.ParentThreadUUID,
		ParentThreadExternalID: thread.ParentThreadExternalID, AgentSnapshot: agentJSONArg(thread.AgentSnapshot),
		Status: thread.Status, Usage: agentJSONArg(thread.Usage), Stats: agentJSONArg(thread.Stats),
		CreatedAt: thread.CreatedAt,
	}
}

func sessionResourceWriteParameters(resource SessionResource) sessionResourceWriteParams {
	return sessionResourceWriteParams{
		UUID: resource.UUID, ExternalID: resource.ExternalID, OrganizationUUID: resource.OrganizationUUID,
		WorkspaceUUID: resource.WorkspaceUUID, SessionExternalID: resource.SessionExternalID,
		ResourceType: resource.ResourceType, Payload: agentJSONArg(resource.Payload),
		SecretPayload: agentJSONArg(resource.SecretPayload), CreatedAt: resource.CreatedAt,
	}
}

func sessionEventWriteParameters(event SessionEvent) sessionEventWriteParams {
	return sessionEventWriteParams{
		UUID: event.UUID, ExternalID: event.ExternalID, OrganizationUUID: event.OrganizationUUID,
		WorkspaceUUID: event.WorkspaceUUID, SessionUUID: event.SessionUUID,
		SessionExternalID: event.SessionExternalID, ThreadUUID: event.ThreadUUID,
		ThreadExternalID: event.ThreadExternalID, EventType: event.EventType,
		Payload: agentJSONArg(event.Payload), ProcessedAt: event.ProcessedAt, CreatedAt: event.CreatedAt,
	}
}

func sessionPageParameters(params ListSessionsPageParams) sessionPageMapperParams {
	return sessionPageMapperParams{
		WorkspaceUUID: params.WorkspaceUUID, FetchLimit: params.Limit + 1,
		Cursor: params.Cursor, Descending: params.Order != "asc", IncludeArchived: params.IncludeArchived,
		AgentExternalID: params.AgentExternalID, AgentVersion: params.AgentVersion,
		DeploymentID: params.DeploymentID, MemoryStoreID: params.MemoryStoreID, Statuses: params.Statuses,
		CreatedAtGT: params.CreatedAtGT, CreatedAtGTE: params.CreatedAtGTE,
		CreatedAtLT: params.CreatedAtLT, CreatedAtLTE: params.CreatedAtLTE,
	}
}

func sessionEventPageParameters(params ListSessionEventsPageParams) sessionEventPageMapperParams {
	return sessionEventPageMapperParams{
		WorkspaceUUID: params.WorkspaceUUID, SessionExternalID: params.SessionExternalID,
		ThreadExternalID: params.ThreadExternalID, PrimaryOnly: params.PrimaryOnly,
		FetchLimit: params.Limit + 1, Cursor: params.Cursor, Descending: params.Order == "desc",
		Types: params.Types, CreatedAtGT: params.CreatedAtGT, CreatedAtGTE: params.CreatedAtGTE,
		CreatedAtLT: params.CreatedAtLT, CreatedAtLTE: params.CreatedAtLTE,
	}
}

func sessionsFromRows(rows []sessionRow) []Session {
	sessions := make([]Session, len(rows))
	for index := range rows {
		sessions[index] = rows[index].session()
	}
	return sessions
}

func sessionThreadsFromRows(rows []sessionThreadRow) []SessionThread {
	threads := make([]SessionThread, len(rows))
	for index := range rows {
		threads[index] = rows[index].thread()
	}
	return threads
}

func sessionResourcesFromRows(rows []sessionResourceRow) []SessionResource {
	resources := make([]SessionResource, len(rows))
	for index := range rows {
		resources[index] = rows[index].resource()
	}
	return resources
}

func sessionEventsFromRows(rows []sessionEventRow) []SessionEvent {
	events := make([]SessionEvent, len(rows))
	for index := range rows {
		events[index] = rows[index].event()
	}
	return events
}

func (r sessionRow) session() Session {
	return Session{
		UUID: r.UUID, ExternalID: r.ExternalID, OrganizationUUID: r.OrganizationUUID,
		WorkspaceUUID: r.WorkspaceUUID, CreatedByAPIKeyUUID: r.CreatedByAPIKeyUUID,
		EnvironmentUUID: r.EnvironmentUUID, EnvironmentExternalID: r.EnvironmentExternalID,
		AgentUUID: r.AgentUUID, AgentExternalID: r.AgentExternalID, AgentVersion: r.AgentVersion,
		AgentSnapshot: bytes.Clone(r.AgentSnapshot), DeploymentUUID: r.DeploymentUUID,
		DeploymentID: r.DeploymentID, Title: r.Title, Metadata: bytes.Clone(r.Metadata),
		VaultIDs: append([]string{}, r.VaultIDs...), Status: r.Status,
		Usage: bytes.Clone(r.Usage), Stats: bytes.Clone(r.Stats),
		OutcomeEvaluations: bytes.Clone(r.OutcomeEvaluations), Budget: bytes.Clone(r.Budget),
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		ArchivedAt: r.ArchivedAt, DeletedAt: r.DeletedAt,
	}
}

func (r sessionThreadRow) thread() SessionThread {
	return SessionThread{
		UUID: r.UUID, ExternalID: r.ExternalID, OrganizationUUID: r.OrganizationUUID,
		WorkspaceUUID: r.WorkspaceUUID, SessionUUID: r.SessionUUID, SessionExternalID: r.SessionExternalID,
		ParentThreadUUID: r.ParentThreadUUID, ParentThreadExternalID: r.ParentThreadExternalID,
		AgentSnapshot: bytes.Clone(r.AgentSnapshot), Status: r.Status, Usage: bytes.Clone(r.Usage), Stats: bytes.Clone(r.Stats),
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ArchivedAt: r.ArchivedAt, DeletedAt: r.DeletedAt,
	}
}

func (r sessionResourceRow) resource() SessionResource {
	return SessionResource{
		UUID: r.UUID, ExternalID: r.ExternalID, OrganizationUUID: r.OrganizationUUID,
		WorkspaceUUID: r.WorkspaceUUID, SessionUUID: r.SessionUUID, SessionExternalID: r.SessionExternalID,
		ResourceType: r.ResourceType, Payload: bytes.Clone(r.Payload), SecretPayload: bytes.Clone(r.SecretPayload),
		Path: filestoreString(r.Path), FileExternalID: filestoreString(r.FileExternalID),
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, DeletedAt: r.DeletedAt,
	}
}

func (r sessionEventRow) event() SessionEvent {
	return SessionEvent{
		UUID: r.UUID, ExternalID: r.ExternalID, OrganizationUUID: r.OrganizationUUID,
		WorkspaceUUID: r.WorkspaceUUID, SessionUUID: r.SessionUUID, SessionExternalID: r.SessionExternalID,
		ThreadUUID: r.ThreadUUID, ThreadExternalID: r.ThreadExternalID, EventType: r.EventType,
		Payload: bytes.Clone(r.Payload), ProcessedAt: r.ProcessedAt, CreatedAt: r.CreatedAt, DeletedAt: r.DeletedAt,
	}
}

func (tx ManagedAgentActivationTx) LockSessionForEvents(
	ctx context.Context,
	workspaceUUID string,
	sessionExternalID string,
) (Session, error) {
	row, found, err := tx.sessionMapper.LockSessionForEvents(ctx, workspaceUUID, sessionExternalID)
	if err != nil {
		return Session{}, err
	}
	if !found {
		return Session{}, ErrNotFound
	}
	return row.session(), nil
}

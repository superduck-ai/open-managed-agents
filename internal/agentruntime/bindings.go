package agentruntime

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type BindingStore interface {
	UpsertRuntimeResourceBinding(ctx context.Context, binding db.RuntimeResourceBinding) (db.RuntimeResourceBinding, error)
	InsertRuntimeResourceBindingIfAbsent(ctx context.Context, binding db.RuntimeResourceBinding) (db.RuntimeResourceBinding, bool, error)
	GetRuntimeResourceBindingByOMAID(ctx context.Context, organizationUUID, workspaceUUID, resourceType, omaExternalID string) (db.RuntimeResourceBinding, error)
	GetRuntimeResourceBindingByRemoteID(ctx context.Context, runtimeProfileUUID, resourceType, remoteID string) (db.RuntimeResourceBinding, error)
	ListRuntimeResourceBindingsByWorkspaceAndType(ctx context.Context, params db.ListRuntimeResourceBindingsParams) ([]db.RuntimeResourceBinding, error)
	SearchRuntimeResourceBindingsByWorkspaceAndType(ctx context.Context, params db.SearchRuntimeResourceBindingsParams) ([]db.RuntimeResourceBinding, error)
	UpdateRuntimeResourceBindingSummaryAndState(ctx context.Context, organizationUUID, workspaceUUID, resourceType, omaExternalID, state string, summary json.RawMessage) (db.RuntimeResourceBinding, error)
	SoftDeleteRuntimeResourceBinding(ctx context.Context, organizationUUID, workspaceUUID, resourceType, omaExternalID string) error
}

type BindingService struct {
	store BindingStore
}

func NewBindingService(store BindingStore) *BindingService {
	return &BindingService{store: store}
}

func (s *BindingService) Bind(ctx context.Context, scope Scope, ref ResourceRef, summary json.RawMessage) (db.RuntimeResourceBinding, error) {
	if err := s.requireExternalScope(scope); err != nil {
		return db.RuntimeResourceBinding{}, err
	}
	if ref.Type == "" || ref.OMAExternalID == "" || ref.RemoteID == "" {
		return db.RuntimeResourceBinding{}, InvalidArgumentError("incomplete resource binding reference", nil)
	}
	binding, err := s.store.UpsertRuntimeResourceBinding(ctx, db.RuntimeResourceBinding{
		OrganizationUUID:    scope.OrganizationUUID,
		WorkspaceUUID:       scope.WorkspaceUUID,
		RuntimeProfileUUID:  scope.RuntimeProfileUUID,
		ProviderKind:        scope.ProviderKind.String(),
		ResourceType:        string(ref.Type),
		OMAExternalID:       ref.OMAExternalID,
		RemoteID:            ref.RemoteID,
		ParentOMAExternalID: ref.ParentOMAExternalID,
		ResourceRole:        ref.Role,
		State:               "active",
		Summary:             summary,
	})
	if err != nil {
		return db.RuntimeResourceBinding{}, MapDBError(err)
	}
	return binding, nil
}

func (s *BindingService) ClaimOutput(ctx context.Context, scope Scope, ref ResourceRef, summary json.RawMessage) (db.RuntimeResourceBinding, bool, error) {
	if err := s.requireExternalScope(scope); err != nil {
		return db.RuntimeResourceBinding{}, false, err
	}
	if ref.Type == "" || ref.OMAExternalID == "" || ref.RemoteID == "" {
		return db.RuntimeResourceBinding{}, false, InvalidArgumentError("incomplete resource claim reference", nil)
	}
	role := ref.Role
	if role == "" {
		role = "session_output"
	}
	binding, inserted, err := s.store.InsertRuntimeResourceBindingIfAbsent(ctx, db.RuntimeResourceBinding{
		OrganizationUUID:    scope.OrganizationUUID,
		WorkspaceUUID:       scope.WorkspaceUUID,
		RuntimeProfileUUID:  scope.RuntimeProfileUUID,
		ProviderKind:        scope.ProviderKind.String(),
		ResourceType:        string(ref.Type),
		OMAExternalID:       ref.OMAExternalID,
		RemoteID:            ref.RemoteID,
		ParentOMAExternalID: ref.ParentOMAExternalID,
		ResourceRole:        role,
		State:               "active",
		Summary:             summary,
	})
	if err != nil {
		return db.RuntimeResourceBinding{}, false, MapDBError(err)
	}
	return binding, inserted, nil
}

func (s *BindingService) ResolveOwned(ctx context.Context, scope Scope, resourceType ResourceType, omaExternalID string) (db.RuntimeResourceBinding, error) {
	if err := s.requireExternalScope(scope); err != nil {
		return db.RuntimeResourceBinding{}, err
	}
	if resourceType == "" || omaExternalID == "" {
		return db.RuntimeResourceBinding{}, NotFoundError("resource not found", ErrResourceNotFound)
	}
	binding, err := s.store.GetRuntimeResourceBindingByOMAID(ctx, scope.OrganizationUUID, scope.WorkspaceUUID, string(resourceType), omaExternalID)
	if err != nil {
		return db.RuntimeResourceBinding{}, MapDBError(err)
	}
	if binding.RuntimeProfileUUID != scope.RuntimeProfileUUID {
		return db.RuntimeResourceBinding{}, NotFoundError("resource not found", ErrResourceNotFound)
	}
	return binding, nil
}

func (s *BindingService) LookupByRemoteID(ctx context.Context, scope Scope, resourceType ResourceType, remoteID string) (db.RuntimeResourceBinding, error) {
	if err := s.requireExternalScope(scope); err != nil {
		return db.RuntimeResourceBinding{}, err
	}
	if resourceType == "" || remoteID == "" {
		return db.RuntimeResourceBinding{}, NotFoundError("resource not found", ErrResourceNotFound)
	}
	binding, err := s.store.GetRuntimeResourceBindingByRemoteID(ctx, scope.RuntimeProfileUUID, string(resourceType), remoteID)
	if err != nil {
		return db.RuntimeResourceBinding{}, MapDBError(err)
	}
	if binding.OrganizationUUID != scope.OrganizationUUID || binding.WorkspaceUUID != scope.WorkspaceUUID {
		return db.RuntimeResourceBinding{}, NotFoundError("resource not found", ErrResourceNotFound)
	}
	return binding, nil
}

func (s *BindingService) ListOwned(
	ctx context.Context,
	scope Scope,
	resourceType ResourceType,
	parentOMAExternalID string,
	role string,
	includeArchived bool,
	limit int,
	offset int,
) ([]db.RuntimeResourceBinding, error) {
	if err := s.requireExternalScope(scope); err != nil {
		return nil, err
	}
	rows, err := s.store.ListRuntimeResourceBindingsByWorkspaceAndType(ctx, db.ListRuntimeResourceBindingsParams{
		OrganizationUUID:    scope.OrganizationUUID,
		WorkspaceUUID:       scope.WorkspaceUUID,
		ResourceType:        string(resourceType),
		ParentOMAExternalID: parentOMAExternalID,
		ResourceRole:        role,
		IncludeArchived:     includeArchived,
		Limit:               limit,
		Offset:              offset,
	})
	if err != nil {
		return nil, MapDBError(err)
	}
	return rows, nil
}

func (s *BindingService) SearchOwned(
	ctx context.Context,
	scope Scope,
	resourceType ResourceType,
	query string,
	includeArchived bool,
	limit int,
	offset int,
) ([]db.RuntimeResourceBinding, error) {
	if err := s.requireExternalScope(scope); err != nil {
		return nil, err
	}
	rows, err := s.store.SearchRuntimeResourceBindingsByWorkspaceAndType(ctx, db.SearchRuntimeResourceBindingsParams{
		OrganizationUUID: scope.OrganizationUUID,
		WorkspaceUUID:    scope.WorkspaceUUID,
		ResourceType:     string(resourceType),
		Query:            query,
		IncludeArchived:  includeArchived,
		Limit:            limit,
		Offset:           offset,
	})
	if err != nil {
		return nil, MapDBError(err)
	}
	return rows, nil
}

func (s *BindingService) UpdateSummary(
	ctx context.Context,
	scope Scope,
	resourceType ResourceType,
	omaExternalID string,
	state string,
	summary json.RawMessage,
) (db.RuntimeResourceBinding, error) {
	if err := s.requireExternalScope(scope); err != nil {
		return db.RuntimeResourceBinding{}, err
	}
	binding, err := s.store.UpdateRuntimeResourceBindingSummaryAndState(
		ctx,
		scope.OrganizationUUID,
		scope.WorkspaceUUID,
		string(resourceType),
		omaExternalID,
		state,
		summary,
	)
	if err != nil {
		return db.RuntimeResourceBinding{}, MapDBError(err)
	}
	return binding, nil
}

func (s *BindingService) SoftDelete(ctx context.Context, scope Scope, resourceType ResourceType, omaExternalID string) error {
	if err := s.requireExternalScope(scope); err != nil {
		return err
	}
	if err := s.store.SoftDeleteRuntimeResourceBinding(ctx, scope.OrganizationUUID, scope.WorkspaceUUID, string(resourceType), omaExternalID); err != nil {
		return MapDBError(err)
	}
	return nil
}

func (s *BindingService) requireExternalScope(scope Scope) error {
	if s == nil || s.store == nil {
		return UnavailableError("binding service unavailable", nil)
	}
	if scope.ProviderKind == "" || scope.ProviderKind.IsNative() {
		return InvalidArgumentError("binding service only manages external runtime providers", ErrExternalProviderOnly)
	}
	if scope.OrganizationUUID == "" || scope.WorkspaceUUID == "" || scope.RuntimeProfileUUID == "" {
		return InvalidArgumentError("incomplete workspace scope", nil)
	}
	return nil
}

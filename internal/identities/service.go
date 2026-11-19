package identities

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

const (
	pageDefault = 20
	pageMax     = 100
)

type Service struct {
	store  Store
	logger *slog.Logger
	jobs   CleanupEnqueuer
}

func NewService(store Store, logger *slog.Logger) *Service {
	return &Service{store: store, logger: logging.LoggerOrDefault(logger)}
}

type IdentityInput struct {
	Namespace         string
	NamespaceProvided bool
	SubjectID         string
	DisplayName       string
	Enabled           *bool
	AutoMemoryStore   *bool
}

func (s *Service) CreateIdentity(ctx context.Context, principal auth.Principal, input IdentityInput) (db.IdentityRecord, error) {
	if strings.HasPrefix(strings.TrimSpace(input.Namespace), "feishu:") {
		return db.IdentityRecord{}, invalid("飞书个人身份由渠道创建", nil)
	}
	return s.createIdentity(ctx, principal, input)
}

func (s *Service) createIdentity(ctx context.Context, principal auth.Principal, input IdentityInput) (db.IdentityRecord, error) {
	name := strings.TrimSpace(input.DisplayName)
	subject := strings.TrimSpace(input.SubjectID)
	namespace := strings.TrimSpace(input.Namespace)
	if name == "" || subject == "" {
		return db.IdentityRecord{}, invalid("请填写业务主体编号和展示名", nil)
	}
	externalID, err := ids.New("idn_")
	if err != nil {
		return db.IdentityRecord{}, unavailable(err)
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	autoMemory := false
	if input.AutoMemoryStore != nil {
		autoMemory = *input.AutoMemoryStore
	}
	row, err := s.store.CreateBusinessIdentity(ctx, db.InsertIdentityParams{
		UUID: uuid.NewString(), ExternalID: externalID, OrganizationUUID: principal.OrganizationUUID,
		WorkspaceUUID: principal.WorkspaceUUID, Namespace: namespace, ExternalSubjectID: subject,
		DisplayName: name, Enabled: enabled, AutoMemoryStore: autoMemory,
	})
	return row, mapStore(err, "执行身份不存在")
}

func (s *Service) ListIdentities(ctx context.Context, principal auth.Principal, cursor, status, subject, namespace, search string, limit int) ([]db.IdentityRecord, string, error) {
	limit = pageLimit(limit)
	rows, err := s.store.ListBusinessIdentities(ctx, db.ListIdentityParams{
		WorkspaceUUID: principal.WorkspaceUUID, Cursor: cursor, Status: status,
		Subject: strings.TrimSpace(subject), Namespace: strings.TrimSpace(namespace), Search: strings.TrimSpace(search), Limit: limit + 1,
	})
	if err != nil {
		return nil, "", unavailable(err)
	}
	next := ""
	if len(rows) > limit {
		next = rows[limit-1].ExternalID
		rows = rows[:limit]
	}
	return rows, next, nil
}

func (s *Service) identityDirectory(ctx context.Context, params db.IdentityDirectoryParams) (string, error) {
	data, err := s.store.IdentityDirectory(ctx, params)
	return data, mapStore(err, "身份列表不可用")
}

func (s *Service) GetIdentity(ctx context.Context, principal auth.Principal, externalID string) (db.IdentityRecord, error) {
	row, err := s.store.FindBusinessIdentity(ctx, principal.WorkspaceUUID, externalID)
	return row, mapStore(err, "执行身份不存在")
}

func (s *Service) UpdateIdentity(ctx context.Context, principal auth.Principal, externalID string, input IdentityInput) (db.IdentityRecord, error) {
	current, err := s.GetIdentity(ctx, principal, externalID)
	if err != nil {
		return db.IdentityRecord{}, err
	}
	subject := strings.TrimSpace(input.SubjectID)
	namespace := strings.TrimSpace(input.Namespace)
	if input.NamespaceProvided && namespace != current.Namespace && strings.HasPrefix(namespace, "feishu:") {
		return db.IdentityRecord{}, invalid("飞书个人身份由渠道创建", nil)
	}
	if (subject != "" && subject != current.ExternalSubjectID) || (input.NamespaceProvided && namespace != current.Namespace) {
		if err := s.requireSharedIdentity(ctx, principal.WorkspaceUUID, current.UUID); err != nil {
			return db.IdentityRecord{}, err
		}
	}
	if input.DisplayName != "" {
		current.DisplayName = strings.TrimSpace(input.DisplayName)
	}
	if input.SubjectID != "" {
		current.ExternalSubjectID = subject
	}
	if input.NamespaceProvided {
		current.Namespace = namespace
	}
	if input.AutoMemoryStore != nil {
		current.AutoMemoryStore = *input.AutoMemoryStore
	}
	if current.DisplayName == "" || current.ExternalSubjectID == "" {
		return db.IdentityRecord{}, invalid("请填写业务主体编号和展示名", nil)
	}
	err = s.store.UpdateBusinessIdentity(ctx, db.UpdateIdentityParams{
		WorkspaceUUID: principal.WorkspaceUUID, ExternalID: externalID, Namespace: current.Namespace,
		ExternalSubjectID: current.ExternalSubjectID, DisplayName: current.DisplayName, AutoMemoryStore: current.AutoMemoryStore,
	})
	if err != nil {
		return db.IdentityRecord{}, mapStore(err, "执行身份不存在")
	}
	if input.Enabled != nil && *input.Enabled != current.Enabled {
		if err := s.setEnabled(ctx, principal, externalID, *input.Enabled); err != nil {
			return db.IdentityRecord{}, err
		}
	}
	return s.GetIdentity(ctx, principal, externalID)
}

func (s *Service) EnableIdentity(ctx context.Context, principal auth.Principal, externalID string) (db.IdentityRecord, error) {
	if err := s.setEnabled(ctx, principal, externalID, true); err != nil {
		return db.IdentityRecord{}, err
	}
	return s.GetIdentity(ctx, principal, externalID)
}

func (s *Service) DisableIdentity(ctx context.Context, principal auth.Principal, externalID string) (db.IdentityRecord, error) {
	if err := s.setEnabled(ctx, principal, externalID, false); err != nil {
		return db.IdentityRecord{}, err
	}
	return s.GetIdentity(ctx, principal, externalID)
}

func (s *Service) ResolveConsoleSessionIdentity(ctx context.Context, principal auth.Principal, identityID string) (string, error) {
	identity, err := s.store.FindBusinessIdentity(ctx, principal.WorkspaceUUID, identityID)
	if err != nil {
		return "", mapStore(err, "执行身份不存在")
	}
	if identity.OrganizationUUID != principal.OrganizationUUID || identity.DeletedAt != nil {
		return "", notFound("执行身份不存在", nil)
	}
	if !identity.Enabled {
		return "", conflict("Identity 已停用，不能创建新的执行", nil)
	}
	return identity.UUID, nil
}

func (s *Service) setEnabled(ctx context.Context, principal auth.Principal, externalID string, enabled bool) error {
	if _, err := s.GetIdentity(ctx, principal, externalID); err != nil {
		return err
	}
	return mapStore(s.store.SetBusinessIdentityEnabled(ctx, principal.WorkspaceUUID, externalID, enabled), "执行身份不存在")
}

func (s *Service) DeleteIdentity(ctx context.Context, principal auth.Principal, externalID string) error {
	identity, err := s.GetIdentity(ctx, principal, externalID)
	if err != nil {
		return err
	}
	if err := s.requireSharedIdentity(ctx, principal.WorkspaceUUID, identity.UUID); err != nil {
		return err
	}
	return mapStore(s.store.DeleteBusinessIdentity(ctx, principal.WorkspaceUUID, externalID), "执行身份不存在")
}

func (s *Service) requireSharedIdentity(ctx context.Context, workspaceUUID, identityUUID string) error {
	personal, err := s.store.IsPersonalIdentity(ctx, workspaceUUID, identityUUID)
	if err != nil {
		return unavailable(err)
	}
	if personal {
		return conflict("个人执行身份固定归属原操作者，请通过身份迁移变更归属", nil)
	}
	return nil
}

func (s *Service) AdmitExecution(ctx context.Context, _, workspaceUUID, sessionExternalID string, newRound bool) error {
	if !newRound {
		return nil
	}
	binding, err := s.store.FindSessionIdentity(ctx, workspaceUUID, sessionExternalID)
	if errors.Is(err, db.ErrNotFound) {
		return nil
	}
	if err != nil {
		return unavailable(err)
	}
	identity, err := s.store.FindBusinessIdentityByUUID(ctx, workspaceUUID, binding.IdentityUUID)
	if errors.Is(err, db.ErrNotFound) {
		return conflict("业务身份已停用，不能开始新的执行", err)
	}
	if err != nil {
		return unavailable(err)
	}
	if !identity.Enabled {
		return conflict("业务身份已停用，不能开始新的执行", nil)
	}
	return nil
}

func pageLimit(limit int) int {
	if limit <= 0 {
		return pageDefault
	}
	if limit > pageMax {
		return pageMax
	}
	return limit
}

func identityView(row db.IdentityRecord) map[string]any {
	return map[string]any{
		"id": row.ExternalID, "namespace": row.Namespace, "external_subject_id": row.ExternalSubjectID,
		"display_name": row.DisplayName, "enabled": row.Enabled, "auto_memory_store": row.AutoMemoryStore,
		"created_at": row.CreatedAt.UTC().Format(time.RFC3339Nano), "updated_at": row.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func decodeObject(raw string) map[string]string {
	if raw == "" {
		return map[string]string{}
	}
	var decoded map[string]string
	if json.Unmarshal([]byte(raw), &decoded) != nil || decoded == nil {
		return map[string]string{}
	}
	return publicEnv(decoded)
}

func decodeList(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var decoded []string
	if json.Unmarshal([]byte(raw), &decoded) != nil || decoded == nil {
		return []string{}
	}
	return decoded
}

func publicEnv(input map[string]string) map[string]string {
	output := map[string]string{}
	for key, value := range input {
		if vaults.PlatformReservedSecretName(key) || secretEnvKey(key) || secretEnvValue(value) {
			continue
		}
		output[key] = value
	}
	return output
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

package channels

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

const channelNameLimit = 40

type localStore interface {
	GetAgent(context.Context, string, string) (db.Agent, error)
	GetEnvironment(context.Context, string, string) (db.Environment, error)
	CreateChannelRecord(context.Context, db.ChannelRecordInput) (db.ChannelRecord, error)
	GetChannelRecord(context.Context, string, string, string, string) (db.ChannelRecord, error)
	ListAgentChannelRecords(context.Context, string, string, string) ([]db.ChannelRecord, error)
	ListWorkspaceChannelRecords(context.Context, string, string) ([]db.ChannelRecord, error)
	UpdateChannelRecord(context.Context, db.ChannelRecordUpdate) (db.ChannelRecord, error)
	SoftDeleteChannelRecord(context.Context, string, string) error
	ListChannelPairingRecords(context.Context, string) ([]db.ChannelPairingRecord, error)
	UpdateChannelPairingStatus(context.Context, string, string, string) error
	GetChannelAgentEnvironment(context.Context, string, string, string) (string, error)
	SetChannelAgentEnvironment(context.Context, string, string, string, string) error
	ClearChannelSessionBindings(context.Context, string) error
}

type localService struct {
	store    localStore
	cipher   *secrets.Service
	runtime  *runtimeSupervisor
	logger   *slog.Logger
	client   *http.Client
	tokenURL string
}

type channelMutation struct {
	Name             *string         `json:"name"`
	Mode             *string         `json:"mode"`
	AppID            *string         `json:"app_id"`
	AppSecret        *string         `json:"app_secret"`
	Enabled          *bool           `json:"enabled"`
	IncludeThinking  *bool           `json:"include_thinking"`
	IncludeToolCalls *bool           `json:"include_tool_calls"`
	ReplyInThread    *bool           `json:"reply_in_thread"`
	Status           *string         `json:"status"`
	AuthorizationID  json.RawMessage `json:"authorization_id"`
	MemoryStoreID    json.RawMessage `json:"memory_store_id"`
	VaultID          json.RawMessage `json:"vault_id"`
	GitRepository    json.RawMessage `json:"git_repository"`
}

type localHandler struct {
	service *localService
	logger  *slog.Logger
}

func newLocalService(store localStore, cipher *secrets.Service, runtime *runtimeSupervisor, logger *slog.Logger) *localService {
	return &localService{
		store:    store,
		cipher:   cipher,
		runtime:  runtime,
		logger:   logging.LoggerOrDefault(logger),
		client:   &http.Client{Timeout: 10 * time.Second},
		tokenURL: feishuTokenURL,
	}
}

func (s *localService) agent(ctx context.Context, workspaceUUID, agentID string) (db.Agent, error) {
	agent, err := s.store.GetAgent(ctx, workspaceUUID, agentID)
	if errors.Is(err, db.ErrNotFound) {
		return db.Agent{}, agentNotFound()
	}
	return agent, err
}

func (s *localService) list(ctx context.Context, principal auth.Principal, agentID string) ([]map[string]any, error) {
	if _, err := s.agent(ctx, principal.WorkspaceUUID, agentID); err != nil {
		return nil, err
	}
	records, err := s.store.ListAgentChannelRecords(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, agentID)
	if err != nil {
		return nil, unavailable(err)
	}
	views := make([]map[string]any, 0, len(records))
	for _, record := range records {
		view, err := s.view(ctx, record)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *localService) view(ctx context.Context, record db.ChannelRecord) (map[string]any, error) {
	pairings, err := s.store.ListChannelPairingRecords(ctx, record.ExternalID)
	if err != nil {
		return nil, unavailable(err)
	}
	pending := 0
	for _, pairing := range pairings {
		if pairing.Status == "pending" {
			pending++
		}
	}
	return map[string]any{
		"id":                 record.ExternalID,
		"name":               record.Name,
		"channel_type":       record.ChannelType,
		"mode":               record.Mode,
		"source":             "console",
		"enabled":            record.Enabled,
		"connected":          s.runtime != nil && s.runtime.online(record.ExternalID),
		"binding_status":     record.BindingStatus,
		"app_id":             record.AppID,
		"include_thinking":   record.IncludeThinking,
		"include_tool_calls": record.IncludeToolCalls,
		"reply_in_thread":    record.ReplyInThread,
		"pending_pairings":   pending,
		"memory_store_id":    nil,
		"vault_id":           nil,
		"git_repository":     nil,
		"created_at":         record.CreatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func (s *localService) create(ctx context.Context, principal auth.Principal, agentID string, input channelMutation) (map[string]any, error) {
	agent, err := s.agent(ctx, principal.WorkspaceUUID, agentID)
	if err != nil {
		return nil, err
	}
	if agent.ArchivedAt != nil {
		return nil, archivedAgent()
	}
	mode := "open"
	if input.Mode != nil {
		mode = *input.Mode
	}
	if mode != "open" && mode != "pairing" {
		return nil, invalidChannelMode()
	}
	name := ""
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
	}
	if name == "" || len([]rune(name)) > channelNameLimit {
		return nil, invalidChannelName()
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	externalID, err := ids.New("chnl_")
	if err != nil {
		return nil, unavailable(err)
	}
	record := db.ChannelRecord{
		ExternalID:       externalID,
		OrganizationUUID: principal.OrganizationUUID,
		WorkspaceUUID:    principal.WorkspaceUUID,
		AgentExternalID:  agentID,
		Name:             name,
		ChannelType:      "feishu",
		Mode:             mode,
		Enabled:          enabled,
		BindingStatus:    "unbound",
	}
	applyChannelFlags(&record, input)
	if input.AppID != nil || input.AppSecret != nil {
		if err := s.bindCredentials(ctx, &record, input); err != nil {
			return nil, err
		}
	}
	created, err := s.store.CreateChannelRecord(ctx, db.ChannelRecordInput{
		ExternalID:        record.ExternalID,
		OrganizationUUID:  record.OrganizationUUID,
		WorkspaceUUID:     record.WorkspaceUUID,
		AgentExternalID:   record.AgentExternalID,
		Name:              record.Name,
		ChannelType:       record.ChannelType,
		Mode:              record.Mode,
		Enabled:           record.Enabled,
		BindingStatus:     record.BindingStatus,
		AppID:             record.AppID,
		AppSecretEnvelope: record.AppSecretEnvelope,
		IncludeThinking:   record.IncludeThinking,
		IncludeToolCalls:  record.IncludeToolCalls,
		ReplyInThread:     record.ReplyInThread,
	})
	if errors.Is(err, db.ErrNotFound) {
		return nil, agentNotFound()
	}
	if err != nil {
		return nil, unavailable(err)
	}
	s.reconcile(ctx, created)
	return s.view(ctx, created)
}

func (s *localService) update(ctx context.Context, principal auth.Principal, agentID, channelID string, input channelMutation) (map[string]any, error) {
	record, err := s.store.GetChannelRecord(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, agentID, channelID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, channelNotFound()
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if input.Mode != nil && *input.Mode != record.Mode {
		return nil, immutableChannelMode()
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len([]rune(name)) > channelNameLimit {
			return nil, invalidChannelName()
		}
		record.Name = name
	}
	applyChannelFlags(&record, input)
	if input.AppID != nil || input.AppSecret != nil {
		if err := s.bindCredentials(ctx, &record, input); err != nil {
			return nil, err
		}
	}
	updated, err := s.store.UpdateChannelRecord(ctx, db.ChannelRecordUpdate{
		ExternalID:        record.ExternalID,
		WorkspaceUUID:     record.WorkspaceUUID,
		Name:              record.Name,
		Enabled:           record.Enabled,
		BindingStatus:     record.BindingStatus,
		AppID:             record.AppID,
		AppSecretEnvelope: record.AppSecretEnvelope,
		IncludeThinking:   record.IncludeThinking,
		IncludeToolCalls:  record.IncludeToolCalls,
		ReplyInThread:     record.ReplyInThread,
	})
	if errors.Is(err, db.ErrNotFound) {
		return nil, channelNotFound()
	}
	if err != nil {
		return nil, unavailable(err)
	}
	s.reconcile(ctx, updated)
	return s.view(ctx, updated)
}

func (s *localService) remove(ctx context.Context, principal auth.Principal, agentID, channelID string) error {
	if _, err := s.store.GetChannelRecord(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, agentID, channelID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return channelNotFound()
		}
		return unavailable(err)
	}
	if s.runtime != nil {
		s.runtime.stop(channelID)
	}
	if err := s.store.ClearChannelSessionBindings(ctx, channelID); err != nil {
		return unavailable(err)
	}
	if err := s.store.SoftDeleteChannelRecord(ctx, principal.WorkspaceUUID, channelID); err != nil {
		return unavailable(err)
	}
	return nil
}

func (s *localService) listPairings(ctx context.Context, principal auth.Principal, agentID, channelID string) ([]map[string]any, error) {
	record, err := s.store.GetChannelRecord(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, agentID, channelID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, channelNotFound()
	}
	if err != nil {
		return nil, unavailable(err)
	}
	return s.pairingViews(ctx, record)
}

func (s *localService) pairingViews(ctx context.Context, record db.ChannelRecord) ([]map[string]any, error) {
	pairings, err := s.store.ListChannelPairingRecords(ctx, record.ExternalID)
	if err != nil {
		return nil, unavailable(err)
	}
	views := make([]map[string]any, 0, len(pairings))
	for _, pairing := range pairings {
		views = append(views, map[string]any{
			"id":         pairing.ExternalID,
			"kind":       pairing.Kind,
			"target":     pairing.Target,
			"name":       pairing.DisplayName,
			"status":     pairing.Status,
			"created_at": pairing.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return views, nil
}

func (s *localService) decidePairing(ctx context.Context, principal auth.Principal, agentID, channelID, pairingID, status string) ([]map[string]any, error) {
	if status != "approved" && status != "rejected" {
		return nil, invalidPairingStatus()
	}
	record, err := s.store.GetChannelRecord(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, agentID, channelID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, channelNotFound()
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if record.Mode != "pairing" {
		return nil, pairingModeRequired()
	}
	pairings, err := s.store.ListChannelPairingRecords(ctx, record.ExternalID)
	if err != nil {
		return nil, unavailable(err)
	}
	found := false
	for _, pairing := range pairings {
		if pairing.ExternalID == pairingID {
			found = true
			break
		}
	}
	if !found {
		return nil, pairingNotFound()
	}
	if err := s.store.UpdateChannelPairingStatus(ctx, record.ExternalID, pairingID, status); err != nil {
		return nil, unavailable(err)
	}
	return s.pairingViews(ctx, record)
}

func (s *localService) environment(ctx context.Context, principal auth.Principal, agentID string) (map[string]any, error) {
	if _, err := s.agent(ctx, principal.WorkspaceUUID, agentID); err != nil {
		return nil, err
	}
	environmentID, err := s.store.GetChannelAgentEnvironment(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, agentID)
	if errors.Is(err, db.ErrNotFound) {
		return map[string]any{"environment_id": nil}, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	return map[string]any{"environment_id": environmentID}, nil
}

func (s *localService) saveEnvironment(ctx context.Context, principal auth.Principal, agentID, environmentID string) (map[string]any, error) {
	if _, err := s.agent(ctx, principal.WorkspaceUUID, agentID); err != nil {
		return nil, err
	}
	environmentID = strings.TrimSpace(environmentID)
	if environmentID == "" {
		return nil, environmentRequired()
	}
	env, err := s.store.GetEnvironment(ctx, principal.WorkspaceUUID, environmentID)
	if errors.Is(err, db.ErrNotFound) || (err == nil && env.ArchivedAt != nil) {
		return nil, invalidEnvironment(err)
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if err := s.store.SetChannelAgentEnvironment(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, agentID, environmentID); err != nil {
		return nil, unavailable(err)
	}
	s.reconcileAgent(ctx, principal, agentID)
	return map[string]any{"environment_id": environmentID}, nil
}

func (s *localService) workspaceChannels(ctx context.Context, principal auth.Principal) ([]map[string]any, error) {
	records, err := s.store.ListWorkspaceChannelRecords(ctx, principal.OrganizationUUID, principal.WorkspaceUUID)
	if err != nil {
		return nil, unavailable(err)
	}
	views := make([]map[string]any, 0, len(records))
	for _, record := range records {
		view, err := s.view(ctx, record)
		if err != nil {
			return nil, err
		}
		agent, err := s.store.GetAgent(ctx, principal.WorkspaceUUID, record.AgentExternalID)
		if err == nil {
			view["agent_id"] = agent.ExternalID
			view["agent_name"] = agent.Name
		}
		environmentID, envErr := s.store.GetChannelAgentEnvironment(ctx, record.OrganizationUUID, record.WorkspaceUUID, record.AgentExternalID)
		if envErr == nil {
			view["environment_id"] = environmentID
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *localService) reconcile(ctx context.Context, record db.ChannelRecord) {
	if s.runtime == nil {
		return
	}
	if err := s.runtime.apply(ctx, record); err != nil {
		s.logger.WarnContext(ctx, "apply channel runtime", "channel_id", record.ExternalID, "error", err)
	}
}

func (s *localService) reconcileAgent(ctx context.Context, principal auth.Principal, agentID string) {
	records, err := s.store.ListAgentChannelRecords(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, agentID)
	if err != nil {
		s.logger.WarnContext(ctx, "reconcile agent channels", "agent_id", agentID, "error", err)
		return
	}
	for _, record := range records {
		s.reconcile(ctx, record)
	}
}

func (s *localService) bindCredentials(ctx context.Context, record *db.ChannelRecord, input channelMutation) error {
	if s.cipher == nil {
		return unavailable(errors.New("secrets service is not configured"))
	}
	appID := record.AppID
	if input.AppID != nil {
		appID = strings.TrimSpace(*input.AppID)
	}
	appSecret := ""
	if input.AppSecret != nil {
		appSecret = strings.TrimSpace(*input.AppSecret)
	}
	if appSecret == "" && input.AppID == nil {
		appSecret, _ = openChannelSecret(ctx, s.cipher, *record)
	}
	if appID == "" || appSecret == "" {
		return invalidAppCredentials()
	}
	if !feishuAppIDPattern.MatchString(appID) {
		return invalidAppCredentials()
	}
	passed, err := s.credentialsMatch(ctx, appID, appSecret)
	if err != nil {
		return credentialCheckUnavailable(err)
	}
	if !passed {
		return invalidAppCredentials()
	}
	envelope, err := sealChannelSecret(ctx, s.cipher, *record, appSecret)
	if err != nil {
		return unavailable(err)
	}
	record.AppID = appID
	record.BindingStatus = "bound"
	record.AppSecretEnvelope = envelope
	return nil
}

func (s *localService) credentialsMatch(ctx context.Context, appID, appSecret string) (bool, error) {
	return matchFeishuCredentials(ctx, s.client, s.tokenURL, appID, appSecret)
}

func applyChannelFlags(record *db.ChannelRecord, input channelMutation) {
	if input.Enabled != nil {
		record.Enabled = *input.Enabled
	}
	if input.IncludeThinking != nil {
		record.IncludeThinking = *input.IncludeThinking
	}
	if input.IncludeToolCalls != nil {
		record.IncludeToolCalls = *input.IncludeToolCalls
	}
	if input.ReplyInThread != nil {
		record.ReplyInThread = *input.ReplyInThread
	}
}

func sealChannelSecret(ctx context.Context, cipher *secrets.Service, record db.ChannelRecord, plaintext string) (*string, error) {
	envelope, err := cipher.Seal(ctx, secrets.Binding{
		OrganizationUUID:     record.OrganizationUUID,
		WorkspaceUUID:        record.WorkspaceUUID,
		VaultExternalID:      "channel-runtime",
		CredentialExternalID: record.ExternalID,
	}, []byte(plaintext))
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	value := string(encoded)
	return &value, nil
}

func openSealedSecret(ctx context.Context, cipher *secrets.Service, record db.ChannelRecord, encoded string) (string, error) {
	var envelope secrets.Envelope
	if err := json.Unmarshal([]byte(encoded), &envelope); err != nil {
		return "", err
	}
	plaintext, err := cipher.Open(ctx, secrets.Binding{
		OrganizationUUID:     record.OrganizationUUID,
		WorkspaceUUID:        record.WorkspaceUUID,
		VaultExternalID:      "channel-runtime",
		CredentialExternalID: record.ExternalID,
	}, envelope)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func channelNotFound() error {
	return apperr.New(apperr.NotFound, "渠道不存在", nil)
}

func invalidChannelName() error {
	return apperr.New(apperr.InvalidArgument, "名称必填，最多 40 字", nil)
}

func invalidChannelMode() error {
	return apperr.New(apperr.InvalidArgument, "请选择渠道模式", nil)
}

func immutableChannelMode() error {
	return apperr.New(apperr.Conflict, "渠道创建后不可切换模式", nil)
}

func invalidPairingStatus() error {
	return apperr.New(apperr.InvalidArgument, "配对状态无效", nil)
}

func pairingModeRequired() error {
	return apperr.New(apperr.Conflict, "此渠道不使用配对模式", nil)
}

func pairingNotFound() error {
	return apperr.New(apperr.NotFound, "配对申请不存在", nil)
}

func channelMountsUnsupported() error {
	return apperr.New(apperr.InvalidArgument, "当前 OMA 原生渠道暂不支持挂载 Memory、Vault 或 Git 仓库，请先在飞书渠道中取消这些挂载", nil)
}

func channelCapabilityUnsupported() error {
	return apperr.New(apperr.InvalidState, "当前 OMA 原生渠道暂不支持该操作", nil)
}

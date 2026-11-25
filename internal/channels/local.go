package channels

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

const channelNameLimit = 40

type localStore interface {
	EnsureChannelWorkspaceCredential(context.Context, platform.CreateConsoleAPIKeyInput, *secrets.Service) (string, error)
	GetAgent(context.Context, string, string) (db.Agent, error)
	GetAgentVersion(context.Context, string, string, int) (db.Agent, error)
	GetMemoryStore(context.Context, string, string) (db.MemoryStore, error)
	GetVault(context.Context, string, string) (db.Vault, error)
	GetFile(context.Context, string, string) (db.FileRecord, error)
	DeleteChannelSessionBinding(context.Context, string, string) error
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
	CreateChannelRegistration(context.Context, db.ChannelRegistrationInput) (db.ChannelRegistrationRecord, error)
	GetChannelRecordByID(context.Context, string, string, string) (db.ChannelRecord, error)
	GetChannelRegistration(context.Context, string, string, string) (db.ChannelRegistrationRecord, error)
	GetChannelQrRegistration(context.Context, string) (db.ChannelRegistrationRecord, error)
	UpdateChannelRegistration(context.Context, db.ChannelRegistrationUpdate) (db.ChannelRegistrationRecord, error)
	TransitionChannelRegistration(context.Context, db.ChannelRegistrationUpdate) (db.ChannelRegistrationRecord, error)
	DeleteChannelRegistration(context.Context, string) error
}

type localService struct {
	store            localStore
	cipher           *secrets.Service
	runtime          *runtimeSupervisor
	logger           *slog.Logger
	client           *http.Client
	tokenURL         string
	instanceID       string
	registrar        appRegistrar
	validAppID       func(string) bool
	checkCredentials func(context.Context, *http.Client, string, string, string) (bool, error)
	runMu            sync.Mutex
	runs             map[string]*registrationRun
	ingress          identityIngress
}

type channelMutation struct {
	PairingProvider    *string         `json:"pairing_oauth_provider"`
	AgentVersion       json.RawMessage `json:"agent_version"`
	EnvironmentID      *string         `json:"environment_id"`
	Name               *string         `json:"name"`
	Mode               *string         `json:"mode"`
	AppID              *string         `json:"app_id"`
	AppSecret          *string         `json:"app_secret"`
	Enabled            *bool           `json:"enabled"`
	IncludeThinking    *bool           `json:"include_thinking"`
	IncludeToolCalls   *bool           `json:"include_tool_calls"`
	ReplyInThread      *bool           `json:"reply_in_thread"`
	Status             *string         `json:"status"`
	AuthorizationID    *string         `json:"authorization_id"`
	MemoryStoreID      json.RawMessage `json:"memory_store_id"`
	VaultID            json.RawMessage `json:"vault_id"`
	VaultIDs           json.RawMessage `json:"vault_ids"`
	GitRepository      json.RawMessage `json:"git_repository"`
	Resources          json.RawMessage `json:"resources"`
	IdentityID         *string         `json:"identity_id"`
	IdentityResolution json.RawMessage `json:"identity_resolution"`
}

type localHandler struct {
	service *localService
	logger  *slog.Logger
}

func newLocalService(store localStore, cipher *secrets.Service, runtime *runtimeSupervisor, logger *slog.Logger, deps Dependencies) *localService {
	return &localService{
		store:     store,
		cipher:    cipher,
		runtime:   runtime,
		logger:    logging.LoggerOrDefault(logger),
		client:    &http.Client{Timeout: 10 * time.Second},
		tokenURL:  deps.TokenURL,
		registrar: deps.Registrar, validAppID: deps.ValidAppID, checkCredentials: deps.CheckCredentials,
		runs:       make(map[string]*registrationRun),
		instanceID: uuid.NewString(),
	}
}

func (s *localService) Stop() {
	s.stopRegistrations()
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
	view := map[string]any{
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
		"created_at":         record.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	for key, value := range channelMountView(record) {
		view[key] = value
	}
	if err := s.attachExecution(ctx, view, record); err != nil {
		return nil, err
	}
	return view, nil
}

func (s *localService) create(ctx context.Context, principal auth.Principal, agentID string, input channelMutation) (map[string]any, error) {
	agent, err := s.agent(ctx, principal.WorkspaceUUID, agentID)
	if err != nil {
		return nil, err
	}
	if agent.ArchivedAt != nil {
		return nil, archivedAgent()
	}
	if input.EnvironmentID != nil {
		id, err := s.validatedEnvironment(ctx, principal.WorkspaceUUID, *input.EnvironmentID)
		if err != nil {
			return nil, err
		}
		input.EnvironmentID = new(id)
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
	if err := s.prepareExecution(ctx, principal, &record, input); err != nil {
		return nil, err
	}
	applyChannelFlags(&record, input)
	if err := s.applyChannelMounts(ctx, principal, &record, input); err != nil {
		return nil, err
	}
	if input.AuthorizationID != nil {
		if err := s.applyAuthorization(ctx, principal, &record, *input.AuthorizationID); err != nil {
			return nil, err
		}
	}
	if input.AppID != nil || input.AppSecret != nil {
		if err := s.bindCredentials(ctx, &record, input); err != nil {
			return nil, err
		}
	}
	if err := s.prepareCredential(ctx, principal); err != nil {
		return nil, err
	}
	created, err := s.store.CreateChannelRecord(ctx, db.ChannelRecordInput{
		EnvironmentID:     input.EnvironmentID,
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
		ExecutionMode:     record.ExecutionMode,
		IdentityUUID:      record.IdentityUUID,
		Mounts:            record.Mounts,
		GitTokenEnvelope:  record.GitTokenEnvelope,
	})
	if errors.Is(err, db.ErrNotFound) {
		return nil, agentNotFound()
	}
	if err != nil {
		return nil, channelRecordWriteError(err)
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
	if input.EnvironmentID != nil {
		id, err := s.validatedEnvironment(ctx, principal.WorkspaceUUID, *input.EnvironmentID)
		if err != nil {
			return nil, err
		}
		input.EnvironmentID = new(id)
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
	if err := s.prepareExecution(ctx, principal, &record, input); err != nil {
		return nil, err
	}
	applyChannelFlags(&record, input)
	if err := s.applyChannelMounts(ctx, principal, &record, input); err != nil {
		return nil, err
	}
	if input.AuthorizationID != nil {
		if err := s.applyAuthorization(ctx, principal, &record, *input.AuthorizationID); err != nil {
			return nil, err
		}
	}
	if input.AppID != nil || input.AppSecret != nil {
		if err := s.bindCredentials(ctx, &record, input); err != nil {
			return nil, err
		}
	}
	if err := s.prepareCredential(ctx, principal); err != nil {
		return nil, err
	}
	updated, err := s.store.UpdateChannelRecord(ctx, db.ChannelRecordUpdate{
		EnvironmentID:     input.EnvironmentID,
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
		ExecutionMode:     record.ExecutionMode,
		IdentityUUID:      record.IdentityUUID,
		Mounts:            record.Mounts,
		GitTokenEnvelope:  record.GitTokenEnvelope,
	})
	if errors.Is(err, db.ErrNotFound) {
		return nil, channelNotFound()
	}
	if err != nil {
		return nil, channelRecordWriteError(err)
	}
	s.reconcile(ctx, updated)
	return s.view(ctx, updated)
}

func (s *localService) prepareCredential(ctx context.Context, principal auth.Principal) error {
	var creator *string
	if principal.UserUUID != "" {
		creator = new(principal.UserUUID)
	}
	_, err := s.store.EnsureChannelWorkspaceCredential(ctx, platform.CreateConsoleAPIKeyInput{
		OrgUUID: principal.OrganizationUUID, WorkspaceUUID: principal.WorkspaceUUID,
		CreatedByUserUUID: creator, RuntimeUserUUID: principal.RuntimeUserUUID(),
	}, s.cipher)
	return channelCredentialError(err)
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
	if registration, err := s.store.GetChannelQrRegistration(ctx, channelID); err == nil {
		s.cancelRun(registration.ExternalID)
		if err := s.store.DeleteChannelRegistration(ctx, registration.ExternalID); err != nil {
			return unavailable(err)
		}
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
	environmentID, err := s.validatedEnvironment(ctx, principal.WorkspaceUUID, environmentID)
	if err != nil {
		return nil, err
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
		var err error
		appSecret, err = openChannelSecret(ctx, s.cipher, *record)
		if err != nil {
			return unavailable(err)
		}
	}
	if appID == "" || appSecret == "" {
		return invalidAppCredentials()
	}
	if !s.validAppID(appID) {
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
	return s.checkCredentials(ctx, s.client, s.tokenURL, appID, appSecret)
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
	return sealRuntimeSecret(ctx, cipher, record.OrganizationUUID, record.WorkspaceUUID, "channel-runtime", record.ExternalID, plaintext)
}

func sealRuntimeSecret(ctx context.Context, cipher *secrets.Service, organizationUUID, workspaceUUID, vault, key, plaintext string) (*string, error) {
	envelope, err := cipher.Seal(ctx, secrets.Binding{
		OrganizationUUID:     organizationUUID,
		WorkspaceUUID:        workspaceUUID,
		VaultExternalID:      vault,
		CredentialExternalID: key,
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
	return openRuntimeSecret(ctx, cipher, record.OrganizationUUID, record.WorkspaceUUID, "channel-runtime", record.ExternalID, encoded)
}

func openRuntimeSecret(ctx context.Context, cipher *secrets.Service, organizationUUID, workspaceUUID, vault, key, encoded string) (string, error) {
	var envelope secrets.Envelope
	if err := json.Unmarshal([]byte(encoded), &envelope); err != nil {
		return "", err
	}
	plaintext, err := cipher.Open(ctx, secrets.Binding{
		OrganizationUUID:     organizationUUID,
		WorkspaceUUID:        workspaceUUID,
		VaultExternalID:      vault,
		CredentialExternalID: key,
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

func registrationUnavailable(cause error) error {
	return apperr.New(apperr.Unavailable, "无法连接飞书生成授权二维码，请稍后重试", cause)
}

func authorizationNotFound() error {
	return apperr.New(apperr.NotFound, "飞书授权不存在或已过期", nil)
}

func authorizationPending() error {
	return apperr.New(apperr.Conflict, "请先完成飞书授权", nil)
}

func (s *localService) validatedEnvironment(ctx context.Context, workspaceUUID, raw string) (string, error) {
	environmentID := strings.TrimSpace(raw)
	if environmentID == "" {
		return "", environmentRequired()
	}
	env, err := s.store.GetEnvironment(ctx, workspaceUUID, environmentID)
	if errors.Is(err, db.ErrNotFound) || (err == nil && env.ArchivedAt != nil) {
		return "", invalidEnvironment(err)
	}
	if err != nil {
		return "", unavailable(err)
	}
	return environmentID, nil
}

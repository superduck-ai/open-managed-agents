package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

// ManagedAgentCreateInput 汇总为 managed agent 创建 code session 和签发 sandbox 凭证所需的上下文。
type ManagedAgentCreateInput struct {
	Session                    db.Session
	Environment                db.Environment
	EnvironmentWork            db.EnvironmentWork
	Model                      string
	Title                      string
	WorkDir                    string
	PermissionMode             string
	DangerouslySkipPermissions bool
	Config                     json.RawMessage
	InitialEvents              []json.RawMessage
	EnvironmentWorkMetadata    json.RawMessage
}

// ManagedAgentCreateResult 只在创建链路内短暂携带两份明文凭证，调用方应立即交给
// environment-manager 的文件描述符合同，不能写入数据库或 session metadata。
type ManagedAgentCreateResult struct {
	CodeSessionID       string
	PublicSessionID     string
	SDKURLPath          string
	OAuthAccessToken    string
	SessionIngressToken string
	EnvironmentWork     db.EnvironmentWork
}

// CreateManagedAgentCodeSession 原子地建立 code-session 身份上下文，并为 sandbox
// 分别签发 Messages OAuth-compatible token 与 worker session-ingress JWT。
func (s *Service) CreateManagedAgentCodeSession(ctx context.Context, input ManagedAgentCreateInput) (ManagedAgentCreateResult, error) {
	if input.EnvironmentWork.ID <= 0 {
		return ManagedAgentCreateResult{}, errors.New("managed agent environment work is required")
	}
	codeSessionID, err := ids.New("cse_")
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	oauthAccessToken, err := newOAuthCompatibleToken()
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	now := time.Now().UTC()
	metadata, err := managedAgentCodeSessionMetadata(input)
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	events, err := managedAgentInitialInboundEvents(codeSessionID, input.Config, input.InitialEvents, now)
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	runtimeMetadata := map[string]any{
		"claude_code_session_id":        codeSessionID,
		"claude_code_public_session_id": input.Session.ExternalID,
		"claude_code_sdk_url_path":      "/v1/code/sessions/" + codeSessionID,
		"runtime":                       "claude_code_local",
	}
	sessionMetadataPatch, err := marshalRaw(runtimeMetadata)
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	workMetadata, err := mergeManagedAgentRuntimeMetadata(input.EnvironmentWorkMetadata, runtimeMetadata)
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	var sessionIngressToken string
	created, err := s.db.CreateManagedAgentRuntime(ctx, db.CreateManagedAgentRuntimeInput{
		CodeSession: db.CreateCodeSessionInput{
			ExternalID:            codeSessionID,
			OrganizationID:        input.Session.OrganizationID,
			WorkspaceID:           input.Session.WorkspaceID,
			SessionID:             input.Session.ID,
			SessionExternalID:     input.Session.ExternalID,
			EnvironmentID:         input.Environment.ID,
			EnvironmentExternalID: input.Environment.ExternalID,
			WorkDir:               strings.TrimSpace(input.WorkDir),
			PermissionMode:        strings.TrimSpace(input.PermissionMode),
			Model:                 strings.TrimSpace(input.Model),
			Status:                "active",
			Metadata:              metadata,
			// OAuth-compatible token 只落 SHA-256 hash；明文仅存在于当前返回值中。
			OAuthAccessTokenHash: auth.HashAPIKey(oauthAccessToken),
			CreatedAt:            now,
		},
		InboundEvents:           events,
		SessionMetadataPatch:    sessionMetadataPatch,
		EnvironmentWorkMetadata: workMetadata,
		EnvironmentExternalID:   input.Environment.ExternalID,
		WorkExternalID:          input.EnvironmentWork.ExternalID,
	}, func(credentialContext db.CodeSessionCredentialContext) error {
		var issueErr error
		sessionIngressToken, issueErr = s.issueSessionIngressToken(credentialContext)
		return issueErr
	})
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	return ManagedAgentCreateResult{
		CodeSessionID:       created.CodeSession.ExternalID,
		PublicSessionID:     created.CodeSession.SessionExternalID,
		SDKURLPath:          "/v1/code/sessions/" + created.CodeSession.ExternalID,
		OAuthAccessToken:    oauthAccessToken,
		SessionIngressToken: sessionIngressToken,
		EnvironmentWork:     created.EnvironmentWork,
	}, nil
}

func managedAgentInitialInboundEvents(codeSessionID string, configRaw json.RawMessage, publicEvents []json.RawMessage, now time.Time) ([]db.AppendCodeSessionEventInput, error) {
	initialize, err := managedAgentInitializePayload(codeSessionID, configRaw, now)
	if err != nil {
		return nil, err
	}
	payloads := initialPublicSessionWorkerPayloads(codeSessionID, publicEvents, now)
	inputs := make([]db.AppendCodeSessionEventInput, 0, len(payloads)+1)
	initializeInput, err := buildInboundEventInput(codeSessionID, initialize, "internal", now)
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, initializeInput)
	for _, payload := range payloads {
		input, err := buildInboundEventInput(codeSessionID, payload, "public-session", now)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

func mergeManagedAgentRuntimeMetadata(raw json.RawMessage, patch map[string]any) (json.RawMessage, error) {
	metadata := map[string]any{}
	if len(raw) > 0 && strings.TrimSpace(string(raw)) != "null" {
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return nil, err
		}
	}
	for key, value := range patch {
		metadata[key] = value
	}
	return marshalRaw(metadata)
}

func managedAgentCodeSessionMetadata(input ManagedAgentCreateInput) (json.RawMessage, error) {
	// metadata 只记录非秘密运行信息，两份明文凭证都不进入 JSON。
	return marshalRaw(map[string]any{
		"source":                         "managed_agents_local",
		"public_session_id":              input.Session.ExternalID,
		"environment_id":                 input.Environment.ExternalID,
		"title":                          input.Title,
		"config":                         rawObject(input.Config),
		"dangerously_skip_permissions":   input.DangerouslySkipPermissions,
		"managed_agent_session_work_dir": strings.TrimSpace(input.WorkDir),
	})
}

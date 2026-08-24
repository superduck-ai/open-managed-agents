package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestManagedAgentRuntimeMapperBuilderContracts(t *testing.T) {
	sessionParams, workParams := managedAgentRuntimeMapperTestParams()
	rotateParams := managedAgentRuntimeRotateParams(sessionParams)
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"merge session metadata", mapperBuilderContract{
			statement: sessionMapperMergeMetadataStatement,
			bound:     buildSessionMapperMergeMetadata(yourbatis.DialectPostgres, sessionParams),
			wantID:    "SessionMapper.MergeMetadata", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.MetadataPatch", "params.OrganizationUUID", "params.WorkspaceUUID", "params.SessionExternalID",
			},
			wantSensitiveArgumentNames: []string{"params.MetadataPatch"},
			wantSQLFragments:           []string{"UPDATE sessions", "CAST($1 AS jsonb)", "organization_uuid = $2"},
		}},
		{"merge work runtime metadata", mapperBuilderContract{
			statement: environmentWorkMapperMergeMetadataStatement,
			bound:     buildEnvironmentWorkMapperMergeMetadata(yourbatis.DialectPostgres, workParams),
			wantID:    "EnvironmentWorkMapper.MergeMetadata", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.MetadataPatch", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.EnvironmentUUID", "params.EnvironmentExternalID", "params.WorkExternalID",
			},
			wantSensitiveArgumentNames: []string{"params.MetadataPatch"},
			wantSQLFragments:           []string{"UPDATE environment_work", "CAST($1 AS jsonb)", "environment_uuid = $4"},
		}},
		{"terminate code session", mapperBuilderContract{
			statement: codeSessionMapperTerminateByExternalIDStatement,
			bound: buildCodeSessionMapperTerminateByExternalID(
				yourbatis.DialectPostgres,
				sessionParams.OrganizationUUID,
				sessionParams.WorkspaceUUID,
				"codeses_test",
			),
			wantID: "CodeSessionMapper.TerminateByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "codeSessionExternalID"},
			wantSQLFragments:  []string{"UPDATE code_sessions", "oauth_access_token_hash = NULL", "external_id = $3"},
		}},
		{"rotate code session credentials", mapperBuilderContract{
			statement: codeSessionMapperRotateCredentialsStatement,
			bound:     buildCodeSessionMapperRotateCredentials(yourbatis.DialectPostgres, rotateParams),
			wantID:    "CodeSessionMapper.RotateCredentials", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.OAuthAccessTokenHash", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.SessionExternalID", "params.CodeSessionExternalID",
			},
			wantSensitiveArgumentNames: []string{"params.OAuthAccessTokenHash"},
			wantSQLFragments: []string{
				"UPDATE code_sessions", "current_worker_epoch = current_worker_epoch + 1",
				"worker_lease_expires_at = NULL", "status = 'active'", "RETURNING",
			},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}
}

func TestManagedAgentRuntimeMapperRowsAffected(t *testing.T) {
	ctx := context.Background()
	sessionParams, workParams := managedAgentRuntimeMapperTestParams()
	tests := []struct {
		name        string
		statementID string
		wantValues  []any
		call        func(yourbatis.Executor) (int64, error)
	}{
		{
			name: "session", statementID: "SessionMapper.MergeMetadata",
			wantValues: []any{
				sessionParams.MetadataPatch, sessionParams.OrganizationUUID,
				sessionParams.WorkspaceUUID, sessionParams.SessionExternalID,
			},
			call: func(executor yourbatis.Executor) (int64, error) {
				mapper := NewSessionMapper(executor)
				return mapper.MergeMetadata(ctx, sessionParams)
			},
		},
		{
			name: "work", statementID: "EnvironmentWorkMapper.MergeMetadata",
			wantValues: []any{
				workParams.MetadataPatch, workParams.OrganizationUUID, workParams.WorkspaceUUID,
				workParams.EnvironmentUUID, workParams.EnvironmentExternalID, workParams.WorkExternalID,
			},
			call: func(executor yourbatis.Executor) (int64, error) {
				mapper := NewEnvironmentWorkMapper(executor)
				return mapper.MergeMetadata(ctx, workParams)
			},
		},
		{
			name: "code session", statementID: "CodeSessionMapper.TerminateByExternalID",
			wantValues: []any{sessionParams.OrganizationUUID, sessionParams.WorkspaceUUID, "codeses_test"},
			call: func(executor yourbatis.Executor) (int64, error) {
				mapper := NewCodeSessionMapper(executor)
				return mapper.TerminateByExternalID(
					ctx,
					sessionParams.OrganizationUUID,
					sessionParams.WorkspaceUUID,
					"codeses_test",
				)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
			rowsAffected, err := test.call(executor)
			if err != nil || rowsAffected != 1 {
				t.Fatalf("mapper call = (%d, %v), want (1, nil)", rowsAffected, err)
			}
			assertMapperTestExecution(t, executor, test.statementID, yourbatis.StatementUpdate, test.wantValues)
		})
	}
}

func TestManagedAgentRuntimeMapperRotateCredentials(t *testing.T) {
	ctx := context.Background()
	sessionParams, _ := managedAgentRuntimeMapperTestParams()
	rotateParams := managedAgentRuntimeRotateParams(sessionParams)
	executor := newMapperTestExecutor(t, mapperTestResponse{
		columns: []string{"current_worker_epoch"},
		rows:    [][]driver.Value{{int64(2)}},
	})

	got, err := NewCodeSessionMapper(executor).RotateCredentials(ctx, rotateParams)
	if err != nil || got != 2 {
		t.Fatalf("RotateCredentials() = (%d, %v), want epoch 2", got, err)
	}
	assertMapperTestExecution(
		t,
		executor,
		"CodeSessionMapper.RotateCredentials",
		yourbatis.StatementUpdate,
		[]any{
			rotateParams.OAuthAccessTokenHash, rotateParams.OrganizationUUID, rotateParams.WorkspaceUUID,
			rotateParams.SessionExternalID, rotateParams.CodeSessionExternalID,
		},
	)
}

func TestManagedAgentRuntimeMapperExecutionErrors(t *testing.T) {
	sessionParams, workParams := managedAgentRuntimeMapperTestParams()
	tests := []struct {
		name string
		call func(yourbatis.Executor) error
	}{
		{"session", func(executor yourbatis.Executor) error {
			mapper := NewSessionMapper(executor)
			_, err := mapper.MergeMetadata(context.Background(), sessionParams)
			return err
		}},
		{"work", func(executor yourbatis.Executor) error {
			mapper := NewEnvironmentWorkMapper(executor)
			_, err := mapper.MergeMetadata(context.Background(), workParams)
			return err
		}},
		{"code session", func(executor yourbatis.Executor) error {
			mapper := NewCodeSessionMapper(executor)
			_, err := mapper.TerminateByExternalID(
				context.Background(),
				sessionParams.OrganizationUUID,
				sessionParams.WorkspaceUUID,
				"codeses_test",
			)
			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executionErr := errors.New("execution failed")
			executor := newMapperTestExecutor(t, mapperTestResponse{execErr: executionErr})
			if err := test.call(executor); !errors.Is(err, executionErr) {
				t.Fatalf("mapper error = %v, want %v", err, executionErr)
			}
		})
	}

	t.Run("rotate credentials", func(t *testing.T) {
		executionErr := errors.New("execution failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: executionErr})
		_, err := NewCodeSessionMapper(executor).RotateCredentials(
			context.Background(),
			managedAgentRuntimeRotateParams(sessionParams),
		)
		if !errors.Is(err, executionErr) {
			t.Fatalf("mapper error = %v, want %v", err, executionErr)
		}
	})
}

func managedAgentRuntimeMapperTestParams() (sessionMetadataPatchParams, environmentWorkMetadataPatchParams) {
	metadataPatch := []byte(`{"runtime":"claude_code_local"}`)
	sessionParams := sessionMetadataPatchParams{
		OrganizationUUID:  "00000000-0000-4000-8000-000000000001",
		WorkspaceUUID:     "00000000-0000-4000-8000-000000000002",
		SessionExternalID: "ses_test", MetadataPatch: metadataPatch,
	}
	return sessionParams, environmentWorkMetadataPatchParams{
		OrganizationUUID: sessionParams.OrganizationUUID, WorkspaceUUID: sessionParams.WorkspaceUUID,
		EnvironmentUUID:       "00000000-0000-4000-8000-000000000003",
		EnvironmentExternalID: "env_test", WorkExternalID: "envwork_test",
		MetadataPatch: metadataPatch,
	}
}

func managedAgentRuntimeRotateParams(sessionParams sessionMetadataPatchParams) rotateCodeSessionCredentialsParams {
	return rotateCodeSessionCredentialsParams{
		OrganizationUUID:      sessionParams.OrganizationUUID,
		WorkspaceUUID:         sessionParams.WorkspaceUUID,
		SessionExternalID:     sessionParams.SessionExternalID,
		CodeSessionExternalID: "codeses_test",
		OAuthAccessTokenHash:  "oauth-hash",
	}
}

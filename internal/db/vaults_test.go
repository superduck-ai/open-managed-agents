package db

import (
	"context"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestVaultBatchMappersPropagateExecutionErrors(t *testing.T) {
	ctx := context.Background()
	contracts := []mapperExecutionErrorContract{
		{
			statementID: "VaultMapper.ListActiveByExternalIDs",
			kind:        yourbatis.StatementSelect,
			query:       true,
			call: func(executor yourbatis.Executor) error {
				_, err := NewVaultMapper(executor).ListActiveByExternalIDs(ctx, "workspace", []string{"vault_a"})
				return err
			},
		},
		{
			statementID: "VaultCredentialMapper.ListActiveByVaultUUIDs",
			kind:        yourbatis.StatementSelect,
			query:       true,
			call: func(executor yourbatis.Executor) error {
				_, err := NewVaultCredentialMapper(executor).ListActiveByVaultUUIDs(ctx, "workspace", []string{"vault-uuid"})
				return err
			},
		},
	}
	for _, contract := range contracts {
		t.Run(contract.statementID, func(t *testing.T) {
			assertMapperExecutionError(t, contract)
		})
	}
}

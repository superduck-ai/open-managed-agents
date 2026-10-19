package db

import (
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestPlatformAdminMapperAuthorizationBindings(t *testing.T) {
	grant := PlatformAdminGrant{AccountUUID: "account", Operator: "运维身份"}
	account := mapperTestResponse{columns: []string{"uuid", "email", "name", "granted"}, rows: [][]driver.Value{{"account", "a@example.com", "账号", true}}}
	for _, tc := range []struct {
		name      string
		call      func(PlatformAdminMapper) error
		response  mapperTestResponse
		args      []any
		kind      yourbatis.StatementKind
		fragments []string
	}{
		{"FindAccount", func(m PlatformAdminMapper) error { _, err := m.FindAccount(t.Context(), grant.AccountUUID); return err }, account, []any{grant.AccountUUID}, yourbatis.StatementSelect, []string{"u.uuid = $1 AND u.uuid = (SELECT identity.uuid", "a.revoked_at IS NULL"}},
		{"ListAdmins", func(m PlatformAdminMapper) error { _, err := m.ListAdmins(t.Context()); return err }, account, []any{}, yourbatis.StatementSelect, []string{"JOIN platform_admins a ON a.account_uuid = u.uuid AND a.revoked_at IS NULL", "ORDER BY u.uuid"}},
		{"LockGrant", func(m PlatformAdminMapper) error { _, err := m.LockGrant(t.Context(), grant.AccountUUID); return err }, mapperTestResponse{columns: []string{"granted"}, rows: [][]driver.Value{{true}}}, []any{grant.AccountUUID}, yourbatis.StatementSelect, []string{"revoked_at IS NULL AS granted", "account_uuid = $1 FOR UPDATE"}},
		{"LockAdministration", func(m PlatformAdminMapper) error { _, err := m.LockAdministration(t.Context()); return err }, mapperTestResponse{columns: []string{"locked"}, rows: [][]driver.Value{{int64(1)}}}, []any{}, yourbatis.StatementSelect, []string{"pg_advisory_xact_lock(734001928)"}},
		{"CountAdmins", func(m PlatformAdminMapper) error { _, err := m.CountAdmins(t.Context()); return err }, mapperTestResponse{columns: []string{"count"}, rows: [][]driver.Value{{int64(1)}}}, []any{}, yourbatis.StatementSelect, []string{"COUNT(*) AS count FROM platform_admins WHERE revoked_at IS NULL"}},
		{"Grant", func(m PlatformAdminMapper) error { return m.Grant(t.Context(), grant) }, mapperTestResponse{}, []any{grant.AccountUUID, grant.Operator}, yourbatis.StatementInsert, []string{"VALUES ($1, $2)", "ON CONFLICT (account_uuid) DO UPDATE", "revoked_at = NULL, revoked_by = NULL"}},
		{"Revoke", func(m PlatformAdminMapper) error { return m.Revoke(t.Context(), grant) }, mapperTestResponse{}, []any{grant.Operator, grant.AccountUUID}, yourbatis.StatementUpdate, []string{"revoked_at = now(), revoked_by = $1 WHERE account_uuid = $2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := errors.New("执行失败")
			executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: failure, execErr: failure})
			if err := tc.call(NewPlatformAdminMapper(executor)); !errors.Is(err, failure) {
				t.Fatalf("未保留执行错误：%v", err)
			}
			executor = newMapperTestExecutor(t, tc.response)
			if err := tc.call(NewPlatformAdminMapper(executor)); err != nil {
				t.Fatal(err)
			}
			assertMapperTestExecution(t, executor, "PlatformAdminMapper."+tc.name, tc.kind, tc.args, tc.fragments...)
		})
	}
}

func TestPlatformAdminMapperAccountPagination(t *testing.T) {
	for _, cursor := range []string{"", "next-account"} {
		query := PlatformAdminPageQuery{Query: "search'payload", After: cursor, Limit: 21}
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid", "email", "name", "granted"}})
		if _, err := NewPlatformAdminMapper(executor).ListAccounts(t.Context(), query); err != nil {
			t.Fatal(err)
		}
		args := []any{query.Query, query.Query}
		fragments := []string{"ORDER BY identity.registration_identity DESC, identity.added_at ASC, identity.uuid ASC LIMIT 1", "u.email ILIKE '%' || $1 || '%' OR u.name ILIKE '%' || $2 || '%'", "ORDER BY u.uuid LIMIT"}
		if cursor != "" {
			args = append(args, cursor)
			fragments = append(fragments, "u.uuid > $3")
		}
		args = append(args, query.Limit)
		assertMapperTestExecution(t, executor, "PlatformAdminMapper.ListAccounts", yourbatis.StatementSelect, args, fragments...)
	}
}

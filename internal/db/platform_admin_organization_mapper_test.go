package db

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestPlatformAdminOrganizationMapperBindings(t *testing.T) {
	member := mapperTestResponse{columns: []string{"uuid", "email", "name", "role"}, rows: [][]driver.Value{{"member", "a@example.com", "成员", "admin"}}}
	organization := mapperTestResponse{columns: []string{"uuid", "name", "website", "industry", "management_version", "created_at", "member_count", "admin_count", "workspace_count"}, rows: [][]driver.Value{{"org", "组织", "", "", int64(2), time.Now(), int64(3), int64(1), int64(2)}}}
	patch := PlatformAdminOrganizationPatch{OrganizationUUID: "org", Name: new("组织'名称"), Website: new(""), Industry: nil}
	input := PlatformAdminMemberInput{UUID: "member", ExternalID: "user_member", OrganizationUUID: "org", Email: "a@example.com", Name: "成员", Role: "admin"}
	request := PlatformAdminOrganizationInput{Name: "组织", AccountUUID: "account", Key: "request-key", Hash: "request-hash"}
	for _, tc := range []struct {
		name      string
		call      func(PlatformAdminOrganizationMapper) error
		response  mapperTestResponse
		args      []any
		kind      yourbatis.StatementKind
		fragments []string
	}{
		{"Exists", func(m PlatformAdminOrganizationMapper) error { _, err := m.Exists(t.Context(), "org"); return err }, mapperTestResponse{columns: []string{"found"}, rows: [][]driver.Value{{true}}}, []any{"org"}, yourbatis.StatementSelect, []string{"SELECT EXISTS (SELECT 1 FROM organizations WHERE uuid = $1)"}},
		{"Find", func(m PlatformAdminOrganizationMapper) error { _, err := m.Find(t.Context(), "org"); return err }, organization, []any{"org"}, yourbatis.StatementSelect, []string{"FROM organizations o WHERE o.uuid = $1", "u.organization_uuid = o.uuid AND u.deleted_at IS NULL AND u.role = 'admin'"}},
		{"Lock", func(m PlatformAdminOrganizationMapper) error { _, err := m.Lock(t.Context(), "org"); return err }, mapperTestResponse{columns: []string{"management_version"}, rows: [][]driver.Value{{int64(2)}}}, []any{"org"}, yourbatis.StatementSelect, []string{"WHERE uuid = $1 FOR UPDATE"}},
		{"Update", func(m PlatformAdminOrganizationMapper) error { return m.Update(t.Context(), patch) }, mapperTestResponse{}, []any{patch.Name, patch.Website, patch.Industry, "org"}, yourbatis.StatementUpdate, []string{"name = COALESCE($1, name)", "jsonb_strip_nulls(jsonb_build_object('website', CAST($2 AS text), 'industry', CAST($3 AS text)))", "WHERE uuid = $4"}},
		{"FindMember", func(m PlatformAdminOrganizationMapper) error {
			_, err := m.FindMember(t.Context(), "org", "member")
			return err
		}, member, []any{"org", "member"}, yourbatis.StatementSelect, []string{"organization_uuid = $1 AND uuid = $2 AND deleted_at IS NULL"}},
		{"FindMemberByEmail", func(m PlatformAdminOrganizationMapper) error {
			_, _, err := m.FindMemberByEmail(t.Context(), "org", "a@example.com")
			return err
		}, member, []any{"org", "a@example.com"}, yourbatis.StatementSelect, []string{"organization_uuid = $1 AND lower(email) = lower($2) AND deleted_at IS NULL"}},
		{"InsertMember", func(m PlatformAdminOrganizationMapper) error { return m.InsertMember(t.Context(), input) }, mapperTestResponse{}, []any{input.UUID, input.ExternalID, input.OrganizationUUID, input.Email, input.Name, input.Role}, yourbatis.StatementInsert, []string{"organization_uuid, email, name, role, registration_identity)", "VALUES ($1, $2, $3, $4, $5, $6, false)"}},
		{"SetMemberRole", func(m PlatformAdminOrganizationMapper) error {
			return m.SetMemberRole(t.Context(), "org", "member", "developer")
		}, mapperTestResponse{}, []any{"developer", "org", "member"}, yourbatis.StatementUpdate, []string{"role = $1", "organization_uuid = $2 AND uuid = $3 AND deleted_at IS NULL"}},
		{"FindCreation", func(m PlatformAdminOrganizationMapper) error {
			row, err := m.FindCreation(t.Context(), request.AccountUUID, request.Key)
			if err == nil && (row.UUID != "org" || row.Hash != request.Hash) {
				t.Fatalf("创建信息扫描错误：%+v", row)
			}
			return err
		}, mapperTestResponse{columns: []string{"uuid", "creation_request_hash"}, rows: [][]driver.Value{{"org", request.Hash}}}, []any{request.AccountUUID, request.Key}, yourbatis.StatementSelect, []string{"FROM organizations", "creation_account_uuid = $1 AND creation_idempotency_key = $2"}},
		{"Insert", func(m PlatformAdminOrganizationMapper) error {
			row, err := m.Insert(t.Context(), request)
			if err == nil && (row.UUID != "org" || row.Hash != request.Hash) {
				t.Fatalf("创建结果扫描错误：%+v", row)
			}
			return err
		}, mapperTestResponse{columns: []string{"uuid", "creation_request_hash"}, rows: [][]driver.Value{{"org", request.Hash}}}, []any{request.Name, request.AccountUUID, request.Key, request.Hash}, yourbatis.StatementInsert, []string{"VALUES ($1, $2, $3, $4)", "ON CONFLICT (creation_account_uuid, creation_idempotency_key) DO NOTHING", "RETURNING uuid, creation_request_hash"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := errors.New("执行失败")
			executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: failure, execErr: failure})
			if err := tc.call(NewPlatformAdminOrganizationMapper(executor)); !errors.Is(err, failure) {
				t.Fatalf("未保留执行错误：%v", err)
			}
			executor = newMapperTestExecutor(t, tc.response)
			if err := tc.call(NewPlatformAdminOrganizationMapper(executor)); err != nil {
				t.Fatal(err)
			}
			assertMapperTestExecution(t, executor, "PlatformAdminOrganizationMapper."+tc.name, tc.kind, tc.args, tc.fragments...)
			for i, arg := range executor.bound.Args {
				sensitive := tc.name == "Insert" && i >= 2 || tc.name == "FindCreation" && i == 1
				if arg.Sensitive != sensitive {
					t.Fatalf("参数敏感标记：%s %d", tc.name, i)
				}
			}
		})
	}
}

func TestPlatformAdminOrganizationMapperPagination(t *testing.T) {
	for _, cursor := range []string{"", "cursor"} {
		query := PlatformAdminPageQuery{OrganizationUUID: "org", Query: "组织'检索", After: cursor, Limit: 21}
		for _, tc := range []struct {
			name         string
			call         func(PlatformAdminOrganizationMapper) error
			args         []any
			columns      []string
			fragments    []string
			cursorColumn string
		}{
			{"List", func(m PlatformAdminOrganizationMapper) error { _, err := m.List(t.Context(), query); return err }, []any{query.Query, query.Query}, []string{"uuid", "name", "website", "industry", "management_version", "created_at", "member_count", "admin_count", "workspace_count"}, []string{"o.name ILIKE '%' || $1 || '%' OR o.uuid::text = $2", "ORDER BY o.uuid LIMIT"}, "o.uuid"},
			{"ListMembers", func(m PlatformAdminOrganizationMapper) error { _, err := m.ListMembers(t.Context(), query); return err }, []any{query.OrganizationUUID}, []string{"uuid", "email", "name", "role"}, []string{"organization_uuid = $1 AND deleted_at IS NULL", "ORDER BY uuid LIMIT"}, "uuid"},
			{"ListWorkspaces", func(m PlatformAdminOrganizationMapper) error {
				_, err := m.ListWorkspaces(t.Context(), query)
				return err
			}, []any{query.OrganizationUUID}, []string{"uuid", "external_id", "name", "is_default", "archived"}, []string{"organization_uuid = $1", "archived_at IS NOT NULL AS archived", "ORDER BY uuid LIMIT"}, "uuid"},
		} {
			t.Run(tc.name+"/"+cursor, func(t *testing.T) {
				failure := errors.New("查询失败")
				executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: failure})
				if err := tc.call(NewPlatformAdminOrganizationMapper(executor)); !errors.Is(err, failure) {
					t.Fatalf("未保留查询错误：%v", err)
				}
				executor = newMapperTestExecutor(t, mapperTestResponse{columns: tc.columns})
				if err := tc.call(NewPlatformAdminOrganizationMapper(executor)); err != nil {
					t.Fatal(err)
				}
				args := tc.args
				if cursor != "" {
					args = append(args, cursor)
				}
				args = append(args, query.Limit)
				assertMapperTestExecution(t, executor, "PlatformAdminOrganizationMapper."+tc.name, yourbatis.StatementSelect, args, tc.fragments...)
				if strings.Contains(executor.bound.SQL, tc.cursorColumn+" >") != (cursor != "") {
					t.Fatalf("游标分支错误：%s", executor.bound.SQL)
				}
				if strings.Contains(executor.bound.SQL, query.Query) {
					t.Fatal("查询值不应拼接到 SQL")
				}
			})
		}
	}
}

func TestPlatformAdminOrganizationMapperMissingAndInvalidRows(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		response := mapperTestResponse{columns: []string{"management_version"}}
		if invalid {
			response.rows = [][]driver.Value{{"不是整数"}}
		}
		_, err := NewPlatformAdminOrganizationMapper(newMapperTestExecutor(t, response)).Lock(t.Context(), "org")
		if invalid && (err == nil || errors.Is(err, sql.ErrNoRows)) {
			t.Fatalf("版本扫描错误未返回：%v", err)
		}
		if !invalid && !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("缺失组织应返回 sql.ErrNoRows：%v", err)
		}
	}
	m := NewPlatformAdminOrganizationMapper(newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid", "email", "name", "role"}}))
	if _, found, err := m.FindMemberByEmail(t.Context(), "org", "a@example.com"); err != nil || found {
		t.Fatalf("缺失成员：found=%v err=%v", found, err)
	}
	m = NewPlatformAdminOrganizationMapper(newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid", "creation_request_hash"}}))
	if _, err := m.FindCreation(t.Context(), "account", "key"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("缺失创建记录：%v", err)
	}
	if _, err := m.Insert(t.Context(), PlatformAdminOrganizationInput{}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("并发已创建：%v", err)
	}
}

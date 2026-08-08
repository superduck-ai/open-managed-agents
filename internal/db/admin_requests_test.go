package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/yourbatis"
)

func TestAdminRequestRowRejectsInvalidDetails(t *testing.T) {
	_, err := (adminRequestRow{Details: []byte(`{"reason":`)}).toAdminRequest()
	if err == nil {
		t.Fatal("toAdminRequest() error = nil, want invalid JSON error")
	}
}

func TestAdminRequestMapperBuilderContract(t *testing.T) {
	params := listAdminRequestsParams{
		OrgUUID:     "11111111-1111-4111-8111-111111111111",
		RequestType: "join_org",
		Status:      "pending",
		Limit:       25,
	}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: adminRequestMapperListStatement,
		bound:     buildAdminRequestMapperList(yourbatis.DialectPostgres, params),
		wantID:    "AdminRequestMapper.List",
		wantKind:  yourbatis.StatementSelect,
		wantArgumentNames: []string{
			"params.OrgUUID", "params.RequestType", "params.Status", "params.Limit",
		},
		wantSQLFragments: []string{
			"FROM admin_requests ar",
			"u.organization_uuid = ar.org_uuid",
			"ar.org_uuid = $1",
			"ar.request_type = $2",
			"ar.status = $3",
			"LIMIT $4",
		},
	})
}

func TestAdminRequestMapperResultSemantics(t *testing.T) {
	ctx := context.Background()
	params := listAdminRequestsParams{OrgUUID: "org", RequestType: "join_org", Status: "pending", Limit: 10}
	createdAt := time.Date(2026, time.July, 23, 9, 30, 0, 0, time.UTC)

	t.Run("query error", func(t *testing.T) {
		wantErr := errors.New("list admin requests failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: wantErr})
		_, err := NewAdminRequestMapper(executor).List(ctx, params)
		if !errors.Is(err, wantErr) {
			t.Fatalf("List() error = %v, want query error", err)
		}
	})

	t.Run("string UUID and JSON row", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: adminRequestMapperTestColumns(),
			rows: [][]driver.Value{{
				"22222222-2222-4222-8222-222222222222",
				"11111111-1111-4111-8111-111111111111",
				"join_org",
				"33333333-3333-4333-8333-333333333333",
				"standard",
				[]byte(`{"reason":"collaboration"}`),
				"pending",
				createdAt,
				nil,
				"requester@example.com",
				"Requester",
				"user",
				nil,
			}},
		})
		rows, err := NewAdminRequestMapper(executor).List(ctx, params)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		requests, conversionErr := adminRequestsFromMapperRows(rows)
		if conversionErr != nil || len(requests) != 1 || requests[0].UUID != "22222222-2222-4222-8222-222222222222" {
			t.Fatalf("List() = (%+v, %v)", requests, conversionErr)
		}
		if requests[0].RequesterUUID == nil || requests[0].Details["reason"] != "collaboration" {
			t.Fatalf("List() request = %+v", requests[0])
		}
	})
}

func TestAdminRequestMapperScansPostgreSQLRows(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("PostgreSQL integration test requires config: %v", err)
	}
	database, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	standardDB := newStandardDB(database.pool)
	defer standardDB.Close()
	tx, err := standardDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		create temporary table organizations (
			id bigint generated always as identity,
			uuid uuid not null
		) on commit drop;
		create temporary table users (
			id bigint generated always as identity,
			uuid uuid not null,
			organization_uuid uuid not null,
			email text,
			name text,
			role text,
			deleted_at timestamptz
		) on commit drop;
		create temporary table admin_requests (
			id bigint generated always as identity,
			request_uuid uuid not null,
			org_uuid uuid not null,
			request_type text not null,
			requester_uuid uuid,
			requested_seat_tier text,
			details jsonb,
			status text not null,
			created_at timestamptz not null,
			resolved_at timestamptz
		) on commit drop;
	`); err != nil {
		t.Fatalf("create temporary tables: %v", err)
	}

	const (
		orgUUID       = "11111111-1111-1111-1111-111111111111"
		requestUUID   = "22222222-2222-2222-2222-222222222222"
		requesterUUID = "33333333-3333-3333-3333-333333333333"
	)
	createdAt := time.Date(2026, time.July, 23, 9, 30, 0, 0, time.UTC)
	if _, err := tx.ExecContext(ctx, `
		insert into organizations (uuid)
		values ($1)
	`, orgUUID); err != nil {
		t.Fatalf("seed temporary organization: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		insert into users (uuid, organization_uuid, email, name, role)
		values ($2, $1, 'requester@example.com', 'Requester', 'user')
	`, orgUUID, requesterUUID); err != nil {
		t.Fatalf("seed temporary user: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		insert into admin_requests (
			request_uuid,
			org_uuid,
			request_type,
			requester_uuid,
			requested_seat_tier,
			details,
			status,
			created_at
		)
		values ($3, $1, 'join_org', $2, 'standard', '{"reason":"collaboration"}', 'pending', $4)
	`, orgUUID, requesterUUID, requestUUID, createdAt); err != nil {
		t.Fatalf("seed temporary admin request: %v", err)
	}

	mapper := NewAdminRequestMapper(sqlTxMapperExecutor{transaction: tx})
	rows, err := mapper.List(ctx, listAdminRequestsParams{
		OrgUUID: orgUUID, RequestType: "join_org", Status: "pending", Limit: 10,
	})
	if err != nil {
		t.Fatalf("AdminRequestMapper.List() error = %v", err)
	}
	requests, err := adminRequestsFromMapperRows(rows)
	if err != nil {
		t.Fatalf("adminRequestsFromMapperRows() error = %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("AdminRequestMapper.List() returned %d requests, want 1", len(requests))
	}
	request := requests[0]
	if request.UUID != requestUUID || request.OrgUUID != orgUUID || request.RequestType != "join_org" {
		t.Fatalf("AdminRequestMapper.List() identity fields = %#v", request)
	}
	if request.RequesterUUID == nil || *request.RequesterUUID != requesterUUID {
		t.Fatalf("AdminRequestMapper.List() requester UUID = %#v, want %q", request.RequesterUUID, requesterUUID)
	}
	if request.RequestedSeatTier == nil || *request.RequestedSeatTier != "standard" {
		t.Fatalf("AdminRequestMapper.List() requested seat tier = %#v, want standard", request.RequestedSeatTier)
	}
	if request.RequesterEmail == nil || *request.RequesterEmail != "requester@example.com" {
		t.Fatalf("AdminRequestMapper.List() requester email = %#v", request.RequesterEmail)
	}
	if request.RequesterName == nil || *request.RequesterName != "Requester" {
		t.Fatalf("AdminRequestMapper.List() requester name = %#v", request.RequesterName)
	}
	if request.RequesterRole == nil || *request.RequesterRole != "user" {
		t.Fatalf("AdminRequestMapper.List() requester role = %#v", request.RequesterRole)
	}
	if request.RequesterSeatTier != nil || request.ResolvedAt != nil {
		t.Fatalf("AdminRequestMapper.List() nullable fields = seat tier %#v, resolved at %#v", request.RequesterSeatTier, request.ResolvedAt)
	}
	if request.Details["reason"] != "collaboration" {
		t.Fatalf("AdminRequestMapper.List() details = %#v", request.Details)
	}
	if !request.CreatedAt.Equal(createdAt) {
		t.Fatalf("AdminRequestMapper.List() created at = %v, want %v", request.CreatedAt, createdAt)
	}
}

func adminRequestMapperTestColumns() []string {
	return []string{
		"request_uuid", "org_uuid", "request_type", "requester_uuid",
		"requested_seat_tier", "details", "status", "created_at", "resolved_at",
		"requester_email", "requester_name", "requester_role", "requester_seat_tier",
	}
}

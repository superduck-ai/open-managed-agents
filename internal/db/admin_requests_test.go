package db

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestAdminRequestRowRejectsInvalidDetails(t *testing.T) {
	_, err := (adminRequestRow{Details: []byte(`{"reason":`)}).toAdminRequest()
	if err == nil {
		t.Fatal("toAdminRequest() error = nil, want invalid JSON error")
	}
}

func TestListAdminRequestsQueryUsesNamedPostgreSQLArguments(t *testing.T) {
	query, arguments, err := bindNamed(postgresRebinder{}, listAdminRequestsSQL, map[string]any{
		"org_uuid":     "11111111-1111-1111-1111-111111111111",
		"request_type": "join_org",
		"status":       "pending",
		"limit":        25,
	})
	if err != nil {
		t.Fatalf("bindNamed() error = %v", err)
	}
	wantArguments := []any{
		"11111111-1111-1111-1111-111111111111",
		"join_org",
		"pending",
		25,
	}
	if !reflect.DeepEqual(arguments, wantArguments) {
		t.Fatalf("bindNamed() arguments = %#v, want %#v", arguments, wantArguments)
	}
	if strings.Contains(query, "::") {
		t.Fatalf("bindNamed() query contains PostgreSQL shorthand cast: %q", query)
	}
	for _, clause := range []string{
		"where CAST(ar.org_uuid AS text) = $1",
		"and ar.request_type = $2",
		"and ar.status = $3",
		"limit $4",
	} {
		if !strings.Contains(query, clause) {
			t.Fatalf("bindNamed() query does not contain %q: %q", clause, query)
		}
	}
}

func TestListAdminRequestsSQLXScansPostgreSQLRows(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("PostgreSQL integration test requires config: %v", err)
	}
	database, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	tx, err := database.sql.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		create temporary table organizations (
			id bigint generated always as identity,
			uuid uuid not null,
			external_id text not null
		) on commit drop;
		create temporary table users (
			id bigint generated always as identity,
			uuid uuid not null,
			organization_id bigint not null,
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
		insert into organizations (uuid, external_id)
		values ($1, 'org_external')
	`, orgUUID); err != nil {
		t.Fatalf("seed temporary organization: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		insert into users (uuid, organization_id, email, name, role)
		select $2, id, 'requester@example.com', 'Requester', 'user'
		from organizations
		where uuid = $1
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

	requests, err := listAdminRequestsSQLX(ctx, tx, orgUUID, "join_org", "pending", 10)
	if err != nil {
		t.Fatalf("listAdminRequestsSQLX() error = %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("listAdminRequestsSQLX() returned %d requests, want 1", len(requests))
	}
	request := requests[0]
	if request.UUID != requestUUID || request.OrgUUID != orgUUID || request.RequestType != "join_org" {
		t.Fatalf("listAdminRequestsSQLX() identity fields = %#v", request)
	}
	if request.RequesterUUID == nil || *request.RequesterUUID != requesterUUID {
		t.Fatalf("listAdminRequestsSQLX() requester UUID = %#v, want %q", request.RequesterUUID, requesterUUID)
	}
	if request.RequestedSeatTier == nil || *request.RequestedSeatTier != "standard" {
		t.Fatalf("listAdminRequestsSQLX() requested seat tier = %#v, want standard", request.RequestedSeatTier)
	}
	if request.RequesterEmail == nil || *request.RequesterEmail != "requester@example.com" {
		t.Fatalf("listAdminRequestsSQLX() requester email = %#v", request.RequesterEmail)
	}
	if request.RequesterName == nil || *request.RequesterName != "Requester" {
		t.Fatalf("listAdminRequestsSQLX() requester name = %#v", request.RequesterName)
	}
	if request.RequesterRole == nil || *request.RequesterRole != "user" {
		t.Fatalf("listAdminRequestsSQLX() requester role = %#v", request.RequesterRole)
	}
	if request.RequesterSeatTier != nil || request.ResolvedAt != nil {
		t.Fatalf("listAdminRequestsSQLX() nullable fields = seat tier %#v, resolved at %#v", request.RequesterSeatTier, request.ResolvedAt)
	}
	if request.Details["reason"] != "collaboration" {
		t.Fatalf("listAdminRequestsSQLX() details = %#v", request.Details)
	}
	if !request.CreatedAt.Equal(createdAt) {
		t.Fatalf("listAdminRequestsSQLX() created at = %v, want %v", request.CreatedAt, createdAt)
	}
}

package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

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

func adminRequestMapperTestColumns() []string {
	return []string{
		"request_uuid", "org_uuid", "request_type", "requester_uuid",
		"requested_seat_tier", "details", "status", "created_at", "resolved_at",
		"requester_email", "requester_name", "requester_role", "requester_seat_tier",
	}
}

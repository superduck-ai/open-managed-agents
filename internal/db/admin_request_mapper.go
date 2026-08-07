package db

import (
	"context"
	"encoding/json"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper AdminRequestMapper -sql ./admin_request_mapper.xml -out ./admin_request_mapper.sqlmap.gen.go -dialect postgres

type adminRequestRow struct {
	UUID              string          `db:"request_uuid"`
	OrgUUID           string          `db:"org_uuid"`
	RequestType       string          `db:"request_type"`
	RequesterUUID     *string         `db:"requester_uuid"`
	RequestedSeatTier *string         `db:"requested_seat_tier"`
	Details           json.RawMessage `db:"details"`
	Status            string          `db:"status"`
	CreatedAt         time.Time       `db:"created_at"`
	ResolvedAt        *time.Time      `db:"resolved_at"`
	RequesterEmail    *string         `db:"requester_email"`
	RequesterName     *string         `db:"requester_name"`
	RequesterRole     *string         `db:"requester_role"`
	RequesterSeatTier *string         `db:"requester_seat_tier"`
}

type listAdminRequestsParams struct {
	OrgUUID     string
	RequestType string
	Status      string
	Limit       int
}

type AdminRequestMapper interface {
	List(ctx context.Context, params listAdminRequestsParams) ([]adminRequestRow, error)
}

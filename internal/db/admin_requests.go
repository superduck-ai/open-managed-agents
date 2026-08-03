package db

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

const listAdminRequestsSQL = `
	select
		ar.request_uuid,
		ar.org_uuid,
		ar.request_type,
		ar.requester_uuid,
		ar.requested_seat_tier,
		ar.details,
		ar.status,
		ar.created_at,
		ar.resolved_at,
		u.email as requester_email,
		nullif(u.name, '') as requester_name,
		u.role as requester_role,
		CAST(null AS text) as requester_seat_tier
	from admin_requests ar
	left join users u
	  on u.uuid = ar.requester_uuid
	 and u.organization_uuid = ar.org_uuid
	 and u.deleted_at is null
	where ar.org_uuid = :org_uuid
	  and ar.request_type = :request_type
	  and ar.status = :status
	order by ar.created_at desc, ar.request_uuid desc
	limit :limit
`

type adminRequestRow struct {
	UUID              uuid.UUID     `db:"request_uuid"`
	OrgUUID           uuid.UUID     `db:"org_uuid"`
	RequestType       string        `db:"request_type"`
	RequesterUUID     uuid.NullUUID `db:"requester_uuid"`
	RequestedSeatTier *string       `db:"requested_seat_tier"`
	Details           []byte        `db:"details"`
	Status            string        `db:"status"`
	CreatedAt         time.Time     `db:"created_at"`
	ResolvedAt        *time.Time    `db:"resolved_at"`
	RequesterEmail    *string       `db:"requester_email"`
	RequesterName     *string       `db:"requester_name"`
	RequesterRole     *string       `db:"requester_role"`
	RequesterSeatTier *string       `db:"requester_seat_tier"`
}

func (r adminRequestRow) toAdminRequest() (platform.AdminRequest, error) {
	request := platform.AdminRequest{
		UUID:              r.UUID.String(),
		OrgUUID:           r.OrgUUID.String(),
		RequestType:       r.RequestType,
		RequesterUUID:     nullableUUIDString(r.RequesterUUID),
		RequestedSeatTier: r.RequestedSeatTier,
		Status:            r.Status,
		CreatedAt:         r.CreatedAt,
		ResolvedAt:        r.ResolvedAt,
		RequesterEmail:    r.RequesterEmail,
		RequesterName:     r.RequesterName,
		RequesterRole:     r.RequesterRole,
		RequesterSeatTier: r.RequesterSeatTier,
	}
	if len(r.Details) == 0 {
		return request, nil
	}
	request.Details = map[string]any{}
	if err := json.Unmarshal(r.Details, &request.Details); err != nil {
		return platform.AdminRequest{}, err
	}
	return request, nil
}

func listAdminRequestsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	orgUUID string,
	requestType string,
	status string,
	limit int,
) ([]platform.AdminRequest, error) {
	var rows []adminRequestRow
	if err := namedSelectContext(ctx, database, &rows, listAdminRequestsSQL, map[string]any{
		"org_uuid":     dbUUID(orgUUID),
		"request_type": requestType,
		"status":       status,
		"limit":        limit,
	}); err != nil {
		return nil, err
	}

	requests := make([]platform.AdminRequest, 0, len(rows))
	for _, row := range rows {
		request, err := row.toAdminRequest()
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func (d *DB) ListAdminRequests(ctx context.Context, orgUUID string, requestType string, status string, limit int) ([]platform.AdminRequest, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" {
		return []platform.AdminRequest{}, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	if status == "" {
		status = "pending"
	}
	requests, err := listAdminRequestsSQLX(
		ctx,
		d.sql,
		strings.TrimSpace(orgUUID),
		requestType,
		status,
		limit,
	)
	if err != nil {
		return nil, err
	}
	return requests, nil
}

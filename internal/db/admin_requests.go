package db

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

const listAdminRequestsSQL = `
	select
		CAST(ar.request_uuid AS text) as request_uuid,
		CAST(ar.org_uuid AS text) as org_uuid,
		ar.request_type,
		CAST(ar.requester_uuid AS text) as requester_uuid,
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
	left join organizations o
	  on CAST(o.uuid AS text) = CAST(ar.org_uuid AS text)
	  or o.external_id = CAST(ar.org_uuid AS text)
	left join users u
	  on CAST(u.uuid AS text) = CAST(ar.requester_uuid AS text)
	 and u.organization_id = o.id
	 and u.deleted_at is null
	where CAST(ar.org_uuid AS text) = :org_uuid
	  and ar.request_type = :request_type
	  and ar.status = :status
	order by ar.created_at desc, ar.id desc
	limit :limit
`

type adminRequestRow struct {
	UUID              string     `db:"request_uuid"`
	OrgUUID           string     `db:"org_uuid"`
	RequestType       string     `db:"request_type"`
	RequesterUUID     *string    `db:"requester_uuid"`
	RequestedSeatTier *string    `db:"requested_seat_tier"`
	Details           []byte     `db:"details"`
	Status            string     `db:"status"`
	CreatedAt         time.Time  `db:"created_at"`
	ResolvedAt        *time.Time `db:"resolved_at"`
	RequesterEmail    *string    `db:"requester_email"`
	RequesterName     *string    `db:"requester_name"`
	RequesterRole     *string    `db:"requester_role"`
	RequesterSeatTier *string    `db:"requester_seat_tier"`
}

func (r adminRequestRow) toAdminRequest() (platform.AdminRequest, error) {
	request := platform.AdminRequest{
		UUID:              r.UUID,
		OrgUUID:           r.OrgUUID,
		RequestType:       r.RequestType,
		RequesterUUID:     r.RequesterUUID,
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
		"org_uuid":     orgUUID,
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
		if isUndefinedTableError(err) {
			return []platform.AdminRequest{}, nil
		}
		return nil, err
	}
	return requests, nil
}

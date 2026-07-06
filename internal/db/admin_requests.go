package db

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

func (d *DB) ListAdminRequests(ctx context.Context, orgUUID string, requestType string, status string, limit int) ([]platform.AdminRequest, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" {
		return []platform.AdminRequest{}, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	if status == "" {
		status = "pending"
	}
	rows, err := d.Pool.Query(ctx, `
		select
			ar.request_uuid::text,
			ar.org_uuid::text,
			ar.request_type,
			ar.requester_uuid::text,
			ar.requested_seat_tier,
			ar.details,
			ar.status,
			ar.created_at,
			ar.resolved_at,
			u.email as requester_email,
			nullif(u.name, '') as requester_name,
			u.role as requester_role,
			null::text as requester_seat_tier
		from admin_requests ar
		left join organizations o
		  on o.uuid::text = ar.org_uuid::text
		  or o.external_id = ar.org_uuid::text
		left join users u
		  on u.uuid::text = ar.requester_uuid::text
		 and u.organization_id = o.id
		 and u.deleted_at is null
		where ar.org_uuid::text = $1
		  and ar.request_type = $2
		  and ar.status = $3
		order by ar.created_at desc, ar.id desc
		limit $4
	`, strings.TrimSpace(orgUUID), requestType, status, limit)
	if err != nil {
		if isUndefinedTableError(err) {
			return []platform.AdminRequest{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	var out []platform.AdminRequest
	for rows.Next() {
		var request platform.AdminRequest
		var detailsBytes []byte
		if err := rows.Scan(
			&request.UUID,
			&request.OrgUUID,
			&request.RequestType,
			&request.RequesterUUID,
			&request.RequestedSeatTier,
			&detailsBytes,
			&request.Status,
			&request.CreatedAt,
			&request.ResolvedAt,
			&request.RequesterEmail,
			&request.RequesterName,
			&request.RequesterRole,
			&request.RequesterSeatTier,
		); err != nil {
			return nil, err
		}
		if len(detailsBytes) > 0 {
			request.Details = map[string]any{}
			if err := json.Unmarshal(detailsBytes, &request.Details); err != nil {
				return nil, err
			}
		}
		out = append(out, request)
	}
	return out, rows.Err()
}

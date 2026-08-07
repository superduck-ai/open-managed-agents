package db

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

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

func adminRequestsFromMapperRows(rows []adminRequestRow) ([]platform.AdminRequest, error) {
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
	if d == nil || d.mapperDB == nil || strings.TrimSpace(orgUUID) == "" {
		return []platform.AdminRequest{}, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	if status == "" {
		status = "pending"
	}
	mapper := NewAdminRequestMapper(d.mapperDB)
	rows, err := mapper.List(ctx, listAdminRequestsParams{
		OrgUUID:     strings.TrimSpace(orgUUID),
		RequestType: requestType,
		Status:      status,
		Limit:       limit,
	})
	if err != nil {
		if postgresErr, ok := postgresError(err); ok && postgresErr.Code == "42P01" {
			return []platform.AdminRequest{}, nil
		}
		return nil, err
	}
	return adminRequestsFromMapperRows(rows)
}

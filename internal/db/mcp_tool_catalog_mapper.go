package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper MCPToolCatalogMapper -sql ./mcp_tool_catalog_mapper.xml -out ./mcp_tool_catalog_mapper.sqlmap.gen.go -dialect postgres

type mcpToolCatalogRow struct {
	ID            int64     `db:"id"`
	UUID          string    `db:"uuid"`
	ExternalID    string    `db:"external_id"`
	TransportType string    `db:"transport_type"`
	EndpointURL   string    `db:"endpoint_url"`
	Tools         []byte    `db:"tools"`
	CreatedAt     time.Time `db:"created_at"`
	UpdatedAt     time.Time `db:"updated_at"`
}

type upsertMCPToolCatalogParams struct {
	ExternalID    string
	TransportType string
	EndpointURL   string
	Tools         []byte
}

type MCPToolCatalogMapper interface {
	FindByEndpoint(ctx context.Context, transportType, endpointURL string) (mcpToolCatalogRow, error)
	Upsert(ctx context.Context, params upsertMCPToolCatalogParams) (mcpToolCatalogRow, error)
}

package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

// MCPToolCatalogItem 是 catalog 持久化的稳定工具元数据；输入 schema 等执行细节不进入该展示快照。
type MCPToolCatalogItem struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// MCPToolCatalog 是按规范化 transport_type + endpoint_url 全局共享的最近一次成功发现快照。
// catalog 不属于任何组织或 workspace，也不保存认证信息和失败探测结果。
type MCPToolCatalog struct {
	ID            int64
	UUID          string
	ExternalID    string
	TransportType string
	EndpointURL   string
	Tools         []MCPToolCatalogItem
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// GetMCPToolCatalog 读取指定 MCP endpoint 最近一次成功发现的工具快照。
// 没有记录表示该 endpoint 从未成功刷新，调用方会收到 ErrNotFound。
func (d *DB) GetMCPToolCatalog(ctx context.Context, transportType, endpointURL string) (MCPToolCatalog, error) {
	mapper := NewMCPToolCatalogMapper(d.mapperDB)
	row, err := mapper.FindByEndpoint(ctx, strings.TrimSpace(transportType), strings.TrimSpace(endpointURL))
	if err != nil {
		return MCPToolCatalog{}, mapNoRows(err)
	}
	return row.catalog()
}

// UpsertMCPToolCatalog 原子保存一次成功的 MCP tools/list 结果。
// nil 工具列表不是有效的成功结果；非 nil 空切片会明确保存为 JSON []，表示 MCP 已确认没有工具。
func (d *DB) UpsertMCPToolCatalog(
	ctx context.Context,
	transportType string,
	endpointURL string,
	tools []MCPToolCatalogItem,
) (MCPToolCatalog, error) {
	transportType = strings.TrimSpace(transportType)
	endpointURL = strings.TrimSpace(endpointURL)
	if transportType != "url" {
		return MCPToolCatalog{}, fmt.Errorf("unsupported MCP transport type %q", transportType)
	}
	if endpointURL == "" {
		return MCPToolCatalog{}, fmt.Errorf("MCP endpoint URL is required")
	}
	if len(endpointURL) > 2048 {
		return MCPToolCatalog{}, fmt.Errorf("MCP endpoint URL exceeds 2048 bytes")
	}
	toolsJSON, err := encodeMCPToolCatalogTools(tools)
	if err != nil {
		return MCPToolCatalog{}, err
	}
	externalID, err := ids.New("mcpc_")
	if err != nil {
		return MCPToolCatalog{}, err
	}

	// 唯一键保证同一个规范化 endpoint 只有一份全局快照。刷新已有记录时保留稳定 ID，
	// 仅替换成功结果并推进 updated_at，保留最近一次成功刷新的数据库时间。
	mapper := NewMCPToolCatalogMapper(d.mapperDB)
	row, err := mapper.Upsert(ctx, upsertMCPToolCatalogParams{
		ExternalID:    externalID,
		TransportType: transportType,
		EndpointURL:   endpointURL,
		Tools:         toolsJSON,
	})
	if err != nil {
		return MCPToolCatalog{}, mapNoRows(err)
	}
	return row.catalog()
}

func (r mcpToolCatalogRow) catalog() (MCPToolCatalog, error) {
	tools, err := decodeMCPToolCatalogTools(r.Tools)
	if err != nil {
		return MCPToolCatalog{}, err
	}
	return MCPToolCatalog{
		ID:            r.ID,
		UUID:          r.UUID,
		ExternalID:    r.ExternalID,
		TransportType: r.TransportType,
		EndpointURL:   r.EndpointURL,
		Tools:         tools,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}, nil
}

func encodeMCPToolCatalogTools(tools []MCPToolCatalogItem) ([]byte, error) {
	if tools == nil {
		return nil, fmt.Errorf("MCP tool catalog success requires a non-nil tool list")
	}
	encoded, err := json.Marshal(tools)
	if err != nil {
		return nil, fmt.Errorf("encode MCP tool catalog tools: %w", err)
	}
	return encoded, nil
}

func decodeMCPToolCatalogTools(raw []byte) ([]MCPToolCatalogItem, error) {
	if raw == nil {
		return nil, fmt.Errorf("decode MCP tool catalog tools: expected JSON array")
	}
	// 预先创建非 nil 空切片，使数据库中的 JSON [] 解码后仍保持“已确认为空”的语义。
	tools := make([]MCPToolCatalogItem, 0)
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, fmt.Errorf("decode MCP tool catalog tools: %w", err)
	}
	if tools == nil {
		return nil, fmt.Errorf("decode MCP tool catalog tools: expected JSON array")
	}
	return tools, nil
}

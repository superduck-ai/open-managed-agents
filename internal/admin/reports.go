package admin

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type usageReportBucket struct {
	StartingAt string              `json:"starting_at"`
	EndingAt   string              `json:"ending_at"`
	Results    []usageReportResult `json:"results"`
}

type usageCacheCreation struct {
	Ephemeral5m int64 `json:"ephemeral_5m_input_tokens"`
	Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
}

type usageServerTool struct {
	WebSearchRequests int64 `json:"web_search_requests"`
}

type usageReportResult struct {
	UncachedInputTokens  int64              `json:"uncached_input_tokens"`
	CacheReadInputTokens int64              `json:"cache_read_input_tokens"`
	OutputTokens         int64              `json:"output_tokens"`
	CacheCreation        usageCacheCreation `json:"cache_creation"`
	ServerToolUse        usageServerTool    `json:"server_tool_use"`
	Model                *string            `json:"model"`
	WorkspaceID          *string            `json:"workspace_id"`
	APIKeyID             *string            `json:"api_key_id"`
	AccountID            *string            `json:"account_id"`
	ServiceAccountID     *string            `json:"service_account_id"`
	ServiceTier          *string            `json:"service_tier"`
	ContextWindow        *string            `json:"context_window"`
	InferenceGeo         *string            `json:"inference_geo"`
}

type costReportBucket struct {
	StartingAt string             `json:"starting_at"`
	EndingAt   string             `json:"ending_at"`
	Results    []costReportResult `json:"results"`
}

type costReportResult struct {
	Currency      string  `json:"currency"`
	Amount        string  `json:"amount"`
	WorkspaceID   *string `json:"workspace_id"`
	Description   *string `json:"description"`
	CostType      *string `json:"cost_type"`
	Model         *string `json:"model"`
	TokenType     *string `json:"token_type"`
	ServiceTier   *string `json:"service_tier"`
	ContextWindow *string `json:"context_window"`
	InferenceGeo  *string `json:"inference_geo"`
}

func (s *Service) messagesUsageReport(ctx context.Context, principal auth.Principal, query reportQuery) (reportResponse, error) {
	if err := validateMessagesReportQuery(query); err != nil {
		return reportResponse{}, invalidRequest(err.Error())
	}
	groupBy, err := oneGroup(query.GroupBy, db.AdminGroupModel, db.AdminGroupWorkspace)
	if err != nil {
		return reportResponse{}, err
	}
	dbQuery, err := s.adminReportQuery(principal, query, usageBucket(query.BucketWidth), groupBy, usageLimit(query.BucketWidth))
	if err != nil {
		return reportResponse{}, err
	}
	report, err := s.db.AdminUsageReport(ctx, dbQuery)
	if err != nil {
		return reportResponse{}, reportLoadError(err)
	}
	data := make([]any, 0, len(report.Buckets))
	for _, bucket := range report.Buckets {
		results := make([]usageReportResult, 0, len(bucket.Usage))
		for _, group := range bucket.Usage {
			results = append(results, usageResult(groupBy, group))
		}
		data = append(data, usageReportBucket{
			StartingAt: bucket.StartingAt.Format(time.RFC3339),
			EndingAt:   bucket.EndingAt.Format(time.RFC3339),
			Results:    results,
		})
	}
	return pagedReport(data, report.HasMore, dbQuery.Offset, dbQuery.Limit), nil
}

func (s *Service) costReport(ctx context.Context, principal auth.Principal, query reportQuery) (reportResponse, error) {
	if err := validateCostReportQuery(query); err != nil {
		return reportResponse{}, invalidRequest(err.Error())
	}
	groupBy, err := oneGroup(query.GroupBy, db.AdminGroupWorkspace, db.AdminGroupDescription)
	if err != nil {
		return reportResponse{}, err
	}
	dbQuery, err := s.adminReportQuery(principal, query, 24*time.Hour, groupBy, 31)
	if err != nil {
		return reportResponse{}, err
	}
	if dbQuery.Limit == 0 {
		dbQuery.Limit = 7
	}
	report, err := s.db.AdminCostReport(ctx, dbQuery)
	if err != nil {
		return reportResponse{}, reportLoadError(err)
	}
	data := make([]any, 0, len(report.Buckets))
	for _, bucket := range report.Buckets {
		results := make([]costReportResult, 0, len(bucket.Costs))
		for _, group := range bucket.Costs {
			results = append(results, costResult(groupBy, group))
		}
		data = append(data, costReportBucket{
			StartingAt: bucket.StartingAt.Format(time.RFC3339),
			EndingAt:   bucket.EndingAt.Format(time.RFC3339),
			Results:    results,
		})
	}
	return pagedReport(data, report.HasMore, dbQuery.Offset, dbQuery.Limit), nil
}

func reportLoadError(err error) error {
	if errors.Is(err, db.ErrInvalidState) {
		return invalidRequest("invalid report query")
	}
	return err
}

func (s *Service) adminReportQuery(principal auth.Principal, query reportQuery, bucket time.Duration, groupBy string, maxLimit int) (db.AdminReportQuery, error) {
	organizationUUID, err := principalOrganizationUUID(principal)
	if err != nil {
		return db.AdminReportQuery{}, mapAdminDBError(db.ErrNotFound, "Organization not found")
	}
	start, err := time.Parse(time.RFC3339, query.StartingAt)
	if err != nil {
		return db.AdminReportQuery{}, invalidRequest("starting_at must be an RFC 3339 timestamp")
	}
	end := time.Now().UTC()
	if query.EndingAt != "" {
		end, err = time.Parse(time.RFC3339, query.EndingAt)
		if err != nil {
			return db.AdminReportQuery{}, invalidRequest("ending_at must be an RFC 3339 timestamp")
		}
	}
	if !end.After(start) {
		return db.AdminReportQuery{}, invalidRequest("ending_at must be after starting_at")
	}
	fallback := 7
	if bucket == time.Hour {
		fallback = 24
	}
	if bucket == time.Minute {
		fallback = 60
	}
	limit, err := boundedLimit(query.Limit, fallback, maxLimit)
	if err != nil {
		return db.AdminReportQuery{}, err
	}
	offset, err := decodePageOffset(query.Page)
	if err != nil {
		return db.AdminReportQuery{}, invalidRequest(err.Error())
	}
	return db.AdminReportQuery{
		OrganizationUUID: organizationUUID.String(),
		StartingAt:       start.UTC(),
		EndingBefore:     end.UTC(),
		Bucket:           bucket,
		GroupBy:          groupBy,
		Limit:            limit,
		Offset:           offset,
		USDPerHour:       s.db.RuntimeUSDPerHour(),
	}, nil
}

func usageResult(groupBy string, group db.AdminUsageGroup) usageReportResult {
	result := usageReportResult{
		UncachedInputTokens:  group.Input,
		CacheReadInputTokens: group.CacheRead,
		OutputTokens:         group.Output,
		CacheCreation:        usageCacheCreation{Ephemeral5m: group.Cache5m, Ephemeral1h: group.Cache1h},
	}
	switch groupBy {
	case db.AdminGroupModel:
		result.Model = optionalReportString(group.Key)
	case db.AdminGroupWorkspace:
		result.WorkspaceID = optionalReportString(group.Key)
	}
	return result
}

func costResult(groupBy string, group db.AdminCostGroup) costReportResult {
	result := costReportResult{Currency: "USD", Amount: formatUSDCents(group.Amount)}
	switch groupBy {
	case db.AdminGroupWorkspace:
		result.WorkspaceID = optionalReportString(group.Key)
	case db.AdminGroupDescription:
		description := "Session runtime"
		costType := "session_usage"
		result.Description = &description
		result.CostType = &costType
	}
	return result
}

func formatUSDCents(usd *big.Rat) string {
	if usd == nil {
		return "0.00"
	}
	return db.FormatDecimalAmount(new(big.Rat).Mul(usd, big.NewRat(100, 1)))
}

func pagedReport(data []any, hasMore bool, offset, limit int) reportResponse {
	response := reportResponse{Data: data, HasMore: hasMore}
	if hasMore {
		next := encodePageOffset(offset + limit)
		response.NextPage = &next
	}
	return response
}

func oneGroup(values []string, allowed ...string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	if len(values) > 1 {
		return "", invalidRequest("only one group_by value is supported")
	}
	for _, group := range allowed {
		if values[0] == group {
			return values[0], nil
		}
	}
	return "", invalidRequest("invalid group_by")
}

func boundedLimit(raw, fallback, max int) (int, error) {
	if raw == 0 {
		return fallback, nil
	}
	if raw < 1 || raw > max {
		return 0, invalidRequest("limit is out of range")
	}
	return raw, nil
}

func usageBucket(width string) time.Duration {
	switch width {
	case "1m":
		return time.Minute
	case "1h":
		return time.Hour
	default:
		return 24 * time.Hour
	}
}

func usageLimit(width string) int {
	switch width {
	case "1m":
		return 1440
	case "1h":
		return 168
	default:
		return 31
	}
}

func optionalReportString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

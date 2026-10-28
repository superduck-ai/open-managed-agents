package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const trajectoryPageSize = 500
const trajectoryListSize = 50
const maxSearchBytes = 16 << 20

var streamNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

type OpenObserve struct {
	config config.OpenObserveConfig
	client *http.Client
}

type searchQuery struct {
	SQL       string `json:"sql"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	Size      int    `json:"size"`
}

func NewOpenObserve(cfg config.OpenObserveConfig, client *http.Client) *OpenObserve {
	if client == nil {
		client = &http.Client{Timeout: cfg.Query.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &OpenObserve{config: cfg, client: client}
}

func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func (b *OpenObserve) scopedQuery(scope Scope) (string, error) {
	if !streamNamePattern.MatchString(b.config.LogsStream) || scope.OrganizationUUID == "" || scope.WorkspaceUUID == "" {
		return "", invalidStorageScope()
	}
	conditions := []string{"oma_organization_uuid = " + quote(scope.OrganizationUUID), "oma_workspace_uuid = " + quote(scope.WorkspaceUUID), "dsh_session_id IS NOT NULL"}
	if scope.SessionID != "" {
		conditions = append(conditions, "oma_session_id = "+quote(scope.SessionID))
	}
	if scope.AgentID != "" {
		conditions = append(conditions, "oma_agent_id = "+quote(scope.AgentID))
	}
	if len(scope.AgentVersions) > 0 {
		versions := make([]string, 0, len(scope.AgentVersions))
		for _, version := range scope.AgentVersions {
			versions = append(versions, strconv.Itoa(version))
		}
		conditions = append(conditions, "CAST(oma_agent_version AS BIGINT) IN ("+strings.Join(versions, ", ")+")")
	}
	return " FROM " + b.config.LogsStream + " WHERE " + strings.Join(conditions, " AND "), nil
}

var emptySearch = []byte(`{"hits":[]}`)

var structuredFields = []string{"dsh_event_type", "dsh_turn", "dsh_step", "dsh_tool_call_id", "dsh_tool_name", "dsh_tool_is_error", "dsh_error_code", "dsh_turn_end_reason", "dsh_model_request_status", "oma_model_request_started_at", "oma_model_request_first_token_at", "gen_ai_request_model", "gen_ai_usage_input_tokens", "gen_ai_usage_output_tokens", "gen_ai_usage_cache_read_input_tokens", "gen_ai_usage_cache_creation_input_tokens", "gen_ai_usage_reasoning_output_tokens"}

var unknownFieldPattern = regexp.MustCompile(`(?i)unknown field\s*['"]?([a-z0-9_]+)`)

func emptyStorage(body []byte) bool {
	var failure struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &failure) != nil {
		return false
	}
	switch failure.Code {
	case 20002:
		return true
	case 20004:
		match := unknownFieldPattern.FindStringSubmatch(failure.Message)
		return match != nil && slices.Contains(structuredFields, match[1])
	}
	return false
}

func (b *OpenObserve) searchRaw(ctx context.Context, window Window, sql string, size int) ([]byte, error) {
	payload, err := json.Marshal(struct {
		Query searchQuery `json:"query"`
	}{Query: searchQuery{SQL: sql, StartTime: window.Start.UnixMicro(), EndTime: window.End.UnixMicro(), Size: size}})
	if err != nil {
		return nil, invalidResponse(err)
	}
	endpoint := strings.TrimRight(b.config.BaseURL, "/") + "/api/" + url.PathEscape(b.config.Organization) + "/_search?type=logs"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, unavailable(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(b.config.Query.Username, b.config.Query.Password)
	response, err := b.client.Do(request)
	if err != nil {
		return nil, unavailable(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSearchBytes+1))
	if err != nil {
		return nil, unavailable(err)
	}
	if len(body) > maxSearchBytes {
		return nil, invalidResponse(fmt.Errorf("search exceeds response limit"))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if emptyStorage(body) {
			return emptySearch, nil
		}
		return nil, unavailable(fmt.Errorf("search returned status %d", response.StatusCode))
	}
	return body, nil
}

func decodeSearch(body []byte, result any) error {
	if err := json.Unmarshal(body, result); err != nil {
		return invalidResponse(err)
	}
	return nil
}

func (b *OpenObserve) search(ctx context.Context, window Window, sql string, size int, result any) error {
	body, err := b.searchRaw(ctx, window, sql, size)
	if err != nil {
		return err
	}
	return decodeSearch(body, result)
}

var listOrders = map[string]string{
	SortRecent:   "last_event_at DESC, session_id ASC",
	SortDuration: "duration_ms DESC, last_event_at DESC, session_id ASC",
	SortErrors:   "error_turns DESC, last_event_at DESC, session_id ASC",
}

func (b *OpenObserve) List(ctx context.Context, scope Scope, window Window, query ListQuery) (ActivityList, error) {
	suffix, err := b.scopedQuery(scope)
	if err != nil {
		return ActivityList{}, err
	}
	order, found := listOrders[query.Sort]
	if !found {
		return ActivityList{}, InvalidSort()
	}
	offset := query.Offset
	sql := "SELECT oma_session_id AS session_id, MIN(CAST(dsh_event_time AS BIGINT)) AS started_at, MAX(CAST(dsh_event_time AS BIGINT)) AS last_event_at, " +
		"MAX(CAST(dsh_event_time AS BIGINT)) - MIN(CAST(dsh_event_time AS BIGINT)) AS duration_ms, " +
		"COUNT(DISTINCT CASE WHEN dsh_event_type = 'turn/end' AND dsh_turn_end_reason = 'error' THEN concat(dsh_session_id, ':', CAST(dsh_event_seq AS VARCHAR)) END) AS error_turns" +
		suffix + " AND oma_session_id IS NOT NULL GROUP BY oma_session_id ORDER BY " + order + fmt.Sprintf(" LIMIT %d OFFSET %d", trajectoryListSize+1, offset)
	var result struct {
		Hits []SessionActivity `json:"hits"`
	}
	if err := b.search(ctx, window, sql, trajectoryListSize+1, &result); err != nil {
		return ActivityList{}, err
	}
	hasMore := len(result.Hits) > trajectoryListSize
	if hasMore {
		result.Hits = result.Hits[:trajectoryListSize]
	}
	list := ActivityList{Items: result.Hits}
	if hasMore {
		next := offset + len(result.Hits)
		list.NextOffset = &next
	}
	return list, nil
}

type eventHit struct {
	Seq      int64    `json:"seq"`
	Headers  []string `json:"headers"`
	Contents []string `json:"contents"`
}

func (b *OpenObserve) Page(ctx context.Context, scope Scope, window Window, id string, after int64) (Page, error) {
	suffix, err := b.scopedQuery(scope)
	if err != nil {
		return Page{}, err
	}
	sql := "SELECT CAST(dsh_event_seq AS BIGINT) AS seq, ARRAY_AGG(DISTINCT dsh_header) AS headers, ARRAY_AGG(DISTINCT content) AS contents" + suffix + " AND dsh_session_id = " + quote(id) + fmt.Sprintf(" AND CAST(dsh_event_seq AS BIGINT) > %d", after) + " GROUP BY dsh_session_id, CAST(dsh_event_seq AS BIGINT) ORDER BY seq ASC" + fmt.Sprintf(" LIMIT %d", trajectoryPageSize+1)
	var result struct {
		Hits []eventHit `json:"hits"`
	}
	window.Start = time.UnixMicro(1)
	if err := b.search(ctx, window, sql, trajectoryPageSize+1, &result); err != nil {
		return Page{}, err
	}
	return nativePage(id, after, result.Hits)
}

func nativePage(id string, after int64, hits []eventHit) (Page, error) {
	page := Page{Events: []json.RawMessage{}, NextSeq: after, HasMore: len(hits) > trajectoryPageSize}
	if page.HasMore {
		hits = hits[:trajectoryPageSize]
	}
	for _, hit := range hits {
		headerJSON, err := replayValue(hit.Headers)
		if err != nil {
			return Page{}, err
		}
		content, err := replayValue(hit.Contents)
		if err != nil {
			return Page{}, err
		}
		var header nativeHeader
		var event nativeEvent
		if json.Unmarshal([]byte(headerJSON), &header) != nil || header.ID != id || header.Version != 4 || json.Unmarshal([]byte(content), &event) != nil || event.Seq == nil || *event.Seq != hit.Seq || event.Time <= 0 || event.Type == "" || !json.Valid(event.Data) {
			return Page{}, invalidResponse(fmt.Errorf("invalid native trajectory event"))
		}
		if hit.Seq != page.NextSeq+1 {
			return Page{}, historyGap(page.NextSeq+1, hit.Seq)
		}
		if page.Header == nil {
			page.Header = json.RawMessage(headerJSON)
		}
		page.Events = append(page.Events, json.RawMessage(content))
		page.NextSeq = hit.Seq
	}
	return page, nil
}

func replayValue(values []string) (string, error) {
	if len(values) == 0 {
		return "", invalidResponse(fmt.Errorf("missing native trajectory payload"))
	}
	if len(values) == 1 {
		return values[0], nil
	}
	var first any
	for index, raw := range values {
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return "", invalidResponse(err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return "", invalidResponse(fmt.Errorf("invalid native trajectory JSON"))
		}
		if index == 0 {
			first = value
		} else if !reflect.DeepEqual(first, value) {
			return "", eventConflict()
		}
	}
	return values[0], nil
}

package db

import (
	"context"
	"math/big"
	"slices"
	"strconv"
	"time"
)

const (
	AdminGroupModel       = "model"
	AdminGroupWorkspace   = "workspace_id"
	AdminGroupDescription = "description"
)

type AdminReportQuery struct {
	OrganizationUUID string
	StartingAt       time.Time
	EndingBefore     time.Time
	Bucket           time.Duration
	GroupBy          string
	Limit            int
	Offset           int
	USDPerHour       *big.Rat
}

type AdminUsageGroup struct {
	Key       string
	Input     int64
	Output    int64
	CacheRead int64
	Cache5m   int64
	Cache1h   int64
}

type AdminCostGroup struct {
	Key    string
	Amount *big.Rat
}

type AdminBucket struct {
	StartingAt time.Time
	EndingAt   time.Time
	Usage      []AdminUsageGroup
	Costs      []AdminCostGroup
}

type AdminReport struct {
	Buckets []AdminBucket
	HasMore bool
}

func (d *DB) AdminUsageReport(ctx context.Context, query AdminReportQuery) (AdminReport, error) {
	points, err := d.listAdminUsagePoints(ctx, query)
	if err != nil {
		return AdminReport{}, err
	}
	return aggregateAdminUsage(points, query), nil
}

func (d *DB) AdminCostReport(ctx context.Context, query AdminReportQuery) (AdminReport, error) {
	points, err := d.listAdminUsagePoints(ctx, query)
	if err != nil {
		return AdminReport{}, err
	}
	return aggregateAdminCost(points, query), nil
}

func (d *DB) listAdminUsagePoints(ctx context.Context, query AdminReportQuery) ([]adminUsagePointRow, error) {
	if query.OrganizationUUID == "" || query.StartingAt.IsZero() || !query.EndingBefore.After(query.StartingAt) || query.Bucket <= 0 {
		return nil, ErrInvalidState
	}
	return NewSessionEventMapper(d.mapperDB).ListAdminUsagePoints(ctx, adminUsageQuery{
		OrganizationUUID: query.OrganizationUUID,
		StartingAt:       query.StartingAt,
		EndingBefore:     query.EndingBefore,
	})
}

type adminTokenSnap struct {
	input, output, read, cache5m, cache1h int64
}

func aggregateAdminUsage(points []adminUsagePointRow, query AdminReportQuery) AdminReport {
	totals := map[time.Time]map[string]*AdminUsageGroup{}
	var previousSession string
	var previous adminTokenSnap
	for _, point := range points {
		current := adminTokenSnap{
			input: adminInt(point.InputTokens), output: adminInt(point.OutputTokens), read: adminInt(point.CacheReadTokens),
			cache5m: adminInt(point.Cache5mTokens), cache1h: adminInt(point.Cache1hTokens),
		}
		if point.SessionUUID != previousSession {
			previousSession = point.SessionUUID
			previous = adminTokenSnap{}
		}
		delta := adminTokenSnap{
			input: current.input - previous.input, output: current.output - previous.output, read: current.read - previous.read,
			cache5m: current.cache5m - previous.cache5m, cache1h: current.cache1h - previous.cache1h,
		}
		previous = current
		if point.ProcessedAt.Before(query.StartingAt) || !adminTokenPositive(delta) {
			continue
		}
		bucket := alignAdminBucket(point.ProcessedAt, query.Bucket)
		key := adminGroupKey(point, query.GroupBy)
		groups := totals[bucket]
		if groups == nil {
			groups = map[string]*AdminUsageGroup{}
			totals[bucket] = groups
		}
		group := groups[key]
		if group == nil {
			group = &AdminUsageGroup{Key: key}
			groups[key] = group
		}
		group.Input += delta.input
		group.Output += delta.output
		group.CacheRead += delta.read
		group.Cache5m += delta.cache5m
		group.Cache1h += delta.cache1h
	}
	return pageAdminBuckets(query, func(start time.Time) AdminBucket {
		return AdminBucket{StartingAt: start, EndingAt: start.Add(query.Bucket), Usage: adminUsageGroups(totals[start])}
	})
}

func aggregateAdminCost(points []adminUsagePointRow, query AdminReportQuery) AdminReport {
	totals := map[time.Time]map[string]*big.Rat{}
	var previousSession string
	previous := new(big.Rat)
	for _, point := range points {
		current := adminSnapshotCost(point, query.USDPerHour)
		if point.SessionUUID != previousSession {
			previousSession = point.SessionUUID
			previous = new(big.Rat)
		}
		delta := new(big.Rat).Sub(current, previous)
		previous = current
		if point.ProcessedAt.Before(query.StartingAt) || delta.Sign() <= 0 {
			continue
		}
		bucket := alignAdminBucket(point.ProcessedAt, query.Bucket)
		key := adminGroupKey(point, query.GroupBy)
		groups := totals[bucket]
		if groups == nil {
			groups = map[string]*big.Rat{}
			totals[bucket] = groups
		}
		addRat(groups, key, delta)
	}
	return pageAdminBuckets(query, func(start time.Time) AdminBucket {
		return AdminBucket{StartingAt: start, EndingAt: start.Add(query.Bucket), Costs: adminCostGroups(totals[start])}
	})
}

func pageAdminBuckets(query AdminReportQuery, build func(time.Time) AdminBucket) AdminReport {
	aligned := alignAdminBucket(query.StartingAt, query.Bucket)
	count := int(query.EndingBefore.Sub(aligned) / query.Bucket)
	if count < 0 {
		count = 0
	}
	offset := query.Offset
	if offset > count {
		offset = count
	}
	end := offset + query.Limit
	if query.Limit <= 0 || end > count {
		end = count
	}
	buckets := make([]AdminBucket, 0, end-offset)
	for index := offset; index < end; index++ {
		buckets = append(buckets, build(aligned.Add(time.Duration(index)*query.Bucket)))
	}
	return AdminReport{Buckets: buckets, HasMore: end < count}
}

func alignAdminBucket(moment time.Time, bucket time.Duration) time.Time {
	moment = moment.UTC()
	if bucket >= 24*time.Hour {
		year, month, day := moment.Date()
		return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	}
	return moment.Truncate(bucket)
}

func adminGroupKey(point adminUsagePointRow, groupBy string) string {
	switch groupBy {
	case AdminGroupModel:
		if point.Model == "" {
			return "unknown"
		}
		return point.Model
	case AdminGroupWorkspace:
		return point.WorkspaceExternalID
	default:
		return ""
	}
}

func adminUsageGroups(groups map[string]*AdminUsageGroup) []AdminUsageGroup {
	if len(groups) == 0 {
		return []AdminUsageGroup{}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	out := make([]AdminUsageGroup, 0, len(keys))
	for _, key := range keys {
		out = append(out, *groups[key])
	}
	return out
}

func adminCostGroups(groups map[string]*big.Rat) []AdminCostGroup {
	if len(groups) == 0 {
		return []AdminCostGroup{}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	out := make([]AdminCostGroup, 0, len(keys))
	for _, key := range keys {
		out = append(out, AdminCostGroup{Key: key, Amount: groups[key]})
	}
	return out
}

func adminSnapshotCost(point adminUsagePointRow, usdPerHour *big.Rat) *big.Rat {
	if point.RuntimeCostUSD != "" {
		parsed := new(big.Rat)
		if _, ok := parsed.SetString(point.RuntimeCostUSD); ok {
			return parsed
		}
	}
	if usdPerHour == nil || usdPerHour.Sign() <= 0 {
		return new(big.Rat)
	}
	millis := adminInt(point.ActiveMilliseconds)
	return runtimeUSD(millis, usdPerHour)
}

func adminTokenPositive(delta adminTokenSnap) bool {
	return delta.input > 0 || delta.output > 0 || delta.read > 0 || delta.cache5m > 0 || delta.cache1h > 0
}

func adminInt(raw string) int64 {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return value
}

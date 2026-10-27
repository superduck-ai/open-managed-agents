package observability

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

func TestMapStatEmptyAndValues(t *testing.T) {
	empty, err := mapStat(nil)
	if err != nil {
		t.Fatalf("mapStat(nil) error = %v", err)
	}
	if empty.Current != nil || empty.Previous != nil || empty.ChangePercent != nil {
		t.Fatalf("empty stat = %+v", empty)
	}

	current := 10.0
	previous := 5.0
	change := 100.0
	got, err := mapStat([]Row{{"current": current, "previous": previous, "change_percent": change}})
	if err != nil {
		t.Fatalf("mapStat() error = %v", err)
	}
	if got.Current == nil || *got.Current != current || got.Previous == nil || *got.Previous != previous || got.ChangePercent == nil || *got.ChangePercent != change {
		t.Fatalf("stat = %+v", got)
	}
}

func TestMapTimeseriesParsesNaiveUTCAndSorts(t *testing.T) {
	data, err := mapTimeseries([]Row{
		{"timestamp": "2026-08-12T01:00:00", "series": "b", "value": 2.0},
		{"timestamp": "2026-08-12T00:00:00", "series": "a", "value": 1.0},
		{"timestamp": "2026-08-12T00:30:00Z", "series": "a", "value": 3.0},
	})
	if err != nil {
		t.Fatalf("mapTimeseries() error = %v", err)
	}
	if len(data.Series) != 2 || data.Series[0].Name != "a" || data.Series[1].Name != "b" {
		t.Fatalf("series = %#v", data.Series)
	}
	if !data.Series[0].Points[0].Timestamp.Equal(time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("first point = %s", data.Series[0].Points[0].Timestamp)
	}
}

func TestMapTimeseriesEmpty(t *testing.T) {
	data, err := mapTimeseries(nil)
	if err != nil {
		t.Fatalf("mapTimeseries(nil) error = %v", err)
	}
	if data.Series == nil || len(data.Series) != 0 {
		t.Fatalf("empty series = %#v", data.Series)
	}
}

func TestMappersRejectMissingColumns(t *testing.T) {
	if _, err := mapTimeseries([]Row{{"series": "a", "value": 1.0}}); !isInternal(err) {
		t.Fatalf("missing timestamp error = %v", err)
	}
	if _, err := mapCategorical([]Row{{"value": 1.0}}); !isInternal(err) {
		t.Fatalf("missing name error = %v", err)
	}
	if _, err := mapStat([]Row{{"current": "nope"}}); !isInternal(err) {
		t.Fatalf("bad current error = %v", err)
	}
}

func TestOptionalFloatColumnOmitsNonFiniteValues(t *testing.T) {
	for _, value := range []any{float32(math.Inf(1)), json.Number("NaN"), "-Inf"} {
		if _, ok, err := optionalFloatColumn(Row{"value": value}, "value"); err != nil || ok {
			t.Fatalf("optionalFloatColumn(%v) = ok %v, err %v", value, ok, err)
		}
	}
}

func TestMapMultistatSplitsWindowAndSparkline(t *testing.T) {
	data, err := mapPanelRows(renderMultistat, []Row{
		{"name": "p95", "value": 4.0},
		{"name": "p50", "value": 2.0},
		{"name": "p50", "value": 1.0, "timestamp": "2026-08-06T10:00:00"},
		{"name": "p50", "value": 3.0, "timestamp": "2026-08-06T11:00:00"},
		{"name": "p50", "value": 2.5, "timestamp": ""},
	})
	if err != nil {
		t.Fatalf("mapPanelRows(multistat) error = %v", err)
	}
	got, ok := data.(MultistatData)
	if !ok || len(got.Items) != 3 || got.Items[0].Name != "p95" || got.Items[2].Value != 2.5 {
		t.Fatalf("items = %#v", data)
	}
	if len(got.Series) != 1 || got.Series[0].Name != "p50" || len(got.Series[0].Points) != 2 {
		t.Fatalf("series = %#v", got.Series)
	}
}

func TestMapMultistatSkipsRowsWithOmittedValue(t *testing.T) {
	data, err := mapPanelRows(renderMultistat, []Row{
		{"name": "p50"},
		{"name": "p90"},
		{"name": "p95"},
		{"name": "p50", "value": 3.0, "timestamp": "2026-08-06T00:00:00"},
	})
	if err != nil {
		t.Fatalf("mapPanelRows(multistat) error = %v", err)
	}
	got, ok := data.(MultistatData)
	if !ok || len(got.Items) != 0 {
		t.Fatalf("items = %#v", data)
	}
	if len(got.Series) != 1 || got.Series[0].Name != "p50" || got.Series[0].Points[0].Value != 3 {
		t.Fatalf("series = %#v", got.Series)
	}
}

func TestMapCategoricalAndTable(t *testing.T) {
	cat, err := mapCategorical([]Row{{"name": "Bash", "value": 3.0}})
	if err != nil || len(cat.Items) != 1 || cat.Items[0].Name != "Bash" {
		t.Fatalf("categorical = %+v err=%v", cat, err)
	}
	table, err := mapTable([]Row{{"model": "claude-sonnet-4", "requests": 2.0}})
	if err != nil || len(table.Rows) != 1 || table.Rows[0]["model"] != "claude-sonnet-4" {
		t.Fatalf("table = %+v err=%v", table, err)
	}
}

func isInternal(err error) bool {
	var appErr *apperr.Error
	return errors.As(err, &appErr) && appErr.Kind == apperr.Internal && strings.TrimSpace(appErr.PublicMessage) != ""
}

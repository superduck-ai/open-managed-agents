package observability

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed panels.json
var panelsJSON []byte

const (
	renderStat        = "stat"
	renderTimeseries  = "timeseries"
	renderCategorical = "categorical"
	renderMultistat   = "multistat"
	renderTable       = "table"
	traceListRef      = "trace.list"
	traceDetailRef    = "trace.detail"
)

type dashboardFile struct {
	Version int         `json:"version"`
	Tabs    []tabFile   `json:"tabs"`
	Queries []queryFile `json:"queries"`
}

type tabFile struct {
	ID       string      `json:"id"`
	TitleKey string      `json:"title_key"`
	Panels   []panelFile `json:"panels"`
}

type panelFile struct {
	ID         string         `json:"id"`
	TitleKey   string         `json:"title_key"`
	RenderType string         `json:"render_type"`
	Unit       string         `json:"unit"`
	QueryRef   string         `json:"query_ref"`
	Grid       map[string]int `json:"grid"`
	Options    map[string]any `json:"options"`
}

type queryFile struct {
	QueryRef  string         `json:"query_ref"`
	Variables []variableSpec `json:"variables"`
}

// DashboardProjection is the SQL-free dashboard returned to the frontend.
type DashboardProjection struct {
	Version int               `json:"version"`
	Tabs    []TabProjection   `json:"tabs"`
	Queries []QueryProjection `json:"queries"`
}

type TabProjection struct {
	ID       string            `json:"id"`
	TitleKey string            `json:"title_key"`
	Panels   []PanelProjection `json:"panels"`
}

type PanelProjection struct {
	ID         string         `json:"id"`
	TitleKey   string         `json:"title_key"`
	RenderType string         `json:"render_type"`
	Unit       string         `json:"unit"`
	QueryRef   string         `json:"query_ref"`
	Grid       map[string]int `json:"grid"`
	Options    map[string]any `json:"options"`
}

type QueryProjection struct {
	QueryRef  string         `json:"query_ref"`
	Variables []variableSpec `json:"variables"`
}

var (
	loadedDashboard dashboardFile
	queriesByRef    map[string]queryFile
	panelsByQuery   map[string]panelFile
)

func init() {
	if err := json.Unmarshal(panelsJSON, &loadedDashboard); err != nil {
		panic("observability: parse panels.json: " + err.Error())
	}
	queriesByRef = make(map[string]queryFile, len(loadedDashboard.Queries))
	for _, query := range loadedDashboard.Queries {
		if strings.TrimSpace(query.QueryRef) == "" {
			panic("observability: panels.json query_ref is empty")
		}
		if _, exists := queriesByRef[query.QueryRef]; exists {
			panic("observability: duplicate query_ref " + query.QueryRef)
		}
		queriesByRef[query.QueryRef] = query
	}
	panelsByQuery = map[string]panelFile{}
	for _, tab := range loadedDashboard.Tabs {
		for _, panel := range tab.Panels {
			if _, ok := queriesByRef[panel.QueryRef]; !ok {
				panic("observability: panel " + panel.ID + " references unknown query_ref " + panel.QueryRef)
			}
			if _, exists := panelsByQuery[panel.QueryRef]; exists {
				panic("observability: query_ref referenced by multiple panels: " + panel.QueryRef)
			}
			panelsByQuery[panel.QueryRef] = panel
		}
	}
}

// QueryRefs returns the complete query_ref set from panels.json.
func QueryRefs() []string {
	refs := make([]string, 0, len(loadedDashboard.Queries))
	for _, query := range loadedDashboard.Queries {
		refs = append(refs, query.QueryRef)
	}
	return refs
}

func querySpec(queryRef string) (queryFile, bool) {
	spec, ok := queriesByRef[queryRef]
	return spec, ok
}

// QueryRenderType returns the panel render type for a query_ref.
func QueryRenderType(queryRef string) (string, bool) {
	if panel, ok := panelsByQuery[queryRef]; ok {
		return panel.RenderType, true
	}
	if strings.HasPrefix(queryRef, "trace.trend.") {
		return renderTimeseries, true
	}
	return "", false
}

func projectDashboard() DashboardProjection {
	tabs := make([]TabProjection, 0, len(loadedDashboard.Tabs))
	for _, tab := range loadedDashboard.Tabs {
		panels := make([]PanelProjection, 0, len(tab.Panels))
		for _, panel := range tab.Panels {
			panels = append(panels, PanelProjection{
				ID:         panel.ID,
				TitleKey:   panel.TitleKey,
				RenderType: panel.RenderType,
				Unit:       panel.Unit,
				QueryRef:   panel.QueryRef,
				Grid:       panel.Grid,
				Options:    panel.Options,
			})
		}
		tabs = append(tabs, TabProjection{ID: tab.ID, TitleKey: tab.TitleKey, Panels: panels})
	}
	queries := make([]QueryProjection, 0, len(loadedDashboard.Queries))
	for _, query := range loadedDashboard.Queries {
		if query.QueryRef == traceListRef || query.QueryRef == traceDetailRef {
			continue
		}
		queries = append(queries, QueryProjection{QueryRef: query.QueryRef, Variables: query.Variables})
	}
	return DashboardProjection{Version: loadedDashboard.Version, Tabs: tabs, Queries: queries}
}

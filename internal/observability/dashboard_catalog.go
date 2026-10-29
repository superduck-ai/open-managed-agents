package observability

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/template"
)

//go:embed dashboard
var dashboardFiles embed.FS

const (
	windowCurrent = "current"
	windowCompare = "compare"
	filterModel   = "model"
	filterTool    = "tool"
	requireAgent  = "agent"
)

const lookbackProbe int64 = 7_919_000_000_001

var panelRenders = []string{"stat", "percentiles", "series", "bars", "table", "subagents"}

type Column struct {
	Key  string `json:"key"`
	Unit string `json:"unit"`
}

type panelSpec struct {
	ID          string   `json:"id"`
	Render      string   `json:"render"`
	Query       string   `json:"query"`
	Field       string   `json:"field"`
	Secondary   string   `json:"secondary"`
	Compare     bool     `json:"compare"`
	Unit        string   `json:"unit"`
	Span        int      `json:"span"`
	Chart       string   `json:"chart"`
	Stacked     bool     `json:"stacked"`
	Order       []string `json:"order"`
	Top         int      `json:"top"`
	Other       bool     `json:"other"`
	Requires    string   `json:"requires"`
	Columns     []Column `json:"columns"`
	Trend       string   `json:"trend"`
	TrendSeries string   `json:"trend_series"`
}

type querySpec struct {
	Window   string `json:"window"`
	Size     int    `json:"size"`
	Lookback bool   `json:"lookback"`
}

type tabSpec struct {
	ID      string      `json:"id"`
	Filter  string      `json:"filter"`
	Options string      `json:"options"`
	Panels  []panelSpec `json:"panels"`
}

type dashboardCatalog struct {
	Queries   map[string]querySpec `json:"queries"`
	Tabs      []tabSpec            `json:"tabs"`
	templates *template.Template
}

type sqlParams struct {
	Source   string
	Start    int64
	From     int64
	End      int64
	Interval int64
	Model    string
	Tool     string
}

var dashboards = sync.OnceValues(loadDashboardCatalog)

func loadDashboardCatalog() (*dashboardCatalog, error) {
	raw, err := dashboardFiles.ReadFile("dashboard/catalog.json")
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	catalog := &dashboardCatalog{}
	if err := decoder.Decode(catalog); err != nil {
		return nil, fmt.Errorf("decode dashboard catalog: %w", err)
	}
	catalog.templates, err = template.New("dashboard").Option("missingkey=error").Funcs(template.FuncMap{"q": quote}).ParseFS(dashboardFiles, "dashboard/queries.sql")
	if err != nil {
		return nil, fmt.Errorf("parse dashboard queries: %w", err)
	}
	return catalog, catalog.validate()
}

func (c *dashboardCatalog) validate() error {
	for name, query := range c.Queries {
		if c.templates.Lookup(name) == nil {
			return fmt.Errorf("dashboard query %s has no SQL template", name)
		}
		if query.Size <= 0 || !slices.Contains([]string{"", windowCurrent, windowCompare}, query.Window) {
			return fmt.Errorf("dashboard query %s has an invalid size or window", name)
		}
		if query.Lookback {
			sql, err := c.render(name, sqlParams{From: lookbackProbe})
			if err != nil || !strings.Contains(sql, strconv.FormatInt(lookbackProbe, 10)) {
				return fmt.Errorf("dashboard query %s looks back but does not bound rows by .From", name)
			}
		}
	}
	seen := map[string]bool{}
	for _, tab := range c.Tabs {
		if tab.ID == "" || seen[tab.ID] || !slices.Contains([]string{"", filterModel, filterTool}, tab.Filter) || (tab.Filter == "") != (tab.Options == "") {
			return fmt.Errorf("dashboard tab %q is invalid", tab.ID)
		}
		seen[tab.ID] = true
		if _, found := c.Queries[tab.Options]; tab.Options != "" && !found {
			return fmt.Errorf("dashboard tab %s references unknown options query %s", tab.ID, tab.Options)
		}
		for _, panel := range tab.Panels {
			if err := c.validatePanel(panel); err != nil {
				return fmt.Errorf("dashboard panel %s.%s: %w", tab.ID, panel.ID, err)
			}
		}
	}
	return nil
}

func (c *dashboardCatalog) validatePanel(panel panelSpec) error {
	query, found := c.Queries[panel.Query]
	switch {
	case panel.ID == "" || !slices.Contains(panelRenders, panel.Render):
		return fmt.Errorf("unknown render %q", panel.Render)
	case !found:
		return fmt.Errorf("unknown query %q", panel.Query)
	case panel.Span < 1 || panel.Span > 12:
		return fmt.Errorf("span must be between 1 and 12")
	case panel.Render == "stat" && panel.Field == "":
		return fmt.Errorf("stat panels need a field")
	case panel.Compare && query.Window != windowCompare:
		return fmt.Errorf("compared panels need a compare query")
	case (panel.Render == "table" || panel.Render == "subagents") && len(panel.Columns) == 0:
		return fmt.Errorf("tables need columns")
	case panel.Requires != "" && panel.Requires != requireAgent:
		return fmt.Errorf("unknown requirement %q", panel.Requires)
	case panel.Trend != "" && panel.Render != "stat":
		return fmt.Errorf("only stat panels have a trend")
	}
	if _, found := c.Queries[panel.Trend]; panel.Trend != "" && !found {
		return fmt.Errorf("unknown trend query %q", panel.Trend)
	}
	return nil
}

func (c *dashboardCatalog) tab(id string) (tabSpec, bool) {
	index := slices.IndexFunc(c.Tabs, func(tab tabSpec) bool { return tab.ID == id })
	if index < 0 {
		return tabSpec{}, false
	}
	return c.Tabs[index], true
}

func (c *dashboardCatalog) render(name string, params sqlParams) (string, error) {
	var sql strings.Builder
	if err := c.templates.ExecuteTemplate(&sql, name, params); err != nil {
		return "", err
	}
	return sql.String(), nil
}

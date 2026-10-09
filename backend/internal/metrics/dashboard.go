package metrics

import (
	"encoding/json"
	"strings"
)

// The Grafana dashboard the chart ships with metrics.grafanaDashboard
// (docs/adr/0060 D3): made from the rows below, so that a renamed or removed
// instrument fails a test of this package instead of leaving a panel empty.
// `make generate` writes it into the chart (tools/dashboard).

// selector is what every query of the dashboard narrows its series to: the
// namespace the dashboard's variable picks. $sel in a query stands for it.
const selector = `namespace=~"$namespace"`

// backendOnly keeps the Go runtime's and the process's series to the backend
// container: the frontend's nginx exporter, when it is scraped, has them too.
const backendOnly = `$sel,container="backend"`

// byPod is the legend of a series per pod, byTenant of one per tenant id.
const (
	byPod    = "{{pod}}"
	byTenant = "{{tenant}}"
)

// The units of the panels, as Grafana names them.
const (
	unitRequests = "reqps"
	unitPerSec   = "ops"
	unitSeconds  = "s"
	unitCount    = "short"
	unitBytes    = "bytes"
	unitNone     = "none"
)

type dashboardRow struct {
	title  string
	panels []dashboardPanel
}

type dashboardPanel struct {
	title string
	// stat makes a single figure of the last value; otherwise a time series.
	stat bool
	unit string
	// red is the value from which a stat turns red; 0 for none.
	red     float64
	queries []dashboardQuery
}

type dashboardQuery struct{ expr, legend string }

var dashboardRows = []dashboardRow{
	{"HTTP", []dashboardPanel{
		{title: "Requests by status", unit: unitRequests, queries: []dashboardQuery{
			{`sum by (status) (rate(cowork_http_requests_total{$sel}[$__rate_interval]))`, "{{status}}"}}},
		{title: "Server errors by route", unit: unitRequests, queries: []dashboardQuery{
			{`sum by (method, route) (rate(cowork_http_requests_total{$sel,status=~"5.."}[$__rate_interval]))`, "{{method}} {{route}}"}}},
		{title: "Latency, 95th percentile, by route", unit: unitSeconds, queries: []dashboardQuery{
			{`histogram_quantile(0.95, sum by (method, route, le) (rate(cowork_http_request_duration_seconds_bucket{$sel}[$__rate_interval])))`, "{{method}} {{route}}"}}},
		{title: "Requests in flight", unit: unitCount, queries: []dashboardQuery{
			{`sum by (pod) (cowork_http_requests_in_flight{$sel})`, byPod}}},
	}},
	{"Database", []dashboardPanel{
		{title: "Pool connections", unit: unitCount, queries: []dashboardQuery{
			{`sum by (state) (cowork_db_pool_connections{$sel})`, "{{state}}"},
			{`sum(cowork_db_pool_max_connections{$sel})`, "maximum"}}},
		{title: "Mean time to acquire a connection", unit: unitSeconds, queries: []dashboardQuery{
			{`sum(rate(cowork_db_pool_acquire_duration_seconds_sum{$sel}[$__rate_interval])) / sum(rate(cowork_db_pool_acquire_duration_seconds_count{$sel}[$__rate_interval]))`, "mean"}}},
		{title: "Waiting for a connection", unit: unitCount, queries: []dashboardQuery{
			{`sum by (pod) (rate(cowork_db_pool_wait_duration_seconds_sum{$sel}[$__rate_interval]))`, "waiting, mean {{pod}}"},
			{`sum by (pod) (rate(cowork_db_pool_canceled_acquires_total{$sel}[$__rate_interval]))`, "given up per second {{pod}}"}}},
		{title: "Query errors by kind", unit: unitPerSec, queries: []dashboardQuery{
			{`sum by (kind) (rate(cowork_db_query_errors_total{$sel}[$__rate_interval]))`, "{{kind}}"}}},
		{title: "Schema version", stat: true, unit: unitNone, queries: []dashboardQuery{
			{`max(cowork_migrations_schema_version{$sel})`, "version"}}},
		{title: "Schema dirty", stat: true, unit: unitNone, red: 1, queries: []dashboardQuery{
			{`max(cowork_migrations_schema_dirty{$sel})`, "dirty"}}},
	}},
	{"Background jobs", []dashboardPanel{
		{title: "Runs and failures per hour", unit: unitCount, queries: []dashboardQuery{
			{`sum by (name) (increase(cowork_jobs_runs_total{$sel}[1h]))`, "{{name}} runs"},
			{`sum by (name) (increase(cowork_jobs_failures_total{$sel}[1h]))`, "{{name}} failures"}}},
		{title: "Consecutive failures", unit: unitCount, queries: []dashboardQuery{
			{`max by (name) (cowork_jobs_consecutive_failures{$sel})`, "{{name}}"}}},
		{title: "Mean duration of a run", unit: unitSeconds, queries: []dashboardQuery{
			{`sum by (name) (rate(cowork_jobs_duration_seconds_sum{$sel}[2h])) / sum by (name) (rate(cowork_jobs_duration_seconds_count{$sel}[2h]))`, "{{name}}"}}},
	}},
	{"Event stream", []dashboardPanel{
		{title: "Open streams", unit: unitCount, queries: []dashboardQuery{
			{`sum by (pod) (cowork_events_open_streams{$sel})`, byPod}}},
		{title: "Events published", unit: unitPerSec, queries: []dashboardQuery{
			{`sum by (pod) (rate(cowork_events_published_total{$sel}[$__rate_interval]))`, byPod}}},
		{title: "Streams dropped by reason", unit: unitPerSec, queries: []dashboardQuery{
			{`sum by (reason) (rate(cowork_events_subscribers_dropped_total{$sel}[$__rate_interval]))`, "{{reason}}"}}},
		{title: "Replays", unit: unitPerSec, queries: []dashboardQuery{
			{`sum by (outcome) (rate(cowork_events_replays_total{$sel}[$__rate_interval]))`, "{{outcome}}"}}},
	}},
	{"Audit and login", []dashboardPanel{
		{title: "Acts by actor", unit: unitPerSec, queries: []dashboardQuery{
			{`sum by (actor) (rate(cowork_audit_acts_total{$sel}[$__rate_interval]))`, "{{actor}}"}}},
		{title: "Logins by method and outcome", unit: unitPerSec, queries: []dashboardQuery{
			{`sum by (method, outcome) (rate(cowork_auth_logins_total{$sel}[$__rate_interval]))`, "{{method}} {{outcome}}"}}},
		{title: "Lockouts and refused tokens", unit: unitPerSec, queries: []dashboardQuery{
			{`sum(rate(cowork_auth_lockouts_total{$sel}[$__rate_interval]))`, "lockouts"},
			{`sum by (reason) (rate(cowork_auth_token_refusals_total{$sel}[$__rate_interval]))`, "token {{reason}}"}}},
	}},
	{"Consistency and export", []dashboardPanel{
		{title: "Dangling attachment metadata by tenant", unit: unitCount, queries: []dashboardQuery{
			{`max by (tenant) (cowork_consistency_dangling_attachments{$sel})`, byTenant}}},
		{title: "Orphaned objects by tenant", unit: unitCount, queries: []dashboardQuery{
			{`max by (tenant) (cowork_consistency_orphaned_objects{$sel})`, byTenant}}},
		{title: "Time since the last export by tenant", unit: unitSeconds, queries: []dashboardQuery{
			{`max by (tenant) (cowork_consistency_last_export_age_seconds{$sel})`, byTenant}}},
	}},
	{"Process", []dashboardPanel{
		{title: "Goroutines", unit: unitCount, queries: []dashboardQuery{
			{`sum by (pod) (go_goroutines{` + backendOnly + `})`, byPod}}},
		{title: "Resident memory", unit: unitBytes, queries: []dashboardQuery{
			{`sum by (pod) (process_resident_memory_bytes{` + backendOnly + `})`, byPod}}},
		{title: "CPU", unit: unitCount, queries: []dashboardQuery{
			{`sum by (pod) (rate(process_cpu_seconds_total{` + backendOnly + `}[$__rate_interval]))`, byPod}}},
	}},
}

// Dashboard is the Grafana dashboard's JSON, indented, with a final newline.
func Dashboard() ([]byte, error) {
	b, err := json.MarshalIndent(dashboard(), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// DashboardQueries are the PromQL expressions of every panel, as Grafana
// sends them but for the variables: what a test checks the instruments
// against.
func DashboardQueries() []string {
	var out []string
	for _, row := range dashboardRows {
		for _, p := range row.panels {
			for _, q := range p.queries {
				out = append(out, expand(q.expr))
			}
		}
	}
	return out
}

func expand(expr string) string {
	return strings.ReplaceAll(expr, "$sel", selector)
}

// The dashboard's JSON model, as far as cowork sets it; Grafana fills the
// rest with its defaults.
type (
	grafanaDashboard struct {
		Title         string       `json:"title"`
		UID           string       `json:"uid"`
		Description   string       `json:"description"`
		Tags          []string     `json:"tags"`
		Editable      bool         `json:"editable"`
		GraphTooltip  int          `json:"graphTooltip"`
		Refresh       string       `json:"refresh"`
		SchemaVersion int          `json:"schemaVersion"`
		Version       int          `json:"version"`
		Time          grafanaTime  `json:"time"`
		Timezone      string       `json:"timezone"`
		Annotations   grafanaList  `json:"annotations"`
		Links         []any        `json:"links"`
		Templating    grafanaVars  `json:"templating"`
		Panels        []grafanaRow `json:"panels"`
	}
	grafanaTime struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	grafanaList struct {
		List []any `json:"list"`
	}
	grafanaVars struct {
		List []grafanaVariable `json:"list"`
	}
	grafanaVariable struct {
		Name       string             `json:"name"`
		Label      string             `json:"label"`
		Type       string             `json:"type"`
		Datasource *grafanaDatasource `json:"datasource,omitempty"`
		Definition string             `json:"definition,omitempty"`
		Query      any                `json:"query"`
		Current    struct{}           `json:"current"`
		Hide       int                `json:"hide"`
		IncludeAll bool               `json:"includeAll"`
		AllValue   string             `json:"allValue,omitempty"`
		Multi      bool               `json:"multi"`
		Options    []any              `json:"options"`
		Refresh    int                `json:"refresh"`
		Regex      string             `json:"regex"`
		Sort       int                `json:"sort"`
	}
	grafanaQuery struct {
		Query string `json:"query"`
		RefID string `json:"refId"`
	}
	grafanaDatasource struct {
		Type string `json:"type"`
		UID  string `json:"uid"`
	}
	grafanaGrid struct {
		H int `json:"h"`
		W int `json:"w"`
		X int `json:"x"`
		Y int `json:"y"`
	}
	// grafanaRow is a row or a panel: Grafana keeps both in one list.
	grafanaRow struct {
		ID          int                 `json:"id"`
		Type        string              `json:"type"`
		Title       string              `json:"title"`
		GridPos     grafanaGrid         `json:"gridPos"`
		Collapsed   *bool               `json:"collapsed,omitempty"`
		Datasource  *grafanaDatasource  `json:"datasource,omitempty"`
		FieldConfig *grafanaFieldConfig `json:"fieldConfig,omitempty"`
		Options     any                 `json:"options,omitempty"`
		Targets     []grafanaTarget     `json:"targets,omitempty"`
	}
	grafanaFieldConfig struct {
		Defaults  grafanaDefaults `json:"defaults"`
		Overrides []any           `json:"overrides"`
	}
	grafanaDefaults struct {
		Unit       string             `json:"unit"`
		Thresholds *grafanaThresholds `json:"thresholds,omitempty"`
	}
	grafanaThresholds struct {
		Mode  string        `json:"mode"`
		Steps []grafanaStep `json:"steps"`
	}
	grafanaStep struct {
		Color string   `json:"color"`
		Value *float64 `json:"value"`
	}
	grafanaTarget struct {
		RefID        string            `json:"refId"`
		Datasource   grafanaDatasource `json:"datasource"`
		Expr         string            `json:"expr"`
		LegendFormat string            `json:"legendFormat"`
	}
	grafanaSeriesOptions struct {
		Legend struct {
			Calcs       []string `json:"calcs"`
			DisplayMode string   `json:"displayMode"`
			Placement   string   `json:"placement"`
			ShowLegend  bool     `json:"showLegend"`
		} `json:"legend"`
		Tooltip struct {
			Mode string `json:"mode"`
			Sort string `json:"sort"`
		} `json:"tooltip"`
	}
	grafanaStatOptions struct {
		ColorMode     string `json:"colorMode"`
		GraphMode     string `json:"graphMode"`
		TextMode      string `json:"textMode"`
		ReduceOptions struct {
			Calcs  []string `json:"calcs"`
			Fields string   `json:"fields"`
			Values bool     `json:"values"`
		} `json:"reduceOptions"`
	}
)

// promSource is the dashboard's data source: the one its variable picks.
var promSource = grafanaDatasource{Type: "prometheus", UID: "${datasource}"}

const namespaces = "label_values(cowork_http_requests_in_flight, namespace)"

func dashboard() grafanaDashboard {
	d := grafanaDashboard{
		Title:         "cowork",
		UID:           "cowork-backend",
		Description:   "The cowork backend's metrics (docs/adr/0060), generated by make generate from backend/internal/metrics/dashboard.go.",
		Tags:          []string{"cowork"},
		Editable:      true,
		GraphTooltip:  1,
		Refresh:       "30s",
		SchemaVersion: 39,
		Version:       1,
		Time:          grafanaTime{From: "now-6h", To: "now"},
		Timezone:      "browser",
		Annotations:   grafanaList{List: []any{}},
		Links:         []any{},
		Templating: grafanaVars{List: []grafanaVariable{
			{Name: "datasource", Label: "Data source", Type: "datasource", Query: promSource.Type, Options: []any{}, Refresh: 1},
			{Name: "namespace", Label: "Namespace", Type: "query", Datasource: &promSource, Definition: namespaces,
				Query: grafanaQuery{Query: namespaces, RefID: "namespace"}, IncludeAll: true, AllValue: ".*",
				Options: []any{}, Refresh: 2, Sort: 1},
		}},
	}
	id, y := 0, 0
	collapsed := false
	for _, row := range dashboardRows {
		id++
		d.Panels = append(d.Panels, grafanaRow{ID: id, Type: "row", Title: row.title, Collapsed: &collapsed,
			GridPos: grafanaGrid{H: 1, W: 24, X: 0, Y: y}})
		y++
		x, height := 0, 0
		for _, p := range row.panels {
			id++
			h := 8
			if p.stat {
				h = 4
			}
			d.Panels = append(d.Panels, panelOf(p, id, grafanaGrid{H: h, W: 12, X: x, Y: y}))
			height = max(height, h)
			if x += 12; x == 24 {
				x, y, height = 0, y+height, 0
			}
		}
		y += height
	}
	return d
}

func panelOf(p dashboardPanel, id int, grid grafanaGrid) grafanaRow {
	targets := make([]grafanaTarget, 0, len(p.queries))
	for i, q := range p.queries {
		targets = append(targets, grafanaTarget{RefID: string(rune('A' + i)), Datasource: promSource,
			Expr: expand(q.expr), LegendFormat: q.legend})
	}
	out := grafanaRow{ID: id, Title: p.title, GridPos: grid, Datasource: &promSource, Targets: targets,
		FieldConfig: &grafanaFieldConfig{Defaults: grafanaDefaults{Unit: p.unit}, Overrides: []any{}}}
	if !p.stat {
		var o grafanaSeriesOptions
		o.Legend.Calcs, o.Legend.DisplayMode, o.Legend.Placement, o.Legend.ShowLegend = []string{}, "list", "bottom", true
		o.Tooltip.Mode, o.Tooltip.Sort = "multi", "desc"
		out.Type, out.Options = "timeseries", o
		return out
	}
	steps := []grafanaStep{{Color: "green"}}
	if p.red > 0 {
		steps = append(steps, grafanaStep{Color: "red", Value: &p.red})
	}
	out.FieldConfig.Defaults.Thresholds = &grafanaThresholds{Mode: "absolute", Steps: steps}
	var o grafanaStatOptions
	o.ColorMode, o.GraphMode, o.TextMode = "value", "none", "value"
	o.ReduceOptions.Calcs, o.ReduceOptions.Fields = []string{"lastNotNull"}, ""
	out.Type, out.Options = "stat", o
	return out
}

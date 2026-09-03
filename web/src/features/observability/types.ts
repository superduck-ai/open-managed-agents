export type ObservabilityScope =
  { kind: 'workspace' } | { kind: 'agent'; agentId: string } | { kind: 'session'; sessionId: string };

export type ObservabilityScopeOption = {
  id: string;
  label: string;
  description?: string;
};

export type ObservabilityTabId = 'overview' | 'model' | 'tool' | 'traces';

export type ObservabilityVariableSpec = {
  name: string;
  type: 'time' | 'string' | 'string_list' | 'int_list';
  required: boolean;
};

export type ObservabilityPanelColumn = {
  key: string;
  label_key: string;
  format: 'string' | 'number' | 'duration_ms' | 'percent' | 'tokens';
  sortable?: boolean;
};

export type ObservabilityPanelOptions = {
  stacked?: boolean;
  series_units?: Record<string, string>;
  columns?: ObservabilityPanelColumn[];
  chart?: 'bar' | 'pie' | 'histogram';
  subtitle_key?: string;
};

export type ObservabilityPanel = {
  id: string;
  title_key: string;
  render_type: 'stat' | 'timeseries' | 'categorical' | 'multistat' | 'table';
  unit: string;
  query_ref: string;
  grid: { x: number; y: number; w: number; h: number };
  options?: ObservabilityPanelOptions;
};

export type ObservabilityTab = {
  id: string;
  title_key: string;
  panels: ObservabilityPanel[];
};

export type ObservabilityQuery = {
  query_ref: string;
  variables: ObservabilityVariableSpec[];
};

export type ObservabilityDashboard = {
  version: number;
  tabs: ObservabilityTab[];
  queries: ObservabilityQuery[];
};

export type StatPanelData = {
  current: number | null;
  previous: number | null;
  change_percent: number | null;
};

export type TimeseriesPoint = {
  timestamp: string;
  value: number;
};

export type TimeseriesSeries = {
  name: string;
  points: TimeseriesPoint[];
};

export type TimeseriesPanelData = {
  series: TimeseriesSeries[];
};

export type CategoricalPanelData = {
  items: Array<{ name: string; value: number }>;
};

export type MultistatPanelData = CategoricalPanelData & {
  series?: TimeseriesSeries[];
};

export type TablePanelData = {
  rows: Array<Record<string, unknown>>;
};

export type ObservabilityPanelResult = {
  query_ref: string;
  render_type: ObservabilityPanel['render_type'];
  data_as_of: string;
  data: StatPanelData | TimeseriesPanelData | CategoricalPanelData | MultistatPanelData | TablePanelData;
};

export type ObservabilityTraceListItem = {
  trace_id: string;
  agent_id?: string;
  session_id: string;
  start_time: string;
  duration_ms: number;
  tokens: number;
  llm_calls: number;
  tool_calls: number;
  input?: string;
  output?: string;
  status: 'ok' | 'error';
};

export type ObservabilityTraceList = {
  data_as_of: string;
  has_more: boolean;
  items: ObservabilityTraceListItem[];
};

export type ObservabilitySpanEvent = {
  name: string;
  timestamp: string;
  attributes: Record<string, string>;
};

export type ObservabilitySpan = {
  span_id: string;
  parent_span_id: string;
  kind: string;
  name: string;
  start_time: string;
  end_time: string;
  duration_ms: number;
  status: 'ok' | 'error';
  attributes: Record<string, string>;
  events?: ObservabilitySpanEvent[];
};

export type ObservabilityTraceDetail = {
  trace_id: string;
  data_as_of: string;
  spans: ObservabilitySpan[];
  truncated: boolean;
};

export type PanelQueryVariables = {
  start_time: string;
  end_time: string;
  agent_id?: string;
  session_id?: string;
  agent_version?: number[];
  model?: string[];
  tool?: string[];
};

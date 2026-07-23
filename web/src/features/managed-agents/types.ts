import { type ServerSentEvent } from '../../shared/api/streaming';
import { useI18n } from '../../shared/i18n';
import { type ComponentType, type MutableRefObject, type ReactNode, type RefObject } from 'react';
import { z } from 'zod';
import { agentEditConfigSchema } from './agentConfig';

export type ManagedAgentSection =
  | 'quickstart'
  | 'agents'
  | 'sessions'
  | 'deployments'
  | 'environments'
  | 'credential-vaults'
  | 'memory-stores'
  | 'dreams';

export type IconComponent = ComponentType<{ className?: string; 'aria-hidden'?: boolean }>;

export type I18nMsg = ReturnType<typeof useI18n>['msg'];

export type TemplateTag = {
  label: string;
  icon: IconComponent;
  tone: string;
};

export type AgentTemplate = {
  id: string;
  slug: string;
  title: string;
  body: string;
  prompt: string;
  tags?: TemplateTag[];
};

export type CodeFormat = 'YAML' | 'JSON';

export type IntegrationSnippetLanguage = 'cli' | 'python' | 'typescript' | 'curl';

export type AgentPanelTab = 'config' | 'preview';

export type AgentApiResponse = {
  id: string;
  archived_at: string | null;
  created_at: string;
  description: string | null;
  mcp_servers: unknown[];
  metadata: Record<string, string>;
  model: string | { id?: string; speed?: string };
  multiagent: unknown | null;
  name: string;
  skills: unknown[];
  system: string | null;
  tools: Array<Record<string, unknown>>;
  type: 'agent';
  updated_at: string;
  version: number;
};

export type AgentPageResponse = {
  data: AgentApiResponse[];
  next_page: string | null;
};

export type AgentDetailTab = 'config' | 'sessions' | 'deployments' | 'observability';

export type AgentDetailVersionFilter = number | null;

export type AgentDetailStatusFilter = 'all' | 'active' | 'idle' | 'running' | 'terminated' | 'rescheduling';

export type AgentDetailCreatedFilter = 'all_time' | 'today' | 'last_hour' | 'last_day' | 'last_7_days' | 'last_30_days';

export type AgentDetailSessionFilters = {
  created: AgentDetailCreatedFilter;
  version: AgentDetailVersionFilter;
  deploymentId: string;
  status: AgentDetailStatusFilter;
  cursor: PageCursor;
};

export type AgentDetailDeploymentFilters = {
  cursor: PageCursor;
};

export type AnalyticsMetricBucket = {
  value?: unknown;
  total?: unknown;
  p50?: unknown;
  p95?: unknown;
  count?: unknown;
  [key: string]: unknown;
};

export type AgentSessionAnalyticsOverview = {
  sessions_count?: number | AnalyticsMetricBucket;
  error_rate?: number | AnalyticsMetricBucket;
  input_tokens?: AnalyticsMetricBucket;
  output_tokens?: AnalyticsMetricBucket;
  duration?: AnalyticsMetricBucket;
  active_time?: AnalyticsMetricBucket;
  input_tokens_per_session?: AnalyticsMetricBucket;
  output_tokens_per_session?: AnalyticsMetricBucket;
  turns_per_session?: AnalyticsMetricBucket;
  tool_call_counts?: Record<string, unknown>;
  stop_reason_counts?: Record<string, unknown>;
  data_as_of?: string | null;
  [key: string]: unknown;
};

export type AgentSessionAnalyticsTimeseries = {
  data?: Array<Record<string, unknown>>;
  data_points?: Array<Record<string, unknown>>;
  [key: string]: unknown;
};

export type AgentUpdateInput = {
  version: number;
  name: string;
  description: string | null;
  model: AgentModelInput;
  system: string | null;
  tools: unknown[];
  mcp_servers: unknown[];
  skills: unknown[];
  metadata: Record<string, unknown>;
  multiagent: unknown | null;
};

export type ManagedEntitySection = Exclude<ManagedAgentSection, 'quickstart' | 'agents' | 'dreams'>;

export type PageResponse<T> = {
  data: T[];
  next_page: string | null;
  prefixes?: unknown[];
};

export type PageCursor = string | null;

export type AgentLoadMode = 'list' | 'search' | 'retrieve';

export type AgentCreatedPreset = 'all' | 'last7' | 'last30';

// `from`/`to` (only present for custom ranges) are calendar dates encoded as
// `yyyy-MM-dd` strings. They are converted to inclusive UTC ISO-8601 bounds at
// the API boundary in `createdFilterRange`.
export type AgentCreatedFilter =
  { kind: 'all' } | { kind: 'last7' } | { kind: 'last30' } | { kind: 'custom'; from: string; to: string };

export type CustomCreatedFilter = Extract<AgentCreatedFilter, { kind: 'custom' }>;

export function isCustomCreatedFilter(filter: AgentCreatedFilter): filter is CustomCreatedFilter {
  return filter.kind === 'custom';
}

export type AgentStatusFilter = 'active' | 'all';

export type AgentFilterMenu = 'created' | 'status';

export type AgentListFilters = {
  created: AgentCreatedFilter;
  status: AgentStatusFilter;
};

export type ManagedEntityListFilters = {
  agentId?: string;
  created?: AgentDetailCreatedFilter;
  deploymentId?: string;
  includeArchived?: boolean;
  status?: 'active' | 'paused' | 'all';
  statuses?: string[];
};

export type AgentSearchResponse = AgentPageResponse & {
  truncated?: boolean;
};

export type SessionApiResponse = {
  id: string;
  agent: unknown;
  archived_at: string | null;
  created_at: string;
  deployment_id?: string | null;
  environment_id: string;
  stats?: unknown;
  status: string;
  title?: string | null;
  type: 'session';
  updated_at: string;
  usage?: unknown;
  vault_ids?: unknown;
};

export type DeploymentApiResponse = {
  id: string;
  agent: unknown;
  archived_at: string | null;
  created_at: string;
  description?: string | null;
  environment_id: string;
  initial_events?: unknown;
  name: string;
  paused_reason?: unknown;
  resources?: unknown;
  schedule?: unknown;
  status: string;
  type: 'deployment';
  updated_at: string;
  vault_ids?: unknown;
};

export type EnvironmentApiResponse = {
  id: string;
  archived_at: string | null;
  config: unknown;
  created_at: string;
  description: string;
  name: string;
  scope: string;
  state: string;
  type: 'environment';
  updated_at: string;
};

export type VaultApiResponse = {
  id: string;
  archived_at: string | null;
  created_at: string;
  display_name: string;
  type: 'vault';
  updated_at: string;
};

export type MemoryStoreApiResponse = {
  id: string;
  archived_at: string | null;
  created_at: string;
  description: string;
  name: string;
  type: 'memory_store';
  updated_at: string;
};

export type ManagedEntityApiResponse =
  SessionApiResponse | DeploymentApiResponse | EnvironmentApiResponse | VaultApiResponse | MemoryStoreApiResponse;

export type VaultCredentialApiResponse = {
  id: string;
  archived_at: string | null;
  auth?: unknown;
  created_at: string;
  display_name: string;
  metadata?: unknown;
  type: 'vault_credential';
  updated_at: string;
  vault_id: string;
};

export type MemoryApiResponse = {
  id: string;
  content?: string | null;
  content_sha256?: string | null;
  content_size_bytes?: number | null;
  created_at: string;
  memory_store_id: string;
  memory_version_id?: string | null;
  path: string;
  type: 'memory' | 'memory_prefix';
  updated_at?: string;
};

export type MemoryViewMode = 'preview' | 'source';

export type MemoryBranchState = {
  loading: boolean;
  error: string | null;
  data: MemoryApiResponse[];
  prefixes: string[];
};

export type MemoryTreeNode =
  | {
      type: 'folder';
      path: string;
      label: string;
      depth: number;
      expanded: boolean;
      loading: boolean;
      error: string | null;
    }
  | { type: 'memory'; memory: MemoryApiResponse; label: string; depth: number };

export type DeploymentRunApiResponse = {
  id: string;
  created_at: string;
  deployment_id: string;
  error?: unknown;
  session_id?: string | null;
  trigger?: unknown;
  trigger_type?: string | null;
  type: 'deployment_run';
};

export type EnvironmentWorkApiResponse = {
  id: string;
  created_at: string;
  environment_id: string;
  metadata?: unknown;
  status?: string;
  type: 'environment_work';
  updated_at?: string;
};

export type SessionResourceApiResponse = {
  id?: string;
  created_at?: string;
  type?: string;
  [key: string]: unknown;
};

export type SessionThreadApiResponse = {
  id: string;
  archived_at?: string | null;
  created_at: string;
  parent_thread_id?: string | null;
  type: string;
  updated_at?: string;
};

export type QuickstartSessionEvent = Record<string, unknown>;

export type SessionDetailEventCache = {
  events: QuickstartSessionEvent[];
  syncedThrough: PageCursor;
  historyComplete: boolean;
  sawTerminated: boolean;
};

export type SessionDetailDeltaFrame = {
  message: QuickstartSessionEvent;
  frames: QuickstartSessionEvent[];
};

export type SessionDetailDeltaFrames = Record<string, SessionDetailDeltaFrame>;

export type SessionEventCachePatch = Partial<Omit<SessionDetailEventCache, 'events'>>;

export type QuickstartStreamEvent = ServerSentEvent<Record<string, unknown>>;

export type SessionEventUsage = {
  input: number;
  output: number;
  input_tokens: number;
  output_tokens: number;
  cache_read_input_tokens: number;
  cache_creation_input_tokens: number;
};

export type SessionTraceView = 'transcript' | 'debug';

export type SessionTraceDisplayKind = 'prose' | 'command' | 'json' | 'log' | 'metric' | 'status' | 'thinking';

export type SessionTraceFamily =
  | 'user'
  | 'agent'
  | 'subagent'
  | 'tool_use'
  | 'tool_result'
  | 'model'
  | 'outcome'
  | 'thread'
  | 'result'
  | 'status'
  | 'error'
  | 'system'
  | 'env'
  | 'span';

export type SessionTraceFilterOption = {
  value: string;
  label: string;
};

export type SessionTraceBuildOptions = {
  platformTranscriptFiltering?: boolean;
};

export type SessionDebugDetailTab = 'content' | 'deltas';

export type SessionTraceEntry = {
  id: string;
  type: string;
  family: SessionTraceFamily;
  label: string;
  preview: string;
  displayText: string;
  displayKind: SessionTraceDisplayKind;
  event: QuickstartSessionEvent;
  resultEvent?: QuickstartSessionEvent;
  confirmationEvent?: QuickstartSessionEvent;
  createdAtMs: number;
  relativeTime: string;
  rawEventId: string;
  searchText: string;
  isError: boolean;
};

export type DisplayEventType =
  | 'user'
  | 'agent'
  | 'thinking'
  | 'tool_use'
  | 'result'
  | 'root'
  | 'status_rescheduled'
  | 'status_running'
  | 'status_idle'
  | 'status_terminated'
  | 'interrupt'
  | 'model_request'
  | 'outcome'
  | 'thread'
  | 'subagent'
  | 'error'
  | 'system_message'
  | 'unknown';

export type DisplayEvent = {
  id: string;
  type: DisplayEventType;
  rawType: string;
  label: string;
  content: string;
  event: QuickstartSessionEvent;
  isQueued: boolean;
  isStreaming: boolean;
  isError: boolean;
  createdAtMs: number;
  processedAtMs: number;
  relativeTime: string;
};

export type TranscriptEntryKind =
  'idle_gap' | 'queued_boundary' | 'outcome' | 'tool_call' | 'tool_batch' | 'message' | 'status' | 'passthrough';

export type ToolLifecycle = 'running' | 'awaiting_approval' | 'completed' | 'failed' | 'denied';

export type BaseSessionEventEntry = {
  id: string;
  kind: TranscriptEntryKind | 'debug';
  displayEvent: DisplayEvent;
  traceEntry: SessionTraceEntry;
  event: QuickstartSessionEvent;
  type: string;
  rawEventId: string;
  createdAtMs: number;
  processedAtMs: number;
  relativeTime: string;
  searchText: string;
  isError: boolean;
};

export type IdleGapEntry = {
  id: string;
  kind: 'idle_gap';
  durationMs: number;
  createdAtMs: number;
  processedAtMs: number;
  relativeTime: string;
  searchText: string;
  isError: false;
};

export type QueuedBoundaryEntry = {
  id: string;
  kind: 'queued_boundary';
  count: number;
  createdAtMs: number;
  processedAtMs: number;
  relativeTime: string;
  searchText: string;
  isError: false;
};

export type ToolCallEntry = BaseSessionEventEntry & {
  kind: 'tool_call';
  name: string;
  inputPreview: string;
  resultEvent?: QuickstartSessionEvent;
  confirmationEvent?: QuickstartSessionEvent;
  usage: SessionEventUsage;
  inferenceMs: number;
  executionMs: number;
  lifecycle: ToolLifecycle;
  bracketId: string;
  bracketStartMs?: number;
};

export type ToolBatchEntry = Omit<BaseSessionEventEntry, 'kind'> & {
  kind: 'tool_batch';
  calls: ToolCallEntry[];
  toolCounts: Array<{ name: string; count: number }>;
  usage: SessionEventUsage;
  inferenceMs: number;
  executionMs: number;
  lifecycle: ToolLifecycle;
  bracketStartMs?: number;
};

export type DisplayEventEntry = BaseSessionEventEntry & {
  kind: 'message' | 'status' | 'passthrough' | 'outcome' | 'debug';
  usage: SessionEventUsage;
  inferenceMs: number;
  executionMs: number;
  inProgress?: boolean;
  outcomeStatus?: string;
  outcomeIteration?: number;
  durationMs?: number;
  bracketId?: string;
  bracketStartMs?: number;
};

export type ModelBracketTargetEntry = DisplayEventEntry | ToolCallEntry;

export type ModelRequestBracket = {
  startId: string;
  startMs: number;
  entries: ModelBracketTargetEntry[];
};

export type ModelRequestBracketMeta = {
  startId: string;
  startMs: number;
  inferenceMs: number;
  usage: SessionEventUsage;
};

export type SessionEventListEntry =
  IdleGapEntry | QueuedBoundaryEntry | ToolCallEntry | ToolBatchEntry | DisplayEventEntry;

export type ManagedEntityFormValues = {
  name: string;
  description: string;
  agentId: string;
  environmentId: string;
  initialMessage: string;
  triggerType: '' | 'manual' | 'schedule';
  cronExpression: string;
  timezone: string;
  vaultIds: string[];
  memoryStoreIds: string[];
};

export type EntityOption = {
  id: string;
  label: string;
  secondary?: string;
};

export type AgentModelInput =
  | string
  | {
      id: string;
      speed?: string;
    };

export type CreateAgentInput = {
  name: string;
  description?: string | null;
  model: AgentModelInput;
  system?: string | null;
  mcp_servers: unknown[];
  tools: Array<Record<string, unknown>>;
  skills: unknown[];
  metadata?: Record<string, string>;
};

export type AgentEditConfig = z.infer<typeof agentEditConfigSchema>;

export type QuickstartToolStatus = 'running' | 'awaiting_user' | 'completed' | 'failed';

export type QuickstartToolCall = {
  id: string;
  name: string;
  input: Record<string, unknown>;
  status: QuickstartToolStatus;
  result?: string;
  error?: string;
};

export type QuickstartChatItem =
  | {
      id: string;
      type: 'message';
      role: 'user' | 'assistant';
      content: string;
    }
  | {
      id: string;
      type: 'create_agent_result';
      agentConfig: CreateAgentInput;
    }
  | {
      id: string;
      type: 'status';
      content: string;
      tone?: 'muted' | 'success' | 'error';
    }
  | {
      id: string;
      type: 'tool';
      call: QuickstartToolCall;
    };

export type QuickstartToolExecutionResult = {
  content: string;
  isError?: boolean;
};

export type QuickstartEnvironmentConfig = {
  type?: string;
  networking?: Record<string, unknown>;
  [key: string]: unknown;
};

export type QuickstartCreateEnvironmentInput = {
  reuse_environment_id?: string;
  name?: string;
  description?: string;
  config?: QuickstartEnvironmentConfig;
};

export type QuickstartDeploymentInput = {
  name?: string;
  cron_expression?: string;
  timezone?: string;
  initial_message?: string;
};

export type CredentialFormValues = {
  displayName: string;
  authType: 'static_bearer' | 'environment_variable';
  mcpServerUrl: string;
  token: string;
  secretName: string;
  secretValue: string;
};

export type MemoryFormValues = {
  path: string;
  content: string;
};

export type QuickstartQuestion = {
  header: string;
  question: string;
  multiSelect: boolean;
  options: Array<{ label: string; description: string }>;
};

export type TranscriptMarkdownBlock =
  | { type: 'paragraph'; text: string }
  | { type: 'heading'; level: number; text: string }
  | { type: 'list'; items: string[] }
  | { type: 'table'; headers: string[]; rows: string[][] }
  | { type: 'code'; language?: string; value: string };

export type SessionThreadHint = { id: string; name: string };

export type HighlightLanguage =
  'bash' | 'bash-yaml' | 'javascript' | 'json' | 'plaintext' | 'python' | 'typescript' | 'yaml';

export type ResourceConfig = {
  section: Exclude<ManagedAgentSection, 'quickstart' | 'dreams'>;
  title: string;
  description: string;
  createLabel?: string;
  searchPrefix?: string;
  searchPlaceholder: string;
  filters: string[];
  columns: string[];
  emptyTitle: string;
  emptyBody?: string;
  emptyAction?: string;
  emptyIcon: IconComponent;
  rows?: Array<Record<string, ReactNode>>;
};

export type EventsTabProps = {
  activeLane: string;
  archivedLaneCount: number;
  childLoading: boolean;
  copyPayload: string;
  detailPanelRef: RefObject<HTMLDivElement | null>;
  entries: SessionEventListEntry[];
  events: QuickstartSessionEvent[];
  filteredEntries: SessionEventListEntry[];
  filterOptions: SessionTraceFilterOption[];
  hasFilter: boolean;
  isMultiAgent: boolean;
  lanes: SessionDetailLane[];
  onClearFilters: () => void;
  onCopyAll: () => void;
  onDetailTabChange: (tab: SessionDebugDetailTab) => void;
  onOpenDeltas: (entryId: string) => void;
  onQueryChange: (value: string) => void;
  onSelectEntry: (entryId: string | null) => void;
  onSelectLane: (laneId: string, targetEntryId?: string | null) => void;
  onThreadClick: (threadId: string, processedAtMs: number, eventType: string) => void;
  onSelectedTypesChange: (types: string[]) => void;
  onTimelineSeek: (entryId: string | null) => void;
  onToggleArchivedLanes: (nextPressed: boolean) => void;
  onViewChange: (view: SessionTraceView) => void;
  query: string;
  scrollerRef: RefObject<HTMLDivElement | null>;
  selectedEntry: SessionEventListEntry | null;
  selectedDetailTab: SessionDebugDetailTab;
  selectedEntryId: string | null;
  selectedTypes: string[];
  showArchivedLanes: boolean;
  suppressScrollSeekUntilRef: MutableRefObject<number>;
  threadNameById: Map<string, string>;
  timeline: SessionTimelineLane[];
  timelineVisibleIds?: Set<string>;
  view: SessionTraceView;
};

export type SessionDetailSummaryChip = {
  key: string;
  icon: IconComponent;
  value: string;
};

export type SessionDetailLane = {
  id: string;
  label: string;
  group?: string;
  archived?: boolean;
  isMain?: boolean;
};

export type SessionDetailLaneState = {
  lanes: SessionDetailLane[];
  threadNameById: Map<string, string>;
  laneIdByThreadId: Map<string, string>;
  archivedLaneCount: number;
  isMultiAgent: boolean;
};

export type SessionTimelineLane = SessionDetailLane & {
  items: SessionTimelineItem[];
};

export type SessionTimelineItem = {
  id: string;
  rowId: string;
  type: DisplayEventType;
  label: string;
  preview: string;
  relativeTime: string;
  processedAtMs: number;
  durationMs: number;
};

export type SessionTimelineTick = SessionTimelineItem & {
  lane: SessionTimelineLane;
  leftPct: number;
  widthPct: number;
  ms: number;
};

export type TimelinePickOptions = {
  laneId?: string;
  includeIdle?: boolean;
  maxDistancePct?: number;
  visibleIds?: Set<string>;
};

export type LaneTabGroup = {
  key: string;
  label: string;
  lanes: SessionDetailLane[];
  collapsed: boolean;
};

export type EnvironmentPackageRow = {
  manager: string;
  value: string;
};

export type EnvironmentMetadataRow = {
  key: string;
  value: string;
};

export type EnvironmentEditValues = {
  name: string;
  description: string;
  networkType: 'unrestricted' | 'limited';
  allowMcpServers: boolean;
  allowPackageManagers: boolean;
  allowedHostsText: string;
  packages: EnvironmentPackageRow[];
  metadataRows: EnvironmentMetadataRow[];
};

import type { ObservabilitySpan } from '../types';

export type TraceTreeNode = ObservabilitySpan & {
  children: TraceTreeNode[];
  offsetMs: number;
  durationMs: number;
  widthPct: number;
  offsetPct: number;
};

export function buildTraceTree(spans: ObservabilitySpan[]): TraceTreeNode[] {
  const byId = new Map<string, TraceTreeNode>();
  for (const span of spans) {
    byId.set(span.span_id, {
      ...span,
      children: [],
      offsetMs: 0,
      durationMs: span.duration_ms,
      widthPct: 0,
      offsetPct: 0,
    });
  }
  const roots: TraceTreeNode[] = [];
  const orphans: TraceTreeNode[] = [];
  for (const node of byId.values()) {
    if (!node.parent_span_id) {
      roots.push(node);
      continue;
    }
    const parent = byId.get(node.parent_span_id);
    if (parent) {
      parent.children.push(node);
    } else {
      orphans.push(node);
    }
  }
  roots.sort(byStart);
  for (const node of byId.values()) {
    node.children.sort(byStart);
  }
  const origin = earliestStart(spans);
  const totalMs = Math.max(1, latestEnd(spans, origin) - origin);
  const attach = (nodes: TraceTreeNode[]) => {
    for (const node of nodes) {
      const startMs = Date.parse(node.start_time);
      node.offsetMs = Number.isFinite(startMs) ? Math.max(0, startMs - origin) : 0;
      node.durationMs = Number.isFinite(node.duration_ms) ? Math.max(node.duration_ms, 1) : 1;
      node.offsetPct = (node.offsetMs / totalMs) * 100;
      node.widthPct = Math.max(0.4, (node.durationMs / totalMs) * 100);
      attach(node.children);
    }
  };
  if (orphans.length) {
    const virtual: TraceTreeNode = {
      span_id: '__orphan_root__',
      parent_span_id: '',
      kind: 'other',
      name: 'orphans',
      start_time: spans[0]?.start_time ?? '',
      end_time: spans[0]?.end_time ?? '',
      duration_ms: 0,
      status: 'ok',
      attributes: {},
      children: orphans.sort(byStart),
      offsetMs: 0,
      durationMs: 1,
      widthPct: 100,
      offsetPct: 0,
    };
    roots.push(virtual);
  }
  attach(roots);
  return roots;
}

export type TraceTreeRow = TraceTreeNode & {
  depth: number;
  childCount: number;
  isLast: boolean;
  ancestorLast: boolean[];
};

export function flattenTraceTree(
  nodes: TraceTreeNode[],
  depth = 0,
  collapsed?: ReadonlySet<string>,
  ancestorLast: boolean[] = [],
): TraceTreeRow[] {
  const out: TraceTreeRow[] = [];
  nodes.forEach((node, index) => {
    const isLast = index === nodes.length - 1;
    if (node.span_id !== '__orphan_root__') {
      out.push({ ...node, depth, childCount: node.children.length, isLast, ancestorLast });
    }
    const nextDepth = node.span_id === '__orphan_root__' ? depth : depth + 1;
    const nextAncestors = node.span_id === '__orphan_root__' ? ancestorLast : [...ancestorLast, isLast];
    if (!collapsed?.has(node.span_id)) {
      out.push(...flattenTraceTree(node.children, nextDepth, collapsed, nextAncestors));
    }
  });
  return out;
}

export function filterCallTreeRows(rows: TraceTreeRow[], query: string) {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return rows;
  }
  const matchIds = new Set(rows.filter((row) => callTreeRowMatches(row, needle)).map((row) => row.span_id));
  if (!matchIds.size) {
    return [];
  }
  const byId = new Map(rows.map((row) => [row.span_id, row]));
  const visible = new Set(matchIds);
  for (const id of matchIds) {
    let current = byId.get(id);
    while (current?.parent_span_id) {
      visible.add(current.parent_span_id);
      current = byId.get(current.parent_span_id);
    }
  }
  return rows.filter((row) => visible.has(row.span_id));
}

function callTreeRowMatches(row: TraceTreeRow, needle: string) {
  const identity = row.attributes.infer_service_name || row.attributes.infer_service_system || '';
  return [
    row.name,
    spanDisplayName(row),
    row.kind,
    row.span_id,
    identity,
    row.attributes.tool_name,
    row.attributes.model,
  ].some((value) => (value ?? '').toLowerCase().includes(needle));
}

export type TraceSummary = {
  name: string;
  startTime: string;
  durationMs: number;
  spanCount: number;
  errorCount: number;
  sessionId: string;
};

export function traceSummary(spans: ObservabilitySpan[]): TraceSummary {
  const roots = spans.filter((span) => !span.parent_span_id).sort(byStart);
  const root = roots[0] ?? spans[0];
  return {
    name: root?.name ?? 'trace',
    startTime: root?.start_time ?? '',
    durationMs: root?.duration_ms ?? 0,
    spanCount: spans.length,
    errorCount: spans.filter((span) => span.status === 'error').length,
    sessionId: spanSessionId(root),
  };
}

export function spanServiceName(span: ObservabilitySpan) {
  return span.attributes.service_name || span.attributes['service.name'] || 'claude-code';
}

export function spanSessionId(span: ObservabilitySpan | undefined) {
  if (!span) {
    return '';
  }
  return span.attributes.oma_session_id || span.attributes.service_oma_session_id || span.attributes.session_id || '';
}

export function spanAgentID(span: ObservabilitySpan) {
  return span.attributes.service_oma_agent_id || span.attributes.oma_agent_id || span.attributes['oma.agent.id'] || '';
}

export function waterfallTickMs(totalMs: number, count = 5) {
  const span = Math.max(totalMs, 1);
  const ticks = Math.max(count, 2);
  return Array.from({ length: ticks }, (_, index) => (span * index) / (ticks - 1));
}

export function waterfallTotalMs(rows: Array<{ offsetMs: number; durationMs: number }>) {
  if (!rows.length) {
    return 1;
  }
  return Math.max(1, ...rows.map((row) => row.offsetMs + row.durationMs));
}

export function tracePreview(spans: ObservabilitySpan[]) {
  const roots = spans.filter((span) => !span.parent_span_id).sort(byStart);
  const input = (roots[0]?.attributes.user_prompt ?? '').trim();
  const llm = spans.filter((span) => span.kind === 'llm' && span.status === 'ok').sort(byStart);
  const output = (llm.at(-1)?.attributes.response_model_output ?? '').trim();
  return { input, output };
}

export type TraceStats = {
  durationMs: number;
  spanCount: number;
  llmCallCount: number;
  toolCallCount: number;
  inputTokens: number;
  outputTokens: number;
  totalTokens: number;
  cacheHitRate: number | null;
  errorCount: number;
};

export function traceStats(spans: ObservabilitySpan[]): TraceStats {
  const origin = earliestStart(spans);
  let inputTokens = 0;
  let outputTokens = 0;
  let cacheReadTokens = 0;
  let cacheCreationTokens = 0;
  let llmCallCount = 0;
  let toolCallCount = 0;
  let errorCount = 0;
  for (const span of spans) {
    if (span.kind === 'llm') {
      llmCallCount += 1;
      inputTokens += Number(span.attributes.input_tokens) || 0;
      outputTokens += Number(span.attributes.output_tokens) || 0;
      cacheReadTokens += Number(span.attributes.cache_read_tokens) || 0;
      cacheCreationTokens += Number(span.attributes.cache_creation_tokens) || 0;
    }
    if (span.kind === 'tool') {
      toolCallCount += 1;
    }
    if (span.status === 'error') {
      errorCount += 1;
    }
  }
  const promptTokens = inputTokens + cacheReadTokens;
  return {
    durationMs: spans.length ? Math.max(0, latestEnd(spans, origin) - origin) : 0,
    spanCount: spans.length,
    llmCallCount,
    toolCallCount,
    inputTokens,
    outputTokens,
    totalTokens: inputTokens + outputTokens + cacheReadTokens + cacheCreationTokens,
    cacheHitRate: promptTokens > 0 ? (cacheReadTokens / promptTokens) * 100 : null,
    errorCount,
  };
}

const CLAUDE_CODE_PREFIX = 'claude_code.';

const LLM_INPUT_KEYS = ['prompt', 'input', 'request', 'user_prompt'] as const;
const LLM_OUTPUT_KEYS = ['output', 'response'] as const;
const TOOL_INPUT_KEYS = ['input', 'tool_input', 'arguments', 'tool_parameters'] as const;
const TOOL_OUTPUT_KEYS = ['output', 'tool_result', 'result'] as const;

export function shortenSpanName(name: string) {
  return name.startsWith(CLAUDE_CODE_PREFIX) ? name.slice(CLAUDE_CODE_PREFIX.length) : name;
}

export function spanDisplayName(span: ObservabilitySpan) {
  if (span.kind === 'tool' || span.kind === 'tool_execution' || span.kind === 'tool_wait') {
    const toolName = span.attributes.tool_name?.trim();
    if (toolName) {
      return toolName;
    }
  }
  if (span.kind === 'llm') {
    const model = span.attributes.model?.trim();
    if (model) {
      return model;
    }
  }
  return shortenSpanName(span.name);
}

export type SpanPreview = {
  input: string;
  output: string;
};

export function spanPreview(span: ObservabilitySpan): SpanPreview {
  if (span.kind === 'interaction') {
    return { input: namedValue(span.attributes, 'user_prompt'), output: '' };
  }
  if (span.kind === 'llm') {
    return {
      input: firstNamedValue(span.attributes, LLM_INPUT_KEYS) || firstEventValue(span.events, LLM_INPUT_KEYS),
      output: namedValue(span.attributes, 'response_model_output') || firstNamedValue(span.attributes, LLM_OUTPUT_KEYS),
    };
  }
  if (span.kind === 'tool' || span.kind === 'tool_execution') {
    return {
      input: firstNamedValue(span.attributes, TOOL_INPUT_KEYS) || firstEventValue(span.events, TOOL_INPUT_KEYS),
      output: firstNamedValue(span.attributes, TOOL_OUTPUT_KEYS) || firstEventValue(span.events, TOOL_OUTPUT_KEYS),
    };
  }
  return {
    input:
      firstNamedValue(span.attributes, TOOL_INPUT_KEYS) ||
      namedValue(span.attributes, 'user_prompt') ||
      firstEventValue(span.events, TOOL_INPUT_KEYS),
    output: firstNamedValue(span.attributes, TOOL_OUTPUT_KEYS) || firstEventValue(span.events, TOOL_OUTPUT_KEYS),
  };
}

function namedValue(record: Record<string, string>, key: string) {
  return record[key]?.trim() ?? '';
}

function firstNamedValue(record: Record<string, string>, keys: readonly string[]) {
  for (const key of keys) {
    const value = namedValue(record, key);
    if (value) {
      return value;
    }
  }
  return '';
}

function firstEventValue(events: ObservabilitySpan['events'], keys: readonly string[]) {
  for (const event of events ?? []) {
    const value = firstNamedValue(event.attributes, keys);
    if (value) {
      return value;
    }
  }
  return '';
}

function byStart(left: Pick<ObservabilitySpan, 'start_time'>, right: Pick<ObservabilitySpan, 'start_time'>) {
  return Date.parse(left.start_time) - Date.parse(right.start_time);
}

function earliestStart(spans: ObservabilitySpan[]) {
  const times = spans.map((span) => Date.parse(span.start_time)).filter(Number.isFinite);
  return times.length ? Math.min(...times) : Date.now();
}

function latestEnd(spans: ObservabilitySpan[], origin: number) {
  const ends = spans.map((span) => {
    const start = Date.parse(span.start_time);
    if (!Number.isFinite(start)) {
      return origin;
    }
    return start + Math.max(span.duration_ms, 0);
  });
  return ends.length ? Math.max(...ends) : origin + 1;
}

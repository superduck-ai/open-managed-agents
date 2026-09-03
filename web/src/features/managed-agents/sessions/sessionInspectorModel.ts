import {
  type AgentApiResponse,
  type QuickstartSessionEvent,
  type SessionDetailLane,
  type SessionEventListEntry,
  type ToolCallEntry,
} from '../types';
import { objectRecord, optionalNumericValueFromKeys, sessionListCost, stringValueFromKeys } from '../utils';
import { extractSessionEventUsage, sessionEventEntrySourceIds, sessionEventThreadId } from './sessionDetailModel';
import {
  buildSessionEventEntries,
  sessionEventAppearsInTranscript,
  sessionEventKey,
  sessionEventProcessedTimestamp,
  sessionEventSummary,
  sessionEventTimestamp,
  sessionEventType,
  sessionIsToolUseEvent,
  sessionModelRequestStartRef,
  normalizedToolPermission,
  sessionToolDisplayText,
} from './sessionTraceModel';
import { SESSION_MAIN_LANE_ID } from './sessionTimeline';
import {
  buildAgentToolDisplayCards,
  configuredAgentToolPermission,
  normalizeRuntimeToolName,
} from '../agents/tools/model';
import { buildSessionTranscriptBlocks } from './sessionTranscriptModel';

export const SESSION_INSPECTOR_TABS = ['session', 'events', 'tools', 'resources', 'threads', 'traces'] as const;

export type SessionInspectorTab = (typeof SESSION_INSPECTOR_TABS)[number];

export function readSessionInspectorTab(): SessionInspectorTab {
  if (typeof window === 'undefined') return 'session';
  const search = new URLSearchParams(window.location.search);
  const value = search.get('inspector');
  if (!value && search.has('trace_id')) return 'traces';
  return SESSION_INSPECTOR_TABS.find((tab) => tab === value) ?? 'session';
}

export function sessionInspectorTabHref(tab: SessionInspectorTab) {
  if (typeof window === 'undefined') return tab === 'session' ? '' : `?inspector=${tab}`;
  const url = new URL(window.location.href);
  if (tab === 'session') url.searchParams.delete('inspector');
  else url.searchParams.set('inspector', tab);
  if (tab !== 'traces') url.searchParams.delete('trace_id');
  return `${url.pathname}${url.search}${url.hash}`;
}

export function writeSessionInspectorUrlState(tab: SessionInspectorTab) {
  if (typeof window === 'undefined') return;
  const href = sessionInspectorTabHref(tab);
  const current = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  if (href !== current) window.history.replaceState(window.history.state, '', href);
}

export type InspectorEventRow = {
  event: QuickstartSessionEvent;
  id: string;
  preview: string;
  processedAtMs: number;
  type: string;
};

export type InspectorEventListItem =
  { type: 'event'; row: InspectorEventRow } | { type: 'turn'; id: string; rows: InspectorEventRow[] };

export type InspectorToolRow = {
  calls: ToolCallEntry[];
  configuredOn?: string;
  failed: number;
  group: string;
  key: string;
  kind: 'built-in' | 'custom' | 'mcp' | 'unconfigured';
  name: string;
  p50Ms?: number;
  permission: 'allow' | 'ask' | 'deny' | 'mixed' | 'unknown';
};

export type InspectorContextPoint = {
  at: number;
  eventId: string;
  tokens: number;
};

export type InspectorSessionUsage = {
  activeSeconds?: number;
  cacheRead: number;
  cacheWrite: number;
  input: number;
  output: number;
  webSearches: number;
};

export type InspectorCostPoint = {
  at: number;
  cents: number;
  currency: string;
  eventId: string;
  stepCents: number;
  usage: InspectorSessionUsage;
};

export type InspectorThreadRow = {
  context: number;
  contextPoints: InspectorContextPoint[];
  id: string;
  isMain: boolean;
  label: string;
  status: 'running' | 'idle' | 'rescheduling' | 'terminated';
};

export function buildInspectorEventRows(events: QuickstartSessionEvent[]) {
  return events.map((event, index): InspectorEventRow => {
    const type = sessionEventType(event) || 'unknown';
    return {
      event,
      id: inspectorEventId(event, index),
      preview: sessionIsToolUseEvent(event) ? sessionToolDisplayText(event) : sessionEventSummary(event),
      processedAtMs: sessionEventProcessedTimestamp(event),
      type,
    };
  });
}

export function filterInspectorEventRows(
  rows: InspectorEventRow[],
  options: { transcriptOnly: boolean; types: string[] },
) {
  const selectedTypes = new Set(options.types);
  return rows.filter(
    (row) =>
      (!options.transcriptOnly || sessionEventAppearsInTranscript(row.event, { platformTranscriptFiltering: true })) &&
      (selectedTypes.size === 0 || selectedTypes.has(row.type)),
  );
}

export function buildInspectorEventListItems(
  events: QuickstartSessionEvent[],
  rows: InspectorEventRow[],
): InspectorEventListItem[] {
  const turnIdByEventId = inspectorTurnIdByEventId(events);
  const items: InspectorEventListItem[] = [];
  let activeTurnId: string | null = null;
  let pendingRows: InspectorEventRow[] = [];
  const flushPendingRows = () => {
    pendingRows.forEach((row) => items.push({ type: 'event', row }));
    pendingRows = [];
  };

  rows.forEach((row) => {
    const turnId = turnIdByEventId.get(row.id) ?? null;
    if (!turnId) {
      if (activeTurnId) pendingRows.push(row);
      else items.push({ type: 'event', row });
      return;
    }

    const previous = items.at(-1);
    if (previous?.type === 'turn' && previous.id === turnId) {
      previous.rows.push(...pendingRows, row);
      pendingRows = [];
      return;
    }

    flushPendingRows();
    items.push({ type: 'turn', id: turnId, rows: [row] });
    activeTurnId = turnId;
  });

  flushPendingRows();
  return items;
}

export function inspectorBackingEventIds(
  events: QuickstartSessionEvent[],
  selectedEntry: SessionEventListEntry | null,
) {
  const selectedIds = new Set(selectedEntry ? sessionEventEntrySourceIds(selectedEntry) : []);
  if (!selectedIds.size) return selectedIds;

  const entries = buildSessionEventEntries(events, 'transcript', 0, undefined, {
    platformTranscriptFiltering: true,
  });
  for (const entry of entries) {
    if (entry.kind === 'tool_batch') {
      const call = entry.calls.find((candidate) =>
        sessionEventEntrySourceIds(candidate).some((id) => selectedIds.has(id)),
      );
      if (call) return new Set(sessionEventEntrySourceIds(call));
      continue;
    }
    const sourceIds = sessionEventEntrySourceIds(entry);
    if (sourceIds.some((id) => selectedIds.has(id))) return new Set(sourceIds);
  }
  return selectedIds;
}

function inspectorTurnIdByEventId(events: QuickstartSessionEvent[]) {
  const turnIdByEventId = new Map<string, string>();
  const turnIdByBracketId = new Map<string, string>();
  const blocks = buildSessionTranscriptBlocks(
    buildSessionEventEntries(events, 'transcript', 0, undefined, { platformTranscriptFiltering: true }),
  );

  blocks.forEach((block) => {
    if (block.kind === 'standalone') return;
    const entries = block.kind === 'user' ? [block.entry] : block.iterations.flatMap((iteration) => iteration.entries);
    entries.forEach((entry) => {
      sessionEventEntrySourceIds(entry).forEach((id) => turnIdByEventId.set(id, block.id));
      if ('event' in entry) addInspectorParentEventId(turnIdByEventId, entry.event, block.id);
    });
    if (block.kind === 'agent') {
      block.iterations.forEach((iteration) => turnIdByBracketId.set(iteration.bracketId, block.id));
    }
  });

  events.forEach((event, index) => {
    const eventId = inspectorEventId(event, index);
    const bracketId =
      sessionEventType(event) === 'span.model_request_start'
        ? sessionEventKey(event)
        : sessionModelRequestStartRef(event);
    const turnId = turnIdByBracketId.get(bracketId);
    if (turnId) turnIdByEventId.set(eventId, turnId);
  });
  return turnIdByEventId;
}

function addInspectorParentEventId(
  turnIdByEventId: Map<string, string>,
  event: QuickstartSessionEvent,
  turnId: string,
) {
  if (typeof event.parent_event_id === 'string' && event.parent_event_id) {
    turnIdByEventId.set(event.parent_event_id, turnId);
  }
}

export function buildInspectorToolRows(events: QuickstartSessionEvent[], agent: AgentApiResponse | null) {
  const calls = inspectorToolCalls(buildSessionEventEntries(events, 'transcript'));
  const configuredCards = agent ? buildAgentToolDisplayCards(agent) : [];
  const configuredTools = configuredInspectorTools(agent, configuredCards);
  const toolsByKey = new Map(configuredTools.map((tool) => [tool.key, tool]));
  calls.forEach((call) => {
    const key = inspectorCallToolKey(call);
    if (!toolsByKey.has(key)) {
      const mcpCard = configuredCards.find(
        (card) => card.kind === 'mcp' && key.startsWith(normalizeRuntimeToolName(`mcp__${card.serverName ?? ''}__`)),
      );
      toolsByKey.set(key, {
        configuredOn: mcpCard && agent ? agent.name : undefined,
        group: mcpCard?.title ?? 'Called, not configured',
        key,
        kind: mcpCard ? 'mcp' : 'unconfigured',
        name: normalizeRuntimeToolName(call.name),
      });
    }
  });
  return [...toolsByKey.values()]
    .map((tool): InspectorToolRow => {
      const matchingCalls = calls.filter((call) => inspectorCallToolKey(call) === tool.key);
      const permissions = new Set(matchingCalls.map(inspectorCallPermission).filter((value) => value !== undefined));
      const configuredPermission = agent ? configuredAgentToolPermission(agent, tool.key) : undefined;
      const permission = configuredPermission
        ? inspectorPermission(configuredPermission)
        : permissions.size > 1
          ? 'mixed'
          : (permissions.values().next().value ?? 'unknown');
      const durations = matchingCalls
        .map(inspectorToolExecutionMs)
        .filter((duration): duration is number => duration !== undefined)
        .sort((left, right) => left - right);
      return {
        calls: matchingCalls,
        configuredOn: tool.configuredOn,
        failed: matchingCalls.filter((call) => call.lifecycle === 'failed').length,
        group: tool.group,
        key: tool.key,
        kind: tool.kind,
        name: tool.name,
        p50Ms: median(durations),
        permission,
      };
    })
    .sort(
      (left, right) =>
        inspectorToolKindOrder(left.kind) - inspectorToolKindOrder(right.kind) ||
        left.group.localeCompare(right.group) ||
        right.calls.length - left.calls.length ||
        left.name.localeCompare(right.name),
    );
}

function inspectorToolKindOrder(kind: InspectorToolRow['kind']) {
  return { 'built-in': 0, custom: 1, mcp: 2, unconfigured: 3 }[kind];
}

export function buildInspectorToolTotals(rows: InspectorToolRow[]) {
  const calls = rows.flatMap((row) => row.calls);
  const executingMs = inspectorToolSpanMs(calls, (call) => {
    if (!call.resultEvent) return null;
    return [sessionEventTimestamp(call.confirmationEvent ?? call.event), sessionEventTimestamp(call.resultEvent)];
  });
  const waitingMs = inspectorToolSpanMs(calls, (call) => {
    if (!call.confirmationEvent) return null;
    return [sessionEventTimestamp(call.event), sessionEventTimestamp(call.confirmationEvent)];
  });
  const timeInToolsMs = inspectorToolSpanMs(calls, (call) => {
    if (!call.resultEvent) return null;
    return [sessionEventTimestamp(call.event), sessionEventTimestamp(call.resultEvent)];
  });
  return {
    calls: calls.length,
    completed: calls.filter((call) => call.lifecycle === 'completed').length,
    denied: calls.filter((call) => call.lifecycle === 'denied').length,
    executingMs,
    failed: calls.filter((call) => call.lifecycle === 'failed').length,
    inFlight: calls.filter((call) => call.lifecycle === 'running' || call.lifecycle === 'awaiting_approval').length,
    timeInToolsMs,
    tools: rows.length,
    used: rows.filter((row) => row.calls.length > 0).length,
    waitingMs,
  };
}

function inspectorToolExecutionMs(call: ToolCallEntry) {
  if ((call.lifecycle !== 'completed' && call.lifecycle !== 'failed') || !call.resultEvent) {
    return undefined;
  }
  const startedAt = sessionEventTimestamp(call.confirmationEvent ?? call.event);
  const finishedAt = sessionEventTimestamp(call.resultEvent);
  if (startedAt && finishedAt >= startedAt) {
    return finishedAt - startedAt;
  }
  return Number.isFinite(call.executionMs) && call.executionMs >= 0 ? call.executionMs : undefined;
}

function inspectorToolSpanMs(calls: ToolCallEntry[], span: (call: ToolCallEntry) => [number, number] | null) {
  const intervalsByThread = new Map<string, Array<[number, number]>>();
  calls.forEach((call) => {
    const interval = span(call);
    if (!interval || !interval[0] || interval[1] < interval[0]) return;
    const thread = sessionEventThreadId(call.event);
    intervalsByThread.set(thread, [...(intervalsByThread.get(thread) ?? []), interval]);
  });
  return [...intervalsByThread.values()].reduce((total, intervals) => {
    const sorted = [...intervals].sort((left, right) => left[0] - right[0]);
    let end = 0;
    return (
      total +
      sorted.reduce((threadTotal, [start, nextEnd]) => {
        const added = Math.max(0, nextEnd - Math.max(start, end));
        end = Math.max(end, nextEnd);
        return threadTotal + added;
      }, 0)
    );
  }, 0);
}

export function buildInspectorCostPoints(events: QuickstartSessionEvent[]) {
  const points: InspectorCostPoint[] = [];
  events.forEach((event) => {
    if (sessionEventType(event) !== 'session.usage') return;
    const usageRecord = objectRecord(event.usage);
    const listCost = inspectorMoneyInCents(usageRecord.list_cost);
    const at = sessionEventProcessedTimestamp(event) || sessionEventTimestamp(event);
    if (!listCost || !at) return;
    const previousCents = points.at(-1)?.cents ?? 0;
    const cents = Math.max(previousCents, listCost.cents);
    points.push({
      at,
      cents,
      currency: listCost.currency,
      eventId: sessionEventKey(event),
      stepCents: cents - previousCents,
      usage: inspectorSessionUsage(usageRecord),
    });
  });
  return points;
}

export const inspectorSessionListCost = sessionListCost;

function inspectorMoneyInCents(value: unknown) {
  const money = objectRecord(value);
  const cents = optionalNumericValueFromKeys(money, ['amount']);
  if (cents === undefined) return null;
  return {
    cents,
    currency: typeof money.currency === 'string' && money.currency ? money.currency : 'USD',
  };
}

function inspectorSessionUsage(usage: Record<string, unknown>): InspectorSessionUsage {
  const cacheCreation = objectRecord(usage.cache_creation);
  const serverToolUse = objectRecord(usage.server_tool_use);
  return {
    activeSeconds: optionalNumericValueFromKeys(usage, ['active_seconds']),
    cacheRead: optionalNumericValueFromKeys(usage, ['cache_read_input_tokens']) ?? 0,
    cacheWrite:
      (optionalNumericValueFromKeys(cacheCreation, ['ephemeral_5m_input_tokens']) ?? 0) +
      (optionalNumericValueFromKeys(cacheCreation, ['ephemeral_1h_input_tokens']) ?? 0),
    input: optionalNumericValueFromKeys(usage, ['input_tokens']) ?? 0,
    output: optionalNumericValueFromKeys(usage, ['output_tokens']) ?? 0,
    webSearches: optionalNumericValueFromKeys(serverToolUse, ['web_search_requests']) ?? 0,
  };
}

export function buildInspectorThreadRows(
  lanes: SessionDetailLane[],
  eventsByLaneId: Map<string, QuickstartSessionEvent[]>,
  live: boolean,
) {
  return lanes.map((lane): InspectorThreadRow => {
    const events = eventsByLaneId.get(lane.id) ?? [];
    const contextPoints = buildInspectorContextPoints(events);
    return {
      context: contextPoints.at(-1)?.tokens ?? 0,
      contextPoints,
      id: lane.id,
      isMain: Boolean(lane.isMain),
      label: lane.label,
      status: inspectorThreadStatus(events, lane, live),
    };
  });
}

export function buildInspectorContextPoints(events: QuickstartSessionEvent[]) {
  const points: InspectorContextPoint[] = [];
  events.forEach((event) => {
    if (sessionEventType(event) !== 'span.model_request_end' || event.is_error === true) {
      return;
    }
    const usage = extractSessionEventUsage(event);
    const tokens = usage.input;
    if (!tokens) {
      return;
    }
    const at = sessionEventProcessedTimestamp(event) || sessionEventTimestamp(event);
    if (!at) {
      return;
    }
    const previous = points.at(-1);
    if (previous?.at === at) {
      if (tokens >= previous.tokens) {
        previous.eventId = sessionEventKey(event);
        previous.tokens = tokens;
      }
      return;
    }
    points.push({ at, eventId: sessionEventKey(event), tokens });
  });
  return points;
}

export function inspectorEventFamily(type: string) {
  const separator = type.indexOf('.');
  return separator === -1 ? type : type.slice(0, separator);
}

export function inspectorEventSuffix(type: string) {
  const separator = type.indexOf('.');
  return separator === -1 ? '' : type.slice(separator);
}

function inspectorEventId(event: QuickstartSessionEvent, index: number) {
  const candidates = [event.id, event.event_id, event.uuid];
  return (
    candidates.find((value): value is string => typeof value === 'string' && value.trim().length > 0) ??
    `event-${index}`
  );
}

function inspectorToolCalls(entries: SessionEventListEntry[]) {
  return entries.flatMap((entry) => {
    if (entry.kind === 'tool_call') return [entry];
    if (entry.kind === 'tool_batch') return entry.calls;
    return [];
  });
}

function configuredInspectorTools(
  agent: AgentApiResponse | null,
  cards: ReturnType<typeof buildAgentToolDisplayCards>,
): Array<Pick<InspectorToolRow, 'configuredOn' | 'group' | 'key' | 'kind' | 'name'>> {
  if (!agent) return [];
  return cards.flatMap((card) =>
    card.tools.map((tool) => ({
      configuredOn: agent.name,
      group: card.title,
      key: normalizeRuntimeToolName(card.kind === 'mcp' ? `mcp__${card.serverName}__${tool.name}` : tool.name),
      kind: card.kind,
      name: tool.name,
    })),
  );
}

function inspectorCallToolKey(call: ToolCallEntry) {
  const event = call.event;
  const rawName = stringValueFromKeys(event, ['name', 'tool_name', 'mcp_tool_name', 'custom_tool_name']) || call.name;
  if (sessionEventType(event) !== 'agent.mcp_tool_use' || rawName.startsWith('mcp__')) {
    return normalizeRuntimeToolName(rawName);
  }
  const serverName = stringValueFromKeys(event, ['mcp_server_name', 'server_name']);
  return normalizeRuntimeToolName(serverName ? `mcp__${serverName}__${rawName}` : rawName);
}

function inspectorCallPermission(call: ToolCallEntry): 'allow' | 'ask' | 'deny' | undefined {
  if (call.lifecycle === 'denied') return 'deny';
  if (call.confirmationEvent) return 'ask';
  const event = call.event;
  if (
    event.requires_action === true ||
    event.evaluated_permission !== undefined ||
    event.permission !== undefined ||
    event.permission_policy !== undefined ||
    event.permission_decision !== undefined
  ) {
    return normalizedToolPermission(event);
  }
  return undefined;
}

function inspectorPermission(permission: 'always_allow' | 'always_ask' | 'always_deny') {
  if (permission === 'always_ask') return 'ask';
  if (permission === 'always_deny') return 'deny';
  return 'allow';
}

function median(values: number[]) {
  if (!values.length) return undefined;
  const middle = Math.floor(values.length / 2);
  return values.length % 2 ? values[middle] : ((values[middle - 1] ?? 0) + (values[middle] ?? 0)) / 2;
}

function inspectorThreadStatus(
  events: QuickstartSessionEvent[],
  lane: SessionDetailLane,
  live: boolean,
): InspectorThreadRow['status'] {
  if (lane.archived) return 'terminated';
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const type = sessionEventType(events[index]);
    if (type.includes('terminated')) return 'terminated';
    if (type.includes('rescheduled')) return 'rescheduling';
    if (type.includes('running')) return 'running';
    if (type.includes('idle')) return 'idle';
  }
  return live && lane.id === SESSION_MAIN_LANE_ID ? 'running' : 'idle';
}

export function inspectorAgentModel(agent: AgentApiResponse | null) {
  if (!agent) return '—';
  const model = objectRecord(agent.model);
  return typeof agent.model === 'string' ? agent.model : typeof model.id === 'string' ? model.id : '—';
}

export function inspectorAgentEffort(agent: AgentApiResponse | null) {
  if (!agent) return '—';
  const model = objectRecord(agent.model);
  const effort = objectRecord(model.effort);
  const value = typeof effort.type === 'string' ? effort.type : typeof model.effort === 'string' ? model.effort : '';
  return value ? `${value.charAt(0).toUpperCase()}${value.slice(1)}` : '—';
}

import { sessionNullableProcessedAt } from '../api';
import {
  type BaseSessionEventEntry,
  type DisplayEvent,
  type DisplayEventEntry,
  type DisplayEventType,
  type HighlightLanguage,
  type I18nMsg,
  type IdleGapEntry,
  type ModelBracketTargetEntry,
  type ModelRequestBracket,
  type ModelRequestBracketMeta,
  type QueuedBoundaryEntry,
  type QuickstartSessionEvent,
  type SessionEventListEntry,
  type SessionEventUsage,
  type SessionThreadHint,
  type SessionTraceBuildOptions,
  type SessionTraceDisplayKind,
  type SessionTraceEntry,
  type SessionTraceFamily,
  type SessionTraceView,
  type ToolBatchEntry,
  type ToolCallEntry,
  type ToolLifecycle,
  type TranscriptMarkdownBlock,
} from '../types';
import { compactEntityId, objectRecord, toRecord } from '../utils';
import {
  addSessionEventUsage,
  emptySessionEventUsage,
  extractSessionEventUsage,
  numericValueFromKeys,
  sessionEventDurationMs,
  stringValueFromKeys,
} from './sessionDetailModel';
import { sessionCanonicalDisplayEvent } from './SessionTracePanel';
import { sessionOutcomeIteration, sessionOutcomeStatus } from './sessionTraceRows';

export function buildSessionTraceEntries(
  events: QuickstartSessionEvent[],
  view: SessionTraceView,
  traceStartMs = 0,
  msg?: I18nMsg,
  options: SessionTraceBuildOptions = {},
): SessionTraceEntry[] {
  const toolResults = new Map<string, QuickstartSessionEvent[]>();
  const toolConfirmations = new Map<string, QuickstartSessionEvent[]>();
  const displayEvents = events.map(sessionCanonicalDisplayEvent);
  const threadHints = buildSessionThreadHints(displayEvents);
  // Transcript 使用只读回放模型：result / confirmation 先按 tool use id 建索引，
  // 之后折回对应 tool_call；Debug 仍保留原始事件用于审计。
  displayEvents.forEach((event) => {
    if (sessionIsToolResultEvent(event)) {
      const toolUseId = sessionToolResultToolUseId(event);
      if (toolUseId) {
        addSessionToolCompanionEvent(toolResults, toolUseId, event);
      }
    }

    if (sessionEventType(event) === 'user.tool_confirmation') {
      const toolUseId = sessionToolConfirmationToolUseId(event);
      if (toolUseId) {
        addSessionToolCompanionEvent(toolConfirmations, toolUseId, event);
      }
    }
  });

  return displayEvents.flatMap((event, index) => {
    const enrichedEvent = sessionEventWithThreadHint(event, threadHints.byThreadId);
    if (view === 'transcript' && !sessionEventAppearsInTranscript(event, options)) {
      return [];
    }

    const contentEntries = sessionContentBlockEntries(
      enrichedEvent,
      index,
      toolResults,
      toolConfirmations,
      view,
      traceStartMs,
      threadHints.byToolUseId,
      msg,
    );
    if (contentEntries.length) {
      return contentEntries;
    }

    const family = sessionEventFamily(enrichedEvent);
    const toolUseId = sessionToolUseId(enrichedEvent);
    const resultEvent =
      family === 'tool_use' && toolUseId
        ? selectSessionToolCompanionEvent(toolResults.get(toolUseId), enrichedEvent, threadHints.byToolUseId)
        : undefined;
    const confirmationEvent =
      family === 'tool_use' && toolUseId
        ? selectSessionToolCompanionEvent(toolConfirmations.get(toolUseId), enrichedEvent, threadHints.byToolUseId)
        : undefined;
    return [
      sessionTraceEntryFromEvent(enrichedEvent, index, family, resultEvent, confirmationEvent, traceStartMs, msg),
    ];
  });
}

function addSessionToolCompanionEvent(
  eventsByToolUseId: Map<string, QuickstartSessionEvent[]>,
  toolUseId: string,
  event: QuickstartSessionEvent,
) {
  const existing = eventsByToolUseId.get(toolUseId);
  if (existing) {
    existing.push(event);
    return;
  }
  eventsByToolUseId.set(toolUseId, [event]);
}

function selectSessionToolCompanionEvent(
  events: QuickstartSessionEvent[] | undefined,
  toolEvent: QuickstartSessionEvent,
  threadHintsByToolUseId: Map<string, SessionThreadHint>,
): QuickstartSessionEvent | undefined {
  if (!events?.length) {
    return undefined;
  }
  const toolThreadId = sessionToolCompanionThreadId(toolEvent, threadHintsByToolUseId);
  if (!toolThreadId) {
    for (let index = events.length - 1; index >= 0; index -= 1) {
      if (!sessionToolCompanionThreadId(events[index], threadHintsByToolUseId)) {
        return events[index];
      }
    }
    return undefined;
  }
  for (let index = events.length - 1; index >= 0; index -= 1) {
    if (sessionSubagentThreadId(events[index]) === toolThreadId) {
      return events[index];
    }
  }
  for (let index = events.length - 1; index >= 0; index -= 1) {
    if (sessionToolCompanionThreadId(events[index], threadHintsByToolUseId) === toolThreadId) {
      return events[index];
    }
  }
  for (let index = events.length - 1; index >= 0; index -= 1) {
    if (!sessionToolCompanionThreadId(events[index], threadHintsByToolUseId)) {
      return events[index];
    }
  }
  return events[events.length - 1];
}

function sessionToolCompanionThreadId(
  event: QuickstartSessionEvent,
  threadHintsByToolUseId: Map<string, SessionThreadHint>,
) {
  const explicitThreadId = sessionSubagentThreadId(event);
  if (explicitThreadId) {
    return explicitThreadId;
  }
  if (sessionIsToolUseEvent(event)) {
    return '';
  }
  const toolUseId = sessionToolCompanionToolUseId(event);
  if (!toolUseId) {
    return '';
  }
  return threadHintsByToolUseId.get(toolUseId)?.id ?? '';
}

function sessionToolCompanionToolUseId(event: QuickstartSessionEvent) {
  return (
    sessionToolConfirmationToolUseId(event) ||
    sessionToolResultToolUseId(event) ||
    (sessionIsToolUseEvent(event) ? sessionToolUseId(event) : '')
  );
}

export function buildSessionEventEntries(
  events: QuickstartSessionEvent[],
  view: SessionTraceView,
  traceStartMs = 0,
  msg?: I18nMsg,
  options: SessionTraceBuildOptions = {},
): SessionEventListEntry[] {
  const traceEntries = buildSessionTraceEntries(events, view, traceStartMs, msg, options);
  if (view === 'debug') {
    return traceEntries.map((traceEntry) => displayEntryFromTraceEntry(traceEntry, 'debug', traceStartMs, msg));
  }

  const entries: SessionEventListEntry[] = [];
  let lastIdleAt = 0;
  let queuedBoundaryInserted = false;
  const sortedRawEvents = [...events.map(sessionCanonicalDisplayEvent)].sort(compareSessionEvents);
  const rawCursorByKey = new Map<string, number>();
  sortedRawEvents.forEach((event, index) => {
    rawCursorByKey.set(sessionEventKey(event), index);
  });
  const traceEntriesByRawKey = new Map<string, SessionTraceEntry[]>();
  traceEntries.forEach((entry) => {
    const rawKey = sessionEventKey(entry.event);
    const list = traceEntriesByRawKey.get(rawKey) ?? [];
    list.push(entry);
    traceEntriesByRawKey.set(rawKey, list);
  });

  sortedRawEvents.forEach((event, eventIndex) => {
    const type = sessionEventType(event);
    if (type === 'session.status_idle' && !sessionIsResultEvent(event)) {
      lastIdleAt = sessionEventTimestamp(event);
      return;
    }
    const isQueuedUserMessage = sessionEventIsQueuedUserMessage(event);
    if (!queuedBoundaryInserted && isQueuedUserMessage) {
      queuedBoundaryInserted = true;
      const queuedCount = sortedRawEvents.slice(eventIndex).filter(sessionEventIsQueuedUserMessage).length;
      entries.push(queuedBoundaryEntry(queuedCount, sessionEventTimestamp(event) || traceStartMs, traceStartMs, msg));
    }

    const rawKey = sessionEventKey(event);
    const matchingTraceEntries = traceEntriesByRawKey.get(rawKey) ?? [];
    matchingTraceEntries.forEach((traceEntry) => {
      if (lastIdleAt && traceEntry.createdAtMs - lastIdleAt >= 30_000) {
        entries.push(idleGapEntry(lastIdleAt, traceEntry.createdAtMs, traceStartMs));
        lastIdleAt = 0;
      }
      entries.push(transcriptEntryFromTraceEntry(traceEntry, traceStartMs, msg));
    });
  });

  traceEntries.forEach((entry) => {
    if (entries.some((candidate) => 'traceEntry' in candidate && candidate.traceEntry.id === entry.id)) {
      return;
    }
    entries.push(transcriptEntryFromTraceEntry(entry, traceStartMs, msg));
  });

  const entriesWithModelMetadata = applyModelRequestBrackets(entries, sortedRawEvents);
  return mergeToolCallBatches(entriesWithModelMetadata).sort((left, right) => left.createdAtMs - right.createdAtMs);
}

export function applyModelRequestBrackets(
  entries: SessionEventListEntry[],
  rawEvents: QuickstartSessionEvent[],
): SessionEventListEntry[] {
  const modelEvents = rawEvents.filter(sessionIsModelEvent);
  if (!modelEvents.length) {
    return entries;
  }

  type ModelTimelineItem =
    | { kind: 'model_start' | 'model_end'; event: QuickstartSessionEvent; time: number; order: number }
    | { kind: 'entry'; entry: SessionEventListEntry; time: number; order: number };

  const items: ModelTimelineItem[] = [
    ...modelEvents.map((event, index) => ({
      kind: sessionEventType(event) === 'span.model_request_start' ? ('model_start' as const) : ('model_end' as const),
      event,
      time: sessionEventTimestamp(event),
      order: index,
    })),
    ...entries.map((entry, index) => ({
      kind: 'entry' as const,
      entry,
      time: entry.createdAtMs,
      order: index,
    })),
  ].sort((left, right) => {
    const byTime = left.time - right.time;
    if (byTime) {
      return byTime;
    }
    const priority = (item: ModelTimelineItem) => (item.kind === 'model_start' ? 0 : item.kind === 'entry' ? 1 : 2);
    return priority(left) - priority(right) || left.order - right.order;
  });

  const bracketsByStartId = new Map<string, ModelRequestBracket>();
  let activeBracket: ModelRequestBracket | null = null;
  let pendingBracketMeta: ModelRequestBracketMeta | null = null;

  items.forEach((item) => {
    if (item.kind === 'model_start') {
      const startId = sessionEventKey(item.event);
      activeBracket = {
        startId,
        startMs: item.time,
        entries: [],
      };
      bracketsByStartId.set(startId, activeBracket);
      return;
    }

    if (item.kind === 'entry') {
      const targetEntry = sessionModelBracketTargetEntry(item.entry);
      if (activeBracket && targetEntry) {
        if (targetEntry.kind === 'tool_call' && !targetEntry.bracketId) {
          targetEntry.bracketId = activeBracket.startId;
        }
        activeBracket.entries.push(targetEntry);
        return;
      }
      if (pendingBracketMeta && sessionIsSubagentSentDisplayEntry(item.entry)) {
        applyModelBracketMetaToDisplayEntry(item.entry, pendingBracketMeta);
        pendingBracketMeta = null;
      }
      return;
    }

    const startId = sessionModelRequestStartRef(item.event);
    const bracket = (startId ? bracketsByStartId.get(startId) : null) ?? activeBracket;
    if (!bracket) {
      return;
    }
    const meta = modelRequestBracketMeta(bracket, item.event);
    const agentMessage = bracket.entries.find(sessionIsAgentMessageDisplayEntry);
    const firstTool = bracket.entries.find((entry): entry is ToolCallEntry => entry.kind === 'tool_call');
    if (agentMessage) {
      applyModelBracketMetaToDisplayEntry(agentMessage, meta);
      bracket.entries.forEach((entry) => {
        if (entry.kind === 'tool_call') {
          entry.usage = emptySessionEventUsage();
          entry.inferenceMs = 0;
        }
      });
    } else if (firstTool) {
      applyModelBracketMetaToToolEntry(firstTool, meta);
    } else {
      pendingBracketMeta = meta;
    }
    bracketsByStartId.delete(bracket.startId);
    if (activeBracket?.startId === bracket.startId) {
      activeBracket = null;
    }
  });

  return entries;
}

export function sessionModelBracketTargetEntry(entry: SessionEventListEntry): ModelBracketTargetEntry | null {
  if (entry.kind === 'message' && entry.displayEvent.type === 'agent') {
    return entry;
  }
  if (entry.kind === 'tool_call') {
    return entry;
  }
  return null;
}

export function sessionIsAgentMessageDisplayEntry(entry: ModelBracketTargetEntry): entry is DisplayEventEntry {
  return entry.kind === 'message' && entry.displayEvent.type === 'agent';
}

export function sessionIsSubagentSentDisplayEntry(entry: SessionEventListEntry): entry is DisplayEventEntry {
  return entry.kind === 'passthrough' && sessionEventType(entry.event) === 'agent.thread_message_sent';
}

export function modelRequestBracketMeta(
  bracket: ModelRequestBracket,
  endEvent: QuickstartSessionEvent,
): ModelRequestBracketMeta {
  const endMs = sessionEventTimestamp(endEvent) || bracket.startMs;
  return {
    startId: bracket.startId,
    startMs: bracket.startMs,
    inferenceMs: Math.max(0, endMs - bracket.startMs),
    usage: extractSessionEventUsage(endEvent),
  };
}

export function applyModelBracketMetaToDisplayEntry(entry: DisplayEventEntry, meta: ModelRequestBracketMeta) {
  if (!entry.bracketId) {
    entry.bracketId = meta.startId;
  }
  entry.bracketStartMs = meta.startMs;
  entry.usage = meta.usage;
  entry.inferenceMs = meta.inferenceMs;
  entry.processedAtMs = Math.max(entry.processedAtMs, meta.startMs);
}

export function applyModelBracketMetaToToolEntry(entry: ToolCallEntry, meta: ModelRequestBracketMeta) {
  if (!entry.bracketId) {
    entry.bracketId = meta.startId;
  }
  entry.bracketStartMs = meta.startMs;
  entry.usage = meta.usage;
  entry.inferenceMs = meta.inferenceMs;
}

export function sessionModelRequestStartRef(event: QuickstartSessionEvent) {
  return stringValueFromKeys(event, ['model_request_start_id', 'start_event_id', 'parent_event_id']);
}

export function h(
  event: QuickstartSessionEvent,
  traceStartMs = 0,
  msg?: I18nMsg,
  traceEntry?: SessionTraceEntry,
): DisplayEvent {
  const canonicalEvent = sessionCanonicalDisplayEvent(event);
  const family = sessionEventFamily(canonicalEvent);
  const type = displayEventType(canonicalEvent, family);
  const createdAtMs = sessionEventTimestamp(canonicalEvent) || traceEntry?.createdAtMs || 0;
  const processedAtMs = sessionEventProcessedTimestamp(canonicalEvent) || createdAtMs;
  const displayText = sessionEventIsThinking(canonicalEvent)
    ? sessionThinkingText(canonicalEvent)
    : sessionEventTranscriptText(canonicalEvent) ||
      sessionEventStructuredContentText(canonicalEvent) ||
      sessionToolResultText(canonicalEvent) ||
      sessionResultText(canonicalEvent);
  return {
    id: sessionEventDisplayId(canonicalEvent, traceEntry?.id ?? sessionEventKey(canonicalEvent)),
    type,
    rawType: sessionEventType(canonicalEvent),
    label: traceEntry?.label ?? sessionEventLabel(canonicalEvent, family, msg),
    content: displayText,
    event: canonicalEvent,
    isQueued: sessionEventIsQueuedUserMessage(canonicalEvent),
    isStreaming:
      canonicalEvent.is_streaming === true ||
      canonicalEvent.streaming === true ||
      ((sessionEventType(canonicalEvent) === 'agent.message' ||
        sessionEventType(canonicalEvent) === 'agent.thinking') &&
        sessionNullableProcessedAt(canonicalEvent) === null),
    isError:
      traceEntry?.isError ??
      (family === 'error' || canonicalEvent.is_error === true || Boolean(toRecord(canonicalEvent.error))),
    createdAtMs,
    processedAtMs,
    relativeTime: traceEntry?.relativeTime ?? sessionEventElapsedTime(canonicalEvent, traceStartMs),
  };
}

export function displayEventType(event: QuickstartSessionEvent, family = sessionEventFamily(event)): DisplayEventType {
  if (sessionEventIsThinking(event)) {
    return 'thinking';
  }
  switch (family) {
    case 'user':
      return 'user';
    case 'agent':
      return 'agent';
    case 'tool_use':
      return 'tool_use';
    case 'tool_result':
    case 'result':
      return 'result';
    case 'model':
    case 'span':
      return sessionIsModelEvent(event) ? 'model_request' : 'unknown';
    case 'outcome':
      return 'outcome';
    case 'thread':
      return 'thread';
    case 'subagent':
      return 'subagent';
    case 'status':
      return sessionStatusBadgeType(event);
    case 'error':
      return 'error';
    case 'system':
    case 'env':
      return sessionEventType(event) === 'system.message' ? 'system_message' : 'unknown';
    default:
      return 'unknown';
  }
}

export function sessionStatusBadgeType(event: QuickstartSessionEvent): DisplayEventType {
  switch (sessionEventType(event)) {
    case 'session.status_rescheduled':
      return 'status_rescheduled';
    case 'session.status_running':
      return 'status_running';
    case 'session.status_idle':
      return 'status_idle';
    case 'session.status_terminated':
      return 'status_terminated';
    case 'user.interrupt':
      return 'interrupt';
    default:
      return 'root';
  }
}

export function transcriptEntryFromTraceEntry(
  traceEntry: SessionTraceEntry,
  traceStartMs: number,
  msg?: I18nMsg,
): SessionEventListEntry {
  if (traceEntry.family === 'tool_use') {
    return toolCallEntryFromTraceEntry(traceEntry, traceStartMs, msg);
  }
  if (traceEntry.family === 'outcome') {
    return displayEntryFromTraceEntry(traceEntry, 'outcome', traceStartMs, msg);
  }
  if (traceEntry.family === 'status' || traceEntry.family === 'thread' || traceEntry.family === 'error') {
    return displayEntryFromTraceEntry(traceEntry, 'status', traceStartMs, msg);
  }
  if (
    traceEntry.family === 'system' ||
    traceEntry.family === 'env' ||
    traceEntry.family === 'span' ||
    traceEntry.family === 'model'
  ) {
    return displayEntryFromTraceEntry(traceEntry, 'passthrough', traceStartMs, msg);
  }
  return displayEntryFromTraceEntry(traceEntry, 'message', traceStartMs, msg);
}

export function displayEntryFromTraceEntry(
  traceEntry: SessionTraceEntry,
  kind: DisplayEventEntry['kind'],
  traceStartMs: number,
  msg?: I18nMsg,
): DisplayEventEntry {
  const event = traceEntry.event;
  const outcomeStatus = kind === 'outcome' ? sessionOutcomeStatus(event) : undefined;
  const durationMs = sessionEventDurationMs(event);
  return {
    ...baseEventEntry(traceEntry, kind, traceStartMs, msg),
    kind,
    usage: extractSessionEventUsage(event),
    inferenceMs: sessionEventInferenceMs(event),
    executionMs: durationMs,
    inProgress: sessionDisplayEntryInProgress(event, kind),
    outcomeStatus,
    outcomeIteration: kind === 'outcome' ? sessionOutcomeIteration(event) : undefined,
    durationMs,
    bracketId: sessionEventBracketId(event),
  };
}

export function toolCallEntryFromTraceEntry(
  traceEntry: SessionTraceEntry,
  traceStartMs: number,
  msg?: I18nMsg,
): ToolCallEntry {
  const event = traceEntry.event;
  const resultEvent = traceEntry.resultEvent;
  const confirmationEvent = traceEntry.confirmationEvent;
  return {
    ...baseEventEntry(traceEntry, 'tool_call', traceStartMs, msg),
    kind: 'tool_call',
    name: sessionToolDisplayName(event),
    inputPreview: sessionToolDisplayText(event),
    resultEvent,
    confirmationEvent,
    usage: extractSessionEventUsage(event),
    inferenceMs: sessionEventInferenceMs(event),
    executionMs: sessionEventDurationMs(resultEvent ?? event) || sessionEventDurationMs(event),
    lifecycle: sessionToolLifecycle(event, resultEvent, confirmationEvent),
    bracketId: sessionEventBracketId(event),
    isError: traceEntry.isError || sessionToolResultIsError(resultEvent),
  };
}

export function baseEventEntry(
  traceEntry: SessionTraceEntry,
  kind: BaseSessionEventEntry['kind'],
  traceStartMs: number,
  msg?: I18nMsg,
): BaseSessionEventEntry {
  const displayEvent = h(traceEntry.event, traceStartMs, msg, traceEntry);
  return {
    id: `${kind}-${traceEntry.id}`,
    kind,
    displayEvent,
    traceEntry,
    event: traceEntry.event,
    type: traceEntry.type,
    rawEventId: traceEntry.rawEventId,
    createdAtMs: traceEntry.createdAtMs,
    processedAtMs: displayEvent.processedAtMs,
    relativeTime: traceEntry.relativeTime,
    searchText: traceEntry.searchText,
    isError: traceEntry.isError,
  };
}

export function sessionDisplayEntryInProgress(event: QuickstartSessionEvent, kind: DisplayEventEntry['kind']) {
  if (event.is_streaming === true || event.streaming === true) {
    return true;
  }
  if (kind !== 'status' && kind !== 'outcome') {
    return false;
  }
  const type = sessionEventType(event);
  if (
    type === 'session.status_running' ||
    type === 'session.status_rescheduled' ||
    type === 'session.thread_status_running' ||
    type === 'session.thread_status_rescheduled' ||
    type === 'span.outcome_evaluation_start' ||
    type === 'span.outcome_evaluation_ongoing'
  ) {
    return true;
  }
  const status = stringValueFromKeys(event, ['status', 'state', 'lifecycle']).toLowerCase();
  return status === 'running' || status === 'queued' || status === 'rescheduled' || status === 'evaluating';
}

export function idleGapEntry(idleAtMs: number, nextAtMs: number, traceStartMs: number): IdleGapEntry {
  const durationMs = Math.max(0, nextAtMs - idleAtMs);
  return {
    id: `idle-gap-${idleAtMs}-${nextAtMs}`,
    kind: 'idle_gap',
    durationMs,
    createdAtMs: idleAtMs,
    processedAtMs: idleAtMs,
    relativeTime: sessionEventElapsedTime({ created_at: new Date(idleAtMs).toISOString() }, traceStartMs),
    searchText: `idle gap ${durationMs}`,
    isError: false,
  };
}

export function queuedBoundaryEntry(
  count: number,
  createdAtMs: number,
  traceStartMs: number,
  msg?: I18nMsg,
): QueuedBoundaryEntry {
  const text = msg
    ? msg('managedAgents.sessions.trace.queuedMessages', '{count} queued messages', { count })
    : `${count} queued messages`;
  return {
    id: `queued-boundary-${createdAtMs}-${count}`,
    kind: 'queued_boundary',
    count,
    createdAtMs,
    processedAtMs: createdAtMs,
    relativeTime: sessionEventElapsedTime({ created_at: new Date(createdAtMs).toISOString() }, traceStartMs),
    searchText: text.toLowerCase(),
    isError: false,
  };
}

export function mergeToolCallBatches(entries: SessionEventListEntry[]): SessionEventListEntry[] {
  const merged: SessionEventListEntry[] = [];
  for (let index = 0; index < entries.length; index += 1) {
    const entry = entries[index];
    const bracketId = sessionBatchSegmentBracketId(entry);
    if (!bracketId) {
      merged.push(entry);
      continue;
    }
    const segment: SessionEventListEntry[] = [entry];
    let nextIndex = index + 1;
    while (nextIndex < entries.length) {
      const next = entries[nextIndex];
      if (sessionBatchSegmentBracketId(next) !== bracketId) {
        break;
      }
      segment.push(next);
      nextIndex += 1;
    }
    const calls = segment.filter((item): item is ToolCallEntry => item.kind === 'tool_call');
    if (calls.length < 2) {
      merged.push(...segment);
      index = nextIndex - 1;
      continue;
    }
    // 官方 Console 在同一个 model request 片段内允许 message 和 tool_call 混排：
    // message 仍保持独立行，只有多个 tool_call 被折叠成一个 tool_batch。
    merged.push(...segment.filter((item) => item.kind !== 'tool_call'), toolBatchEntry(calls));
    index = nextIndex - 1;
  }
  return merged;
}

export function sessionBatchSegmentBracketId(entry: SessionEventListEntry) {
  if (entry.kind === 'tool_call') {
    return entry.bracketId;
  }
  if (entry.kind === 'message' && entry.displayEvent.type === 'agent') {
    return entry.bracketId ?? '';
  }
  return '';
}

export function toolBatchEntry(calls: ToolCallEntry[]): ToolBatchEntry {
  const first = calls[0];
  const usage = calls.reduce<SessionEventUsage>(
    (total, call) => addSessionEventUsage(total, call.usage),
    emptySessionEventUsage(),
  );
  const executionMs = calls.reduce((max, call) => Math.max(max, call.executionMs), 0);
  const inferenceMs = calls.reduce((total, call) => total + call.inferenceMs, 0);
  const toolCounts = [
    ...calls.reduce((counts, call) => {
      counts.set(call.name, (counts.get(call.name) ?? 0) + 1);
      return counts;
    }, new Map<string, number>()),
  ].map(([name, count]) => ({ name, count }));
  const lifecyclePriority: Record<ToolLifecycle, number> = {
    awaiting_approval: 0,
    running: 1,
    failed: 2,
    denied: 3,
    completed: 4,
  };
  const lifecycle = calls
    .map((call) => call.lifecycle)
    .reduce((current, next) => (lifecyclePriority[current] <= lifecyclePriority[next] ? current : next), 'completed');
  return {
    ...first,
    id: `tool-batch-${first.bracketId}-${calls.map((call) => call.rawEventId).join('-')}`,
    kind: 'tool_batch',
    calls,
    toolCounts,
    usage,
    inferenceMs,
    executionMs,
    lifecycle,
    displayEvent: {
      ...first.displayEvent,
      label: 'Tool batch',
      content: calls.map((call) => `${call.name}: ${call.inputPreview}`).join('\n'),
    },
    isError: calls.some((call) => call.isError),
  };
}

export function sessionEventProcessedTimestamp(event: QuickstartSessionEvent) {
  const processedAt = typeof event.processed_at === 'string' ? Date.parse(event.processed_at) : NaN;
  return Number.isFinite(processedAt) ? processedAt : 0;
}

export function sessionEventInferenceMs(event: QuickstartSessionEvent) {
  return numericValueFromKeys(event, ['inference_ms', 'model_duration_ms', 'model_request_duration_ms']);
}

export function sessionToolLifecycle(
  event: QuickstartSessionEvent,
  resultEvent?: QuickstartSessionEvent,
  confirmationEvent?: QuickstartSessionEvent,
): ToolLifecycle {
  const permission = normalizedToolPermission(event);
  const confirmationResult = normalizedToolConfirmationResult(confirmationEvent);
  const resultIsError = sessionToolResultIsError(resultEvent);

  // Session detail 只回放已经写入事件流的权限状态：策略拒绝或用户拒绝都表示工具不会继续执行。
  if (permission === 'deny' || confirmationResult === 'deny') {
    return 'denied';
  }
  if (resultEvent) {
    return resultIsError ? 'failed' : 'completed';
  }
  if (permission === 'ask' && !confirmationEvent) {
    return 'awaiting_approval';
  }
  return 'running';
}

export function sessionToolResultIsError(event?: QuickstartSessionEvent) {
  return event?.is_error === true || Boolean(toRecord(event?.error));
}

export function normalizedToolPermission(event: QuickstartSessionEvent): 'ask' | 'allow' | 'deny' {
  const data = toRecord(event.data);
  const metadata = toRecord(event.metadata);
  const permissionRecord =
    toRecord(event.evaluated_permission) ??
    toRecord(event.permission) ??
    toRecord(event.permission_policy) ??
    toRecord(event.permission_decision) ??
    toRecord(event.confirmation) ??
    (data ? toRecord(data.evaluated_permission) : null) ??
    (data ? toRecord(data.permission) : null) ??
    (data ? toRecord(data.permission_policy) : null) ??
    (metadata ? toRecord(metadata.evaluated_permission) : null) ??
    (metadata ? toRecord(metadata.permission) : null) ??
    (metadata ? toRecord(metadata.permission_policy) : null);
  const requiresActionDetails =
    toRecord(event.requires_action_details) ??
    (data ? toRecord(data.requires_action_details) : null) ??
    (metadata ? toRecord(metadata.requires_action_details) : null);
  const stopReason =
    toRecord(event.stop_reason) ??
    (data ? toRecord(data.stop_reason) : null) ??
    (metadata ? toRecord(metadata.stop_reason) : null);
  const permissionRaw = [
    event.evaluated_permission,
    event.permission,
    event.permission_policy,
    event.permission_decision,
    event.confirmation,
    data?.evaluated_permission,
    data?.permission,
    data?.permission_policy,
    data?.permission_decision,
    metadata?.evaluated_permission,
    metadata?.permission,
    metadata?.permission_policy,
    metadata?.permission_decision,
    permissionRecord?.result,
    permissionRecord?.decision,
    permissionRecord?.type,
    permissionRecord?.policy,
    permissionRecord?.state,
    permissionRecord?.status,
  ]
    .map(normalizedStringValue)
    .find(Boolean);
  const requiresActionRaw = [
    requiresActionDetails?.type,
    requiresActionDetails?.status,
    stopReason?.type,
    stopReason?.status,
  ]
    .map(normalizedStringValue)
    .find(Boolean);
  const statusRaw = [
    event.status,
    event.lifecycle,
    data?.status,
    data?.lifecycle,
    metadata?.status,
    metadata?.lifecycle,
  ]
    .map(normalizedStringValue)
    .find(Boolean);
  const raw = permissionRaw || requiresActionRaw || statusRaw;

  switch (raw) {
    case 'ask':
    case 'always_ask':
    case 'await_permission':
    case 'awaiting_permission':
    case 'awaiting_approval':
    case 'needs_permission':
    case 'needs_approval':
    case 'permission_required':
    case 'approval_required':
    case 'requires_action':
      return 'ask';
    case 'deny':
    case 'denied':
      return 'deny';
    case 'allow':
    case 'always_allow':
    case 'allowed':
      return 'allow';
    default:
      return event.requires_action === true ? 'ask' : 'allow';
  }
}

export function normalizedToolConfirmationResult(event?: QuickstartSessionEvent): 'allow' | 'deny' | undefined {
  if (!event) {
    return undefined;
  }
  const result = normalizedStringValue(event.result);
  if (result === 'deny' || result === 'denied') {
    return 'deny';
  }
  if (result === 'allow' || result === 'allowed') {
    return 'allow';
  }
  return undefined;
}

export function normalizedStringValue(value: unknown) {
  return typeof value === 'string' ? value.trim().toLowerCase() : '';
}

export function sessionEventBracketId(event: QuickstartSessionEvent) {
  const candidates = [
    event.bracket_id,
    event.model_request_id,
    event.span_id,
    event.parent_span_id,
    event.parent_event_id,
    toRecord(event.metadata)?.bracket_id,
    toRecord(event.metadata)?.model_request_id,
  ];
  const bracketId = candidates.find((value): value is string => typeof value === 'string' && value.trim().length > 0);
  return bracketId ? bracketId.trim() : '';
}

export function sessionEventIsQueuedUserMessage(event: QuickstartSessionEvent) {
  if (sessionEventType(event) !== 'user.message') {
    return false;
  }
  return event.is_queued === true || event.queued === true || toRecord(event.metadata)?.queued === true;
}

export function sessionContentBlockEntries(
  event: QuickstartSessionEvent,
  index: number,
  toolResults: Map<string, QuickstartSessionEvent[]>,
  toolConfirmations: Map<string, QuickstartSessionEvent[]>,
  view: SessionTraceView,
  traceStartMs: number,
  threadHintsByToolUseId: Map<string, { id: string; name: string }>,
  msg?: I18nMsg,
) {
  const content = sessionEventContentBlocks(event);
  if (!content.length) {
    return [];
  }

  const entries: SessionTraceEntry[] = [];
  const textBlocks = content.filter((block) => {
    const type = toRecord(block)?.type;
    return type === 'text';
  });
  const text = contentBlocksText(textBlocks);
  if (text) {
    const textEvent = { ...event, content: textBlocks };
    entries.push(
      sessionTraceEntryFromEvent(textEvent, index, sessionEventFamily(event), undefined, undefined, traceStartMs, msg),
    );
  }

  content.forEach((block, blockIndex) => {
    const record = toRecord(block);
    if (!record) {
      return;
    }
    if (record.type === 'thinking') {
      const thinkingEvent: QuickstartSessionEvent = {
        ...event,
        id: `${sessionEventKey(event)}-thinking-${blockIndex}`,
        type: 'agent.thinking',
        content: [record],
        parent_event_id: sessionEventKey(event),
      };
      entries.push(sessionTraceEntryFromEvent(thinkingEvent, index, 'agent', undefined, undefined, traceStartMs, msg));
      return;
    }
    if (record.type === 'tool_use') {
      const id = typeof record.id === 'string' ? record.id : `${sessionEventKey(event)}-tool-${blockIndex}`;
      const toolEvent: QuickstartSessionEvent = {
        id,
        type: 'agent.tool_use',
        // content block 派生事件需要继承外层 owner，否则子线程工具会回落到主 lane。
        session_id: event.session_id,
        session_thread_id: event.session_thread_id,
        thread_id: event.thread_id,
        created_at: event.created_at,
        name: typeof record.name === 'string' ? record.name : 'tool_use',
        input: record.input ?? {},
        parent_event_id: sessionEventKey(event),
      };
      if (view === 'transcript' && sessionIsAgentSubagentToolUse(toolEvent)) {
        return;
      }
      entries.push(
        sessionTraceEntryFromEvent(
          toolEvent,
          index,
          'tool_use',
          selectSessionToolCompanionEvent(toolResults.get(id), toolEvent, threadHintsByToolUseId),
          selectSessionToolCompanionEvent(toolConfirmations.get(id), toolEvent, threadHintsByToolUseId),
          traceStartMs,
          msg,
        ),
      );
      return;
    }
    if (record.type === 'tool_result' && view === 'debug') {
      const resultEvent: QuickstartSessionEvent = {
        id:
          typeof record.tool_use_id === 'string'
            ? `${record.tool_use_id}-result`
            : `${sessionEventKey(event)}-result-${blockIndex}`,
        type: 'agent.tool_result',
        session_id: event.session_id,
        session_thread_id: event.session_thread_id,
        thread_id: event.thread_id,
        created_at: event.created_at,
        tool_use_id: record.tool_use_id,
        content: record.content,
        is_error: record.is_error,
        parent_event_id: sessionEventKey(event),
      };
      entries.push(
        sessionTraceEntryFromEvent(resultEvent, index, 'tool_result', undefined, undefined, traceStartMs, msg),
      );
      return;
    }
    if (record.type === 'tool_result' && view === 'transcript') {
      const toolUseId = typeof record.tool_use_id === 'string' ? record.tool_use_id : '';
      const threadHint = toolUseId ? threadHintsByToolUseId.get(toolUseId) : undefined;
      if (!threadHint) {
        return;
      }
      const resultEvent: QuickstartSessionEvent = {
        id: `${toolUseId}-thread-result`,
        type: 'agent.thread_message_received',
        created_at: event.created_at,
        tool_use_id: toolUseId,
        from_session_thread_id: threadHint.id,
        from_agent_name: threadHint.name,
        content: sessionVisibleToolResultContent(record),
        parent_event_id: sessionEventKey(event),
      };
      entries.push(sessionTraceEntryFromEvent(resultEvent, index, 'subagent', undefined, undefined, traceStartMs, msg));
    }
  });

  return entries;
}

export function buildSessionThreadHints(events: QuickstartSessionEvent[]) {
  const byThreadId = new Map<string, SessionThreadHint>();
  events.forEach((event) => {
    const threadId = sessionSubagentThreadId(event);
    if (!threadId) {
      return;
    }
    const name = sessionSubagentName(event);
    if (!name) {
      return;
    }
    const existing = byThreadId.get(threadId);
    if (!existing || (!existing.name && name)) {
      byThreadId.set(threadId, { id: threadId, name });
    }
  });

  const byToolUseId = new Map<string, SessionThreadHint>();
  events.forEach((event) => {
    const toolUseId = sessionThreadHintToolUseId(event);
    if (!toolUseId) {
      return;
    }
    const threadId = sessionSubagentThreadId(event);
    if (!threadId) {
      return;
    }
    const name = sessionSubagentName(event) || byThreadId.get(threadId)?.name || '';
    const existing = byToolUseId.get(toolUseId);
    if (!existing || (!existing.name && name)) {
      byToolUseId.set(toolUseId, { id: threadId, name });
    }
  });
  return { byThreadId, byToolUseId };
}

export function sessionThreadHintToolUseId(event: QuickstartSessionEvent) {
  if (sessionIsToolUseEvent(event)) {
    return sessionToolUseId(event);
  }
  return sessionToolConfirmationToolUseId(event) || sessionToolResultToolUseId(event);
}

export function sessionEventWithThreadHint(
  event: QuickstartSessionEvent,
  threadHintsByThreadId: Map<string, SessionThreadHint>,
) {
  if (sessionSubagentName(event)) {
    return event;
  }
  const threadId = sessionSubagentThreadId(event);
  if (!threadId) {
    return event;
  }
  const hint = threadHintsByThreadId.get(threadId);
  if (!hint?.name) {
    return event;
  }
  const type = sessionEventType(event);
  if (type === 'agent.thread_message_received') {
    return { ...event, from_agent_name: hint.name };
  }
  if (type === 'agent.thread_message_sent') {
    return { ...event, to_agent_name: hint.name };
  }
  if (type === 'session.thread_created' || type.startsWith('session.thread_status_')) {
    return { ...event, agent_name: hint.name };
  }
  return event;
}

export function sessionVisibleToolResultContent(block: Record<string, unknown>) {
  const content = block.content;
  if (!Array.isArray(content)) {
    return content;
  }
  const filtered = content.filter((item) => {
    const record = toRecord(item);
    const text = typeof record?.text === 'string' ? record.text.trim() : '';
    return !text.startsWith('agentId:') && !text.includes('<usage>');
  });
  return filtered.length ? filtered : content;
}

export function sessionTraceEntryFromEvent(
  event: QuickstartSessionEvent,
  index: number,
  family = sessionEventFamily(event),
  resultEvent?: QuickstartSessionEvent,
  confirmationEvent?: QuickstartSessionEvent,
  traceStartMs = 0,
  msg?: I18nMsg,
): SessionTraceEntry {
  const type = sessionEventType(event);
  const createdAtMs = sessionEventTimestamp(event) || index;
  const displayText = sessionEventIsThinking(event)
    ? ''
    : sessionEventTranscriptText(event) ||
      sessionEventStructuredContentText(event) ||
      sessionToolResultText(event) ||
      sessionResultText(event);
  const displayKind = sessionTraceDisplayKind(event, family, displayText);
  const label = sessionEventLabel(event, family, msg);
  const preview = sessionEventPreview(event, displayText, family, msg);
  const id = `${sessionEventKey(event)}-${index}-${family}`;
  const rawEventId = sessionEventDisplayId(event, id);
  const isError = family === 'error' || event.is_error === true || Boolean(toRecord(event.error));
  const searchText = [
    id,
    type,
    label,
    preview,
    displayText,
    typeof event.id === 'string' ? event.id : '',
    resultEvent ? sessionEventDebugJson(resultEvent) : '',
    confirmationEvent ? sessionEventDebugJson(confirmationEvent) : '',
    sessionEventDebugJson(event),
  ]
    .join('\n')
    .toLowerCase();

  return {
    id,
    type,
    family,
    label,
    preview,
    displayText,
    displayKind,
    event,
    resultEvent,
    confirmationEvent,
    createdAtMs,
    relativeTime: sessionEventElapsedTime(event, traceStartMs),
    rawEventId,
    searchText,
    isError,
  };
}

export function sessionTraceFilterValue(entry: SessionTraceEntry, view: SessionTraceView) {
  if (view === 'debug') {
    return entry.type;
  }
  if (entry.family === 'tool_use' || entry.family === 'tool_result') {
    return 'tool';
  }
  if (entry.family === 'model') {
    return 'model';
  }
  if (entry.family === 'outcome') {
    return 'system';
  }
  if (entry.family === 'thread') {
    return 'status';
  }
  if (entry.family === 'result') {
    return 'result';
  }
  if (entry.family === 'subagent') {
    return 'subagent';
  }
  if (entry.family === 'status') {
    return 'status';
  }
  if (entry.family === 'system' || entry.family === 'env' || entry.family === 'span') {
    return 'system';
  }
  return entry.family;
}

export function sessionTraceDisplayKind(
  event: QuickstartSessionEvent,
  family: SessionTraceFamily,
  displayText: string,
): SessionTraceDisplayKind {
  if (sessionEventIsThinking(event)) {
    return 'thinking';
  }
  if (family === 'tool_use') {
    return 'command';
  }
  if (family === 'model') {
    return 'metric';
  }
  if (sessionTraceTextIsJson(displayText)) {
    return 'json';
  }
  if (family === 'tool_result' || family === 'env') {
    return 'log';
  }
  if (family === 'status' || family === 'system' || family === 'span' || family === 'thread') {
    return 'status';
  }
  return 'prose';
}

export function sessionTraceTextIsJson(value: string) {
  const trimmed = value.trim();
  if (!looksLikeJson(trimmed) || trimmed.length > 100000) {
    return false;
  }
  try {
    JSON.parse(trimmed);
    return true;
  } catch {
    return false;
  }
}

export function sessionTraceDetailTitle(entry: SessionTraceEntry) {
  if (sessionEventIsThinking(entry.event)) {
    return 'Thinking';
  }
  if (entry.family === 'user' || entry.family === 'agent') {
    return 'Message';
  }
  if (entry.family === 'subagent') {
    return entry.label;
  }
  if (entry.family === 'env') {
    return 'Env';
  }
  if (entry.family === 'system') {
    return entry.type === 'system.message' ? 'System message' : 'System';
  }
  if (entry.family === 'status') {
    return sessionStatusDescription(entry.type, entry.event) ?? entry.label;
  }
  if (entry.family === 'model' || entry.family === 'outcome' || entry.family === 'result') {
    return entry.label;
  }
  return entry.label;
}

export function sessionEventAppearsInTranscript(event: QuickstartSessionEvent, options: SessionTraceBuildOptions = {}) {
  if (sessionIsClaudeUserEchoEvent(event)) {
    return false;
  }
  if (options.platformTranscriptFiltering) {
    if (sessionIsToolResultEvent(event) || sessionEventType(event) === 'user.tool_confirmation') {
      return false;
    }
    if (sessionIsAgentSubagentToolUse(event)) {
      return false;
    }
    if (sessionEventType(event) === 'system.message' || (sessionIsModelEvent(event) && event.is_error !== true)) {
      return false;
    }
    const filteredFamily = sessionEventFamily(event);
    if (filteredFamily === 'system' || filteredFamily === 'env' || filteredFamily === 'span') {
      return false;
    }
    if (sessionIsThreadStatusEvent(event)) {
      return false;
    }
    if (sessionEventType(event) === 'session.status_running') {
      return false;
    }
    if (sessionEventType(event) === 'session.status_idle' && !sessionIsResultEvent(event)) {
      return false;
    }
  }
  const family = sessionEventFamily(event);
  if (
    family === 'user' ||
    family === 'agent' ||
    family === 'subagent' ||
    family === 'tool_use' ||
    family === 'model' ||
    family === 'outcome' ||
    family === 'result' ||
    family === 'thread' ||
    family === 'status' ||
    family === 'error' ||
    family === 'system' ||
    family === 'env'
  ) {
    return true;
  }
  return sessionEventType(event) === 'user.interrupt';
}

export function sessionIsClaudeUserEchoEvent(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  if (type !== 'user' && type !== 'user.message') {
    return false;
  }
  if (sessionIsToolResultEvent(event)) {
    return false;
  }
  const message = toRecord(event.message);
  return message?.role === 'user';
}

export function sessionEventFamily(event: QuickstartSessionEvent): SessionTraceFamily {
  const type = sessionEventType(event);
  if (type === 'env_manager_log' || type.startsWith('environment.')) {
    return 'env';
  }
  if (sessionIsOutcomeEvent(event)) {
    return 'outcome';
  }
  if (sessionIsModelEvent(event)) {
    return 'model';
  }
  if (sessionIsResultEvent(event)) {
    return 'result';
  }
  if (type.startsWith('span.')) {
    return 'span';
  }
  if (
    (type === 'user' || type.startsWith('user.')) &&
    !sessionIsToolResultEvent(event) &&
    type !== 'user.tool_confirmation'
  ) {
    return 'user';
  }
  if (type === 'user.tool_confirmation') {
    return 'status';
  }
  if (type === 'agent.message' || type === 'assistant.message' || type === 'agent' || type === 'agent.thinking') {
    return 'agent';
  }
  if (
    type === 'session.error' ||
    type.endsWith('.error') ||
    event.is_error === true ||
    Boolean(toRecord(event.error))
  ) {
    return 'error';
  }
  if (sessionIsThreadLifecycleEvent(event)) {
    return 'thread';
  }
  if (
    type.startsWith('session.status_') ||
    type === 'session.deleted' ||
    type === 'session.updated' ||
    type === 'user.interrupt'
  ) {
    return 'status';
  }
  if (sessionIsSubagentEvent(event)) {
    return 'subagent';
  }
  if (sessionIsToolUseEvent(event)) {
    return 'tool_use';
  }
  if (sessionIsToolResultEvent(event)) {
    return 'tool_result';
  }
  return 'system';
}

export function sessionIsModelEvent(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  return type === 'span.model_request_start' || type === 'span.model_request_end';
}

export function sessionIsOutcomeEvent(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  return (
    type === 'span.outcome_evaluation_start' ||
    type === 'span.outcome_evaluation_ongoing' ||
    type === 'span.outcome_evaluation_end' ||
    type === 'user.define_outcome'
  );
}

export function sessionIsThreadLifecycleEvent(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  return (
    type === 'session.thread_created' ||
    type === 'session_thread.created' ||
    type === 'agent.thread_context_compacted' ||
    sessionIsThreadStatusEvent(event)
  );
}

export function sessionIsThreadStatusEvent(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  return (
    type.startsWith('session.thread_status_') ||
    type === 'session.thread_idle' ||
    type === 'session.thread_idled' ||
    type === 'session.thread_terminated'
  );
}

export function sessionIsResultEvent(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  if (type === 'result') {
    return true;
  }
  if (type !== 'session.status_idle') {
    return false;
  }
  return typeof event.result === 'string' || Boolean(toRecord(event.result)) || typeof event.message_link === 'string';
}

export function sessionIsToolUseEvent(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  return (
    type === 'agent.tool_use' ||
    type === 'agent.mcp_tool_use' ||
    type === 'agent.custom_tool_use' ||
    type === 'tool_use' ||
    type.endsWith('.tool_use') ||
    type.endsWith('_tool_use') ||
    (typeof event.name === 'string' && 'input' in event)
  );
}

export function sessionIsAgentSubagentToolUse(event: QuickstartSessionEvent) {
  if (!sessionIsToolUseEvent(event)) {
    return false;
  }
  const name = sessionToolDisplayName(event).toLowerCase();
  if (name === 'agent') {
    return true;
  }
  if (name !== 'task') {
    return false;
  }
  const input = objectRecord(sessionToolUseInput(event));
  return typeof input.prompt === 'string' && input.prompt.trim().length > 0;
}

export function sessionIsToolResultEvent(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  return (
    sessionIsLegacyToolResultThreadMessage(event) ||
    type === 'agent.tool_result' ||
    type === 'agent.mcp_tool_result' ||
    type === 'agent.custom_tool_result' ||
    type === 'user.tool_result' ||
    type === 'user.custom_tool_result' ||
    type === 'tool_result' ||
    type.endsWith('.tool_result') ||
    type.endsWith('_tool_result') ||
    sessionEventContentBlocks(event).some((block) => toRecord(block)?.type === 'tool_result')
  );
}

export function sessionIsSubagentEvent(event: QuickstartSessionEvent) {
  if (sessionIsLegacyToolResultThreadMessage(event)) {
    return false;
  }
  const type = sessionEventType(event);
  return (
    type === 'agent.subagent_spawned' ||
    type === 'subagent.spawned' ||
    type === 'agent.thread_message_received' ||
    type === 'agent.thread_message_sent'
  );
}

export function sessionIsLegacyToolResultThreadMessage(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  if (type !== 'agent.thread_message_received' && type !== 'agent.thread_message_sent') {
    return false;
  }
  if (sessionThreadMessageHasCanonicalPeerRef(event)) {
    return false;
  }
  return Boolean(sessionRawToolResult(event));
}

export function sessionThreadMessageHasCanonicalPeerRef(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  const data = toRecord(event.data);
  const metadata = toRecord(event.metadata);
  const toolUseResult = toRecord(event.tool_use_result);
  const candidates =
    type === 'agent.thread_message_received'
      ? [event.from_agent_name, data?.from_agent_name, metadata?.from_agent_name]
      : [event.to_agent_name, data?.to_agent_name, metadata?.to_agent_name];
  return candidates.some((value) => typeof value === 'string' && value.trim().length > 0) || Boolean(toolUseResult);
}

export function sessionRawToolResult(event: QuickstartSessionEvent) {
  const data = toRecord(event.data);
  const metadata = toRecord(event.metadata);
  return toRecord(event.raw_tool_result) ?? toRecord(data?.raw_tool_result) ?? toRecord(metadata?.raw_tool_result);
}

export function sessionToolUseId(event: QuickstartSessionEvent) {
  const id = firstStringField(event, ['tool_use_id', 'mcp_tool_use_id', 'custom_tool_use_id', 'id']);
  if (id) {
    return id;
  }
  const message = toRecord(event.message);
  return firstStringField(message, ['tool_use_id', 'mcp_tool_use_id', 'custom_tool_use_id', 'id']);
}

export function sessionToolConfirmationToolUseId(event: QuickstartSessionEvent) {
  return firstStringField(event, ['tool_use_id', 'mcp_tool_use_id', 'custom_tool_use_id']);
}

export function firstStringField(record: Record<string, unknown> | null, keys: string[]) {
  if (!record) {
    return '';
  }
  for (const key of keys) {
    const value = record[key];
    if (typeof value === 'string' && value.trim()) {
      return value.trim();
    }
  }
  return '';
}

export function sessionToolResultToolUseId(event: QuickstartSessionEvent) {
  const direct = firstStringField(event, ['tool_use_id', 'mcp_tool_use_id', 'custom_tool_use_id']);
  if (direct) {
    return direct;
  }
  const rawToolResult = sessionRawToolResult(event);
  const raw = firstStringField(rawToolResult, ['tool_use_id', 'mcp_tool_use_id', 'custom_tool_use_id']);
  if (raw) {
    return raw;
  }
  const content = event.content;
  if (Array.isArray(content)) {
    for (const block of content) {
      const blockId = firstStringField(toRecord(block), ['tool_use_id', 'mcp_tool_use_id', 'custom_tool_use_id']);
      if (blockId) {
        return blockId;
      }
    }
  }
  return '';
}

export function sessionSubagentThreadId(event: QuickstartSessionEvent) {
  const data = toRecord(event.data);
  const metadata = toRecord(event.metadata);
  const sessionThread = toRecord(event.session_thread);
  const subagent = toRecord(event.subagent);
  const candidates = [
    event.session_thread_id,
    event.from_session_thread_id,
    event.to_session_thread_id,
    event.thread_id,
    sessionThread?.id,
    data?.session_thread_id,
    data?.from_session_thread_id,
    data?.to_session_thread_id,
    data?.thread_id,
    metadata?.session_thread_id,
    metadata?.from_session_thread_id,
    metadata?.to_session_thread_id,
    metadata?.thread_id,
    event.subagent_id,
    event.subagent,
    subagent?.id,
    data?.subagent_id,
    metadata?.subagent_id,
  ];
  const threadId = candidates.find((value): value is string => typeof value === 'string' && value.trim().length > 0);
  return threadId ? threadId.trim() : '';
}

export function sessionSubagentName(event: QuickstartSessionEvent) {
  const data = toRecord(event.data);
  const metadata = toRecord(event.metadata);
  const sessionThread = toRecord(event.session_thread);
  const subagent = toRecord(event.subagent);
  const agent = toRecord(event.agent);
  const candidates = [
    event.agent_name,
    event.from_agent_name,
    event.to_agent_name,
    event.thread_name,
    event.subagent_name,
    data?.agent_name,
    data?.from_agent_name,
    data?.to_agent_name,
    data?.thread_name,
    data?.subagent_name,
    metadata?.agent_name,
    metadata?.from_agent_name,
    metadata?.to_agent_name,
    metadata?.thread_name,
    metadata?.subagent_name,
    subagent?.name,
    agent?.name,
    sessionThread?.name,
    sessionThread?.role,
  ];
  const name = candidates.find((value): value is string => typeof value === 'string' && value.trim().length > 0);
  return name ? name.trim() : '';
}

export function sessionSubagentPreview(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  const threadId = sessionSubagentThreadId(event);
  const name = sessionSubagentName(event) || (threadId ? compactEntityId(threadId) : '');
  const text = sessionEventTranscriptText(event);
  if (type === 'agent.thread_context_compacted') {
    return text || 'Thread context compacted';
  }
  if (type === 'agent.thread_message_received') {
    const prefix = name ? `← ${name}` : '← subagent';
    return text ? `${prefix}: ${text}` : threadId ? `${prefix} (${compactEntityId(threadId)})` : prefix;
  }
  if (type === 'agent.thread_message_sent') {
    const prefix = name ? `→ ${name}` : '→ subagent';
    return text ? `${prefix}: ${text}` : threadId ? `${prefix} (${compactEntityId(threadId)})` : prefix;
  }
  if (name && threadId) {
    return `→ ${name} (${compactEntityId(threadId)})`;
  }
  if (name) {
    return `→ ${name}`;
  }
  return threadId ? `→ ${compactEntityId(threadId)}` : '';
}

export function sessionToolUseInput(event: QuickstartSessionEvent) {
  if ('input' in event) {
    return event.input;
  }
  const message = toRecord(event.message);
  if (message && 'input' in message) {
    return message.input;
  }
  return {};
}

export function sessionToolDisplayName(event: QuickstartSessionEvent) {
  const message = toRecord(event.message);
  const tool = toRecord(event.tool);
  const input = toRecord(event.input);
  const rawName =
    typeof event.name === 'string' && event.name.trim()
      ? event.name.trim()
      : typeof event.tool_name === 'string' && event.tool_name.trim()
        ? event.tool_name.trim()
        : typeof event.mcp_tool_name === 'string' && event.mcp_tool_name.trim()
          ? event.mcp_tool_name.trim()
          : typeof event.custom_tool_name === 'string' && event.custom_tool_name.trim()
            ? event.custom_tool_name.trim()
            : typeof tool?.name === 'string' && tool.name.trim()
              ? tool.name.trim()
              : typeof input?.tool_name === 'string' && input.tool_name.trim()
                ? input.tool_name.trim()
                : typeof message?.name === 'string' && message.name.trim()
                  ? message.name.trim()
                  : '';
  if (!rawName) {
    return 'Tool';
  }
  return rawName
    .replace(/^(agent_|mcp_|computer_)/, '')
    .split('_')
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

export function sessionToolDisplayText(event: QuickstartSessionEvent) {
  const input = objectRecord(sessionToolUseInput(event));
  const description = input.description;
  if (typeof description === 'string' && description.trim()) {
    return truncateSessionToolPreview(description.trim());
  }
  const command = input.command;
  if (typeof command === 'string' && command.trim()) {
    return truncateSessionToolPreview(command.trim().replace(/\s+/g, ' '));
  }
  const filePath = input.file_path;
  if (typeof filePath === 'string' && filePath.trim()) {
    return filePath;
  }
  const path = input.path;
  if (typeof path === 'string' && path.trim()) {
    return path;
  }
  const query = input.query;
  if (typeof query === 'string' && query.trim()) {
    return truncateSessionToolPreview(query.trim());
  }
  const url = input.url;
  if (typeof url === 'string' && url.trim()) {
    return truncateSessionToolPreview(url.trim().replace(/^https?:\/\//, ''));
  }
  const pattern = input.pattern;
  if (typeof pattern === 'string' && pattern.trim()) {
    return truncateSessionToolPreview(pattern.trim());
  }
  const prompt = input.prompt;
  if (typeof prompt === 'string' && prompt.trim()) {
    return truncateSessionToolPreview(prompt.trim().replace(/\s+/g, ' '));
  }
  const text = input.text;
  if (typeof text === 'string' && text.trim()) {
    return truncateSessionToolPreview(text.trim().replace(/\s+/g, ' '));
  }
  for (const value of Object.values(input)) {
    if (typeof value === 'string' && value.trim() && value.length <= 200) {
      return truncateSessionToolPreview(value.trim());
    }
  }
  return '';
}

export function truncateSessionToolPreview(value: string, maxLength = 60) {
  return value.length > maxLength ? `${value.slice(0, maxLength)}…` : value;
}

export function sessionToolUseCodeLanguage(event: QuickstartSessionEvent): HighlightLanguage {
  const name = sessionToolDisplayName(event).toLowerCase();
  if (name === 'bash' || name === 'shell' || name.includes('terminal')) {
    return 'bash';
  }
  return 'plaintext';
}

export function sessionToolResultText(event: QuickstartSessionEvent) {
  const content = event.content;
  if (typeof content === 'string') {
    return content;
  }
  if (Array.isArray(content)) {
    const texts = content
      .map((block) => {
        const record = toRecord(block);
        if (!record) {
          return '';
        }
        if (typeof record.text === 'string') {
          return record.text;
        }
        if (typeof record.content === 'string') {
          return record.content;
        }
        if (Array.isArray(record.content)) {
          return contentBlocksText(record.content);
        }
        return '';
      })
      .filter(Boolean);
    if (texts.length) {
      return texts.join('\n');
    }
  }
  if (typeof event.result === 'string') {
    return event.result;
  }
  const rawToolResult = sessionRawToolResult(event);
  if (typeof rawToolResult?.content === 'string') {
    return rawToolResult.content;
  }
  if (Array.isArray(rawToolResult?.content)) {
    return contentBlocksText(rawToolResult.content);
  }
  return '';
}

export function sessionResultText(event: QuickstartSessionEvent) {
  if (typeof event.result === 'string' && event.result.trim()) {
    return event.result.trim();
  }
  if (event.result !== undefined) {
    return JSON.stringify(event.result);
  }
  if (typeof event.message_link === 'string' && event.message_link.trim()) {
    return JSON.stringify({ message_link: event.message_link.trim() });
  }
  return '';
}

export function sessionEventContentBlocks(event: QuickstartSessionEvent) {
  if (Array.isArray(event.content)) {
    return event.content;
  }
  const message = toRecord(event.message);
  if (Array.isArray(message?.content)) {
    return message.content;
  }
  return [];
}

export function sessionEventStructuredContentText(event: QuickstartSessionEvent) {
  const content = sessionEventContentBlocks(event);
  if (!content.length) {
    return '';
  }
  const structuredBlocks = content
    .map(toRecord)
    .filter(
      (block): block is Record<string, unknown> =>
        block !== null && block.type !== 'text' && block.type !== 'tool_use' && block.type !== 'tool_result',
    );
  if (!structuredBlocks.length) {
    return '';
  }
  const value = structuredBlocks.length === 1 ? structuredBlocks[0] : structuredBlocks;
  return JSON.stringify(value, null, 2);
}

export function sessionEventHasThinkingContent(event: QuickstartSessionEvent) {
  return sessionEventContentBlocks(event).some((block) => toRecord(block)?.type === 'thinking');
}

export function sessionEventIsThinking(event: QuickstartSessionEvent) {
  return (
    sessionEventType(event) === 'agent.thinking' ||
    sessionEventHasThinkingContent(event) ||
    typeof event.thinking === 'string'
  );
}

export function sessionThinkingText(event: QuickstartSessionEvent) {
  if (typeof event.thinking === 'string' && event.thinking.trim()) {
    return event.thinking.trim();
  }
  const texts = sessionEventContentBlocks(event)
    .map(toRecord)
    .filter((block): block is Record<string, unknown> => block !== null && block.type === 'thinking')
    .map((block) => {
      if (typeof block.thinking === 'string') {
        return block.thinking.trim();
      }
      if (typeof block.text === 'string') {
        return block.text.trim();
      }
      if (typeof block.content === 'string') {
        return block.content.trim();
      }
      if (Array.isArray(block.content)) {
        return contentBlocksText(block.content).trim();
      }
      return '';
    })
    .filter(Boolean);
  return texts.join('\n\n');
}

export function sessionThinkingLabel(msg?: I18nMsg) {
  return msg ? msg('managedAgents.sessions.trace.thinking', 'Thinking') : 'Thinking';
}

export function sessionThinkingPreview(msg?: I18nMsg) {
  return msg ? msg('managedAgents.sessions.trace.thinkingInProgress', 'Thinking...') : 'Thinking...';
}

export function sessionEventLabel(event: QuickstartSessionEvent, family: SessionTraceFamily, msg?: I18nMsg) {
  if (family === 'tool_use' || family === 'tool_result') {
    return family === 'tool_use'
      ? sessionToolDisplayName(event)
      : msg
        ? msg('managedAgents.sessions.trace.result', 'Result')
        : 'Result';
  }
  if (family === 'model') {
    return msg ? msg('managedAgents.sessions.trace.model', 'Model') : 'Model';
  }
  if (family === 'outcome') {
    return msg ? msg('managedAgents.sessions.trace.outcome', 'Outcome') : 'Outcome';
  }
  if (family === 'thread') {
    return msg ? msg('managedAgents.sessions.trace.thread', 'Thread') : 'Thread';
  }
  if (family === 'result') {
    return msg ? msg('managedAgents.sessions.trace.result', 'Result') : 'Result';
  }
  if (family === 'user') {
    return 'User';
  }
  if (family === 'agent') {
    return sessionEventIsThinking(event) ? sessionThinkingLabel(msg) : 'Agent';
  }
  if (family === 'subagent') {
    return 'Subagent';
  }
  if (family === 'env') {
    return 'Env';
  }
  if (family === 'system') {
    return 'System';
  }
  if (family === 'status') {
    return sessionStatusLabel(sessionEventType(event));
  }
  if (family === 'error') {
    return 'Error';
  }
  if (family === 'span') {
    return 'Span';
  }
  return sessionEventType(event);
}

export function sessionEventPreview(
  event: QuickstartSessionEvent,
  displayText: string,
  family: SessionTraceFamily,
  msg?: I18nMsg,
) {
  if (family === 'agent' && sessionEventIsThinking(event)) {
    return sessionThinkingPreview(msg);
  }
  if (family === 'system' && sessionEventType(event) === 'system.message') {
    return 'System message';
  }
  if (family === 'status') {
    return sessionStatusDescription(sessionEventType(event), event) ?? '';
  }
  if (family === 'subagent') {
    return sessionSubagentPreview(event);
  }
  if (family === 'model') {
    return sessionSpanDescription(event, msg);
  }
  if (family === 'outcome') {
    return sessionOutcomeDescription(event, msg);
  }
  if (family === 'thread') {
    return sessionStatusDescription(sessionEventType(event), event) ?? '';
  }
  if (family === 'result') {
    return displayText || sessionResultText(event) || sessionEventSummary(event);
  }
  if (displayText) {
    return displayText.replace(/\s+/g, ' ').trim();
  }
  if (family === 'tool_use') {
    const primaryText = sessionToolDisplayText(event);
    if (primaryText) {
      return primaryText;
    }
    const input = sessionToolUseInput(event);
    const json = JSON.stringify(input);
    return json === '{}' ? '' : json;
  }
  if (family === 'error') {
    return sessionEventErrorMessage(event);
  }
  if (family === 'span') {
    return sessionSpanDescription(event, msg);
  }
  return sessionEventSummary(event);
}

export function sessionStatusLabel(type: string) {
  switch (type) {
    case 'session.status_idle':
      return 'Idle';
    case 'session.status_running':
      return 'Running';
    case 'session.status_rescheduled':
      return 'Queued';
    case 'session.status_terminated':
      return 'Terminated';
    case 'session.thread_status_running':
      return 'Thread running';
    case 'session.thread_status_idle':
    case 'session.thread_idled':
      return 'Thread idle';
    case 'session.thread_status_rescheduled':
      return 'Thread queued';
    case 'session.thread_status_terminated':
    case 'session.thread_terminated':
      return 'Thread terminated';
    case 'session.deleted':
      return 'Deleted';
    case 'session.updated':
      return 'Updated';
    case 'user.interrupt':
      return 'Interrupted';
    case 'user.tool_confirmation':
      return 'Tool confirmation';
    default:
      return type;
  }
}

export function sessionStatusDescription(type: string, event?: QuickstartSessionEvent) {
  const thread = sessionThreadStatusReference(event);
  switch (type) {
    case 'user.interrupt':
      return 'Run interrupted by the user.';
    case 'user.tool_confirmation':
      return 'Tool confirmation submitted.';
    case 'session.status_rescheduled':
      return 'Session queued - waiting to start.';
    case 'session.status_running':
      return 'Session is processing.';
    case 'session.status_terminated':
      return 'Session terminated.';
    case 'session.status_idle':
      return 'Session idle';
    case 'session.thread_created':
    case 'session_thread.created':
      return thread ? `Thread spawned ${thread}` : 'Thread spawned.';
    case 'session.thread_status_running':
      return thread ? `Thread running ${thread}` : 'Thread is processing.';
    case 'session.thread_status_idle':
    case 'session.thread_idled':
      return thread ? `Thread idle ${thread}` : 'Thread idle';
    case 'session.thread_status_rescheduled':
      return thread ? `Thread queued ${thread}` : 'Thread queued.';
    case 'session.thread_status_terminated':
    case 'session.thread_terminated':
      return thread ? `Thread terminated ${thread}` : 'Thread terminated.';
    case 'agent.thread_context_compacted':
      return thread ? `Thread context compacted ${thread}` : 'Thread context compacted.';
    case 'session.deleted':
      return 'Session deleted.';
    case 'session.updated':
      return 'Session updated.';
    default:
      return null;
  }
}

export function sessionThreadStatusReference(event?: QuickstartSessionEvent) {
  if (!event) {
    return '';
  }
  const name = sessionSubagentName(event);
  const threadId = sessionSubagentThreadId(event);
  if (name && threadId) {
    return `${name} (${compactEntityId(threadId)})`;
  }
  if (name) {
    return name;
  }
  return threadId ? compactEntityId(threadId) : '';
}

export function sessionOutcomeDescription(event: QuickstartSessionEvent, msg?: I18nMsg) {
  const type = sessionEventType(event);
  const iteration = typeof event.iteration === 'number' ? event.iteration : 0;
  switch (type) {
    case 'span.outcome_evaluation_start':
      return msg
        ? msg('managedAgents.sessions.trace.outcomeEvaluationStarted', 'Grading started (iteration {iteration})', {
            iteration,
          })
        : `Grading started (iteration ${iteration})`;
    case 'span.outcome_evaluation_ongoing':
      return msg
        ? msg('managedAgents.sessions.trace.outcomeEvaluationOngoing', 'Grading in progress (iteration {iteration})', {
            iteration,
          })
        : `Grading in progress (iteration ${iteration})`;
    case 'span.outcome_evaluation_end': {
      const result = typeof event.result === 'string' && event.result ? ` ${event.result}` : '';
      return msg
        ? msg(
            'managedAgents.sessions.trace.outcomeEvaluationCompleted',
            'Grading result (iteration {iteration}){result}',
            { iteration, result },
          )
        : `Grading result (iteration ${iteration})${result}`;
    }
    case 'user.define_outcome':
      return typeof event.description === 'string' && event.description
        ? event.description
        : msg
          ? msg('managedAgents.sessions.trace.outcomeDefined', 'Outcome defined')
          : 'Outcome defined';
    default:
      return type;
  }
}

export function sessionSpanDescription(event: QuickstartSessionEvent, msg?: I18nMsg) {
  const type = sessionEventType(event);
  const usage = extractSessionEventUsage(event);
  const usageText =
    usage.input || usage.output
      ? msg
        ? msg('managedAgents.sessions.trace.modelUsage', '{input} input → {output} output', {
            input: String(usage.input),
            output: String(usage.output),
          })
        : `${usage.input} input → ${usage.output} output`
      : '';
  switch (type) {
    case 'span.model_request_start':
      return msg ? msg('managedAgents.sessions.trace.modelRequestStart', 'Model request start') : 'Model request start';
    case 'span.model_request_end':
      return (
        usageText ||
        (msg
          ? msg('managedAgents.sessions.trace.modelRequestComplete', 'Model request complete')
          : 'Model request complete')
      );
    case 'span.outcome_evaluation_start':
      return 'Outcome evaluation started';
    case 'span.outcome_evaluation_ongoing':
      return 'Outcome evaluation ongoing';
    case 'span.outcome_evaluation_end':
      return 'Outcome evaluation completed';
    default:
      return type;
  }
}

export function sessionEventErrorMessage(event: QuickstartSessionEvent) {
  const error = toRecord(event.error);
  if (typeof error?.message === 'string') {
    return error.message;
  }
  if (typeof event.error === 'string') {
    return event.error;
  }
  if (typeof event.message === 'string') {
    return event.message;
  }
  return sessionEventDebugJson(event);
}

export function prettyCode(value: string) {
  if (value.length > 100000) {
    return value;
  }
  const trimmed = value.trim();
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) {
    return value;
  }
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2);
  } catch {
    return value;
  }
}

export function parseTranscriptMarkdownBlocks(value: string): TranscriptMarkdownBlock[] {
  const lines = value.replace(/\r\n/g, '\n').split('\n');
  const blocks: TranscriptMarkdownBlock[] = [];
  let index = 0;
  while (index < lines.length) {
    const line = lines[index];
    const trimmed = line.trim();
    if (!trimmed) {
      index += 1;
      continue;
    }

    const fence = trimmed.match(/^```([a-zA-Z0-9_-]+)?\s*$/);
    if (fence) {
      const codeLines: string[] = [];
      index += 1;
      while (index < lines.length && !lines[index].trim().startsWith('```')) {
        codeLines.push(lines[index]);
        index += 1;
      }
      if (index < lines.length) {
        index += 1;
      }
      blocks.push({ type: 'code', language: fence[1]?.toLowerCase(), value: codeLines.join('\n') });
      continue;
    }

    const heading = trimmed.match(/^(#{1,4})\s+(.+)$/);
    if (heading) {
      blocks.push({ type: 'heading', level: heading[1].length, text: heading[2].trim() });
      index += 1;
      continue;
    }

    if (isTranscriptMarkdownTableStart(lines, index)) {
      const headers = splitTranscriptMarkdownTableRow(lines[index]);
      index += 2;
      const rows: string[][] = [];
      while (index < lines.length && isTranscriptMarkdownTableRow(lines[index])) {
        rows.push(splitTranscriptMarkdownTableRow(lines[index]));
        index += 1;
      }
      blocks.push({ type: 'table', headers, rows });
      continue;
    }

    const listItem = trimmed.match(/^[-*]\s+(.+)$/);
    if (listItem) {
      const items: string[] = [];
      while (index < lines.length) {
        const current = lines[index].trim().match(/^[-*]\s+(.+)$/);
        if (!current) {
          break;
        }
        items.push(current[1].trim());
        index += 1;
      }
      blocks.push({ type: 'list', items });
      continue;
    }

    const paragraphLines: string[] = [];
    while (index < lines.length) {
      const current = lines[index];
      const currentTrimmed = current.trim();
      if (
        !currentTrimmed ||
        currentTrimmed.startsWith('```') ||
        currentTrimmed.match(/^(#{1,4})\s+/) ||
        currentTrimmed.match(/^[-*]\s+/) ||
        isTranscriptMarkdownTableStart(lines, index)
      ) {
        break;
      }
      paragraphLines.push(currentTrimmed);
      index += 1;
    }
    blocks.push({ type: 'paragraph', text: paragraphLines.join(' ') });
  }
  return blocks.length ? blocks : [{ type: 'paragraph', text: value }];
}

export function isTranscriptMarkdownTableStart(lines: string[], index: number) {
  if (index + 1 >= lines.length || !isTranscriptMarkdownTableRow(lines[index])) {
    return false;
  }
  const separator = splitTranscriptMarkdownTableRow(lines[index + 1]);
  return separator.length >= 2 && separator.every((cell) => /^:?-{3,}:?$/.test(cell.trim()));
}

export function isTranscriptMarkdownTableRow(line: string) {
  const trimmed = line.trim();
  return trimmed.startsWith('|') && trimmed.endsWith('|') && splitTranscriptMarkdownTableRow(trimmed).length >= 2;
}

export function splitTranscriptMarkdownTableRow(line: string) {
  return line
    .trim()
    .replace(/^\|/, '')
    .replace(/\|$/, '')
    .split('|')
    .map((cell) => cell.trim());
}

export function isSafeTranscriptMarkdownHref(value: string) {
  return /^(https?:|mailto:|\/|#)/i.test(value);
}

export function parseTranscriptCode(value: string): { value: string; language?: string } | null {
  const trimmed = value.trim();
  const fenced = trimmed.match(/^```([a-zA-Z0-9_-]+)?\s*\n([\s\S]*?)\n?```$/);
  if (fenced) {
    const language = fenced[1]?.toLowerCase();
    const body = fenced[2].trim();
    return {
      value: language === 'json' && !body.includes('\n') ? prettyCode(body) : body,
      language,
    };
  }
  if (looksLikeJson(trimmed)) {
    return { value: trimmed.includes('\n') ? trimmed : prettyCode(trimmed), language: 'json' };
  }
  return null;
}

export function looksLikeJson(value: string) {
  const trimmed = value.trim();
  return (trimmed.startsWith('{') && trimmed.endsWith('}')) || (trimmed.startsWith('[') && trimmed.endsWith(']'));
}

export function mergeSessionEvents(current: QuickstartSessionEvent[], incoming: QuickstartSessionEvent[]) {
  const byKey = new Map<string, QuickstartSessionEvent>();
  const canonicalToKey = new Map<string, string>();
  [...current, ...incoming].forEach((event) => {
    const key = sessionEventKey(event);
    const canonicalKey = sessionEventCanonicalKey(event);
    const existingByKey = byKey.get(key);
    if (existingByKey) {
      byKey.set(key, preferSessionEvent(existingByKey, event));
      if (canonicalKey) {
        canonicalToKey.set(canonicalKey, key);
      }
      return;
    }

    if (canonicalKey) {
      const existingKey = canonicalToKey.get(canonicalKey);
      if (!existingKey) {
        byKey.set(key, event);
        canonicalToKey.set(canonicalKey, key);
        return;
      }

      const existing = byKey.get(existingKey);
      if (existing && sessionEventsShouldCoalesce(existing, event)) {
        const preferred = preferSessionEvent(existing, event);
        const preferredKey = sessionEventKey(preferred);
        byKey.delete(existingKey);
        byKey.set(preferredKey, preferred);
        canonicalToKey.set(canonicalKey, preferredKey);
        return;
      }
    }

    byKey.set(key, event);
    if (canonicalKey) {
      canonicalToKey.set(canonicalKey, key);
    }
  });
  return [...byKey.values()].sort(compareSessionEvents);
}

export function compareSessionEvents(left: QuickstartSessionEvent, right: QuickstartSessionEvent) {
  const leftTimestamp = sessionEventTimestamp(left);
  const rightTimestamp = sessionEventTimestamp(right);
  if (leftTimestamp && rightTimestamp && leftTimestamp !== rightTimestamp) {
    return leftTimestamp - rightTimestamp;
  }
  if (leftTimestamp && !rightTimestamp) {
    return -1;
  }
  if (!leftTimestamp && rightTimestamp) {
    return 1;
  }
  return 0;
}

export function sessionEventCanonicalKey(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  if (
    type !== 'user.message' &&
    type !== 'agent.message' &&
    type !== 'assistant.message' &&
    !type.startsWith('session.status_')
  ) {
    return null;
  }
  const text =
    sessionEventTranscriptText(event) || sessionToolResultText(event) || sessionStatusDescription(type, event) || '';
  return `${type}:${text}`;
}

export function sessionEventsShouldCoalesce(left: QuickstartSessionEvent, right: QuickstartSessionEvent) {
  return !sessionEventTimestamp(left) || !sessionEventTimestamp(right);
}

export function preferSessionEvent(left: QuickstartSessionEvent, right: QuickstartSessionEvent) {
  const leftScore = sessionEventCompletenessScore(left);
  const rightScore = sessionEventCompletenessScore(right);
  if (rightScore >= leftScore) {
    return { ...left, ...right };
  }
  return { ...right, ...left };
}

export function sessionEventCompletenessScore(event: QuickstartSessionEvent) {
  return (
    (sessionEventTimestamp(event) ? 4 : 0) +
    (typeof event.id === 'string' && event.id ? 2 : 0) +
    Object.keys(event).length
  );
}

export function sessionEventKey(event: QuickstartSessionEvent) {
  if (typeof event.id === 'string' && event.id) {
    return event.id;
  }
  return JSON.stringify(event);
}

export function sessionEventType(event: QuickstartSessionEvent) {
  return typeof event.type === 'string' && event.type ? event.type : 'event';
}

export function sessionEventTimestamp(event: QuickstartSessionEvent) {
  const createdAt = typeof event.created_at === 'string' ? Date.parse(event.created_at) : NaN;
  return Number.isFinite(createdAt) ? createdAt : 0;
}

export function sessionEventElapsedTime(event: QuickstartSessionEvent, traceStartMs: number) {
  const timestamp = sessionEventTimestamp(event);
  if (!timestamp) {
    return 'now';
  }
  if (!traceStartMs) {
    return '0:00:00';
  }
  const elapsedSeconds = Math.max(0, Math.floor((timestamp - traceStartMs) / 1000));
  const hours = Math.floor(elapsedSeconds / 3600);
  const minutes = Math.floor((elapsedSeconds % 3600) / 60);
  const seconds = elapsedSeconds % 60;
  return `${hours}:${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
}

export function sessionEventDisplayId(event: QuickstartSessionEvent, fallback: string) {
  if (typeof event.id === 'string' && event.id) {
    return event.id;
  }
  if (typeof event.uuid === 'string' && event.uuid) {
    return event.uuid;
  }
  return fallback;
}

export function compactSessionEventId(value: string) {
  if (value.length <= 18) {
    return value;
  }
  return `${value.slice(0, 5)}…${value.slice(-7)}`;
}

export function sessionEventSummary(event: QuickstartSessionEvent) {
  if (sessionEventIsThinking(event)) {
    return 'Thinking...';
  }
  const text = sessionEventTranscriptText(event);
  if (text) {
    return text;
  }
  const data = toRecord(event.data);
  if (typeof data?.content === 'string' && data.content.trim()) {
    return data.content;
  }
  const content = event.content;
  if (Array.isArray(content)) {
    if (content.some((block) => toRecord(block)?.type === 'thinking')) {
      return 'Agent';
    }
  }
  const message = event.message;
  if (typeof message === 'string' && message.trim()) {
    return message;
  }
  const error = event.error;
  if (typeof error === 'string' && error.trim()) {
    return error;
  }
  return JSON.stringify(event).slice(0, 120);
}

export function sessionEventTranscriptText(event: QuickstartSessionEvent) {
  if (typeof event.content === 'string' && event.content.trim()) {
    return event.content.trim();
  }
  const contentText = contentBlocksText(event.content);
  if (contentText) {
    return contentText;
  }
  if (typeof event.message === 'string' && event.message.trim()) {
    return event.message.trim();
  }
  const message = toRecord(event.message);
  if (typeof message?.content === 'string' && message.content.trim()) {
    return message.content.trim();
  }
  const messageText = contentBlocksText(message?.content);
  return messageText;
}

export function contentBlocksText(content: unknown) {
  if (!Array.isArray(content)) {
    return '';
  }
  return content.map(contentBlockText).filter(Boolean).join('\n');
}

export function contentBlockText(block: unknown): string {
  const record = toRecord(block);
  if (!record) {
    return '';
  }
  if (typeof record.text === 'string') {
    return record.text;
  }
  if (typeof record.thinking === 'string') {
    return record.thinking;
  }
  if (typeof record.content === 'string') {
    return record.content;
  }
  if (Array.isArray(record.content)) {
    return contentBlocksText(record.content);
  }
  return '';
}

export function sessionEventDebugJson(event: QuickstartSessionEvent) {
  const json = JSON.stringify(event, null, 2);
  if (json.length <= 5000) {
    return json;
  }
  return `${json.slice(0, 5000)}\n... truncated`;
}

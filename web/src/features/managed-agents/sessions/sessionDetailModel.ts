import { useFormatters } from "../../../shared/i18n";
import {
  sessionNullableProcessedAt,
  sessionStableEventId,
  sessionThreadIsArchived,
  sessionThreadIsChild,
} from "../api";
import { entityAgentId, entityStatusLabel, entityVaultIds } from "../resources/ManagedResources";
import {
  type DisplayEventEntry,
  type DisplayEventType,
  type I18nMsg,
  type IdleGapEntry,
  type QueuedBoundaryEntry,
  type QuickstartSessionEvent,
  type SessionApiResponse,
  type SessionDetailLane,
  type SessionDetailLaneState,
  type SessionDetailSummaryChip,
  type SessionEventListEntry,
  type SessionEventUsage,
  type SessionResourceApiResponse,
  type SessionThreadApiResponse,
  type SessionTimelineItem,
  type SessionTimelineLane,
  type SessionTraceFilterOption,
  type SessionTraceView,
  type ToolBatchEntry,
  type ToolCallEntry,
} from "../types";
import { compactEntityId, toRecord } from "../utils";
import { Bot, CalendarClock, Cloud, Database, FileText, LockKeyhole } from "lucide-react";
import { SESSION_ARCHIVED_LANES_STORAGE_KEY, SESSION_MAIN_LANE_ID } from "./sessionTimeline";
import {
  sessionEventSummary,
  sessionEventTimestamp,
  sessionEventType,
  sessionTraceFilterValue,
} from "./sessionTraceModel";

export function buildSessionDetailSummary(
  session: SessionApiResponse,
  resources: SessionResourceApiResponse[],
  events: QuickstartSessionEvent[],
  formatters: ReturnType<typeof useFormatters>,
  msg: I18nMsg,
) {
  const title = session.title || sessionAgentDisplayName(session.agent) || session.id;
  const statusLabel = entityStatusLabel(session);
  const chips: SessionDetailSummaryChip[] = [];
  const agentLabel = sessionAgentDisplayName(session.agent) || entityAgentId(session);
  if (agentLabel) {
    chips.push({ key: "agent", icon: Bot, value: agentLabel });
  }
  if (session.environment_id) {
    chips.push({ key: "environment", icon: Cloud, value: session.environment_id });
  }
  const resourceLabel = sessionResourcesLabel(resources, msg);
  if (resourceLabel) {
    chips.push({ key: "resources", icon: FileText, value: resourceLabel });
  }
  const vaults = entityVaultIds(session);
  if (vaults.length) {
    chips.push({
      key: "vaults",
      icon: LockKeyhole,
      value:
        vaults.length === 1
          ? vaults[0]
          : msg("managedAgents.sessions.detail.vaultCount", "{count} vaults", { count: vaults.length }),
    });
  }
  chips.push({
    key: "created",
    icon: CalendarClock,
    value: formatRelativeFromNow(session.created_at, formatters, msg),
  });
  const elapsedMs = sessionElapsedMs(session, events);
  if (elapsedMs > 0) {
    chips.push({ key: "duration", icon: CalendarClock, value: formatSessionDuration(elapsedMs, formatters, msg) });
  }
  const usage = events.reduce<{ input: number; output: number }>(
    (total, event) => {
      const current = extractSessionEventUsage(event);
      total.input += current.input;
      total.output += current.output;
      return total;
    },
    { input: 0, output: 0 },
  );
  if (usage.input || usage.output) {
    chips.push({
      key: "tokens",
      icon: Database,
      value: msg("managedAgents.sessions.detail.tokensInOut", "{input} in / {output} out", {
        input: formatCompactTokenCount(usage.input, formatters),
        output: formatCompactTokenCount(usage.output, formatters),
      }),
    });
  }
  return { title, statusLabel, chips };
}

export function buildSessionDetailFilterOptions(
  entries: SessionEventListEntry[],
  view: SessionTraceView,
  msg: I18nMsg,
): SessionTraceFilterOption[] {
  if (view === "transcript") {
    return localizedTranscriptFilterOptions(msg);
  }
  const seen = new Set<string>();
  return entries
    .filter((entry): entry is DisplayEventEntry | ToolCallEntry | ToolBatchEntry => "traceEntry" in entry)
    .map((entry) => entry.type)
    .filter((type) => {
      if (seen.has(type)) {
        return false;
      }
      seen.add(type);
      return true;
    })
    .sort((left, right) => left.localeCompare(right))
    .map((type) => ({ value: type, label: type }));
}

export function localizedTranscriptFilterOptions(msg: I18nMsg): SessionTraceFilterOption[] {
  return [
    { value: "user", label: msg("managedAgents.sessions.trace.user", "User") },
    { value: "agent", label: msg("managedAgents.sessions.trace.agent", "Agent") },
    { value: "subagent", label: msg("managedAgents.sessions.trace.subagent", "Subagent") },
    { value: "tool", label: msg("managedAgents.sessions.trace.tool", "Tool") },
    { value: "model", label: msg("managedAgents.sessions.trace.model", "Model") },
    { value: "result", label: msg("managedAgents.sessions.trace.result", "Result") },
    { value: "status", label: msg("managedAgents.sessions.trace.status", "Status") },
    { value: "system", label: msg("managedAgents.sessions.trace.system", "System") },
    { value: "error", label: msg("managedAgents.sessions.trace.error", "Error") },
  ];
}

export function sessionEventListFilterValue(entry: SessionEventListEntry, view: SessionTraceView) {
  if (!("traceEntry" in entry)) {
    return entry.kind;
  }
  if (entry.kind === "tool_batch") {
    return view === "debug" ? entry.type : "tool";
  }
  return sessionTraceFilterValue(entry.traceEntry, view);
}

export function sessionEventEntryLaneId(entry: SessionEventListEntry, laneIdByThreadId: Map<string, string>) {
  if (!("event" in entry)) {
    return SESSION_MAIN_LANE_ID;
  }
  return sessionEventLaneId(entry.event, laneIdByThreadId);
}

export function buildSessionEventsByLane(
  lanes: SessionDetailLane[],
  events: QuickstartSessionEvent[],
  laneIdByThreadId: Map<string, string>,
) {
  const eventsByLaneId = new Map<string, QuickstartSessionEvent[]>();
  lanes.forEach((lane) => eventsByLaneId.set(lane.id, []));
  events.forEach((event) => {
    const laneId = sessionEventLaneId(event, laneIdByThreadId);
    const laneEvents = eventsByLaneId.get(laneId);
    if (laneEvents) {
      laneEvents.push(event);
    }
  });
  return eventsByLaneId;
}

export function flattenSessionEntriesByLane(
  lanes: SessionDetailLane[],
  entriesByLaneId: Map<string, SessionEventListEntry[]>,
) {
  return lanes.flatMap((lane) => entriesByLaneId.get(lane.id) ?? []);
}

export function buildSessionDetailLaneState(
  threads: SessionThreadApiResponse[],
  msg: I18nMsg,
  showArchivedLanes: boolean,
): SessionDetailLaneState {
  const sortedChildren = [...threads].filter(sessionThreadIsChild).sort((left, right) => {
    const created = String(left.created_at || "").localeCompare(String(right.created_at || ""));
    return created || String(left.id || "").localeCompare(String(right.id || ""));
  });
  const lanes: SessionDetailLane[] = [
    {
      id: SESSION_MAIN_LANE_ID,
      label: msg("managedAgents.sessions.detail.orchestrator", "Orchestrator"),
      group: "Main",
      isMain: true,
    },
  ];
  const threadNameById = new Map<string, string>();
  const laneIdByThreadId = new Map<string, string>();
  threads.forEach((thread) => {
    if (!sessionThreadIsChild(thread)) {
      laneIdByThreadId.set(thread.id, SESSION_MAIN_LANE_ID);
    }
  });

  const childNames = uniqueSessionThreadLabels(sortedChildren, msg);
  sortedChildren.forEach((thread) => {
    const group = sessionThreadBaseName(thread) || sessionThreadFallbackLabel(thread, lanes.length, msg);
    const name = childNames.get(thread.id) ?? group;
    threadNameById.set(thread.id, name);
    if (sessionThreadIsArchived(thread) && !showArchivedLanes) {
      return;
    }
    laneIdByThreadId.set(thread.id, thread.id);
    lanes.push({ id: thread.id, label: name, group, archived: sessionThreadIsArchived(thread) });
  });

  return {
    lanes,
    threadNameById,
    laneIdByThreadId,
    archivedLaneCount: sortedChildren.filter(sessionThreadIsArchived).length,
    isMultiAgent: sortedChildren.length > 0,
  };
}

export function uniqueSessionThreadLabels(threads: SessionThreadApiResponse[], msg: I18nMsg) {
  const baseCounts = new Map<string, number>();
  const labels = new Map<string, string>();
  threads.forEach((thread, index) => {
    const base = sessionThreadBaseName(thread) || sessionThreadFallbackLabel(thread, index + 1, msg);
    const count = (baseCounts.get(base) ?? 0) + 1;
    baseCounts.set(base, count);
    labels.set(thread.id, count > 1 ? `${base} ${count}` : base);
  });
  return labels;
}

export function buildSessionTimeline(
  lanes: SessionDetailLane[],
  entriesByLaneId: Map<string, SessionEventListEntry[]>,
): SessionTimelineLane[] {
  return lanes.map((lane) => ({
    ...lane,
    items: (entriesByLaneId.get(lane.id) ?? [])
      .map(sessionTimelineItemFromEntry)
      .filter((item): item is SessionTimelineItem => Boolean(item)),
  }));
}

export function sessionTimelineItemFromEntry(entry: SessionEventListEntry): SessionTimelineItem | null {
  if (entry.kind === "queued_boundary") {
    return null;
  }
  if (entry.kind === "idle_gap") {
    return {
      id: entry.id,
      rowId: entry.id,
      type: "status_idle",
      label: "",
      preview: "",
      relativeTime: entry.relativeTime,
      processedAtMs: entry.processedAtMs,
      durationMs: entry.durationMs,
    };
  }
  if (!Number.isFinite(entry.processedAtMs)) {
    return null;
  }
  if (entry.kind === "message" && entry.displayEvent.isQueued) {
    return null;
  }
  if (entry.kind === "passthrough" && entry.displayEvent.isStreaming) {
    return null;
  }
  if (entry.kind === "tool_call") {
    const startMs = entry.bracketStartMs ?? entry.processedAtMs;
    const executionMs = Math.max(0, entry.executionMs ?? 0);
    const durationMs = entry.bracketStartMs
      ? Math.max(0, entry.processedAtMs - entry.bracketStartMs + executionMs)
      : executionMs;
    return {
      id: entry.id,
      rowId: entry.traceEntry.id,
      type: "tool_use",
      label: entry.name,
      preview: entry.inputPreview || entry.displayEvent.content,
      relativeTime: entry.relativeTime,
      processedAtMs: startMs,
      durationMs,
    };
  }
  if (entry.kind === "tool_batch") {
    const startMs = entry.bracketStartMs ?? entry.processedAtMs;
    const executionMs = Math.max(0, entry.executionMs ?? 0);
    const durationMs = entry.bracketStartMs
      ? Math.max(0, entry.processedAtMs - entry.bracketStartMs + executionMs)
      : executionMs;
    const label =
      entry.toolCounts.length === 1 ? (entry.toolCounts[0]?.name ?? "Tool") : `${entry.calls.length} parallel calls`;
    return {
      id: entry.id,
      rowId: entry.traceEntry.id,
      type: "tool_use",
      label,
      preview: entry.toolCounts.map((tool) => (tool.count > 1 ? `${tool.name} x${tool.count}` : tool.name)).join(", "),
      relativeTime: entry.relativeTime,
      processedAtMs: startMs,
      durationMs,
    };
  }

  const startMs = entry.kind === "message" && entry.bracketStartMs ? entry.bracketStartMs : entry.processedAtMs;
  return {
    id: entry.id,
    rowId: entry.traceEntry.id,
    type: sessionTimelineItemType(entry.displayEvent.type),
    label: entry.displayEvent.label || entry.traceEntry.label || sessionEventType(entry.event),
    preview: entry.traceEntry.preview || entry.traceEntry.displayText || entry.displayEvent.content,
    relativeTime: entry.relativeTime,
    processedAtMs: startMs,
    durationMs:
      entry.kind === "message"
        ? Math.max(0, entry.inferenceMs ?? 0)
        : entry.kind === "outcome"
          ? Math.max(0, entry.durationMs ?? 0)
          : Math.max(0, entry.executionMs ?? entry.durationMs ?? 0),
  };
}

export function sessionTimelineItemType(type: DisplayEventType): DisplayEventType {
  if (type === "system_message" || type === "unknown") {
    return "root";
  }
  return type;
}

export function buildSessionTimelineVisibleIds(
  entriesByLaneId: Map<string, SessionEventListEntry[]>,
  filteredActiveEntries: SessionEventListEntry[],
  timeline: SessionTimelineLane[],
  activeLane: string,
  selectedTypes: string[],
  query: string,
  view: SessionTraceView,
) {
  const selected = new Set(selectedTypes);
  const needle = query.trim().toLowerCase();
  if (selected.size === 0 && !needle) {
    return undefined;
  }
  const ids = new Set<string>();
  if (view === "debug") {
    entriesByLaneId.forEach((entries) => {
      entries.forEach((entry) => {
        const matchesType = selected.size === 0 || selected.has(sessionEventListFilterValue(entry, view));
        const matchesQuery = !needle || entry.searchText.includes(needle);
        if (matchesType && matchesQuery) {
          ids.add(entry.id);
        }
      });
    });
    return ids;
  }
  filteredActiveEntries.forEach((entry) => ids.add(entry.id));
  timeline.forEach((lane) => {
    if (lane.id === activeLane) {
      return;
    }
    lane.items.forEach((item) => ids.add(item.id));
  });
  return ids;
}

export function sessionDetailEventCopyPayload(entries: SessionEventListEntry[], view: SessionTraceView) {
  const selectableEntries = entries.filter(
    (entry): entry is Exclude<SessionEventListEntry, IdleGapEntry | QueuedBoundaryEntry> => "traceEntry" in entry,
  );
  if (view === "debug") {
    return JSON.stringify(
      selectableEntries.map((entry) => entry.event),
      null,
      2,
    );
  }
  return selectableEntries
    .map((entry) => {
      if (entry.kind === "tool_batch") {
        const summary = entry.calls
          .map((call) => `${call.name}: ${call.inputPreview || call.displayEvent.content}`)
          .join("; ");
        return `[${entry.relativeTime}] Tool batch: ${summary}`;
      }
      const label = entry.traceEntry.label || sessionEventType(entry.event);
      const text =
        entry.traceEntry.displayText ||
        entry.traceEntry.preview ||
        entry.displayEvent.content ||
        sessionEventSummary(entry.event);
      return `[${entry.relativeTime}] ${label}: ${text}`;
    })
    .join("\n");
}

export function resolveSelectedSessionEventEntry(entries: SessionEventListEntry[], selectedId: string | null) {
  if (!selectedId) {
    return null;
  }
  const direct = entries.find((entry) => entry.id === selectedId);
  if (direct) {
    return direct;
  }
  return entries.find((entry) => sessionEventEntrySourceIds(entry).includes(selectedId)) ?? null;
}

export function sessionEventEntrySelectionId(entry: SessionEventListEntry) {
  if (!("traceEntry" in entry)) {
    return null;
  }
  if (entry.kind === "tool_batch") {
    return entry.displayEvent.id || entry.calls[0]?.displayEvent.id || entry.calls[0]?.id || entry.id;
  }
  return entry.displayEvent.id || entry.traceEntry.rawEventId || entry.traceEntry.id || entry.id;
}

export function sessionEventEntryMatchesSelectedId(entry: SessionEventListEntry, selectedId: string | null) {
  return Boolean(selectedId && (entry.id === selectedId || sessionEventEntrySourceIds(entry).includes(selectedId)));
}

export function sessionEventEntryRowId(entry: SessionEventListEntry) {
  return "traceEntry" in entry ? entry.traceEntry.id : entry.id;
}

export function sessionEventEntrySourceIds(entry: SessionEventListEntry): string[] {
  if (!("traceEntry" in entry)) {
    return [entry.id];
  }
  const ids = new Set<string>([entry.id, entry.displayEvent.id, entry.traceEntry.id, entry.traceEntry.rawEventId]);
  addSessionEventSourceId(ids, entry.event);
  addSessionEventSourceId(ids, entry.displayEvent.event);
  if (entry.kind === "tool_call") {
    addSessionEventSourceId(ids, entry.resultEvent);
    addSessionEventSourceId(ids, entry.confirmationEvent);
  } else if (entry.kind === "tool_batch") {
    entry.calls.forEach((call) => {
      ids.add(call.id);
      ids.add(call.traceEntry.id);
      ids.add(call.traceEntry.rawEventId);
      addSessionEventSourceId(ids, call.event);
      addSessionEventSourceId(ids, call.resultEvent);
      addSessionEventSourceId(ids, call.confirmationEvent);
    });
  }
  return Array.from(ids).filter(Boolean);
}

export function addSessionEventSourceId(ids: Set<string>, event?: QuickstartSessionEvent) {
  if (!event) {
    return;
  }
  const id = sessionStableEventId(event);
  if (id) {
    ids.add(id);
  }
  if (typeof event.uuid === "string" && event.uuid) {
    ids.add(event.uuid);
  }
  if (typeof event._wrapped_event_id === "string" && event._wrapped_event_id) {
    ids.add(event._wrapped_event_id);
  }
}

export function sessionAgentDisplayName(agent: unknown) {
  if (typeof agent === "string") {
    return agent;
  }
  const record = toRecord(agent);
  if (!record) {
    return "";
  }
  if (typeof record.name === "string" && record.name) {
    return record.name;
  }
  if (typeof record.id === "string" && record.id) {
    return record.version ? `${record.id} v${record.version}` : record.id;
  }
  return "";
}

export function sessionResourcesLabel(resources: SessionResourceApiResponse[], msg: I18nMsg) {
  if (!resources.length) {
    return "";
  }
  const fileCount = resources.filter((resource) =>
    String(resource.type || resource.resource_type || "").includes("file"),
  ).length;
  if (fileCount > 0 && fileCount === resources.length) {
    return msg("managedAgents.sessions.detail.fileCount", "{count, plural, one {# file} other {# files}}", {
      count: fileCount,
    });
  }
  return msg("managedAgents.sessions.detail.resourceCount", "{count, plural, one {# resource} other {# resources}}", {
    count: resources.length,
  });
}

export function sessionThreadBaseName(thread: SessionThreadApiResponse) {
  const record = thread as unknown as Record<string, unknown>;
  const metadata = toRecord(record.metadata);
  const agent = toRecord(record.agent);
  const candidates = [
    record.name,
    record.title,
    record.role,
    record.label,
    metadata?.name,
    metadata?.role,
    agent?.name,
    agent?.id,
  ];
  const label = candidates.find((value): value is string => typeof value === "string" && value.trim().length > 0);
  return label ? label.trim() : "";
}

export function sessionThreadFallbackLabel(thread: SessionThreadApiResponse, index: number, msg: I18nMsg) {
  return (
    msg("managedAgents.sessions.detail.agentLaneFallback", "Agent {index}", { index }) ||
    compactEntityId(thread.id || `thread-${index}`)
  );
}

export function sessionEventLaneId(event: QuickstartSessionEvent, laneIdByThreadId: Map<string, string>) {
  const threadId = sessionEventThreadId(event);
  if (!threadId) {
    return SESSION_MAIN_LANE_ID;
  }
  return laneIdByThreadId.get(threadId) ?? threadId;
}

export function sessionEventThreadId(event: QuickstartSessionEvent) {
  const data = toRecord(event.data);
  const metadata = toRecord(event.metadata);
  const candidates = [
    event.session_thread_id,
    event.thread_id,
    data?.session_thread_id,
    data?.thread_id,
    metadata?.session_thread_id,
    metadata?.thread_id,
  ];
  return (
    candidates.find((value): value is string => typeof value === "string" && value.trim().length > 0)?.trim() ??
    SESSION_MAIN_LANE_ID
  );
}

export function nearestSessionEventEntry<T extends SessionEventListEntry>(
  entries: T[],
  processedAtMs: number,
): T | null {
  if (!entries.length) {
    return null;
  }
  return entries.reduce((nearest, entry) => {
    const nearestDelta = Math.abs(nearest.processedAtMs - processedAtMs);
    const entryDelta = Math.abs(entry.processedAtMs - processedAtMs);
    return entryDelta < nearestDelta ? entry : nearest;
  }, entries[0]);
}

export function scrollSessionEntryIntoView(scroller: HTMLDivElement | null, entryId: string) {
  if (!scroller) {
    return;
  }
  const target = Array.from(scroller.querySelectorAll<HTMLElement>("[data-event-id]")).find(
    (node) => node.getAttribute("data-event-id") === entryId,
  );
  target?.scrollIntoView({ block: "center" });
}

export function truncateLaneLabel(label: string) {
  const value = label.trim();
  if (value.length <= 16) {
    return value;
  }
  return `${value.slice(0, 11)}...${value.slice(-4)}`;
}

export function readSessionDetailInitialView(): SessionTraceView {
  if (typeof window === "undefined") {
    return "transcript";
  }
  const segment = new URLSearchParams(window.location.search).get("segment");
  return segment === "debug" ? "debug" : "transcript";
}

export function readSessionDetailInitialEventId() {
  if (typeof window === "undefined") {
    return null;
  }
  const eventId = new URLSearchParams(window.location.search).get("event");
  return eventId && eventId.trim() ? eventId.trim() : null;
}

export function readSessionDetailInitialLaneId() {
  if (typeof window === "undefined") {
    return SESSION_MAIN_LANE_ID;
  }
  const laneId = new URLSearchParams(window.location.search).get("lane");
  return laneId && laneId.trim() ? laneId.trim() : SESSION_MAIN_LANE_ID;
}

export function writeSessionDetailUrlState(
  view: SessionTraceView,
  eventId: string | null,
  laneId: string,
  showArchivedLanes: boolean,
) {
  if (typeof window === "undefined") {
    return;
  }
  const url = new URL(window.location.href);
  if (view === "debug") {
    url.searchParams.set("segment", "debug");
  } else {
    url.searchParams.delete("segment");
  }
  if (eventId) {
    url.searchParams.set("event", eventId);
  } else {
    url.searchParams.delete("event");
  }
  if (laneId) {
    url.searchParams.set("lane", laneId);
  } else {
    url.searchParams.delete("lane");
  }
  if (showArchivedLanes) {
    url.searchParams.set("archived_lanes", "true");
  } else {
    url.searchParams.delete("archived_lanes");
  }
  const next = `${url.pathname}${url.search}${url.hash}`;
  const current = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  if (next !== current) {
    window.history.replaceState(window.history.state, "", next);
  }
}

export function readSessionArchivedLanePreference() {
  if (typeof window === "undefined") {
    return false;
  }
  const archivedLanes = new URLSearchParams(window.location.search).get("archived_lanes");
  if (archivedLanes !== null) {
    return archivedLanes === "true" || archivedLanes === "1";
  }
  try {
    return window.localStorage.getItem(SESSION_ARCHIVED_LANES_STORAGE_KEY) === "true";
  } catch {
    return false;
  }
}

export function writeSessionArchivedLanePreference(value: boolean) {
  try {
    window.localStorage.setItem(SESSION_ARCHIVED_LANES_STORAGE_KEY, value ? "true" : "false");
  } catch {
    // The current session state still applies when storage is unavailable.
  }
}

export function sessionStatusIsLive(status: string) {
  const normalized = status.toLowerCase();
  return normalized === "running" || normalized === "queued" || normalized === "rescheduled";
}

export function sessionStatusFromEventType(type: string) {
  switch (type) {
    case "session.status_running":
      return "running";
    case "session.status_idle":
      return "idle";
    case "session.status_rescheduled":
      return "rescheduled";
    case "session.status_terminated":
      return "terminated";
    case "session.deleted":
      return "deleted";
    default:
      return null;
  }
}

export function sessionEventUpdateTimestamp(event: QuickstartSessionEvent, fallback: string) {
  return sessionNullableProcessedAt(event) ?? (typeof event.created_at === "string" ? event.created_at : fallback);
}

export function sessionShouldStreamEvents(session: Pick<SessionApiResponse, "archived_at" | "status"> | null) {
  if (!session || session.archived_at) {
    return false;
  }
  return sessionStatusIsLive(session.status);
}

export function sessionElapsedMs(session: SessionApiResponse, events: QuickstartSessionEvent[]) {
  const start = Date.parse(session.created_at);
  if (!Number.isFinite(start)) {
    return 0;
  }
  const eventEnd = Math.max(...events.map(sessionEventTimestamp).filter(Boolean), 0);
  const end =
    sessionStatusIsLive(session.status) && !session.archived_at
      ? Date.now()
      : eventEnd || Date.parse(session.updated_at);
  return Number.isFinite(end) ? Math.max(0, end - start) : 0;
}

export function emptySessionEventUsage(): SessionEventUsage {
  return {
    input: 0,
    output: 0,
    input_tokens: 0,
    output_tokens: 0,
    cache_read_input_tokens: 0,
    cache_creation_input_tokens: 0,
  };
}

export function addSessionEventUsage(left: SessionEventUsage, right: SessionEventUsage): SessionEventUsage {
  const inputTokens = left.input_tokens + right.input_tokens;
  const outputTokens = left.output_tokens + right.output_tokens;
  const cacheReadInputTokens = left.cache_read_input_tokens + right.cache_read_input_tokens;
  const cacheCreationInputTokens = left.cache_creation_input_tokens + right.cache_creation_input_tokens;
  return {
    input: inputTokens + cacheReadInputTokens + cacheCreationInputTokens,
    output: outputTokens,
    input_tokens: inputTokens,
    output_tokens: outputTokens,
    cache_read_input_tokens: cacheReadInputTokens,
    cache_creation_input_tokens: cacheCreationInputTokens,
  };
}

export function extractSessionEventUsage(event: QuickstartSessionEvent): SessionEventUsage {
  const modelUsage = aggregateSessionModelUsage(event.model_usage ?? event.modelUsage);
  const usage = toRecord(event.usage) ?? toRecord(event.metrics) ?? modelUsage ?? event;
  const modelInput = modelUsage
    ? numericValueFromKeys(modelUsage, ["input_tokens", "inputTokens", "tokens_in", "tokensIn", "input"])
    : 0;
  const modelOutput = modelUsage
    ? numericValueFromKeys(modelUsage, ["output_tokens", "outputTokens", "tokens_out", "tokensOut", "output"])
    : 0;
  const modelCacheRead = modelUsage
    ? numericValueFromKeys(modelUsage, [
        "cache_read_input_tokens",
        "cacheReadInputTokens",
        "cache_read_tokens",
        "cacheReadTokens",
        "cache_read",
        "cacheRead",
      ])
    : 0;
  const modelCacheCreation = modelUsage
    ? numericValueFromKeys(modelUsage, [
        "cache_creation_input_tokens",
        "cacheCreationInputTokens",
        "cache_creation_tokens",
        "cacheCreationTokens",
        "cache_creation",
        "cacheCreation",
      ])
    : 0;
  const inputTokens =
    numericValueFromKeys(usage, ["input_tokens", "inputTokens", "tokens_in", "tokensIn", "input"]) || modelInput || 0;
  const outputTokens =
    numericValueFromKeys(usage, ["output_tokens", "outputTokens", "tokens_out", "tokensOut", "output"]) ||
    modelOutput ||
    0;
  const cacheReadInputTokens =
    numericValueFromKeys(usage, [
      "cache_read_input_tokens",
      "cacheReadInputTokens",
      "cache_read_tokens",
      "cacheReadTokens",
      "cache_read",
      "cacheRead",
    ]) ||
    modelCacheRead ||
    0;
  const cacheCreationInputTokens =
    numericValueFromKeys(usage, [
      "cache_creation_input_tokens",
      "cacheCreationInputTokens",
      "cache_creation_tokens",
      "cacheCreationTokens",
      "cache_creation",
      "cacheCreation",
    ]) ||
    modelCacheCreation ||
    0;
  return {
    input: inputTokens + cacheReadInputTokens + cacheCreationInputTokens,
    output: outputTokens,
    input_tokens: inputTokens,
    output_tokens: outputTokens,
    cache_read_input_tokens: cacheReadInputTokens,
    cache_creation_input_tokens: cacheCreationInputTokens,
  };
}

export function aggregateSessionModelUsage(value: unknown) {
  const record = toRecord(value);
  if (!record) {
    return null;
  }
  const directInput = numericValueFromKeys(record, ["input_tokens", "inputTokens", "tokens_in", "tokensIn", "input"]);
  const directOutput = numericValueFromKeys(record, [
    "output_tokens",
    "outputTokens",
    "tokens_out",
    "tokensOut",
    "output",
  ]);
  const directCacheRead = numericValueFromKeys(record, [
    "cache_read_input_tokens",
    "cacheReadInputTokens",
    "cache_read_tokens",
    "cacheReadTokens",
    "cache_read",
    "cacheRead",
  ]);
  const directCacheCreation = numericValueFromKeys(record, [
    "cache_creation_input_tokens",
    "cacheCreationInputTokens",
    "cache_creation_tokens",
    "cacheCreationTokens",
    "cache_creation",
    "cacheCreation",
  ]);
  if (directInput || directOutput || directCacheRead || directCacheCreation) {
    return record;
  }
  let input = 0;
  let output = 0;
  let cacheRead = 0;
  let cacheCreation = 0;
  Object.values(record).forEach((modelValue) => {
    const usage = toRecord(modelValue);
    if (!usage) {
      return;
    }
    input += numericValueFromKeys(usage, ["input_tokens", "inputTokens", "tokens_in", "tokensIn", "input"]);
    output += numericValueFromKeys(usage, ["output_tokens", "outputTokens", "tokens_out", "tokensOut", "output"]);
    cacheRead += numericValueFromKeys(usage, [
      "cache_read_input_tokens",
      "cacheReadInputTokens",
      "cache_read_tokens",
      "cacheReadTokens",
      "cache_read",
      "cacheRead",
    ]);
    cacheCreation += numericValueFromKeys(usage, [
      "cache_creation_input_tokens",
      "cacheCreationInputTokens",
      "cache_creation_tokens",
      "cacheCreationTokens",
      "cache_creation",
      "cacheCreation",
    ]);
  });
  return input || output || cacheRead || cacheCreation
    ? {
        input_tokens: input,
        output_tokens: output,
        cache_read_input_tokens: cacheRead,
        cache_creation_input_tokens: cacheCreation,
      }
    : null;
}

export function numericValueFromKeys(record: Record<string, unknown>, keys: string[]) {
  for (const key of keys) {
    const value = record[key];
    if (typeof value === "number" && Number.isFinite(value)) {
      return value;
    }
    if (typeof value === "string") {
      const parsed = Number(value.trim());
      if (Number.isFinite(parsed)) {
        return parsed;
      }
    }
  }
  return 0;
}

export function stringValueFromKeys(record: Record<string, unknown>, keys: string[]) {
  for (const key of keys) {
    const value = record[key];
    if (typeof value === "string" && value.trim()) {
      return value.trim();
    }
  }
  return "";
}

export function sessionEventDurationMs(event: QuickstartSessionEvent) {
  return numericValueFromKeys(event, [
    "duration_ms",
    "elapsed_ms",
    "latency_ms",
    "run_time_ms",
    "processing_duration_ms",
  ]);
}

export function formatCompactTokenCount(value: number, formatters: ReturnType<typeof useFormatters>) {
  return value >= 1000 ? `${(value / 1000).toFixed(1)}k` : formatters.number(value);
}

export function sessionTokenUsageTitle(
  usage: SessionEventUsage,
  formatters: ReturnType<typeof useFormatters>,
  msg: I18nMsg,
) {
  const rows = [
    msg("managedAgents.sessions.trace.tokensInOut", "Tokens in / out"),
    `${msg("managedAgents.sessions.trace.cacheRead", "Cache read")}: ${formatCompactTokenCount(usage.cache_read_input_tokens, formatters)}`,
    `${msg("managedAgents.sessions.trace.cacheCreation", "Cache creation")}: ${formatCompactTokenCount(usage.cache_creation_input_tokens, formatters)}`,
    `${msg("managedAgents.sessions.trace.uncached", "Uncached")}: ${formatCompactTokenCount(usage.input_tokens, formatters)}`,
    `${msg("managedAgents.sessions.trace.output", "Output")}: ${formatCompactTokenCount(usage.output_tokens, formatters)}`,
  ];
  return rows.join("\n");
}

export function formatSessionDuration(ms: number, formatters: ReturnType<typeof useFormatters>, msg: I18nMsg) {
  const safeMs = Math.max(0, ms);
  if (safeMs < 1000) {
    return msg("managedAgents.sessions.detail.durationMilliseconds", "{milliseconds}ms", {
      milliseconds: formatters.number(Math.round(safeMs)),
    });
  }
  const secondsFloat = safeMs / 1000;
  if (secondsFloat < 60) {
    return msg("managedAgents.sessions.detail.durationSeconds", "{seconds}s", {
      seconds: formatters.number(Number(secondsFloat.toFixed(1)), {
        minimumFractionDigits: 1,
        maximumFractionDigits: 1,
      }),
    });
  }
  const minutesFloat = secondsFloat / 60;
  if (minutesFloat < 60) {
    const minutes = Math.floor(minutesFloat);
    const seconds = Math.floor(secondsFloat % 60);
    return seconds === 0
      ? msg("managedAgents.sessions.detail.durationMinutes", "{minutes}m", { minutes: formatters.number(minutes) })
      : msg("managedAgents.sessions.detail.durationMinutesSeconds", "{minutes}m {seconds}s", {
          minutes: formatters.number(minutes),
          seconds: formatters.number(seconds),
        });
  }
  const hoursFloat = minutesFloat / 60;
  if (hoursFloat < 24) {
    const hours = Math.floor(hoursFloat);
    const minutes = Math.floor(minutesFloat % 60);
    return minutes === 0
      ? msg("managedAgents.sessions.detail.durationHours", "{hours}h", { hours: formatters.number(hours) })
      : msg("managedAgents.sessions.detail.durationHoursMinutes", "{hours}h {minutes}m", {
          hours: formatters.number(hours),
          minutes: formatters.number(minutes),
        });
  }
  const days = Math.floor(hoursFloat / 24);
  const hours = Math.floor(hoursFloat % 24);
  return hours === 0
    ? msg("managedAgents.sessions.detail.durationDays", "{days}d", { days: formatters.number(days) })
    : msg("managedAgents.sessions.detail.durationDaysHours", "{days}d {hours}h", {
        days: formatters.number(days),
        hours: formatters.number(hours),
      });
}

export function formatRelativeFromNow(value: string, formatters: ReturnType<typeof useFormatters>, msg: I18nMsg) {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) {
    return msg("managedAgents.sessions.detail.unknownTime", "Unknown time");
  }
  const seconds = Math.round((timestamp - Date.now()) / 1000);
  const absSeconds = Math.abs(seconds);
  if (absSeconds < 45) {
    return msg("managedAgents.sessions.detail.justNow", "just now");
  }
  if (absSeconds < 3600) {
    return formatters.relativeTime(Math.round(seconds / 60), "minute");
  }
  if (absSeconds < 86400) {
    return formatters.relativeTime(Math.round(seconds / 3600), "hour");
  }
  return formatters.relativeTime(Math.round(seconds / 86400), "day");
}

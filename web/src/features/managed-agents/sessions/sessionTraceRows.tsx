import { useFormatters, useI18n } from "../../../shared/i18n";
import { Button } from "../../../shared/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "../../../shared/ui/tooltip";
import {
  type DisplayEvent,
  type DisplayEventEntry,
  type I18nMsg,
  type IdleGapEntry,
  type QueuedBoundaryEntry,
  type QuickstartSessionEvent,
  type SessionEventListEntry,
  type ToolBatchEntry,
  type ToolCallEntry,
} from "../types";
import { compactEntityId, toRecord } from "../utils";
import clsx from "clsx";
import { ArrowLeft, ArrowRight } from "lucide-react";
import { type MouseEvent as ReactMouseEvent, type ReactNode, useContext } from "react";
import { SessionDetailDeltaFramesContext } from "./sessionDetailData";
import { formatSessionDuration, numericValueFromKeys } from "./sessionDetailModel";
import { HeaderRow, InProgressChip, MetaStrip, OutcomeStatusChip, SynchronizedShimmerText } from "./sessionTimeline";
import {
  sessionEventFamily,
  sessionEventIsThinking,
  sessionEventLabel,
  sessionEventStructuredContentText,
  sessionEventSummary,
  sessionEventTranscriptText,
  sessionEventType,
  sessionResultText,
  sessionSubagentName,
  sessionSubagentThreadId,
  sessionThinkingPreview,
  sessionThinkingText,
  sessionToolResultText,
} from "./sessionTraceModel";
import { EventTypeBadge } from "./SessionTracePanel";

export function IdleGapRow({ entry }: { entry: IdleGapEntry }) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const duration = formatSessionDuration(entry.durationMs, formatters, msg);
  return (
    <div
      role="separator"
      aria-label={msg("managedAgents.sessions.trace.sessionIdleGap", "Session idle for {duration}", { duration })}
      data-entry-kind="idle_gap"
      className="oma-session-idle-gap relative my-2 flex h-6 items-center justify-center overflow-hidden rounded-md border text-xs"
    >
      <span className="oma-session-idle-gap-stripes absolute inset-0" aria-hidden />
      <span className="relative">
        {msg("managedAgents.sessions.trace.sessionIdleDot", "Session idle · {duration}", { duration })}
      </span>
    </div>
  );
}

export function QueuedBoundaryRow({ entry }: { entry: QueuedBoundaryEntry }) {
  const { msg } = useI18n();
  const label = msg(
    "managedAgents.sessions.trace.queuedMessages",
    "{count, plural, one {# queued message} other {# queued messages}}",
    { count: entry.count },
  );
  return (
    <div
      role="separator"
      aria-label={label}
      data-entry-kind="queued_boundary"
      className="relative my-2 flex h-6 items-center gap-3 text-xs text-muted-foreground"
    >
      <span className="h-px flex-1 bg-border/30" aria-hidden />
      <span>{label}</span>
      <span className="h-px flex-1 bg-border/30" aria-hidden />
    </div>
  );
}

export function DisplayEventRow({
  entry,
  selected,
  onSelect,
  threadNameById,
  onThreadClick,
}: {
  entry: DisplayEventEntry;
  selected: boolean;
  onSelect: () => void;
  threadNameById: Map<string, string>;
  onThreadClick: (threadId: string, processedAtMs: number, eventType: string) => void;
}) {
  const { msg } = useI18n();
  const title = sessionDisplayEventInlinePreview(entry, msg);
  const textInProgress = Boolean(entry.inProgress || entry.displayEvent.isQueued || entry.displayEvent.isStreaming);
  const showGenerating = Boolean(entry.inProgress || entry.displayEvent.isStreaming);
  return (
    <div
      data-event-id={entry.traceEntry.id}
      data-entry-kind={entry.kind}
      data-display-kind={entry.traceEntry.displayKind}
      className="w-full"
    >
      <HeaderRow isSelected={selected} onSelect={onSelect}>
        <span className="flex w-14 shrink-0 items-center">
          <EventTypeBadge
            type={entry.displayEvent.type}
            label={sessionDisplayEventBadge(entry, msg)}
            variant="compact"
          />
        </span>
        {entry.displayEvent.type === "subagent" ? (
          <SubagentLabel entry={entry} msg={msg} threadNameById={threadNameById} onThreadClick={onThreadClick} />
        ) : (
          <TraceRowText inProgress={textInProgress}>
            {entry.displayEvent.isStreaming ? <LiveRowPreview displayEvent={entry.displayEvent} msg={msg} /> : title}
          </TraceRowText>
        )}
        {showGenerating ? (
          <InProgressChip label={msg("managedAgents.sessions.trace.generating", "Generating")} />
        ) : null}
        <MetaStrip
          usage={entry.kind === "passthrough" || entry.kind === "message" ? entry.usage : undefined}
          inferenceMs={entry.kind === "passthrough" || entry.kind === "message" ? entry.inferenceMs : undefined}
          isError={entry.displayEvent.isError && entry.displayEvent.type !== "error"}
          relativeTime={entry.relativeTime}
          processedAtMs={entry.processedAtMs}
        />
      </HeaderRow>
    </div>
  );
}

export function ToolCallRow({
  entry,
  selected,
  onSelect,
}: {
  entry: ToolCallEntry;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <div
      data-event-id={entry.traceEntry.id}
      data-entry-kind={entry.kind}
      data-display-kind={entry.traceEntry.displayKind}
      className="w-full"
    >
      <HeaderRow isSelected={selected} onSelect={onSelect}>
        <span className="flex w-14 shrink-0 items-center">
          <EventTypeBadge type="tool_use" variant="compact" />
        </span>
        <TraceRowText inProgress={entry.lifecycle === "running"} suffix={entry.inputPreview || undefined}>
          {entry.name}
        </TraceRowText>
        <MetaStrip
          usage={entry.usage}
          inferenceMs={entry.inferenceMs}
          executionMs={entry.executionMs}
          lifecycle={entry.lifecycle}
          isError={entry.isError}
          relativeTime={entry.relativeTime}
          processedAtMs={entry.processedAtMs}
        />
      </HeaderRow>
    </div>
  );
}

export function ToolBatchRow({
  entry,
  selected,
  onSelect,
}: {
  entry: ToolBatchEntry;
  selected: boolean;
  onSelect: () => void;
}) {
  const summary = sessionToolBatchSummary(entry);
  return (
    <div
      data-event-id={entry.traceEntry.id}
      data-entry-kind={entry.kind}
      data-display-kind={entry.traceEntry.displayKind}
      className="w-full"
    >
      <HeaderRow isSelected={selected} onSelect={onSelect}>
        <span className="flex w-14 shrink-0 items-center">
          <EventTypeBadge type="tool_use" variant="compact" />
        </span>
        <TraceRowText inProgress={entry.lifecycle === "running"}>{sessionInlineRowPreview(summary)}</TraceRowText>
        <MetaStrip
          usage={entry.usage}
          inferenceMs={entry.inferenceMs}
          executionMs={entry.executionMs}
          lifecycle={entry.lifecycle}
          isError={entry.isError}
          relativeTime={entry.relativeTime}
          processedAtMs={entry.processedAtMs}
        />
      </HeaderRow>
    </div>
  );
}

export function OutcomeRow({
  entry,
  selected,
  onSelect,
}: {
  entry: DisplayEventEntry;
  selected: boolean;
  onSelect: () => void;
}) {
  const { msg } = useI18n();
  const iteration = entry.outcomeIteration ?? sessionOutcomeIteration(entry.event);
  const status = entry.outcomeStatus ?? sessionOutcomeStatus(entry.event);
  return (
    <div
      data-event-id={entry.traceEntry.id}
      data-entry-kind={entry.kind}
      data-display-kind={entry.traceEntry.displayKind}
      className="w-full"
    >
      <HeaderRow isSelected={selected} onSelect={onSelect}>
        <span className="flex w-14 shrink-0 items-center">
          <EventTypeBadge type="outcome" variant="compact" />
        </span>
        <TraceRowText inProgress={!status}>
          {msg("managedAgents.sessions.trace.gradingIteration", "Grading iteration {iteration}", { iteration })}
        </TraceRowText>
        {status ? (
          <OutcomeStatusChip status={status} />
        ) : (
          <InProgressChip label={msg("managedAgents.sessions.trace.evaluating", "Evaluating")} />
        )}
        <MetaStrip
          usage={entry.usage}
          executionMs={entry.durationMs}
          isError={entry.isError}
          relativeTime={entry.relativeTime}
          processedAtMs={entry.processedAtMs}
        />
      </HeaderRow>
    </div>
  );
}

export function SubagentLabel({
  entry,
  msg,
  threadNameById,
  onThreadClick,
}: {
  entry: DisplayEventEntry;
  msg: I18nMsg;
  threadNameById: Map<string, string>;
  onThreadClick: (threadId: string, processedAtMs: number, eventType: string) => void;
}) {
  const eventType = sessionEventType(entry.event);
  const sent = eventType === "agent.thread_message_sent";
  const received = eventType === "agent.thread_message_received";
  const direction = sessionSubagentDirection(entry.event);
  const threadRef = sessionSubagentThreadRef(entry.event);
  const threadId = threadRef.threadId;
  const label =
    sent || received
      ? threadNameById.get(threadId) ||
        threadRef.agentName ||
        (threadId ? compactSubagentThreadId(threadId) : msg("managedAgents.sessions.trace.thread", "Thread"))
      : sessionSubagentRowLabel(entry.event, msg);
  const Icon = direction === "received" ? ArrowLeft : ArrowRight;
  const clickable = Boolean(threadId);
  const handleClick = (event: ReactMouseEvent<HTMLButtonElement>) => {
    if (!threadId) {
      return;
    }
    event.stopPropagation();
    onThreadClick(threadId, entry.processedAtMs, sent ? "agent.thread_message_received" : "agent.thread_message_sent");
  };
  return (
    <span className="flex min-w-0 flex-1 items-center gap-1.5 truncate text-sm text-foreground">
      <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      {clickable ? (
        <Button
          type="button"
          variant="link"
          size="xs"
          className="h-auto min-w-0 justify-start px-0 py-0 text-sm font-normal text-foreground hover:bg-transparent hover:text-foreground"
          onClick={handleClick}
        >
          <span className="min-w-0 truncate">{label}</span>
        </Button>
      ) : (
        <span className="min-w-0 truncate">{label}</span>
      )}
    </span>
  );
}

export function TraceRowText({
  children,
  suffix,
  inProgress = false,
}: {
  children: ReactNode;
  suffix?: string;
  inProgress?: boolean;
}) {
  return (
    <span className={clsx("min-w-0 flex-1 truncate text-sm", !inProgress && "text-foreground")}>
      {inProgress ? <SynchronizedShimmerText>{children}</SynchronizedShimmerText> : children}
      {suffix ? (
        inProgress ? (
          <SynchronizedShimmerText className="ml-2" variant="secondary">
            {suffix}
          </SynchronizedShimmerText>
        ) : (
          <span className="ml-2 text-muted-foreground">{suffix}</span>
        )
      ) : null}
    </span>
  );
}

export function sessionToolBatchSummary(entry: ToolBatchEntry) {
  return entry.toolCounts.map((tool) => (tool.count > 1 ? `${tool.name} ×${tool.count}` : tool.name)).join(", ");
}

export function sessionOutcomeIteration(event: QuickstartSessionEvent) {
  const data = toRecord(event.data);
  const metadata = toRecord(event.metadata);
  return (
    numericValueFromKeys(event, ["iteration", "iteration_index", "index"]) ||
    (data ? numericValueFromKeys(data, ["iteration", "iteration_index", "index"]) : 0) ||
    (metadata ? numericValueFromKeys(metadata, ["iteration", "iteration_index", "index"]) : 0) ||
    1
  );
}

export function sessionOutcomeStatus(event: QuickstartSessionEvent) {
  const data = toRecord(event.data);
  const metadata = toRecord(event.metadata);
  const candidates = [
    event.status,
    event.result,
    event.outcome_status,
    data?.status,
    data?.result,
    data?.outcome_status,
    metadata?.status,
    metadata?.result,
    metadata?.outcome_status,
  ];
  const status = candidates.find((value): value is string => typeof value === "string" && value.trim().length > 0);
  return status?.trim();
}

export function outcomeStatusLabel(status: string, msg: I18nMsg) {
  switch (status.toLowerCase()) {
    case "satisfied":
      return msg("managedAgents.sessions.trace.outcomeSatisfied", "Satisfied");
    case "needs_revision":
    case "needs-revision":
      return msg("managedAgents.sessions.trace.outcomeNeedsRevision", "Needs revision");
    case "max_iterations_reached":
    case "max-iterations-reached":
      return msg("managedAgents.sessions.trace.outcomeMaxIterationsReached", "Max iterations reached");
    case "failed":
      return msg("managedAgents.sessions.trace.outcomeFailed", "Failed");
    case "interrupted":
      return msg("managedAgents.sessions.trace.outcomeInterrupted", "Interrupted");
    default:
      return status;
  }
}

export function outcomeStatusChipClass(status: string) {
  switch (status.toLowerCase()) {
    case "satisfied":
      return "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400";
    case "needs_revision":
    case "needs-revision":
      return "bg-amber-500/10 text-amber-600 dark:text-amber-400";
    case "max_iterations_reached":
    case "max-iterations-reached":
      return "bg-secondary text-secondary-foreground";
    case "failed":
      return "bg-destructive/10 text-destructive";
    default:
      return "bg-secondary text-secondary-foreground";
  }
}

export function sessionSubagentDirection(event: QuickstartSessionEvent): "sent" | "received" {
  return sessionEventType(event) === "agent.thread_message_received" ? "received" : "sent";
}

export function sessionSubagentRowLabel(event: QuickstartSessionEvent, msg: I18nMsg) {
  const direction = sessionSubagentDirection(event);
  const data = toRecord(event.data);
  const metadata = toRecord(event.metadata);
  const sessionThread = toRecord(event.session_thread);
  const subagent = toRecord(event.subagent);
  const agent = toRecord(event.agent);
  const nameCandidates =
    direction === "received"
      ? [
          event.from_agent_name,
          data?.from_agent_name,
          metadata?.from_agent_name,
          event.agent_name,
          data?.agent_name,
          metadata?.agent_name,
          subagent?.name,
          agent?.name,
          sessionThread?.name,
          sessionThread?.role,
        ]
      : [
          event.to_agent_name,
          data?.to_agent_name,
          metadata?.to_agent_name,
          event.agent_name,
          data?.agent_name,
          metadata?.agent_name,
          subagent?.name,
          agent?.name,
          sessionThread?.name,
          sessionThread?.role,
        ];
  const name = nameCandidates
    .find((value): value is string => typeof value === "string" && value.trim().length > 0)
    ?.trim();
  if (name) {
    return name;
  }
  const threadId = sessionSubagentThreadId(event);
  if (threadId) {
    return compactEntityId(threadId);
  }
  return msg("managedAgents.sessions.trace.thread", "Thread");
}

export function sessionSubagentThreadRef(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  const sent = type === "agent.thread_message_sent";
  const data = toRecord(event.data);
  const metadata = toRecord(event.metadata);
  const threadCandidates = sent
    ? [event.to_session_thread_id, data?.to_session_thread_id, metadata?.to_session_thread_id]
    : [event.from_session_thread_id, data?.from_session_thread_id, metadata?.from_session_thread_id];
  const agentCandidates = sent
    ? [event.to_agent_name, data?.to_agent_name, metadata?.to_agent_name]
    : [event.from_agent_name, data?.from_agent_name, metadata?.from_agent_name];
  const threadId =
    threadCandidates.find((value): value is string => typeof value === "string" && value.trim().length > 0)?.trim() ||
    sessionSubagentThreadId(event);
  const agentName =
    agentCandidates.find((value): value is string => typeof value === "string" && value.trim().length > 0)?.trim() ||
    sessionSubagentName(event);
  return { threadId, agentName };
}

export function compactSubagentThreadId(threadId: string) {
  const value = threadId.trim();
  if (value.length <= 14) {
    return value;
  }
  return `${value.slice(0, 8)}...${value.slice(-4)}`;
}

export function TranscriptRow({
  entry,
  selected,
  onSelect,
  threadNameById,
  onThreadClick,
}: {
  entry: SessionEventListEntry;
  selected: boolean;
  onSelect: () => void;
  threadNameById: Map<string, string>;
  onThreadClick: (threadId: string, processedAtMs: number, eventType: string) => void;
}) {
  switch (entry.kind) {
    case "idle_gap":
      return <IdleGapRow entry={entry} />;
    case "queued_boundary":
      return <QueuedBoundaryRow entry={entry} />;
    case "outcome":
      return <OutcomeRow entry={entry} selected={selected} onSelect={onSelect} />;
    case "tool_call":
      return <ToolCallRow entry={entry} selected={selected} onSelect={onSelect} />;
    case "tool_batch":
      return <ToolBatchRow entry={entry} selected={selected} onSelect={onSelect} />;
    case "message":
    case "status":
    case "passthrough":
      return (
        <DisplayEventRow
          entry={entry}
          selected={selected}
          onSelect={onSelect}
          threadNameById={threadNameById}
          onThreadClick={onThreadClick}
        />
      );
    case "debug":
      return null;
  }
}

export function DebugRow({
  entry,
  selected,
  onSelect,
  onOpenDeltas,
}: {
  entry: DisplayEventEntry;
  selected: boolean;
  onSelect: () => void;
  onOpenDeltas: () => void;
}) {
  const { msg } = useI18n();
  const title = sessionDisplayEventInlinePreview(entry, msg);
  const hasDeltas = entry.type === "agent.message" || entry.type === "agent.thinking";
  const deltasLabel = msg("managedAgents.sessions.trace.openDeltas", "Open deltas");
  return (
    <div
      data-event-id={entry.traceEntry.id}
      data-entry-kind={entry.kind}
      data-display-kind={entry.traceEntry.displayKind}
      className="w-full"
    >
      <HeaderRow isSelected={selected} onSelect={onSelect}>
        <span className="flex min-w-32 shrink-0 items-center">
          <EventTypeBadge
            type={entry.displayEvent.type}
            label={sessionDebugBadge(entry.type)}
            title={entry.type}
            variant="compact"
            className="font-mono"
          />
        </span>
        <span
          className={clsx("min-w-0 flex-1 truncate text-sm", entry.isError ? "text-destructive" : "text-foreground")}
        >
          {title}
        </span>
        {hasDeltas ? (
          <Tooltip>
            <TooltipTrigger
              render={
                <span className="inline-flex shrink-0">
                  <Button
                    type="button"
                    variant="ghost"
                    size="xs"
                    className="h-auto shrink-0 px-1.5 py-0.5 text-[10px] font-normal text-muted-foreground hover:bg-accent"
                    onClick={(event) => {
                      event.stopPropagation();
                      onOpenDeltas();
                    }}
                  >
                    {msg("managedAgents.sessions.trace.deltas", "Deltas")}
                  </Button>
                </span>
              }
            />
            <TooltipContent>{deltasLabel}</TooltipContent>
          </Tooltip>
        ) : null}
        <MetaStrip
          isError={entry.displayEvent.isError && entry.displayEvent.type !== "error"}
          relativeTime={entry.relativeTime}
          processedAtMs={entry.processedAtMs}
        />
      </HeaderRow>
    </div>
  );
}

export function LiveRowPreview({ displayEvent, msg }: { displayEvent: DisplayEvent; msg: I18nMsg }) {
  const deltaFrames = useContext(SessionDetailDeltaFramesContext);
  const liveEvent = deltaFrames[displayEvent.id]?.message ?? displayEvent.event;
  const family = sessionEventFamily(liveEvent);
  const label = sessionEventLabel(liveEvent, family, msg);
  const value = sessionEventIsThinking(liveEvent)
    ? sessionThinkingText(liveEvent)
    : sessionEventTranscriptText(liveEvent) ||
      sessionEventStructuredContentText(liveEvent) ||
      sessionToolResultText(liveEvent) ||
      sessionResultText(liveEvent) ||
      displayEvent.content ||
      displayEvent.label ||
      label;
  return <>{sessionInlineRowPreview(value)}</>;
}

export function sessionDisplayEventInlinePreview(entry: DisplayEventEntry, msg: I18nMsg) {
  if (entry.displayEvent.type === "thinking") {
    return sessionThinkingPreview(msg);
  }
  const preview =
    entry.traceEntry.preview ||
    entry.displayEvent.content ||
    entry.displayEvent.label ||
    sessionEventSummary(entry.event);
  return sessionInlineRowPreview(preview);
}

export function sessionInlineRowPreview(value: string, maxLength = 80) {
  const compact = value.replace(/\s+/g, " ").trim();
  return compact.length > maxLength ? `${compact.slice(0, maxLength)}…` : compact;
}

export function sessionDisplayEventBadge(_entry: DisplayEventEntry, _msg: I18nMsg) {
  return undefined;
}

export function sessionDebugBadge(type: string) {
  return sessionDebugBadgeLabels[type] ?? type;
}

export const sessionDebugBadgeLabels: Record<string, string> = {
  "agent.thread_message_received": "agent.thread…received",
  "agent.thread_message_sent": "agent.thread…sent",
  "agent.thread_context_compacted": "agent.thread…compacted",
  "agent.custom_tool_use": "agent.custom…use",
  "agent.mcp_tool_result": "agent.mcp…result",
  "user.custom_tool_result": "user.custom…result",
  "user.tool_confirmation": "user.…confirmation",
  "session.status_idle": "session.…idle",
  "session.status_running": "session.…running",
  "session.status_rescheduled": "session.…rescheduled",
  "session.status_terminated": "session.…terminated",
  "session.thread_status_idle": "session.thread…idle",
  "session.thread_status_running": "session.thread…running",
  "session.thread_status_rescheduled": "session.thread…rescheduled",
  "session.thread_status_terminated": "session.thread…terminated",
  "span.model_request_start": "span.model…start",
  "span.model_request_end": "span.model…end",
  "span.outcome_evaluation_start": "span.outcome…start",
  "span.outcome_evaluation_ongoing": "span.outcome…ongoing",
  "span.outcome_evaluation_end": "span.outcome…end",
};

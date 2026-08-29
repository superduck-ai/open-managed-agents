import { useFormatters, useI18n } from '../../../shared/i18n';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import { Bubble, BubbleContent } from '../../../shared/ui/bubble';
import { MessageHeader } from '../../../shared/ui/message';
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
  type ToolLifecycle,
} from '../types';
import { compactEntityId, numericValueFromKeys, toRecord } from '../utils';
import clsx from 'clsx';
import { ArrowLeft, ArrowRight, Ban, Check, CircleX, Clock3, Loader2, Wrench } from 'lucide-react';
import { type MouseEvent as ReactMouseEvent, type ReactNode, useContext } from 'react';
import { SessionDetailDeltaFramesContext } from './sessionDetailData';
import { formatSessionDuration } from './sessionDetailModel';
import { HeaderRow, InProgressChip, MetaStrip, OutcomeStatusChip, SynchronizedShimmerText } from './sessionTimeline';
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
} from './sessionTraceModel';
import { EventTypeBadge } from './SessionTracePanel';
import { TranscriptContent } from './SessionTranscriptContent';
import { Tooltip, TooltipContent, TooltipTrigger } from '../../../shared/ui/tooltip';

export function IdleGapRow({ entry }: { entry: IdleGapEntry }) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const duration = formatSessionDuration(entry.durationMs, formatters, msg);
  const resumedAt = entry.processedAtMs;
  const resumedTime = formatters.time(resumedAt, { hour12: false });
  const crossesDate = new Date(entry.createdAtMs).toDateString() !== new Date(resumedAt).toDateString();
  const resumedWhen = crossesDate
    ? `${formatters.date(resumedAt, { month: 'short', day: 'numeric' })} · ${resumedTime}`
    : resumedTime;
  return (
    <div
      role="separator"
      aria-label={msg('managedAgents.sessions.trace.sessionIdleGap', 'Session idle for {duration}', { duration })}
      data-entry-kind="idle_gap"
      className="my-2 flex min-h-6 items-center gap-3 py-1 text-xs text-muted-foreground"
    >
      <span className="h-0 flex-1 border-t-[0.5px] border-border/60" aria-hidden />
      <Tooltip>
        <TooltipTrigger
          render={
            <time dateTime={new Date(resumedAt).toISOString()} className="flex-none">
              <span className="text-session-secondary-foreground">{resumedWhen}</span> ({duration})
            </time>
          }
        />
        <TooltipContent>
          {msg('managedAgents.sessions.trace.idleRange', 'Idle since {from}; resumed {to}', {
            from: formatters.date(entry.createdAtMs, { dateStyle: 'medium', timeStyle: 'medium' }),
            to: formatters.date(resumedAt, { dateStyle: 'medium', timeStyle: 'medium' }),
          })}
        </TooltipContent>
      </Tooltip>
      <span className="h-0 flex-1 border-t-[0.5px] border-border/60" aria-hidden />
    </div>
  );
}

export function QueuedBoundaryRow({ entry }: { entry: QueuedBoundaryEntry }) {
  const { msg } = useI18n();
  const label = msg(
    'managedAgents.sessions.trace.queuedMessages',
    '{count, plural, one {# queued message} other {# queued messages}}',
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
  presentation = 'standalone',
  threadNameById,
  onThreadClick,
}: {
  entry: DisplayEventEntry;
  selected: boolean;
  onSelect: () => void;
  threadNameById: Map<string, string>;
  onThreadClick: (threadId: string, processedAtMs: number, eventType: string) => void;
  presentation?: 'standalone' | 'iteration';
}) {
  const { msg } = useI18n();
  if (entry.displayEvent.type === 'user' || entry.displayEvent.type === 'agent') {
    return <TranscriptMessageRow entry={entry} selected={selected} onSelect={onSelect} presentation={presentation} />;
  }
  if (entry.displayEvent.type === 'thinking') {
    return <TranscriptThinkingRow entry={entry} selected={selected} onSelect={onSelect} presentation={presentation} />;
  }
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
          <EventTypeBadge type={entry.displayEvent.type} variant="compact" />
        </span>
        {entry.displayEvent.type === 'subagent' ? (
          <SubagentLabel entry={entry} msg={msg} threadNameById={threadNameById} onThreadClick={onThreadClick} />
        ) : (
          <TraceRowText inProgress={textInProgress}>
            {entry.displayEvent.isStreaming ? <LiveRowPreview displayEvent={entry.displayEvent} msg={msg} /> : title}
          </TraceRowText>
        )}
        {showGenerating ? (
          <InProgressChip label={msg('managedAgents.sessions.trace.generating', 'Generating')} />
        ) : null}
        <MetaStrip
          usage={entry.kind === 'passthrough' || entry.kind === 'message' ? entry.usage : undefined}
          inferenceMs={entry.kind === 'passthrough' || entry.kind === 'message' ? entry.inferenceMs : undefined}
          isError={entry.displayEvent.isError && entry.displayEvent.type !== 'error'}
          relativeTime={entry.relativeTime}
          processedAtMs={entry.processedAtMs}
        />
      </HeaderRow>
    </div>
  );
}

function TranscriptMessageRow({
  entry,
  selected,
  onSelect,
  presentation,
}: {
  entry: DisplayEventEntry;
  selected: boolean;
  onSelect: () => void;
  presentation: 'standalone' | 'iteration';
}) {
  const { msg } = useI18n();
  const inProgress = Boolean(entry.inProgress || entry.displayEvent.isQueued || entry.displayEvent.isStreaming);
  const content = entry.displayEvent.content || sessionDisplayEventInlinePreview(entry, msg);
  const speaker = entry.displayEvent.type === 'agent' ? 'agent' : 'user';
  const handleClick = (event: ReactMouseEvent<HTMLElement>) => {
    if (!transcriptEventTargetIsInteractive(event.target)) {
      onSelect();
    }
  };
  return (
    <article
      data-event-id={entry.traceEntry.id}
      data-entry-kind={entry.kind}
      data-display-kind={entry.traceEntry.displayKind}
      className="group/event relative flex w-full min-w-0 flex-col"
      onClick={handleClick}
    >
      {presentation === 'standalone' ? (
        <TranscriptSpeakerHeader
          label={entry.displayEvent.label}
          speaker={speaker}
          processedAtMs={entry.processedAtMs}
          relativeTime={entry.relativeTime}
          selected={selected}
          onSelect={onSelect}
        />
      ) : (
        <Button
          type="button"
          variant="ghost"
          size="xs"
          aria-label={msg('managedAgents.sessions.trace.selectEvent', 'Select {label} event', {
            label: entry.displayEvent.label,
          })}
          className="sr-only absolute right-1 top-1 z-10 focus:not-sr-only"
          onClick={onSelect}
        >
          {msg('managedAgents.sessions.trace.selectEventShort', 'Select')}
        </Button>
      )}
      {presentation === 'iteration' ? (
        <Bubble align="start" variant="ghost" className="w-full max-w-full">
          <BubbleContent
            data-transcript-message-body
            className={clsx(
              '!w-full !max-w-full !overflow-visible !rounded-md !border-0 !bg-transparent !px-1.5 !py-0.5 text-sm leading-5 whitespace-pre-wrap text-foreground transition-colors group-hover/event:!bg-session-hover',
              selected && '!bg-session-selected',
              entry.isError && 'text-destructive',
            )}
          >
            <TranscriptMessageContent entry={entry} content={content} inProgress={inProgress} msg={msg} />
          </BubbleContent>
        </Bubble>
      ) : (
        <Bubble
          align={speaker === 'user' ? 'end' : 'start'}
          variant="ghost"
          className={speaker === 'user' ? 'max-w-full' : 'w-full max-w-full'}
        >
          <BubbleContent
            data-transcript-message-body
            className={clsx(
              'max-w-full !rounded-[10px] !px-[11px] !py-[6px] text-sm leading-5 whitespace-pre-wrap text-foreground shadow-none transition-colors',
              speaker === 'user'
                ? '!border-[0.5px] !border-session-border !bg-session-speaker-user/10 group-hover/event:!bg-session-hover'
                : '!w-full !border-0 !bg-transparent !px-1.5 !py-0.5 group-hover/event:!bg-session-hover',
              selected && '!bg-session-selected',
              entry.isError && '!bg-destructive/5 text-destructive',
            )}
          >
            <TranscriptMessageContent entry={entry} content={content} inProgress={inProgress} msg={msg} />
          </BubbleContent>
        </Bubble>
      )}
    </article>
  );
}

function TranscriptMessageContent({
  entry,
  content,
  inProgress,
  msg,
}: {
  entry: DisplayEventEntry;
  content: string;
  inProgress: boolean;
  msg: I18nMsg;
}) {
  if (entry.displayEvent.isStreaming) {
    return <LiveRowPreview displayEvent={entry.displayEvent} msg={msg} compact={false} />;
  }
  if (inProgress) {
    return <SynchronizedShimmerText>{content}</SynchronizedShimmerText>;
  }
  return <TranscriptContent value={content} />;
}

export function TranscriptSpeakerHeader({
  label,
  speaker,
  processedAtMs,
  relativeTime,
  selected,
  onSelect,
}: {
  label: string;
  speaker: 'agent' | 'user';
  processedAtMs: number;
  relativeTime: string;
  selected: boolean;
  onSelect: () => void;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const time = processedAtMs
    ? formatters.date(processedAtMs, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
    : relativeTime;
  return (
    <MessageHeader
      className={clsx('mb-0.5 min-w-0 gap-2 px-1.5 text-xs leading-[17px]', speaker === 'user' && 'justify-end')}
    >
      <Button
        type="button"
        variant="ghost"
        size="xs"
        aria-pressed={selected}
        aria-label={msg('managedAgents.sessions.trace.selectEvent', 'Select {label} event', { label })}
        className={clsx(
          'h-auto min-w-0 gap-2 rounded-sm border-transparent px-0 py-0 font-semibold hover:bg-transparent focus-visible:ring-1 focus-visible:ring-ring/35',
          speaker === 'agent' ? 'text-session-speaker-agent' : 'text-session-speaker-user',
        )}
        onClick={onSelect}
      >
        <span className="truncate">{label}</span>
      </Button>
      <time className="shrink-0 font-mono text-[11px] font-normal text-muted-foreground">{time}</time>
    </MessageHeader>
  );
}

function transcriptEventTargetIsInteractive(target: EventTarget) {
  return (
    target instanceof Element &&
    Boolean(target.closest('a, button, input, select, textarea, summary, [role="button"], [contenteditable="true"]'))
  );
}

function TranscriptThinkingRow({
  entry,
  selected,
  onSelect,
  presentation,
}: {
  entry: DisplayEventEntry;
  selected: boolean;
  onSelect: () => void;
  presentation: 'standalone' | 'iteration';
}) {
  const { msg } = useI18n();
  const inProgress = Boolean(entry.inProgress || entry.displayEvent.isStreaming);
  const durationSeconds =
    entry.bracketStartMs === undefined ? undefined : (entry.processedAtMs - entry.bracketStartMs) / 1000;
  const duration =
    durationSeconds !== undefined && Number.isFinite(durationSeconds) && durationSeconds >= 0
      ? `${durationSeconds.toFixed(durationSeconds < 10 ? 1 : 0)}s`
      : undefined;
  const label = inProgress
    ? sessionThinkingPreview(msg)
    : duration
      ? msg('managedAgents.sessions.trace.thoughtFor', 'Thought for {duration}', { duration })
      : msg('managedAgents.sessions.trace.thought', 'Thought');
  return (
    <Bubble
      align="start"
      variant="ghost"
      className={clsx('w-full max-w-full', presentation === 'standalone' && 'my-1')}
    >
      <BubbleContent className="!w-full !max-w-full !overflow-visible !rounded-md !px-0 !py-0">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          data-event-id={entry.traceEntry.id}
          data-entry-kind={entry.kind}
          data-display-kind={entry.traceEntry.displayKind}
          data-transcript-thinking-row
          aria-pressed={selected}
          className={clsx(
            'h-auto min-h-6 w-full justify-start rounded-md border-transparent px-1.5 py-0.5 text-left text-sm leading-5 font-normal italic text-muted-foreground transition-colors hover:bg-session-hover hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring/30',
            selected && 'bg-session-selected text-foreground',
          )}
          onClick={onSelect}
        >
          {inProgress ? (
            <SynchronizedShimmerText className="min-w-0 flex-1 truncate">{label}</SynchronizedShimmerText>
          ) : (
            <span className="min-w-0 flex-1 truncate">{label}</span>
          )}
        </Button>
      </BubbleContent>
    </Bubble>
  );
}

export function ToolCallRow({
  entry,
  selected,
  onSelect,
  presentation = 'standalone',
  renderToolApproval,
}: {
  entry: ToolCallEntry;
  selected: boolean;
  onSelect: () => void;
  presentation?: 'standalone' | 'iteration';
  renderToolApproval?: (entry: ToolCallEntry) => ReactNode;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const duration = formatSessionDuration(entry.executionMs, formatters, msg);
  const approval = renderToolApproval?.(entry);
  return (
    <div
      data-event-id={entry.traceEntry.id}
      data-entry-kind={entry.kind}
      data-display-kind={entry.traceEntry.displayKind}
      className={clsx('w-full rounded-md', presentation === 'standalone' && 'my-0.5 bg-transparent')}
    >
      <button
        type="button"
        data-transcript-tool-row
        data-transcript-header
        aria-pressed={selected}
        className={clsx(
          'flex h-6 w-full min-w-0 items-center justify-start gap-1.5 rounded-md border-0 bg-transparent px-1.5 py-0.5 text-left text-sm leading-5 font-normal text-foreground outline-none hover:bg-session-hover focus-visible:ring-1 focus-visible:ring-ring/30',
          selected && 'bg-session-selected',
          entry.isError && 'text-destructive',
        )}
        onClick={onSelect}
      >
        <Wrench className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
        <CompactToolRowContent
          name={entry.name}
          preview={entry.inputPreview}
          duration={duration}
          lifecycle={entry.lifecycle}
        />
      </button>
      {approval ? <div className="pb-1.5 pl-6 pr-1.5 pt-0.5">{approval}</div> : null}
    </div>
  );
}

export function ToolBatchRow({
  entry,
  selected,
  onSelect,
  presentation = 'standalone',
  renderToolApproval,
}: {
  entry: ToolBatchEntry;
  selected: boolean;
  onSelect: () => void;
  presentation?: 'standalone' | 'iteration';
  renderToolApproval?: (entry: ToolCallEntry) => ReactNode;
}) {
  return (
    <div
      data-event-id={entry.traceEntry.id}
      data-entry-kind={entry.kind}
      data-display-kind={entry.traceEntry.displayKind}
      className={clsx('w-full rounded-md', presentation === 'standalone' && 'my-0.5 bg-transparent')}
    >
      {entry.calls.map((call) => (
        <ToolCallRow
          key={call.id}
          entry={call}
          selected={selected}
          onSelect={onSelect}
          presentation={presentation}
          renderToolApproval={renderToolApproval}
        />
      ))}
    </div>
  );
}

function CompactToolRowContent({
  name,
  preview,
  duration,
  lifecycle,
}: {
  name: string;
  preview: string;
  duration: string;
  lifecycle: ToolLifecycle;
}) {
  return (
    <>
      {lifecycle === 'running' ? (
        <SynchronizedShimmerText className="shrink-0 truncate font-medium">{name}</SynchronizedShimmerText>
      ) : (
        <span className="shrink-0 truncate font-medium text-foreground">{name}</span>
      )}
      {lifecycle === 'running' ? (
        <SynchronizedShimmerText className="min-w-0 flex-1 truncate" variant="secondary">
          {preview}
        </SynchronizedShimmerText>
      ) : (
        <span className="min-w-0 flex-1 truncate text-muted-foreground">{preview}</span>
      )}
      <CompactToolLifecycleBadge lifecycle={lifecycle} />
      {lifecycle !== 'running' && duration ? (
        <span className="shrink-0 font-mono text-xs tabular-nums text-muted-foreground">{duration}</span>
      ) : null}
    </>
  );
}

function CompactToolLifecycleBadge({ lifecycle }: { lifecycle: ToolLifecycle }) {
  const { msg } = useI18n();
  const state = compactToolLifecycleState(lifecycle, msg);
  const Icon = state.icon;
  return (
    <Badge
      variant="secondary"
      data-tool-lifecycle={lifecycle}
      role={lifecycle === 'running' ? 'status' : undefined}
      className={clsx('h-4 shrink-0 gap-1 rounded px-1 py-0 text-[10px] leading-none font-medium', state.className)}
    >
      <Icon className={clsx('size-2.5!', lifecycle === 'running' && 'animate-spin')} aria-hidden />
      {state.label}
    </Badge>
  );
}

function compactToolLifecycleState(lifecycle: ToolLifecycle, msg: I18nMsg) {
  switch (lifecycle) {
    case 'awaiting_approval':
      return {
        className: 'bg-accent text-accent-foreground',
        icon: Clock3,
        label: msg('managedAgents.sessions.trace.awaitingApproval', 'awaiting approval'),
      };
    case 'denied':
      return {
        className: 'bg-warning-bg text-warning',
        icon: Ban,
        label: msg('managedAgents.sessions.trace.denied', 'denied'),
      };
    case 'failed':
      return {
        className: 'bg-destructive/10 text-destructive',
        icon: CircleX,
        label: msg('managedAgents.sessions.inspector.failed', 'Failed'),
      };
    case 'completed':
      return {
        className: 'bg-secondary text-secondary-foreground',
        icon: Check,
        label: msg('managedAgents.sessions.inspector.completed', 'Completed'),
      };
    case 'running':
      return {
        className: 'bg-accent text-accent-foreground',
        icon: Loader2,
        label: msg('managedAgents.sessions.inspector.executing', 'Executing'),
      };
  }
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
          {msg('managedAgents.sessions.trace.gradingIteration', 'Grading iteration {iteration}', { iteration })}
        </TraceRowText>
        {status ? (
          <OutcomeStatusChip status={status} />
        ) : (
          <InProgressChip label={msg('managedAgents.sessions.trace.evaluating', 'Evaluating')} />
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
  const sent = eventType === 'agent.thread_message_sent';
  const received = eventType === 'agent.thread_message_received';
  const direction = sessionSubagentDirection(entry.event);
  const threadRef = sessionSubagentThreadRef(entry.event);
  const threadId = threadRef.threadId;
  const label =
    sent || received
      ? threadNameById.get(threadId) ||
        threadRef.agentName ||
        (threadId ? compactSubagentThreadId(threadId) : msg('managedAgents.sessions.trace.thread', 'Thread'))
      : sessionSubagentRowLabel(entry.event, msg);
  const Icon = direction === 'received' ? ArrowLeft : ArrowRight;
  const clickable = Boolean(threadId);
  const handleClick = (event: ReactMouseEvent<HTMLButtonElement>) => {
    if (!threadId) {
      return;
    }
    event.stopPropagation();
    onThreadClick(threadId, entry.processedAtMs, sent ? 'agent.thread_message_received' : 'agent.thread_message_sent');
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
    <span className={clsx('min-w-0 flex-1 truncate text-sm', !inProgress && 'text-foreground')}>
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
  return entry.toolCounts.map((tool) => (tool.count > 1 ? `${tool.name} ×${tool.count}` : tool.name)).join(', ');
}

export function sessionOutcomeIteration(event: QuickstartSessionEvent) {
  const data = toRecord(event.data);
  const metadata = toRecord(event.metadata);
  return (
    numericValueFromKeys(event, ['iteration', 'iteration_index', 'index']) ||
    (data ? numericValueFromKeys(data, ['iteration', 'iteration_index', 'index']) : 0) ||
    (metadata ? numericValueFromKeys(metadata, ['iteration', 'iteration_index', 'index']) : 0) ||
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
  const status = candidates.find((value): value is string => typeof value === 'string' && value.trim().length > 0);
  return status?.trim();
}

export function outcomeStatusLabel(status: string, msg: I18nMsg) {
  switch (status.toLowerCase()) {
    case 'satisfied':
      return msg('managedAgents.sessions.trace.outcomeSatisfied', 'Satisfied');
    case 'needs_revision':
    case 'needs-revision':
      return msg('managedAgents.sessions.trace.outcomeNeedsRevision', 'Needs revision');
    case 'max_iterations_reached':
    case 'max-iterations-reached':
      return msg('managedAgents.sessions.trace.outcomeMaxIterationsReached', 'Max iterations reached');
    case 'failed':
      return msg('managedAgents.sessions.trace.outcomeFailed', 'Failed');
    case 'interrupted':
      return msg('managedAgents.sessions.trace.outcomeInterrupted', 'Interrupted');
    default:
      return status;
  }
}

export function outcomeStatusChipClass(status: string) {
  switch (status.toLowerCase()) {
    case 'satisfied':
      return 'bg-success-bg text-success';
    case 'needs_revision':
    case 'needs-revision':
      return 'bg-warning-bg text-warning';
    case 'max_iterations_reached':
    case 'max-iterations-reached':
      return 'bg-secondary text-secondary-foreground';
    case 'failed':
      return 'bg-destructive/10 text-destructive';
    default:
      return 'bg-secondary text-secondary-foreground';
  }
}

export function sessionSubagentDirection(event: QuickstartSessionEvent): 'sent' | 'received' {
  return sessionEventType(event) === 'agent.thread_message_received' ? 'received' : 'sent';
}

export function sessionSubagentRowLabel(event: QuickstartSessionEvent, msg: I18nMsg) {
  const direction = sessionSubagentDirection(event);
  const data = toRecord(event.data);
  const metadata = toRecord(event.metadata);
  const sessionThread = toRecord(event.session_thread);
  const subagent = toRecord(event.subagent);
  const agent = toRecord(event.agent);
  const nameCandidates =
    direction === 'received'
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
    .find((value): value is string => typeof value === 'string' && value.trim().length > 0)
    ?.trim();
  if (name) {
    return name;
  }
  const threadId = sessionSubagentThreadId(event);
  if (threadId) {
    return compactEntityId(threadId);
  }
  return msg('managedAgents.sessions.trace.thread', 'Thread');
}

export function sessionSubagentThreadRef(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  const sent = type === 'agent.thread_message_sent';
  const data = toRecord(event.data);
  const metadata = toRecord(event.metadata);
  const threadCandidates = sent
    ? [event.to_session_thread_id, data?.to_session_thread_id, metadata?.to_session_thread_id]
    : [event.from_session_thread_id, data?.from_session_thread_id, metadata?.from_session_thread_id];
  const agentCandidates = sent
    ? [event.to_agent_name, data?.to_agent_name, metadata?.to_agent_name]
    : [event.from_agent_name, data?.from_agent_name, metadata?.from_agent_name];
  const threadId =
    threadCandidates.find((value): value is string => typeof value === 'string' && value.trim().length > 0)?.trim() ||
    sessionSubagentThreadId(event);
  const agentName =
    agentCandidates.find((value): value is string => typeof value === 'string' && value.trim().length > 0)?.trim() ||
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
  presentation = 'standalone',
  renderToolApproval,
}: {
  entry: SessionEventListEntry;
  selected: boolean;
  onSelect: () => void;
  threadNameById: Map<string, string>;
  onThreadClick: (threadId: string, processedAtMs: number, eventType: string) => void;
  presentation?: 'standalone' | 'iteration';
  renderToolApproval?: (entry: ToolCallEntry) => ReactNode;
}) {
  switch (entry.kind) {
    case 'idle_gap':
      return <IdleGapRow entry={entry} />;
    case 'queued_boundary':
      return <QueuedBoundaryRow entry={entry} />;
    case 'outcome':
      return <OutcomeRow entry={entry} selected={selected} onSelect={onSelect} />;
    case 'tool_call':
      return (
        <ToolCallRow
          entry={entry}
          selected={selected}
          onSelect={onSelect}
          presentation={presentation}
          renderToolApproval={renderToolApproval}
        />
      );
    case 'tool_batch':
      return (
        <ToolBatchRow
          entry={entry}
          selected={selected}
          onSelect={onSelect}
          presentation={presentation}
          renderToolApproval={renderToolApproval}
        />
      );
    case 'message':
    case 'status':
    case 'passthrough':
      return (
        <DisplayEventRow
          entry={entry}
          selected={selected}
          onSelect={onSelect}
          threadNameById={threadNameById}
          onThreadClick={onThreadClick}
          presentation={presentation}
        />
      );
    case 'debug':
      return null;
  }
}

export function DebugRow({
  entry,
  selected,
  onSelect,
}: {
  entry: DisplayEventEntry;
  selected: boolean;
  onSelect: () => void;
}) {
  const { msg } = useI18n();
  const title = sessionDisplayEventInlinePreview(entry, msg);
  return (
    <div
      data-event-id={entry.traceEntry.id}
      data-entry-kind={entry.kind}
      data-display-kind={entry.traceEntry.displayKind}
      className="w-full"
    >
      <HeaderRow isSelected={selected} density="compact" onSelect={onSelect}>
        <DebugEventType type={entry.type} error={entry.isError} />
        <span
          className={clsx(
            'min-w-0 flex-1 truncate font-mono text-xs',
            entry.isError ? 'text-destructive' : 'text-muted-foreground',
          )}
        >
          {title}
        </span>
        <MetaStrip
          isError={entry.displayEvent.isError && entry.displayEvent.type !== 'error'}
          relativeTime={entry.relativeTime}
          processedAtMs={entry.processedAtMs}
        />
      </HeaderRow>
    </div>
  );
}

export function DebugListHeader() {
  const { msg } = useI18n();
  return (
    <div
      aria-hidden
      className="sticky top-0 z-10 -mx-1 flex h-7 items-center gap-1.5 border-b border-border/60 bg-card px-1 text-[11px] font-medium text-muted-foreground"
    >
      <span className="min-w-48 shrink-0">{msg('managedAgents.sessions.trace.eventColumn', 'Event')}</span>
      <span className="min-w-0 flex-1">{msg('managedAgents.sessions.trace.previewColumn', 'Preview')}</span>
      <span className="w-16 shrink-0 text-right">{msg('managedAgents.sessions.trace.timeColumn', 'Time')}</span>
    </div>
  );
}

export function DebugEventType({ type, error = false }: { type: string; error?: boolean }) {
  const separator = type.indexOf('.');
  const namespace = separator === -1 ? type : type.slice(0, separator);
  const suffix = separator === -1 ? '' : type.slice(separator);
  return (
    <span className="min-w-48 shrink-0 truncate font-mono text-xs" title={type}>
      <span className={debugEventNamespaceClass(namespace, error)}>{namespace}</span>
      {suffix}
    </span>
  );
}

export function debugEventNamespaceClass(namespace: string, error: boolean) {
  if (error) {
    return 'text-destructive';
  }
  switch (namespace) {
    case 'agent':
      return 'text-session-speaker-agent';
    case 'user':
      return 'text-session-speaker-user';
    case 'span':
      return 'text-session-event-span';
    default:
      return 'text-muted-foreground';
  }
}

export function LiveRowPreview({
  displayEvent,
  msg,
  compact = true,
}: {
  displayEvent: DisplayEvent;
  msg: I18nMsg;
  compact?: boolean;
}) {
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
  return <>{compact ? sessionInlineRowPreview(value) : value}</>;
}

export function sessionDisplayEventInlinePreview(entry: DisplayEventEntry, msg: I18nMsg) {
  if (entry.displayEvent.type === 'thinking') {
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
  const compact = value.replace(/\s+/g, ' ').trim();
  return compact.length > maxLength ? `${compact.slice(0, maxLength)}…` : compact;
}

export function sessionDebugBadge(type: string) {
  return sessionDebugBadgeLabels[type] ?? type;
}

export const sessionDebugBadgeLabels: Record<string, string> = {
  'agent.thread_message_received': 'agent.thread…received',
  'agent.thread_message_sent': 'agent.thread…sent',
  'agent.thread_context_compacted': 'agent.thread…compacted',
  'agent.custom_tool_use': 'agent.custom…use',
  'agent.mcp_tool_result': 'agent.mcp…result',
  'user.custom_tool_result': 'user.custom…result',
  'user.tool_confirmation': 'user.…confirmation',
  'session.status_idle': 'session.…idle',
  'session.status_running': 'session.…running',
  'session.status_rescheduled': 'session.…rescheduled',
  'session.status_terminated': 'session.…terminated',
  'session.thread_status_idle': 'session.thread…idle',
  'session.thread_status_running': 'session.thread…running',
  'session.thread_status_rescheduled': 'session.thread…rescheduled',
  'session.thread_status_terminated': 'session.thread…terminated',
  'span.model_request_start': 'span.model…start',
  'span.model_request_end': 'span.model…end',
  'span.outcome_evaluation_start': 'span.outcome…start',
  'span.outcome_evaluation_ongoing': 'span.outcome…ongoing',
  'span.outcome_evaluation_end': 'span.outcome…end',
};

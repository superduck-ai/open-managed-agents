import { useFormatters, useI18n } from '../../../shared/i18n';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
  InputGroupTextarea,
} from '../../../shared/ui/input-group';
import { ScrollArea } from '../../../shared/ui/scroll-area';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { Tooltip, TooltipContent, TooltipTrigger } from '../../../shared/ui/tooltip';
import {
  quickstartComposerFrameClassName,
  quickstartComposerSendButtonClassName,
  quickstartComposerTextareaClassName,
} from '../components/composerStyles';
import { CopyButton, HighlightedCode, SyntaxCodeBlock } from '../components/CodeBlocks';
import {
  type DisplayEvent,
  type DisplayEventEntry,
  type DisplayEventType,
  type HighlightLanguage,
  type I18nMsg,
  type IdleGapEntry,
  type QueuedBoundaryEntry,
  type QuickstartSessionEvent,
  type SessionEventListEntry,
  type SessionTraceEntry,
  type SessionTraceView,
  type ToolBatchEntry,
  type ToolCallEntry,
  type ToolLifecycle,
} from '../types';
import { copyText, toRecord } from '../utils';
import clsx from 'clsx';
import { ArrowUp, Loader2, Search, Timer, X } from 'lucide-react';
import { type ReactNode, useContext, useEffect, useRef, useState } from 'react';
import { SessionDetailDeltaFramesContext } from './sessionDetailData';
import { formatSessionDuration, sessionEventThreadId } from './sessionDetailModel';
import { ApprovalChip, OutcomeStatusChip, SynchronizedShimmerText } from './sessionTimeline';
import {
  compactSessionEventId,
  prettyCode,
  sessionEventDebugJson,
  sessionEventErrorMessage,
  sessionEventIsThinking,
  sessionEventStructuredContentText,
  sessionEventTranscriptText,
  sessionEventType,
  sessionOutcomeDescription,
  sessionResultText,
  sessionStatusDescription,
  sessionSubagentThreadId,
  sessionThinkingLabel,
  sessionThinkingText,
  sessionToolResultText,
  sessionToolUseCodeLanguage,
  sessionToolUseInput,
  sessionTraceDetailTitle,
  sessionTraceTextIsJson,
} from './sessionTraceModel';
import {
  compactSubagentThreadId,
  sessionDebugBadge,
  sessionOutcomeStatus,
  sessionSubagentDirection,
  sessionSubagentThreadRef,
  sessionToolBatchSummary,
} from './sessionTraceRows';
import { TranscriptContent } from './SessionTranscriptContent';

export function SessionTraceSearch({
  className,
  value,
  onChange,
}: {
  className?: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const { msg } = useI18n();
  const inputRef = useRef<HTMLInputElement>(null);

  return (
    <InputGroup
      className={clsx('h-8 max-w-full bg-background shadow-xs', className ? 'shrink-0' : 'w-72 shrink', className)}
    >
      <InputGroupAddon className="pl-2.5 pr-2">
        <Search className="size-4" aria-hidden />
      </InputGroupAddon>
      <InputGroupInput
        ref={inputRef}
        type="search"
        data-custom-clear
        aria-label={msg('managedAgents.sessions.trace.filterEvents', 'Filter events')}
        value={value}
        placeholder={msg('managedAgents.sessions.trace.filterEvents', 'Filter events')}
        className="pr-2 text-sm"
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Escape') {
            event.stopPropagation();
            onChange('');
            inputRef.current?.blur();
          }
        }}
      />
      {value ? (
        <InputGroupAddon align="inline-end" className="py-0 pl-0 pr-1">
          <InputGroupButton
            type="button"
            size="icon-xs"
            aria-label={msg('managedAgents.sessions.trace.clearFilter', 'Clear filter')}
            className="size-7 shrink-0 text-muted-foreground hover:bg-transparent hover:text-foreground"
            onClick={() => {
              onChange('');
              inputRef.current?.focus({ preventScroll: true });
            }}
          >
            <X className="size-3.5" aria-hidden />
          </InputGroupButton>
        </InputGroupAddon>
      ) : null}
    </InputGroup>
  );
}

export function SessionTraceSkeleton() {
  return (
    <div className="flex flex-col pt-1">
      {Array.from({ length: 7 }).map((_, index) => (
        <div key={index} className="-mx-8 flex h-9 w-[calc(100%+4rem)] items-center gap-2 px-8">
          <span className="h-5 w-12 rounded bg-accent" />
          <span className="h-4 w-60 rounded bg-accent" />
          <span className="ml-auto h-3 w-14 rounded bg-accent" />
        </div>
      ))}
    </div>
  );
}

export function SessionTraceEmpty({
  message,
  danger = false,
  onClear,
}: {
  message: string;
  danger?: boolean;
  onClear?: () => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="flex h-full min-h-[220px] flex-col items-center justify-center px-8 py-24 text-center">
      <p className={clsx('text-sm', danger ? 'text-destructive' : 'text-muted-foreground')}>{message}</p>
      {onClear ? (
        <Button type="button" variant="outline" className="mt-4 bg-accent hover:bg-accent" onClick={onClear}>
          {msg('managedAgents.sessions.trace.clearFilters', 'Clear filters')}
        </Button>
      ) : null}
    </div>
  );
}

export function EventTypeBadge({
  type,
  label,
  variant = 'pill',
  title,
  className,
}: {
  type?: DisplayEventType;
  label?: string;
  variant?: 'pill' | 'compact';
  title?: string;
  className?: string;
}) {
  const { msg } = useI18n();
  const config = sessionEventBadgeConfig(type ?? 'unknown', msg);
  const badgeText = label ?? config.label;
  const badge = (
    <Badge
      variant="secondary"
      className={clsx(
        'h-5 max-w-full shrink-0 items-center justify-center overflow-hidden',
        variant === 'pill'
          ? 'rounded-full px-2 text-[11px] font-medium leading-none'
          : 'rounded-md px-1.5 text-[10px] font-normal leading-[1.4]',
        config.className,
        className,
      )}
    >
      <span className="min-w-0 truncate">{badgeText}</span>
    </Badge>
  );
  if (!title) {
    return badge;
  }
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="inline-flex min-w-0">{badge}</span>} />
      <TooltipContent>{title}</TooltipContent>
    </Tooltip>
  );
}

export function sessionEventBadgeConfig(
  type: DisplayEventType,
  msg: I18nMsg,
): {
  label: string;
  className: string;
} {
  const family = sessionBadgeFamily(type);
  const label = sessionBadgeTypeLabel(type, msg);
  switch (family) {
    case 'user':
      return { label, className: 'bg-session-speaker-user text-white' };
    case 'agent':
      return { label, className: 'bg-session-speaker-agent text-white' };
    case 'subagent':
      return { label, className: 'bg-success text-white' };
    case 'tool':
      return { label, className: 'bg-accent text-muted-foreground' };
    case 'error':
      return { label, className: 'bg-destructive text-background' };
    default:
      return { label, className: 'bg-transparent text-muted-foreground ring-1 ring-inset ring-border' };
  }
}

export function sessionBadgeFamily(
  type: DisplayEventType,
): 'user' | 'agent' | 'tool' | 'subagent' | 'system' | 'error' {
  switch (type) {
    case 'user':
      return 'user';
    case 'agent':
    case 'thinking':
      return 'agent';
    case 'tool_use':
    case 'result':
      return 'tool';
    case 'subagent':
      return 'subagent';
    case 'error':
      return 'error';
    default:
      return 'system';
  }
}

export function sessionBadgeTypeLabel(type: DisplayEventType, msg: I18nMsg) {
  switch (type) {
    case 'user':
      return msg('managedAgents.sessions.trace.user', 'User');
    case 'agent':
      return msg('managedAgents.sessions.trace.agent', 'Agent');
    case 'tool_use':
      return msg('managedAgents.sessions.trace.tool', 'Tool');
    case 'result':
      return msg('managedAgents.sessions.trace.result', 'Result');
    case 'error':
      return msg('managedAgents.sessions.trace.error', 'Error');
    case 'thinking':
      return sessionThinkingLabel(msg);
    case 'root':
      return msg('managedAgents.sessions.trace.session', 'Session');
    case 'status_rescheduled':
      return msg('managedAgents.sessions.trace.rescheduled', 'Rescheduled');
    case 'status_running':
      return msg('managedAgents.sessions.trace.running', 'Running');
    case 'status_idle':
      return msg('managedAgents.sessions.trace.idle', 'Idle');
    case 'status_terminated':
      return msg('managedAgents.sessions.trace.terminated', 'Terminated');
    case 'interrupt':
      return msg('managedAgents.sessions.trace.interrupt', 'Interrupt');
    case 'model_request':
      return msg('managedAgents.sessions.trace.model', 'Model');
    case 'outcome':
      return msg('managedAgents.sessions.trace.outcome', 'Outcome');
    case 'thread':
      return msg('managedAgents.sessions.trace.thread', 'Thread');
    case 'subagent':
      return msg('managedAgents.sessions.trace.subagent', 'Subagent');
    case 'system_message':
      return msg('managedAgents.sessions.trace.system', 'System');
    default:
      return msg('managedAgents.sessions.trace.unknown', 'Unknown');
  }
}

export function sessionDisplayEventTypeIsStatus(type: DisplayEventType) {
  return (
    type === 'root' ||
    type === 'status_rescheduled' ||
    type === 'status_running' ||
    type === 'status_idle' ||
    type === 'status_terminated' ||
    type === 'interrupt'
  );
}

export function EventDetailPanel({
  entry,
  view,
  placement = 'side',
  onClose,
}: {
  entry: SessionEventListEntry;
  view: SessionTraceView;
  placement?: 'overlay' | 'side';
  onClose: () => void;
}) {
  if (!('traceEntry' in entry)) {
    return null;
  }
  const { msg } = useI18n();
  const traceEntry = entry.traceEntry;
  const title = sessionTraceDetailTitle(traceEntry);
  const eventIdLabel = compactSessionEventId(entry.rawEventId);
  const isDebug = view === 'debug';
  return (
    <div
      className={clsx(
        'relative flex flex-col overflow-hidden',
        placement === 'overlay' && 'absolute inset-0 z-10 bg-secondary',
        placement === 'side' && 'h-full bg-transparent',
      )}
      data-placement={placement}
      data-testid="session-trace-detail"
    >
      <div className="flex h-8 shrink-0 items-center gap-1.5 border-b border-border/60 px-3 whitespace-nowrap">
        <EventTypeBadge
          type={entry.displayEvent.type}
          label={
            isDebug
              ? sessionDebugBadge(entry.type)
              : entry.kind === 'tool_batch'
                ? msg('managedAgents.sessions.trace.toolBatch', 'Tools')
                : undefined
          }
          title={isDebug ? entry.type : undefined}
          className={isDebug ? 'font-mono' : undefined}
        />
        <h2
          className={clsx(
            'min-w-0 flex-1 truncate text-xs font-medium',
            entry.isError ? 'text-destructive' : 'text-foreground',
          )}
        >
          {isDebug ? entry.displayEvent.label || title : title}
        </h2>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          className="h-6 min-w-0 max-w-28 p-1.5 font-mono text-[11px] font-normal text-muted-foreground hover:bg-muted/60 hover:text-foreground focus-visible:ring-0"
          aria-label={`Copy ${entry.rawEventId}`}
          onClick={() => void copyText(entry.rawEventId)}
        >
          <span className="truncate">{eventIdLabel}</span>
        </Button>
        <span className="text-[11px] text-muted-foreground" aria-hidden>
          ·
        </span>
        <time className="font-mono text-[11px] tabular-nums text-muted-foreground">{entry.relativeTime}</time>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label={msg('managedAgents.sessions.trace.closeDetailPanel', 'Close detail panel')}
          className="text-muted-foreground hover:bg-muted/60 hover:text-foreground"
          onClick={onClose}
        >
          <X className="size-3.5" aria-hidden />
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-hidden">
        <ScrollArea
          data-testid="session-inspector-event-detail-scroll"
          className="[&_code]:break-words [&_code]:whitespace-pre-wrap [&_pre]:max-h-none [&_pre]:overflow-visible [&_pre]:break-words [&_pre]:whitespace-pre-wrap"
        >
          <div className="pb-8">
            {isDebug ? (
              <DebugDetailPanel entry={entry} />
            ) : entry.kind === 'tool_batch' ? (
              <BatchDetailPanel entry={entry} />
            ) : (
              <EventDetailContent entry={entry} />
            )}
          </div>
        </ScrollArea>
      </div>
    </div>
  );
}

export function EventDetailContent({
  entry,
}: {
  entry: Exclude<SessionEventListEntry, IdleGapEntry | QueuedBoundaryEntry>;
}) {
  if (entry.kind === 'tool_batch') {
    return <BatchDetailPanel entry={entry} />;
  }
  if (entry.kind === 'tool_call') {
    return <ToolCallDetailContent entry={entry} />;
  }
  if (entry.displayEvent.type === 'thinking') {
    return <ThinkingEventDetail entry={entry} />;
  }
  if (entry.displayEvent.type === 'subagent') {
    return <SubagentMessageDetail entry={entry} />;
  }
  if (entry.displayEvent.type === 'thread') {
    return <ThreadEventDetail entry={entry} />;
  }
  if (sessionDisplayEventTypeIsStatus(entry.displayEvent.type)) {
    return <StatusEventDetail entry={entry} />;
  }
  if (entry.displayEvent.type === 'error') {
    return <ErrorEventDetail entry={entry} />;
  }
  if (entry.displayEvent.type === 'outcome') {
    return sessionEventType(entry.event) === 'user.define_outcome' ? (
      <DefineOutcomeEventDetail entry={entry} />
    ) : (
      <OutcomeEventDetail entry={entry} />
    );
  }
  if (
    entry.displayEvent.type === 'user' ||
    entry.displayEvent.type === 'agent' ||
    entry.displayEvent.type === 'result'
  ) {
    return <MessageEventDetail entry={entry} />;
  }
  return <GenericEventDetail entry={entry} />;
}

export function MessageEventDetail({
  entry,
}: {
  entry: Exclude<SessionEventListEntry, IdleGapEntry | QueuedBoundaryEntry>;
}) {
  const { msg } = useI18n();
  if (entry.displayEvent.isStreaming) {
    return <LiveMessageContent displayEvent={entry.displayEvent} />;
  }
  const value = entry.displayEvent.content || entry.traceEntry.displayText || entry.traceEntry.preview;
  return (
    <div className="px-5 py-4">
      <div className="mb-2 text-xs text-muted-foreground">{msg('managedAgents.sessions.trace.content', 'Content')}</div>
      {value ? (
        <TranscriptTypedContent entry={entry.traceEntry} value={value} />
      ) : (
        <div className="text-xs italic text-muted-foreground">
          {msg('managedAgents.sessions.trace.noContent', 'No content.')}
        </div>
      )}
    </div>
  );
}

export function SubagentMessageDetail({ entry }: { entry: DisplayEventEntry }) {
  const { msg } = useI18n();
  const direction = sessionSubagentDirection(entry.event);
  const ref = sessionSubagentThreadRef(entry.event);
  const content = entry.displayEvent.content || entry.traceEntry.displayText || sessionEventTranscriptText(entry.event);
  return (
    <div className="space-y-5 px-5 py-4">
      <dl className="space-y-2">
        <PropertyRow
          label={
            direction === 'received'
              ? msg('managedAgents.sessions.trace.receivedFrom', 'Received from')
              : msg('managedAgents.sessions.trace.sentTo', 'Sent to')
          }
          value={
            ref.agentName ||
            (ref.threadId
              ? compactSubagentThreadId(ref.threadId)
              : msg('managedAgents.sessions.trace.thread', 'Thread'))
          }
        />
        {ref.threadId ? (
          <PropertyRow
            label={msg('managedAgents.sessions.trace.threadId', 'Thread ID')}
            value={<span className="font-mono">{ref.threadId}</span>}
          />
        ) : null}
      </dl>
      {content ? (
        <div>
          <div className="mb-2 text-xs text-muted-foreground">
            {msg('managedAgents.sessions.trace.content', 'Content')}
          </div>
          <TranscriptContent value={content} />
        </div>
      ) : null}
    </div>
  );
}

export function ThreadEventDetail({ entry }: { entry: DisplayEventEntry }) {
  const { msg } = useI18n();
  const threadId = sessionSubagentThreadId(entry.event) || sessionEventThreadId(entry.event);
  return (
    <div className="space-y-4 px-5 py-4">
      <dl className="space-y-2">
        {threadId ? (
          <PropertyRow
            label={msg('managedAgents.sessions.trace.threadId', 'Thread ID')}
            value={<span className="font-mono">{threadId}</span>}
          />
        ) : null}
        <PropertyRow
          label={msg('managedAgents.sessions.trace.transition', 'Transition')}
          value={sessionStatusDescription(entry.type, entry.event) ?? entry.traceEntry.preview ?? entry.type}
        />
      </dl>
      <GenericEventDetail entry={entry} compact />
    </div>
  );
}

export function StatusEventDetail({ entry }: { entry: DisplayEventEntry }) {
  const { msg } = useI18n();
  const description = sessionStatusDescription(entry.type, entry.event);
  if (!description && entry.type === 'session.status_idle') {
    return null;
  }
  return (
    <div className="px-5 py-4">
      <div className="mb-2 text-xs text-muted-foreground">{msg('managedAgents.sessions.trace.status', 'Status')}</div>
      <p className="text-sm text-foreground">{description ?? entry.traceEntry.preview ?? entry.type}</p>
    </div>
  );
}

export function ErrorEventDetail({ entry }: { entry: DisplayEventEntry }) {
  return (
    <div className="px-5 py-4">
      <pre className="whitespace-pre-wrap break-words rounded-md border border-destructive/50 bg-destructive/10 p-3 font-mono text-xs text-destructive">
        {sessionEventErrorMessage(entry.event)}
      </pre>
    </div>
  );
}

export function OutcomeEventDetail({ entry }: { entry: DisplayEventEntry }) {
  const { msg } = useI18n();
  const status = entry.outcomeStatus ?? sessionOutcomeStatus(entry.event);
  const description = sessionOutcomeDescription(entry.event, msg);
  return (
    <div className="space-y-4 px-5 py-4">
      {status ? (
        <PropertyRow
          label={msg('managedAgents.sessions.trace.verdict', 'Verdict')}
          value={<OutcomeStatusChip status={status} />}
        />
      ) : null}
      <div>
        <div className="mb-2 text-xs text-muted-foreground">
          {msg('managedAgents.sessions.trace.explanation', 'Explanation')}
        </div>
        <p className="text-sm text-foreground">
          {description || msg('managedAgents.sessions.trace.gradingInProgress', 'Grading in progress...')}
        </p>
      </div>
    </div>
  );
}

export function DefineOutcomeEventDetail({ entry }: { entry: DisplayEventEntry }) {
  const { msg } = useI18n();
  const description = sessionOutcomeDescription(entry.event, msg);
  return (
    <div className="space-y-3 px-5 py-4">
      <PropertyRow label={msg('managedAgents.sessions.trace.description', 'Description')} value={description} />
      {typeof entry.event.outcome_id === 'string' ? (
        <PropertyRow
          label={msg('managedAgents.sessions.trace.outcomeId', 'Outcome ID')}
          value={<span className="font-mono">{entry.event.outcome_id}</span>}
        />
      ) : null}
      {typeof entry.event.max_iterations === 'number' ? (
        <PropertyRow
          label={msg('managedAgents.sessions.trace.maxIterations', 'Max iterations')}
          value={String(entry.event.max_iterations)}
        />
      ) : null}
    </div>
  );
}

export function BatchDetailPanel({ entry }: { entry: ToolBatchEntry }) {
  const { msg } = useI18n();
  const summary = sessionToolBatchSummary(entry);
  return (
    <div className="space-y-5 px-5 py-4">
      <SectionHeader
        title={msg('managedAgents.sessions.trace.toolBatchSummary', '{count} tool calls: {summary}', {
          count: entry.calls.length,
          summary,
        })}
      />
      <dl className="space-y-2">
        <PropertyRow label={msg('managedAgents.sessions.trace.tool', 'Tool')} value={summary} />
      </dl>
      {entry.calls.map((call, index) => (
        <CallSection
          key={call.id}
          title={`${index + 1}. ${call.inputPreview || call.name}`}
          lifecycle={call.lifecycle}
          executionMs={call.executionMs}
        >
          <ToolUseJsonSection
            title={msg('managedAgents.sessions.trace.toolUse', 'Tool use')}
            value={sessionToolUseInput(call.event)}
          />
          {call.confirmationEvent ? <ToolConfirmationSection event={call.confirmationEvent} /> : null}
          {call.resultEvent ? <ToolResultSection event={call.resultEvent} /> : null}
        </CallSection>
      ))}
    </div>
  );
}

export function DebugDetailPanel({
  entry,
}: {
  entry: Exclude<SessionEventListEntry, IdleGapEntry | QueuedBoundaryEntry>;
}) {
  const { msg } = useI18n();
  const deltaFrames = useContext(SessionDetailDeltaFramesContext);
  const frame = deltaFrames[entry.displayEvent.id];
  const supportsDeltas = entry.type === 'agent.message' || entry.type === 'agent.thinking';
  const [view, setView] = useState<'raw' | 'deltas'>('raw');
  const activeView = view === 'deltas' && frame ? 'deltas' : 'raw';
  const contentEvent = frame?.message ?? entry.event;
  if (!supportsDeltas) {
    return <DebugEventDetail event={contentEvent} type={entry.type} />;
  }
  const unavailableReason = msg(
    'managedAgents.sessions.trace.deltasUnavailable',
    'Deltas are only captured for messages streamed live in this browser tab — they are not stored in history.',
  );
  return (
    <Tabs
      value={activeView}
      onValueChange={(nextView) => nextView && setView(nextView as 'raw' | 'deltas')}
      className="gap-0"
    >
      <div className="flex items-center justify-end px-3 pt-2">
        <TabsList aria-label={msg('managedAgents.sessions.trace.eventView', 'Event view')} className="h-7">
          <TabsTrigger value="raw" className="h-5 px-2 text-xs">
            {msg('managedAgents.sessions.trace.raw', 'Raw')}
          </TabsTrigger>
          {frame ? (
            <TabsTrigger value="deltas" className="h-5 px-2 text-xs">
              {msg('managedAgents.sessions.trace.deltas', 'Deltas')}
            </TabsTrigger>
          ) : (
            <Tooltip>
              <TooltipTrigger
                render={
                  <span className="inline-flex" tabIndex={0} aria-label={unavailableReason}>
                    <TabsTrigger value="deltas" disabled className="h-5 px-2 text-xs">
                      {msg('managedAgents.sessions.trace.deltas', 'Deltas')}
                    </TabsTrigger>
                  </span>
                }
              />
              <TooltipContent>{unavailableReason}</TooltipContent>
            </Tooltip>
          )}
        </TabsList>
      </div>
      <TabsContent value="raw">
        <DebugEventDetail event={contentEvent} type={entry.type} />
      </TabsContent>
      <TabsContent value="deltas">
        <DebugDeltasDetail frames={frame?.frames ?? []} />
      </TabsContent>
    </Tabs>
  );
}

export function DebugDeltasDetail({ frames }: { frames: QuickstartSessionEvent[] }) {
  const { msg } = useI18n();
  let deltaNumber = 0;
  return (
    <div className="px-3 pb-5 pt-2">
      <Table className="table-fixed text-xs">
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="h-7 w-10 px-1.5 text-right">#</TableHead>
            <TableHead className="h-7 w-24 px-1.5">{msg('managedAgents.sessions.trace.frame', 'Frame')}</TableHead>
            <TableHead className="h-7 px-1.5">{msg('managedAgents.sessions.trace.text', 'Text')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {frames.map((frame, index) => {
            const isDelta = sessionEventType(frame) === 'event_delta';
            if (isDelta) deltaNumber += 1;
            const text = sessionDeltaFramePreview(frame);
            return (
              <TableRow key={index} aria-label={isDelta ? `event_delta #${deltaNumber}` : sessionEventType(frame)}>
                <TableCell className="h-7 px-1.5 text-right font-mono tabular-nums text-muted-foreground">
                  {isDelta ? deltaNumber : ''}
                </TableCell>
                <TableCell className="h-7 truncate px-1.5 font-mono">{sessionEventType(frame)}</TableCell>
                <TableCell className="h-7 truncate px-1.5 font-mono text-muted-foreground" title={text}>
                  {text}
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

function sessionDeltaFramePreview(frame: QuickstartSessionEvent) {
  if (sessionEventType(frame) === 'event_start') {
    return `start · ${sessionEventType(toRecord(frame.event) ?? {})}`;
  }
  const delta = toRecord(frame.delta);
  const content = toRecord(delta?.content);
  const text = content?.text ?? content?.thinking;
  return typeof text === 'string' ? text.replaceAll('\n', '↵') : '';
}

export function LiveMessageContent({ displayEvent }: { displayEvent: DisplayEvent }) {
  const { msg } = useI18n();
  const deltaFrames = useContext(SessionDetailDeltaFramesContext);
  const liveEvent = deltaFrames[displayEvent.id]?.message ?? displayEvent.event;
  const value = sessionEventIsThinking(liveEvent)
    ? sessionThinkingText(liveEvent)
    : sessionEventTranscriptText(liveEvent) ||
      sessionEventStructuredContentText(liveEvent) ||
      sessionToolResultText(liveEvent) ||
      sessionResultText(liveEvent) ||
      displayEvent.content;
  return (
    <div className="px-5 py-4">
      <div className="mb-2 text-xs text-muted-foreground">{msg('managedAgents.sessions.trace.content', 'Content')}</div>
      {value ? (
        <TranscriptContent value={value} />
      ) : (
        <SynchronizedShimmerText className="text-sm">
          {msg('managedAgents.sessions.trace.generatingEllipsis', 'Generating...')}
        </SynchronizedShimmerText>
      )}
    </div>
  );
}

export function ThinkingEventDetail({ entry }: { entry: DisplayEventEntry }) {
  const { msg } = useI18n();
  const deltaFrames = useContext(SessionDetailDeltaFramesContext);
  const liveEvent = deltaFrames[entry.displayEvent.id]?.message ?? entry.event;
  const thinkingText = sessionThinkingText(liveEvent);
  return (
    <div className="px-5 py-4">
      <div className="mb-2 text-xs text-muted-foreground">{msg('managedAgents.sessions.trace.content', 'Content')}</div>
      {thinkingText ? (
        <TranscriptContent value={thinkingText} />
      ) : (
        <div className="text-xs italic text-muted-foreground">
          {msg('managedAgents.sessions.trace.noContent', 'No content.')}
        </div>
      )}
    </div>
  );
}

export function ToolCallDetailContent({ entry }: { entry: ToolCallEntry }) {
  const { msg } = useI18n();
  return (
    <div className="space-y-6 px-5 py-4">
      <ApprovalChip lifecycle={entry.lifecycle} />
      <ToolUseJsonSection
        title={msg('managedAgents.sessions.trace.toolUse', 'Tool use')}
        value={sessionToolUseInput(entry.event)}
      />
      {entry.confirmationEvent ? <ToolConfirmationSection event={entry.confirmationEvent} /> : null}
      {entry.resultEvent ? (
        <ToolResultSection event={entry.resultEvent} />
      ) : (
        <p className="text-xs italic text-muted-foreground">
          {msg('managedAgents.sessions.trace.noResult', 'No result')}
        </p>
      )}
    </div>
  );
}

export function GenericEventDetail({
  entry,
  compact = false,
}: {
  entry: Exclude<SessionEventListEntry, IdleGapEntry | QueuedBoundaryEntry>;
  compact?: boolean;
}) {
  const { msg } = useI18n();
  const value = entry.displayEvent.content || entry.traceEntry.displayText || entry.traceEntry.preview;
  return (
    <div className={compact ? 'space-y-4' : 'space-y-4 px-5 py-4'}>
      <dl className="space-y-2">
        <PropertyRow
          label={msg('managedAgents.sessions.trace.type', 'Type')}
          value={<span className="font-mono">{entry.type}</span>}
        />
      </dl>
      {value ? (
        <div>
          <div className="mb-2 text-xs text-muted-foreground">
            {msg('managedAgents.sessions.trace.content', 'Content')}
          </div>
          <TranscriptTypedContent entry={entry.traceEntry} value={value} />
        </div>
      ) : null}
    </div>
  );
}

export function CallSection({
  title,
  lifecycle,
  executionMs,
  children,
}: {
  title: string;
  lifecycle?: ToolLifecycle;
  executionMs?: number;
  children: ReactNode;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between gap-3">
        <SectionHeader title={title} />
        <div className="flex shrink-0 items-center gap-2 text-xs text-muted-foreground">
          <ApprovalChip lifecycle={lifecycle} />
          {executionMs ? (
            <span className="inline-flex items-center gap-1 font-mono">
              <Timer className="size-3.5" aria-hidden />
              {formatSessionDuration(executionMs, formatters, msg)}
            </span>
          ) : null}
        </div>
      </div>
      {children}
      <SectionDivider />
    </section>
  );
}

export function SectionDivider() {
  return <div className="h-px bg-border" aria-hidden />;
}

export function SectionHeader({ title }: { title: string }) {
  return <h3 className="text-sm font-semibold text-foreground">{title}</h3>;
}

export function PropertyRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="grid grid-cols-[112px_minmax(0,1fr)] gap-3 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words text-foreground">{value}</dd>
    </div>
  );
}

export function DebugEventDetail({ event, type }: { event: QuickstartSessionEvent; type: string }) {
  const { msg } = useI18n();
  const debugJson = sessionEventDebugJson(event);
  return (
    <div className="px-3 pb-5 pt-2">
      <div className="mb-2 flex items-center justify-between gap-3">
        <div className="font-mono text-xs text-muted-foreground">{type}</div>
        <CopyButton value={debugJson} label={msg('managedAgents.quickstart.copyCode', 'Copy code')} />
      </div>
      <SyntaxCodeBlock value={debugJson} language="json" />
    </div>
  );
}

export function TranscriptTypedContent({ entry, value }: { entry: SessionTraceEntry; value: string }) {
  if (entry.displayKind === 'json') {
    return <SyntaxCodeBlock value={prettyCode(value)} language="json" />;
  }
  if (entry.displayKind === 'log') {
    return <SyntaxCodeBlock value={value || '(empty)'} language="plaintext" maxHeightClassName="max-h-80" />;
  }
  if (entry.displayKind === 'metric') {
    return (
      <div className="rounded-md border border-border bg-secondary px-3 py-2 font-mono text-sm leading-6 tabular-nums text-foreground">
        {value}
      </div>
    );
  }
  if (entry.displayKind === 'command') {
    return <SyntaxCodeBlock value={value} language={sessionToolUseCodeLanguage(entry.event)} />;
  }
  return <TranscriptContent value={value} />;
}

export function ToolUseJsonSection({ title, value }: { title: string; value: unknown }) {
  const code = JSON.stringify(value ?? {}, null, 2);
  return (
    <div>
      <div className="mb-1.5 flex items-baseline justify-between gap-3">
        <span className="text-xs text-muted-foreground">{title}</span>
      </div>
      <pre className="subtle-scrollbar max-h-80 overflow-auto rounded-md border border-border bg-secondary p-3 font-mono text-xs leading-[18px] text-foreground">
        <HighlightedCode code={code} language="json" />
      </pre>
    </div>
  );
}

export function ToolResultSection({ event }: { event: QuickstartSessionEvent }) {
  const { msg } = useI18n();
  const text = sessionToolResultText(event);
  const parsed = prettyCode(text || '(empty)');
  const language: HighlightLanguage = sessionTraceTextIsJson(parsed) ? 'json' : 'plaintext';
  return (
    <div>
      <div className="mb-1.5 flex items-baseline justify-between gap-3">
        <span className="text-xs text-muted-foreground">
          {msg('managedAgents.sessions.trace.toolResult', 'Tool result')}
        </span>
        {typeof event.id === 'string' && event.id ? (
          <span className="font-mono text-xs text-muted-foreground">{event.id}</span>
        ) : null}
      </div>
      <pre
        className={clsx(
          'subtle-scrollbar max-h-80 overflow-auto rounded-md border p-3 font-mono text-xs leading-[18px]',
          event.is_error === true
            ? 'border-destructive/50 bg-destructive/10 text-destructive'
            : 'border-border bg-secondary text-foreground',
        )}
      >
        <HighlightedCode code={parsed} language={language} />
      </pre>
    </div>
  );
}

export function ToolConfirmationSection({ event }: { event: QuickstartSessionEvent }) {
  const { msg } = useI18n();
  const payload: Record<string, unknown> = {
    result: event.result,
  };
  if (typeof event.deny_message === 'string' && event.deny_message.trim()) {
    payload.deny_message = event.deny_message;
  } else if (event.deny_message === null) {
    payload.deny_message = null;
  }
  return (
    <ToolUseJsonSection
      title={msg('managedAgents.sessions.trace.toolConfirmation', 'Tool confirmation')}
      value={payload}
    />
  );
}

export function QuickstartSessionComposer({
  value,
  placeholder,
  disabled,
  loading,
  onChange,
  onSubmit,
}: {
  value: string;
  placeholder: string;
  disabled: boolean;
  loading: boolean;
  onChange: (value: string) => void;
  onSubmit: () => void;
}) {
  const { msg } = useI18n();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const canSubmit = !disabled && !loading && value.trim().length > 0;

  useEffect(() => {
    const textarea = textareaRef.current;
    if (!textarea) {
      return;
    }
    textarea.style.height = 'auto';
    textarea.style.height = `${Math.min(textarea.scrollHeight, 160)}px`;
  }, [value]);

  return (
    <InputGroup
      data-disabled={disabled || loading ? 'true' : undefined}
      className={clsx('w-full', quickstartComposerFrameClassName)}
    >
      <InputGroupTextarea
        ref={textareaRef}
        aria-label={placeholder}
        rows={1}
        value={value}
        disabled={disabled || loading}
        placeholder={placeholder}
        className={clsx(
          'subtle-scrollbar block max-h-40 overflow-y-auto disabled:cursor-not-allowed disabled:bg-transparent disabled:opacity-50',
          quickstartComposerTextareaClassName,
        )}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
            event.preventDefault();
            if (canSubmit) {
              onSubmit();
            }
          }
        }}
      />
      <InputGroupAddon align="inline-end" className="shrink-0 self-end py-0 pr-0">
        <InputGroupButton
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={msg('playground.send', 'Send')}
          disabled={!canSubmit}
          className={quickstartComposerSendButtonClassName}
          onClick={onSubmit}
        >
          {loading ? (
            <Loader2 className="size-4 animate-spin" aria-hidden />
          ) : (
            <ArrowUp className="size-4" aria-hidden />
          )}
        </InputGroupButton>
      </InputGroupAddon>
    </InputGroup>
  );
}

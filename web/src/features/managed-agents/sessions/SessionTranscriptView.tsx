import { type QuickstartSessionEvent, type SessionEventListEntry, type ToolCallEntry } from '../types';
import { useI18n } from '../../../shared/i18n';
import { Message, MessageContent } from '../../../shared/ui/message';
import { sessionEventEntryMatchesSelectedId, sessionEventEntrySelectionId } from './sessionDetailModel';
import { TranscriptRow, TranscriptSpeakerHeader } from './sessionTraceRows';
import { InProgressChip, MetaStrip, SynchronizedShimmerText } from './sessionTimeline';
import {
  buildSessionTranscriptBlocks,
  filterSessionTranscriptBlocks,
  type SessionTranscriptBlock,
  type SessionTranscriptIteration,
} from './sessionTranscriptModel';
import clsx from 'clsx';
import { type ReactNode } from 'react';
import {
  sessionEventElapsedTime,
  sessionEventKey,
  sessionEventProcessedTimestamp,
  sessionEventTimestamp,
  sessionSubagentName,
} from './sessionTraceModel';

export function SessionTranscriptView({
  entries,
  visibleEntries = entries,
  hoveredEventId = null,
  selectedEntryId,
  onHoverEvent = () => {},
  onSelectEntry,
  threadNameById,
  onThreadClick,
  openModelRequest = null,
  renderToolApproval,
  traceStartMs = 0,
}: {
  entries: SessionEventListEntry[];
  visibleEntries?: SessionEventListEntry[];
  hoveredEventId?: string | null;
  selectedEntryId: string | null;
  onHoverEvent?: (entryId: string | null) => void;
  onSelectEntry: (entryId: string | null) => void;
  threadNameById: Map<string, string>;
  onThreadClick: (threadId: string, processedAtMs: number, eventType: string) => void;
  openModelRequest?: QuickstartSessionEvent | null;
  renderToolApproval?: (entry: ToolCallEntry) => ReactNode;
  traceStartMs?: number;
}) {
  const { msg } = useI18n();
  const generatingLabel = msg('managedAgents.sessions.trace.generatingEllipsis', 'Generating…');
  const visibleIds = new Set(visibleEntries.map((entry) => entry.id));
  const transcriptBlocks = buildSessionTranscriptBlocks(entries);
  const blocks = filterSessionTranscriptBlocks(transcriptBlocks, (entry) => visibleIds.has(entry.id));
  const showEmptyOpenTurn =
    openModelRequest !== null &&
    !transcriptBlocks.some((block) => block.kind === 'agent' && sessionTranscriptBlockIsOpen(block));
  const renderEntry = (entry: SessionEventListEntry, presentation: 'standalone' | 'iteration') => {
    const entryId = sessionEventEntrySelectionId(entry);
    const hovered = sessionEventEntryMatchesSelectedId(entry, hoveredEventId);
    return (
      <div
        key={entry.id}
        data-transcript-entry-hovered={hovered || undefined}
        className={clsx(
          'contents',
          hovered &&
            '[&_[data-transcript-header]]:bg-session-hover [&_[data-transcript-message-body]]:!bg-session-hover [&_[data-transcript-thinking-row]]:bg-session-hover [&_[data-transcript-tool-row]]:bg-session-hover',
        )}
        onMouseEnter={() => entryId && onHoverEvent(entryId)}
        onMouseLeave={() => onHoverEvent(null)}
      >
        <TranscriptRow
          entry={entry}
          selected={sessionEventEntryMatchesSelectedId(entry, selectedEntryId)}
          onSelect={() => onSelectEntry(entryId)}
          threadNameById={threadNameById}
          onThreadClick={onThreadClick}
          presentation={presentation}
          renderToolApproval={renderToolApproval}
        />
      </div>
    );
  };

  return (
    <div className="flex w-full flex-col" data-testid="session-transcript-view">
      {blocks.map((block) => {
        if (block.kind === 'user') {
          return (
            <Message key={block.id} align="end" data-transcript-block="user" className="mt-3 items-start first:mt-1.5">
              <MessageContent className="w-auto max-w-[92%] gap-0 sm:max-w-[80%]">
                {renderEntry(block.entry, 'standalone')}
              </MessageContent>
            </Message>
          );
        }
        if (block.kind === 'standalone') {
          const alignment = sessionTranscriptStandaloneMessageAlignment(block.entry);
          if (alignment) {
            return (
              <Message
                key={block.id}
                align={alignment}
                data-transcript-block="standalone-message"
                className="mt-3 items-start first:mt-1.5"
              >
                <MessageContent
                  className={clsx('gap-0', alignment === 'end' ? 'w-auto max-w-[92%] sm:max-w-[80%]' : 'w-full')}
                >
                  {renderEntry(block.entry, 'standalone')}
                </MessageContent>
              </Message>
            );
          }
          return (
            <div key={block.id} className="mt-1.5 py-0.5">
              {renderEntry(block.entry, 'standalone')}
            </div>
          );
        }
        const selected = block.iterations.some((iteration) =>
          iteration.entries.some((entry) => sessionEventEntryMatchesSelectedId(entry, selectedEntryId)),
        );
        const open = sessionTranscriptBlockIsOpen(block);
        return (
          <Message
            key={block.id}
            align="start"
            data-transcript-block="agent"
            data-open={open || undefined}
            data-entering={open || undefined}
            className="-mx-[10px] mt-[6px] w-[calc(100%+20px)] items-start px-[10px] py-[2px] first:mt-0"
          >
            <MessageContent className="w-full gap-0">
              <TranscriptSpeakerHeader
                label={block.speakerLabel}
                speaker="agent"
                processedAtMs={block.headerEntry.processedAtMs}
                relativeTime={block.headerEntry.relativeTime}
                selected={selected}
                onSelect={() => onSelectEntry(sessionEventEntrySelectionId(block.headerEntry))}
              />
              <div className="mt-[3px]">
                {block.iterations.map((iteration) => (
                  <TranscriptIteration key={iteration.id} iteration={iteration} generatingLabel={generatingLabel}>
                    {iteration.entries.map((entry) => renderEntry(entry, 'iteration'))}
                  </TranscriptIteration>
                ))}
              </div>
            </MessageContent>
          </Message>
        );
      })}
      {showEmptyOpenTurn ? (
        <PendingModelRequestTurn
          event={openModelRequest}
          generatingLabel={generatingLabel}
          traceStartMs={traceStartMs}
          onSelectEntry={onSelectEntry}
        />
      ) : null}
    </div>
  );
}

function PendingModelRequestTurn({
  event,
  generatingLabel,
  onSelectEntry,
  traceStartMs,
}: {
  event: QuickstartSessionEvent;
  generatingLabel: string;
  onSelectEntry: (entryId: string | null) => void;
  traceStartMs: number;
}) {
  const { msg } = useI18n();
  const eventId = sessionEventKey(event);
  const processedAtMs = sessionEventProcessedTimestamp(event) || sessionEventTimestamp(event);
  return (
    <Message
      align="start"
      data-transcript-block="agent"
      data-open="true"
      data-entering="true"
      className="-mx-[10px] mt-[6px] w-[calc(100%+20px)] items-start px-[10px] py-[2px] first:mt-0"
    >
      <MessageContent className="w-full gap-0">
        <TranscriptSpeakerHeader
          label={sessionSubagentName(event) || msg('managedAgents.sessions.trace.agent', 'Agent')}
          speaker="agent"
          processedAtMs={processedAtMs}
          relativeTime={sessionEventElapsedTime(event, traceStartMs)}
          selected={false}
          onSelect={() => onSelectEntry(eventId)}
        />
        <div className="mt-[3px]">
          <TranscriptIteration
            iteration={{ id: `iteration-${eventId}`, bracketId: eventId, entries: [] }}
            eventId={eventId}
            generatingLabel={generatingLabel}
            openOverride
          >
            {null}
          </TranscriptIteration>
        </div>
      </MessageContent>
    </Message>
  );
}

function sessionTranscriptBlockIsOpen(block: Extract<SessionTranscriptBlock, { kind: 'agent' }>) {
  return block.iterations.some((iteration) => iteration.entries.some(sessionTranscriptEntryIsOpen));
}

function sessionTranscriptEntryIsOpen(entry: SessionEventListEntry) {
  if (entry.kind === 'tool_call') {
    return entry.lifecycle === 'running' || entry.lifecycle === 'awaiting_approval';
  }
  if (entry.kind === 'tool_batch') {
    return entry.calls.some(sessionTranscriptEntryIsOpen);
  }
  return 'displayEvent' in entry && Boolean(entry.inProgress || entry.displayEvent.isStreaming);
}

function sessionTranscriptStandaloneMessageAlignment(entry: SessionEventListEntry): 'start' | 'end' | null {
  if (!('displayEvent' in entry)) {
    return null;
  }
  if (entry.displayEvent.type === 'user') {
    return 'end';
  }
  if (entry.displayEvent.type === 'agent' || entry.displayEvent.type === 'thinking') {
    return 'start';
  }
  return null;
}

function TranscriptIteration({
  iteration,
  children,
  generatingLabel,
  eventId,
  openOverride = false,
}: {
  iteration: SessionTranscriptIteration;
  children: ReactNode;
  eventId?: string;
  generatingLabel: string;
  openOverride?: boolean;
}) {
  const meta = sessionTranscriptIterationMetaEntry(iteration);
  const thinkingOnly = sessionTranscriptIterationIsThinkingOnly(iteration);
  const open = openOverride || iteration.entries.some(sessionTranscriptEntryIsOpen);
  return (
    <div
      data-transcript-iteration={iteration.bracketId}
      data-event-id={eventId}
      data-thinking-only={thinkingOnly || undefined}
      className="group/iteration relative mb-[5px] flex min-w-0 flex-col rounded-[10px] border-[0.5px] border-session-border bg-session-surface px-[10px] py-1 last:mb-0"
    >
      {meta || open ? (
        <div
          className={clsx(
            'pointer-events-none absolute -top-2 right-2.5 z-10 flex items-center gap-2 rounded-full border-[0.5px] border-session-border bg-popover px-2 py-0.5 opacity-0 shadow-xs transition-opacity group-hover/iteration:pointer-events-auto group-hover/iteration:opacity-100 group-focus-within/iteration:pointer-events-auto group-focus-within/iteration:opacity-100',
            open && 'pointer-events-auto opacity-100',
          )}
        >
          {open && meta?.lifecycle !== 'running' ? <InProgressChip label={generatingLabel} /> : null}
          {meta ? (
            <MetaStrip
              usage={meta.usage}
              inferenceMs={meta.inferenceMs}
              executionMs={meta.executionMs}
              lifecycle={meta.lifecycle}
              isError={meta.isError}
              relativeTime={meta.relativeTime}
              processedAtMs={meta.processedAtMs}
            />
          ) : null}
        </div>
      ) : null}
      {open && !iteration.entries.length ? (
        <div className="-mx-1.5 px-1.5 py-0.5 text-sm leading-5 italic text-muted-foreground">
          <SynchronizedShimmerText>{generatingLabel}</SynchronizedShimmerText>
        </div>
      ) : null}
      {children}
    </div>
  );
}

function sessionTranscriptIterationIsThinkingOnly(iteration: SessionTranscriptIteration) {
  return (
    iteration.entries.length > 0 &&
    iteration.entries.every((entry) => 'displayEvent' in entry && entry.displayEvent.type === 'thinking')
  );
}

function sessionTranscriptIterationMetaEntry(iteration: SessionTranscriptIteration) {
  const entries = iteration.entries.filter((entry) => 'traceEntry' in entry);
  const latest = entries.at(-1);
  if (!latest) {
    return null;
  }
  const usageEntry = entries.find((entry) => 'usage' in entry && entry.usage.input + entry.usage.output > 0);
  const inferenceMs = Math.max(...entries.map((entry) => ('inferenceMs' in entry ? entry.inferenceMs : 0)));
  const executionMs = Math.max(...entries.map((entry) => ('executionMs' in entry ? entry.executionMs : 0)));
  const lifecycleEntry = [...entries].reverse().find((entry) => 'lifecycle' in entry);
  return {
    usage: usageEntry && 'usage' in usageEntry ? usageEntry.usage : undefined,
    inferenceMs,
    executionMs,
    lifecycle: lifecycleEntry && 'lifecycle' in lifecycleEntry ? lifecycleEntry.lifecycle : undefined,
    isError: entries.some((entry) => entry.isError),
    relativeTime: latest.relativeTime,
    processedAtMs: latest.processedAtMs,
  };
}

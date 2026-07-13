import { useFormatters, useI18n } from '../../../shared/i18n';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { Input } from '../../../shared/ui/input';
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupTextarea } from '../../../shared/ui/input-group';
import { Tabs, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
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
  type SessionDebugDetailTab,
  type SessionEventListEntry,
  type SessionTraceEntry,
  type SessionTraceFamily,
  type SessionTraceFilterOption,
  type SessionTraceView,
  type ToolBatchEntry,
  type ToolCallEntry,
  type ToolLifecycle,
  type TranscriptMarkdownBlock,
} from '../types';
import { copyText, toRecord } from '../utils';
import clsx from 'clsx';
import { ArrowUp, ChevronDown, Loader2, Search, Timer, X } from 'lucide-react';
import { type CSSProperties, type ReactNode, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { SessionDetailDeltaFramesContext } from './sessionDetailData';
import { formatSessionDuration, localizedTranscriptFilterOptions, sessionEventThreadId } from './sessionDetailModel';
import { ApprovalChip, OutcomeStatusChip, SynchronizedShimmerText } from './sessionTimeline';
import {
  buildSessionTraceEntries,
  compactSessionEventId,
  compareSessionEvents,
  isSafeTranscriptMarkdownHref,
  parseTranscriptCode,
  parseTranscriptMarkdownBlocks,
  prettyCode,
  sessionEventDebugJson,
  sessionEventErrorMessage,
  sessionEventIsThinking,
  sessionEventStructuredContentText,
  sessionEventTimestamp,
  sessionEventTranscriptText,
  sessionEventType,
  sessionIsToolResultEvent,
  sessionOutcomeDescription,
  sessionResultText,
  sessionStatusDescription,
  sessionSubagentThreadId,
  sessionThinkingLabel,
  sessionThinkingText,
  sessionToolLifecycle,
  sessionToolResultText,
  sessionToolUseCodeLanguage,
  sessionToolUseInput,
  sessionTraceDetailTitle,
  sessionTraceFilterValue,
  sessionTraceTextIsJson,
} from './sessionTraceModel';
import {
  compactSubagentThreadId,
  sessionDebugBadge,
  sessionInlineRowPreview,
  sessionOutcomeStatus,
  sessionSubagentDirection,
  sessionSubagentThreadRef,
  sessionToolBatchSummary,
} from './sessionTraceRows';

export function SessionTracePanel({
  events,
  loading,
  error,
  sessionStartedAt,
}: {
  events: QuickstartSessionEvent[];
  loading: boolean;
  error: string | null;
  sessionStartedAt?: string;
}) {
  const { msg } = useI18n();
  const [view, setView] = useState<SessionTraceView>('transcript');
  const [query, setQuery] = useState('');
  const [selectedTypes, setSelectedTypes] = useState<string[]>([]);
  const [selectedEntryId, setSelectedEntryId] = useState<string | null>(null);
  const scrollerRef = useRef<HTMLDivElement | null>(null);
  const lastEntryCountRef = useRef(0);
  const sortedEvents = useMemo(() => [...events].sort(compareSessionEvents), [events]);
  const traceStartMs = useMemo(() => {
    const sessionStart = typeof sessionStartedAt === 'string' ? Date.parse(sessionStartedAt) : NaN;
    if (Number.isFinite(sessionStart)) {
      return sessionStart;
    }
    return sortedEvents.map(sessionEventTimestamp).find(Boolean) ?? 0;
  }, [sessionStartedAt, sortedEvents]);
  const entries = useMemo(
    () => buildSessionTraceEntries(sortedEvents, view, traceStartMs),
    [sortedEvents, view, traceStartMs],
  );
  const filterOptions = useMemo<SessionTraceFilterOption[]>(() => {
    if (view === 'transcript') {
      return localizedTranscriptFilterOptions(msg);
    }
    const seen = new Set<string>();
    return entries
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
  }, [entries, msg, view]);
  const filteredEntries = useMemo(() => {
    const selected = new Set(selectedTypes);
    const needle = query.trim().toLowerCase();
    return entries.filter((entry) => {
      const matchesType = selected.size === 0 || selected.has(sessionTraceFilterValue(entry, view));
      const matchesQuery = !needle || entry.searchText.includes(needle);
      return matchesType && matchesQuery;
    });
  }, [entries, query, selectedTypes, view]);
  const selectedEntry = filteredEntries.find((entry) => entry.id === selectedEntryId) ?? null;
  const hasFilter = query.trim().length > 0 || selectedTypes.length > 0;

  useEffect(() => {
    setSelectedTypes([]);
    setQuery('');
  }, [view]);

  useEffect(() => {
    if (selectedEntryId && !filteredEntries.some((entry) => entry.id === selectedEntryId)) {
      setSelectedEntryId(null);
    }
  }, [filteredEntries, selectedEntryId]);

  useEffect(() => {
    const scroller = scrollerRef.current;
    if (!scroller) {
      return;
    }
    const previousCount = lastEntryCountRef.current;
    lastEntryCountRef.current = filteredEntries.length;
    if (!filteredEntries.length) {
      return;
    }
    const distanceFromBottom = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight;
    const shouldFollow = filteredEntries.length > previousCount || distanceFromBottom < 96;
    if (!shouldFollow) {
      return;
    }
    scroller.scrollTop = scroller.scrollHeight;
  }, [filteredEntries.length]);

  return (
    <div className="relative flex min-h-0 flex-1 flex-col bg-secondary">
      <div className="flex shrink-0 items-center justify-between gap-3 px-4 pb-2 pt-2">
        <div className="flex min-w-0 items-center gap-2">
          <SessionTraceViewMode value={view} onChange={setView} />
          <div className="h-5 w-px bg-accent" aria-hidden />
          <SessionEventTypeFilter
            options={filterOptions}
            selectedTypes={selectedTypes}
            view={view}
            onChange={setSelectedTypes}
          />
          <SessionTraceSearch value={query} onChange={setQuery} />
        </div>
      </div>
      <div className="relative min-h-0 flex-1 overflow-hidden border-t border-border">
        <div ref={scrollerRef} className="subtle-scrollbar absolute inset-0 overflow-auto px-8 pt-2">
          {loading && !events.length ? (
            <SessionTraceSkeleton />
          ) : error && !events.length ? (
            <SessionTraceEmpty message={error} danger />
          ) : filteredEntries.length ? (
            <div className="flex flex-col pb-8">
              {filteredEntries.map((entry) => (
                <SessionTraceRow
                  key={entry.id}
                  entry={entry}
                  selected={entry.id === selectedEntryId}
                  onSelect={() => setSelectedEntryId(entry.id)}
                />
              ))}
            </div>
          ) : (
            <SessionTraceEmpty
              message={
                entries.length === 0
                  ? msg(
                      'managedAgents.sessions.trace.noEvents',
                      'No events yet. Events will appear here as they occur.',
                    )
                  : msg('managedAgents.sessions.trace.noMatchingEvents', 'No events match the current filters.')
              }
              onClear={
                hasFilter
                  ? () => {
                      setSelectedTypes([]);
                      setQuery('');
                    }
                  : undefined
              }
            />
          )}
        </div>
        {selectedEntry ? (
          <SessionTraceDetail entry={selectedEntry} view={view} onClose={() => setSelectedEntryId(null)} />
        ) : null}
      </div>
    </div>
  );
}

export function SessionTraceViewMode({
  value,
  onChange,
}: {
  value: SessionTraceView;
  onChange: (value: SessionTraceView) => void;
}) {
  const { msg } = useI18n();
  const label = msg('managedAgents.sessions.trace.viewMode', 'View mode');
  return (
    <Tabs value={value} className="gap-0" onValueChange={(nextValue) => onChange(nextValue as SessionTraceView)}>
      <TabsList aria-label={label} className="h-7 rounded-full bg-accent p-0.5">
        {(['transcript', 'debug'] as const).map((item) => (
          <TabsTrigger
            key={item}
            value={item}
            className="h-6 flex-none rounded-full border-transparent bg-transparent px-3 text-sm font-medium text-muted-foreground shadow-none after:hidden data-active:bg-card data-active:text-foreground"
          >
            {item === 'transcript'
              ? msg('managedAgents.sessions.trace.transcript', 'Transcript')
              : msg('managedAgents.sessions.trace.debug', 'Debug')}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  );
}

function toggleSessionFilterOption({
  checked,
  option,
  options,
  selectedTypes,
  onChange,
}: {
  checked: boolean;
  option: string;
  options: SessionTraceFilterOption[];
  selectedTypes: string[];
  onChange: (value: string[]) => void;
}) {
  const nextTypes = checked ? selectedTypes.filter((item) => item !== option) : [...selectedTypes, option];
  onChange(nextTypes.length === 0 || nextTypes.length === options.length ? [] : nextTypes);
}

export function SessionEventTypeFilter({
  options,
  selectedTypes,
  view,
  onChange,
}: {
  options: SessionTraceFilterOption[];
  selectedTypes: string[];
  view: SessionTraceView;
  onChange: (value: string[]) => void;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const selectedSet = useMemo(() => new Set(selectedTypes), [selectedTypes]);
  const allSelected = selectedTypes.length === 0 || selectedTypes.length === options.length;
  const label = allSelected
    ? msg('managedAgents.sessions.trace.allEvents', 'All events')
    : msg('managedAgents.common.selectedCount', '{count} selected', { count: selectedTypes.length });
  const showAllEventsFirst = view === 'transcript';

  useEffect(() => {
    setOpen(false);
  }, [view]);

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="secondary"
            size="sm"
            aria-label={label}
            disabled={options.length === 0}
            className="gap-2 bg-accent text-foreground hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
          />
        }
      >
        {label}
        <ChevronDown className="size-3.5 text-muted-foreground" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" sideOffset={6} className="max-h-72 w-80 overflow-auto bg-popover">
        {showAllEventsFirst ? (
          <DropdownMenuCheckboxItem
            checked={allSelected}
            className="h-8 px-2 text-left text-sm text-foreground"
            onCheckedChange={() => onChange([])}
          >
            <span className="min-w-0 flex-1 truncate">
              {msg('managedAgents.sessions.trace.allEvents', 'All events')}
            </span>
          </DropdownMenuCheckboxItem>
        ) : null}
        {options.map((option) => {
          const checked = selectedSet.has(option.value);
          return (
            <DropdownMenuCheckboxItem
              key={option.value}
              checked={checked}
              className="h-8 px-2 text-left text-sm text-foreground"
              onCheckedChange={() =>
                toggleSessionFilterOption({
                  checked,
                  option: option.value,
                  options,
                  selectedTypes,
                  onChange,
                })
              }
            >
              <span className={clsx('min-w-0 flex-1 truncate', view === 'debug' && 'font-mono text-[12px]')}>
                {option.label}
              </span>
            </DropdownMenuCheckboxItem>
          );
        })}
        {!showAllEventsFirst ? (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuCheckboxItem
              checked={allSelected}
              className="h-8 px-2 text-sm font-semibold text-foreground"
              onCheckedChange={() => onChange([])}
            >
              {msg('managedAgents.sessions.trace.selectAll', 'Select all')}
            </DropdownMenuCheckboxItem>
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function SessionTraceSearch({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const { msg } = useI18n();
  const [focused, setFocused] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const expanded = focused || value.length > 0;
  const openSearch = () => {
    setFocused(true);
    inputRef.current?.focus({ preventScroll: true });
  };

  return (
    <div
      role={expanded ? undefined : 'button'}
      tabIndex={expanded ? undefined : 0}
      aria-label={expanded ? undefined : msg('managedAgents.sessions.trace.openSearchFilter', 'Open search filter')}
      className={clsx(
        'relative flex h-7 shrink-0 items-center overflow-hidden rounded-md transition-[width,background-color,box-shadow]',
        expanded
          ? 'w-56 bg-secondary ring-1 ring-border'
          : 'w-7 cursor-pointer text-muted-foreground hover:bg-accent hover:text-foreground',
      )}
      onClick={expanded ? undefined : openSearch}
      onKeyDown={(event) => {
        if (!expanded && (event.key === 'Enter' || event.key === ' ')) {
          event.preventDefault();
          openSearch();
        }
      }}
    >
      <span className="grid size-7 shrink-0 place-items-center">
        <Search className="size-4" aria-hidden />
      </span>
      <Input
        ref={inputRef}
        aria-label={msg('managedAgents.sessions.trace.filterEvents', 'Filter events')}
        value={value}
        placeholder={msg('managedAgents.sessions.trace.filterEvents', 'Filter events')}
        tabIndex={expanded ? 0 : -1}
        aria-hidden={!expanded}
        className={clsx(
          'h-7 min-w-0 flex-1 rounded-none border-0 bg-transparent px-0 pr-2 text-sm placeholder:text-muted-foreground focus-visible:ring-0',
          !expanded && 'pointer-events-none opacity-0',
        )}
        onFocus={() => setFocused(true)}
        onBlur={() => {
          if (!value) {
            setFocused(false);
          }
        }}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Escape') {
            event.stopPropagation();
            onChange('');
            setFocused(false);
            inputRef.current?.blur();
          }
        }}
      />
      {expanded && value ? (
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={msg('managedAgents.sessions.trace.clearFilter', 'Clear filter')}
          className="size-7 shrink-0 text-muted-foreground hover:bg-transparent hover:text-foreground"
          onClick={() => {
            onChange('');
            setFocused(false);
            inputRef.current?.blur();
          }}
        >
          <X className="size-3.5" aria-hidden />
        </Button>
      ) : null}
    </div>
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

export function SessionTraceRow({
  entry,
  selected,
  onSelect,
}: {
  entry: SessionTraceEntry;
  selected: boolean;
  onSelect: () => void;
}) {
  const { msg } = useI18n();
  const title = sessionInlineRowPreview(entry.preview || entry.label);
  const preview = '';
  return (
    <Button
      type="button"
      variant="ghost"
      data-event-id={entry.id}
      aria-label={[entry.label, title, preview, entry.relativeTime].filter(Boolean).join(' ')}
      className={clsx(
        '-mx-8 h-9 w-[calc(100%+4rem)] justify-start gap-2 overflow-hidden rounded-none border-0 bg-transparent px-8 text-left font-normal active:translate-y-0',
        selected ? 'bg-accent outline outline-2 -outline-offset-2 outline-ring' : 'hover:bg-accent',
        entry.isError && 'bg-destructive/10',
      )}
      onClick={onSelect}
    >
      <SessionEventBadge family={entry.family} label={sessionEventBadgeName(entry, msg)} />
      <span
        className={clsx('min-w-0 truncate text-sm leading-5', entry.isError ? 'text-destructive' : 'text-foreground')}
      >
        {title}
      </span>
      {preview ? (
        <span className="min-w-0 flex-1 truncate text-sm text-muted-foreground">{preview}</span>
      ) : (
        <span className="flex-1" />
      )}
      <time className="shrink-0 font-mono text-xs tabular-nums text-muted-foreground">{entry.relativeTime}</time>
    </Button>
  );
}

export function SessionEventBadge({ family, label }: { family: SessionTraceFamily; label?: string }) {
  return <EventTypeBadge type={sessionFamilyBadgeType(family)} label={label} />;
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
      style={config.style}
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
  style?: CSSProperties;
} {
  const family = sessionBadgeFamily(type);
  const label = sessionBadgeTypeLabel(type, msg);
  switch (family) {
    case 'user':
      return { label, className: 'text-white', style: { backgroundColor: '#c46686' } };
    case 'agent':
      return { label, className: 'bg-accent/80 text-accent-foreground' };
    case 'subagent':
      return { label, className: 'text-white', style: { backgroundColor: '#629987' } };
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

export function sessionFamilyBadgeType(family: SessionTraceFamily): DisplayEventType {
  switch (family) {
    case 'user':
      return 'user';
    case 'agent':
      return 'agent';
    case 'subagent':
      return 'subagent';
    case 'tool_use':
      return 'tool_use';
    case 'tool_result':
    case 'result':
      return 'result';
    case 'model':
      return 'model_request';
    case 'outcome':
      return 'outcome';
    case 'thread':
      return 'thread';
    case 'status':
      return 'root';
    case 'error':
      return 'error';
    case 'system':
    case 'env':
    case 'span':
    default:
      return 'unknown';
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

export function sessionEventBadgeName(entry: SessionTraceEntry, msg?: I18nMsg) {
  if (sessionEventIsThinking(entry.event)) {
    return sessionThinkingLabel(msg);
  }
  return undefined;
}

export function SessionTraceDetail({
  entry,
  view,
  placement = 'overlay',
  onClose,
}: {
  entry: SessionTraceEntry;
  view: SessionTraceView;
  placement?: 'overlay' | 'side';
  onClose: () => void;
}) {
  const { msg } = useI18n();
  const title = sessionTraceDetailTitle(entry);
  const eventIdLabel = compactSessionEventId(entry.rawEventId);
  return (
    <div
      className={clsx(
        'relative flex flex-col overflow-hidden',
        placement === 'overlay'
          ? 'absolute inset-0 z-10 bg-secondary'
          : 'border-t border-border bg-transparent lg:max-h-[calc(100vh-330px)] lg:border-l lg:border-t-0',
      )}
      data-placement={placement}
      data-testid="session-trace-detail"
    >
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={msg('managedAgents.sessions.trace.closeDetailPanel', 'Close detail panel')}
        className="absolute right-3 top-3 z-20 text-muted-foreground hover:bg-accent hover:text-foreground"
        onClick={onClose}
      >
        <X className="size-4" aria-hidden />
      </Button>
      <div className="shrink-0 border-b border-border pb-4 pl-6 pr-12 pt-3">
        <div className="flex h-6 items-center gap-2">
          <SessionEventBadge family={entry.family} label={sessionEventBadgeName(entry, msg)} />
          <h2 className={clsx('truncate text-sm font-medium', entry.isError ? 'text-destructive' : 'text-foreground')}>
            {title}
          </h2>
        </div>
        <div className="mt-1 flex h-5 items-center gap-2 text-xs text-muted-foreground">
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="h-auto p-0 font-mono text-xs font-normal text-muted-foreground hover:bg-transparent hover:text-foreground"
            aria-label={`Copy ${entry.rawEventId}`}
            onClick={() => void copyText(entry.rawEventId)}
          >
            {eventIdLabel}
          </Button>
          <span aria-hidden>·</span>
          <time className="font-mono tabular-nums">{entry.relativeTime}</time>
        </div>
      </div>
      <div className="subtle-scrollbar min-h-0 flex-1 overflow-auto pb-8">
        {view === 'debug' ? (
          <DebugEventDetail event={entry.event} type={entry.type} />
        ) : (
          <TranscriptEventDetail entry={entry} />
        )}
      </div>
    </div>
  );
}

export function EventDetailPanel({
  entry,
  view,
  detailTab = 'content',
  placement = 'side',
  onClose,
  onDetailTabChange,
}: {
  entry: SessionEventListEntry;
  view: SessionTraceView;
  detailTab?: SessionDebugDetailTab;
  placement?: 'overlay' | 'side';
  onClose: () => void;
  onDetailTabChange?: (tab: SessionDebugDetailTab) => void;
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
        placement === 'overlay'
          ? 'absolute inset-0 z-10 bg-secondary'
          : 'border-t border-border bg-transparent lg:max-h-[calc(100vh-330px)] lg:border-l lg:border-t-0',
      )}
      data-placement={placement}
      data-testid="session-trace-detail"
    >
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={msg('managedAgents.sessions.trace.closeDetailPanel', 'Close detail panel')}
        className="absolute right-3 top-3 z-20 text-muted-foreground hover:bg-accent hover:text-foreground"
        onClick={onClose}
      >
        <X className="size-4" aria-hidden />
      </Button>
      <div className="shrink-0 border-b border-border pb-4 pl-6 pr-12 pt-3">
        <div className="flex h-6 items-center gap-2">
          <EventTypeBadge
            type={entry.displayEvent.type}
            label={isDebug ? sessionDebugBadge(entry.type) : eventDetailBadge(entry, msg)}
            title={isDebug ? entry.type : undefined}
            className={isDebug ? 'font-mono' : undefined}
          />
          <h2 className={clsx('truncate text-sm font-medium', entry.isError ? 'text-destructive' : 'text-foreground')}>
            {isDebug ? entry.displayEvent.label || title : title}
          </h2>
        </div>
        <div className="mt-1 flex h-5 items-center gap-2 text-xs text-muted-foreground">
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="h-auto p-0 font-mono text-xs font-normal text-muted-foreground hover:bg-transparent hover:text-foreground"
            aria-label={`Copy ${entry.rawEventId}`}
            onClick={() => void copyText(entry.rawEventId)}
          >
            {eventIdLabel}
          </Button>
          <span aria-hidden>·</span>
          <time className="font-mono tabular-nums">{entry.relativeTime}</time>
        </div>
      </div>
      <div className="subtle-scrollbar min-h-0 flex-1 overflow-auto pb-8">
        {isDebug ? (
          <DebugDetailPanel entry={entry} tab={detailTab} onTabChange={onDetailTabChange} />
        ) : entry.kind === 'tool_batch' ? (
          <BatchDetailPanel entry={entry} />
        ) : (
          <EventDetailContent entry={entry} />
        )}
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
    return <ToolBatchEventDetail entry={entry} />;
  }
  if (entry.kind === 'tool_call') {
    return <ToolUseEventDetail entry={entry} />;
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

export function ToolUseEventDetail({ entry }: { entry: ToolCallEntry }) {
  return <ToolCallDetailContent entry={entry} />;
}

export function ToolBatchEventDetail({ entry }: { entry: ToolBatchEntry }) {
  return <BatchDetailPanel entry={entry} />;
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
  tab,
  onTabChange,
}: {
  entry: Exclude<SessionEventListEntry, IdleGapEntry | QueuedBoundaryEntry>;
  tab: SessionDebugDetailTab;
  onTabChange?: (tab: SessionDebugDetailTab) => void;
}) {
  const { msg } = useI18n();
  const deltaFrames = useContext(SessionDetailDeltaFramesContext);
  const frame = deltaFrames[entry.displayEvent.id];
  const canShowDeltas = entry.type === 'agent.message' || entry.type === 'agent.thinking';
  const activeTab: SessionDebugDetailTab = canShowDeltas ? tab : 'content';
  const contentEvent = frame?.message ?? entry.event;
  return (
    <div>
      {canShowDeltas ? (
        <div className="flex gap-1 border-b border-border px-5 py-2">
          <Button
            type="button"
            variant="ghost"
            className={clsx(
              'h-auto rounded-md px-2 py-1 text-xs font-medium',
              activeTab === 'content'
                ? 'bg-accent text-foreground'
                : 'text-muted-foreground hover:bg-accent hover:text-foreground',
            )}
            onClick={() => onTabChange?.('content')}
          >
            {msg('managedAgents.sessions.trace.content', 'Content')}
          </Button>
          <Button
            type="button"
            variant="ghost"
            className={clsx(
              'h-auto rounded-md px-2 py-1 text-xs font-medium',
              activeTab === 'deltas'
                ? 'bg-accent text-foreground'
                : 'text-muted-foreground hover:bg-accent hover:text-foreground',
            )}
            onClick={() => onTabChange?.('deltas')}
          >
            {msg('managedAgents.sessions.trace.deltas', 'Deltas')}
          </Button>
        </div>
      ) : null}
      {activeTab === 'deltas' ? (
        <DebugDeltasDetail frames={frame?.frames ?? []} />
      ) : (
        <DebugEventDetail event={contentEvent} type={entry.type} />
      )}
    </div>
  );
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

export function eventDetailBadge(
  entry: Exclude<SessionEventListEntry, IdleGapEntry | QueuedBoundaryEntry>,
  msg: I18nMsg,
) {
  if (entry.kind === 'tool_batch') {
    return msg('managedAgents.sessions.trace.toolBatch', 'Tools');
  }
  return undefined;
}

export function TranscriptEventDetail({ entry }: { entry: SessionTraceEntry }) {
  const { msg } = useI18n();
  if (sessionEventIsThinking(entry.event)) {
    const thinkingText = sessionThinkingText(entry.event);
    return (
      <div className="px-5 py-4">
        <div className="mb-2 text-xs text-muted-foreground">
          {msg('managedAgents.sessions.trace.content', 'Content')}
        </div>
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

  if (entry.family === 'tool_use') {
    return (
      <div className="space-y-6 px-5 py-4">
        <ApprovalChip lifecycle={sessionToolLifecycle(entry.event, entry.resultEvent, entry.confirmationEvent)} />
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

  if (entry.family === 'tool_result') {
    return (
      <div className="px-5 py-4">
        <ToolResultSection event={entry.event} />
      </div>
    );
  }

  if (entry.family === 'status') {
    return (
      <p className="px-5 py-4 text-sm text-muted-foreground">
        {sessionStatusDescription(entry.type, entry.event) ?? entry.preview}
      </p>
    );
  }

  if (entry.family === 'error') {
    return (
      <div className="px-5 py-4">
        <pre className="whitespace-pre-wrap break-words rounded-md border border-destructive/50 bg-destructive/10 p-3 font-mono text-xs text-destructive">
          {sessionEventErrorMessage(entry.event)}
        </pre>
      </div>
    );
  }

  return (
    <div className="px-5 py-4">
      <div className="mb-2 text-xs text-muted-foreground">{msg('managedAgents.sessions.trace.content', 'Content')}</div>
      {entry.displayText || entry.preview ? (
        <TranscriptTypedContent entry={entry} value={entry.displayText || entry.preview} />
      ) : (
        <div className="text-xs italic text-muted-foreground">
          {msg('managedAgents.sessions.trace.noContent', 'No content.')}
        </div>
      )}
    </div>
  );
}

export function DebugEventDetail({ event, type }: { event: QuickstartSessionEvent; type: string }) {
  const { msg } = useI18n();
  const debugJson = sessionEventDebugJson(event);
  return (
    <div className="px-5 pb-6 pt-3">
      <div className="mb-2 flex items-center justify-between gap-3">
        <div className="font-mono text-xs text-muted-foreground">{type}</div>
        <CopyButton value={debugJson} label={msg('managedAgents.quickstart.copyCode', 'Copy code')} />
      </div>
      <SyntaxCodeBlock value={debugJson} language="json" />
    </div>
  );
}

export function DebugDeltasDetail({ frames }: { frames: QuickstartSessionEvent[] }) {
  const { msg } = useI18n();
  const debugJson = JSON.stringify(frames, null, 2);
  return (
    <div className="px-5 pb-6 pt-3">
      <div className="mb-2 flex items-center justify-between gap-3">
        <div className="font-mono text-xs text-muted-foreground">
          {msg(
            'managedAgents.sessions.trace.deltaFrameCount',
            '{count, plural, one {# delta frame} other {# delta frames}}',
            { count: frames.length },
          )}
        </div>
        <CopyButton value={debugJson} label={msg('managedAgents.quickstart.copyCode', 'Copy code')} />
      </div>
      {frames.length ? (
        <SyntaxCodeBlock value={debugJson} language="json" />
      ) : (
        <div className="text-xs italic text-muted-foreground">
          {msg('managedAgents.sessions.trace.noDeltas', 'No deltas captured.')}
        </div>
      )}
    </div>
  );
}

export function TranscriptContent({ value }: { value: string }) {
  const code = parseTranscriptCode(value);
  if (code) {
    return <SyntaxCodeBlock value={code.value} language={code.language} />;
  }

  return <MarkdownTranscriptContent value={value} />;
}

export function MarkdownTranscriptContent({ value }: { value: string }) {
  const blocks = parseTranscriptMarkdownBlocks(value);
  return (
    <div data-testid="session-trace-markdown" className="space-y-4 text-sm leading-relaxed text-foreground">
      {blocks.map((block, index) => renderTranscriptMarkdownBlock(block, index))}
    </div>
  );
}

export function renderTranscriptMarkdownBlock(block: TranscriptMarkdownBlock, index: number) {
  switch (block.type) {
    case 'heading': {
      const HeadingTag = block.level <= 2 ? 'h3' : 'h4';
      return (
        <HeadingTag key={index} className="text-base font-semibold leading-6 text-foreground">
          {renderTranscriptMarkdownInline(block.text, `heading-${index}`)}
        </HeadingTag>
      );
    }
    case 'list':
      return (
        <ul key={index} className="list-disc space-y-1 pl-5">
          {block.items.map((item, itemIndex) => (
            <li key={itemIndex} className="pl-1">
              {renderTranscriptMarkdownInline(item, `list-${index}-${itemIndex}`)}
            </li>
          ))}
        </ul>
      );
    case 'table':
      return (
        <div key={index} className="subtle-scrollbar overflow-x-auto rounded-md border border-border">
          <table className="min-w-full border-collapse text-left text-sm">
            <thead className="bg-secondary">
              <tr>
                {block.headers.map((header, headerIndex) => (
                  <th
                    key={headerIndex}
                    scope="col"
                    className="border-b border-border px-3 py-2 font-semibold text-foreground"
                  >
                    {renderTranscriptMarkdownInline(header, `table-${index}-head-${headerIndex}`)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {block.rows.map((row, rowIndex) => (
                <tr key={rowIndex} className="border-t border-border first:border-t-0">
                  {block.headers.map((_, cellIndex) => (
                    <td key={cellIndex} className="align-top px-3 py-2 text-foreground">
                      {renderTranscriptMarkdownInline(row[cellIndex] ?? '', `table-${index}-${rowIndex}-${cellIndex}`)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    case 'code':
      return <SyntaxCodeBlock key={index} value={block.value} language={block.language} />;
    default:
      return (
        <p key={index} className="whitespace-pre-wrap break-words text-sm leading-relaxed text-foreground">
          {renderTranscriptMarkdownInline(block.text, `paragraph-${index}`)}
        </p>
      );
  }
}

export function renderTranscriptMarkdownInline(text: string, keyPrefix: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  const pattern = /(`[^`]+`|\*\*[\s\S]+?\*\*|\[[^\]]+\]\([^)]+\))/g;
  let lastIndex = 0;
  let match: RegExpExecArray | null;
  let index = 0;
  while ((match = pattern.exec(text)) !== null) {
    if (match.index > lastIndex) {
      nodes.push(text.slice(lastIndex, match.index));
    }
    const token = match[0];
    const key = `${keyPrefix}-${index}`;
    if (token.startsWith('`') && token.endsWith('`')) {
      nodes.push(
        <code
          key={key}
          className="rounded border border-border bg-secondary px-1 py-0.5 font-mono text-[0.92em] text-foreground"
        >
          {token.slice(1, -1)}
        </code>,
      );
    } else if (token.startsWith('**') && token.endsWith('**')) {
      nodes.push(
        <strong key={key} className="font-semibold text-foreground">
          {renderTranscriptMarkdownInline(token.slice(2, -2), `${key}-strong`)}
        </strong>,
      );
    } else {
      const link = token.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
      const href = link?.[2]?.trim() ?? '';
      nodes.push(
        isSafeTranscriptMarkdownHref(href) ? (
          <a
            key={key}
            href={href}
            target="_blank"
            rel="noreferrer"
            className="text-primary underline-offset-2 hover:underline"
          >
            {renderTranscriptMarkdownInline(link?.[1] ?? token, `${key}-link`)}
          </a>
        ) : (
          token
        ),
      );
    }
    lastIndex = match.index + token.length;
    index += 1;
  }
  if (lastIndex < text.length) {
    nodes.push(text.slice(lastIndex));
  }
  return nodes;
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

export function sessionCanonicalDisplayEvent(event: QuickstartSessionEvent): QuickstartSessionEvent {
  const currentType = sessionEventType(event);
  if (!sessionIsToolResultEvent(event) && currentType !== 'event' && currentType !== 'system.message') {
    return event;
  }

  const payload = sessionSerializedCanonicalPayload(event);
  if (!payload) {
    return event;
  }

  return {
    ...payload,
    created_at: payload.created_at ?? event.created_at,
    processed_at: payload.processed_at ?? event.processed_at,
    session_id: payload.session_id ?? event.session_id,
    session_thread_id: payload.session_thread_id ?? event.session_thread_id,
    thread_id: payload.thread_id ?? event.thread_id,
    _wrapped_event_id: event.id,
  };
}

export function sessionSerializedCanonicalPayload(event: QuickstartSessionEvent) {
  const text = sessionSingleTextPayload(event);
  if (!text || text.length > 200000) {
    return null;
  }
  const trimmed = text.trim();
  if (!trimmed.startsWith('{') || !trimmed.endsWith('}')) {
    return null;
  }
  try {
    const parsed = JSON.parse(trimmed) as unknown;
    const record = toRecord(parsed);
    const type = typeof record?.type === 'string' ? record.type : '';
    if (!type || !sessionSerializedTypeIsCanonical(type)) {
      return null;
    }
    return record as QuickstartSessionEvent;
  } catch {
    return null;
  }
}

export function sessionSingleTextPayload(event: QuickstartSessionEvent) {
  if (typeof event.content === 'string') {
    return event.content;
  }
  if (Array.isArray(event.content) && event.content.length === 1) {
    const block = toRecord(event.content[0]);
    if (typeof block?.text === 'string') {
      return block.text;
    }
    if (typeof block?.content === 'string') {
      return block.content;
    }
  }
  if (typeof event.message === 'string') {
    return event.message;
  }
  return '';
}

export function sessionSerializedTypeIsCanonical(type: string) {
  return (
    type.startsWith('session.') ||
    type.startsWith('span.') ||
    type === 'system.message' ||
    type === 'agent.thread_message_received' ||
    type === 'agent.thread_message_sent' ||
    type === 'agent.thread_context_compacted'
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

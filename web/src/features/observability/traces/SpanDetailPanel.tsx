import { Clock, Copy, Tag, X } from 'lucide-react';
import { useMemo, useState, type ReactNode } from 'react';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Button } from '../../../shared/ui/button';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { PRESS_SCALE_CLASS } from '../chrome';
import { formatDurationMs } from '../format';
import type { ObservabilitySpanEvent } from '../types';
import { TraceIOPanels } from './TraceTextPreview';
import { spanDisplayName, spanPreview, type TraceTreeRow } from './traceTree';

export function SpanDetailPanel({
  span,
  color,
  agentName,
  onClose,
}: {
  span: TraceTreeRow;
  color: string;
  agentName: string;
  onClose: () => void;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const [view, setView] = useState<'json' | 'table'>('json');
  const entries = useMemo(
    () => Object.entries(span.attributes).sort(([left], [right]) => left.localeCompare(right)),
    [span.attributes],
  );
  const events = span.events ?? [];
  const preview = useMemo(() => spanPreview(span), [span]);
  const metadataEntries = useMemo(
    () =>
      [
        ['span_id', span.span_id],
        ['parent_span_id', span.parent_span_id],
        ['kind', span.kind],
        ['status', span.status],
        ['start_time', span.start_time],
        ['end_time', span.end_time],
        ['duration_ms', String(span.duration_ms)],
      ] satisfies Array<[string, string]>,
    [span],
  );

  return (
    <aside className="flex h-full min-h-0 w-full min-w-0 flex-col bg-background">
      <div className="flex h-8 shrink-0 items-center gap-2 border-b border-border px-3">
        <p className="min-w-0 flex-1 truncate text-sm font-medium">{spanDisplayName(span)}</p>
        <Button
          type="button"
          size="icon-xs"
          variant="ghost"
          aria-label={msg('observability.trace.close', 'Close')}
          onClick={onClose}
        >
          <X className="size-3.5" aria-hidden />
        </Button>
      </div>
      <div className="flex shrink-0 items-center gap-x-4 gap-y-1 overflow-x-auto border-b border-border px-3 py-1.5">
        <MetricChip
          icon={<span className="size-2.5 rounded-sm" style={{ backgroundColor: color }} />}
          label={msg('observability.column.agent', 'Agent')}
          value={agentName || '—'}
        />
        <MetricChip
          icon={<Clock className="size-3" aria-hidden />}
          label={msg('observability.trace.duration', 'Duration')}
          value={formatDurationMs(span.duration_ms, formatters)}
        />
        <MetricChip
          icon={<Clock className="size-3" aria-hidden />}
          label={msg('observability.trace.offset', 'Start')}
          value={formatDurationMs(span.offsetMs, formatters)}
        />
        <button
          type="button"
          className={cn(
            'inline-flex h-[22px] shrink-0 items-center gap-1 rounded-md px-1 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground',
            PRESS_SCALE_CLASS,
          )}
          title={span.span_id}
          onClick={() => void navigator.clipboard?.writeText(span.span_id)}
        >
          <Tag className="size-3" aria-hidden />
          <span className="max-w-28 truncate font-mono">{span.span_id}</span>
          <Copy className="size-3 opacity-60" aria-hidden />
        </button>
      </div>
      {preview.input || preview.output ? (
        <TraceIOPanels
          input={preview.input}
          output={preview.output}
          className="shrink-0 grid-cols-1 border-b border-border px-3 py-2"
          contentClassName="max-h-32"
        />
      ) : null}
      <Tabs defaultValue="attributes" className="flex min-h-0 flex-1 flex-col gap-0">
        <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border px-3">
          <TabsList variant="line" className="h-9">
            <TabsTrigger value="attributes">{msg('observability.trace.attributes', 'Attributes')}</TabsTrigger>
            {events.length ? (
              <TabsTrigger value="events">{msg('observability.trace.events', 'Events')}</TabsTrigger>
            ) : null}
            <TabsTrigger value="metadata">{msg('observability.trace.metadata', 'Metadata')}</TabsTrigger>
          </TabsList>
          <ViewToggle view={view} onChange={setView} />
        </div>
        <TabsContent value="attributes" className="mt-0 min-h-0 flex-1 overflow-x-hidden overflow-y-auto px-3 py-2">
          <AttributeView view={view} entries={entries} />
        </TabsContent>
        {events.length ? (
          <TabsContent value="events" className="mt-0 min-h-0 flex-1 overflow-x-hidden overflow-y-auto px-3 py-2">
            <div className="flex flex-col gap-3">
              {events.map((event, index) => (
                <SpanEventView key={`${event.name}-${index}`} event={event} view={view} />
              ))}
            </div>
          </TabsContent>
        ) : null}
        <TabsContent value="metadata" className="mt-0 min-h-0 flex-1 overflow-x-hidden overflow-y-auto px-3 py-2">
          <AttributeView view={view} entries={metadataEntries} />
        </TabsContent>
      </Tabs>
    </aside>
  );
}

function SpanEventView({ event, view }: { event: ObservabilitySpanEvent; view: 'json' | 'table' }) {
  const entries = useMemo(
    () => Object.entries(event.attributes).sort(([left], [right]) => left.localeCompare(right)),
    [event.attributes],
  );
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline justify-between gap-2">
        <p className="truncate font-mono text-[11px] font-semibold">{event.name}</p>
        <p className="shrink-0 font-mono text-[10px] text-muted-foreground">{event.timestamp}</p>
      </div>
      <AttributeView view={view} entries={entries} />
    </div>
  );
}

function MetricChip({ icon, label, value }: { icon: ReactNode; label: string; value: string }) {
  return (
    <span className="inline-flex h-[22px] max-w-56 shrink-0 items-center gap-1 text-[11px]" title={`${label} ${value}`}>
      {icon}
      <span className="text-muted-foreground">{label}</span>
      <span className="truncate font-medium tabular-nums">{value}</span>
    </span>
  );
}

function ViewToggle({ view, onChange }: { view: 'json' | 'table'; onChange: (next: 'json' | 'table') => void }) {
  const { msg } = useI18n();
  return (
    <div className="flex rounded-lg border border-border p-0.5">
      <Button
        type="button"
        size="xs"
        variant={view === 'json' ? 'secondary' : 'ghost'}
        onClick={() => onChange('json')}
      >
        {msg('observability.trace.json', 'JSON')}
      </Button>
      <Button
        type="button"
        size="xs"
        variant={view === 'table' ? 'secondary' : 'ghost'}
        onClick={() => onChange('table')}
      >
        {msg('observability.trace.table', 'Table')}
      </Button>
    </div>
  );
}

function AttributeView({ view, entries }: { view: 'json' | 'table'; entries: Array<[string, string]> }) {
  if (view === 'table') {
    if (!entries.length) {
      return <p className="text-xs text-muted-foreground">—</p>;
    }
    return (
      <dl className="border border-border">
        {entries.map(([key, value]) => (
          <div
            key={key}
            className="grid grid-cols-[minmax(0,38%)_minmax(0,62%)] border-b border-border last:border-b-0"
          >
            <dt className="truncate border-r border-border px-2 py-1 font-mono text-[11px] text-muted-foreground">
              {key}
            </dt>
            <dd className="break-all px-2 py-1 font-mono text-[11px]">{value}</dd>
          </div>
        ))}
      </dl>
    );
  }
  return (
    <pre className="max-w-full min-w-0 whitespace-pre-wrap break-all rounded-md bg-muted/40 p-3 font-mono text-[11px] leading-5">
      <span className="text-muted-foreground">{'{'}</span>
      {entries.map(([key, value], index) => (
        <span key={key}>
          {'\n  '}
          <span className="text-muted-foreground">"{key}"</span>
          {': '}
          <span className="text-foreground">"{value}"</span>
          {index < entries.length - 1 ? <span className="text-muted-foreground">,</span> : ''}
        </span>
      ))}
      {entries.length ? '\n' : ''}
      <span className="text-muted-foreground">{'}'}</span>
    </pre>
  );
}

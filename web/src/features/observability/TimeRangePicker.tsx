import { Calendar as CalendarIcon, Check, ChevronDown } from 'lucide-react';
import { useId, useRef, useState, type Ref } from 'react';
import { enUS, zhCN } from 'react-day-picker/locale';
import type { DateRange } from 'react-day-picker';
import { useI18n, useLocale } from '../../shared/i18n';
import { cn } from '../../shared/lib/utils';
import { Button } from '../../shared/ui/button';
import { Calendar } from '../../shared/ui/calendar';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../shared/ui/collapsible';
import { Field, FieldError, FieldLabel } from '../../shared/ui/field';
import { Input } from '../../shared/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '../../shared/ui/popover';
import { Separator } from '../../shared/ui/separator';
import { formatTimeRangeAbsolute } from './format';
import {
  customRangeFieldErrors,
  filtersForPreset,
  formatDateTimeInput,
  parseDateTimeInput,
  replaceDateKeepingTime,
  TIME_PRESETS,
  utcOffsetLabel,
  type ObservabilityFilters,
} from './model';

const DATE_TIME_PLACEHOLDER = 'YYYY-MM-DD HH:mm:ss';

export function TimeRangePicker({
  filters,
  onChange,
}: {
  filters: ObservabilityFilters;
  onChange: (next: ObservabilityFilters) => void;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const [customOpen, setCustomOpen] = useState(false);
  const preset = TIME_PRESETS.find((item) => item.id === filters.preset);
  const presetLabel =
    filters.preset === 'custom' || !preset
      ? msg('observability.filter.custom', 'Custom')
      : msg(preset.labelId, preset.fallback);
  const absolute = filters.preset === 'custom' ? formatTimeRangeAbsolute(filters.start, filters.end) : null;
  const timeLabel = msg('observability.filter.time', 'Time range');

  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (nextOpen) {
          setCustomOpen(filters.preset === 'custom');
        }
      }}
    >
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="sm"
            aria-label={absolute ? `${timeLabel}, ${presetLabel}, ${absolute}` : `${timeLabel}, ${presetLabel}`}
            className="max-w-[min(32rem,calc(100vw-5rem))] min-w-0 justify-start gap-2 px-2.5 font-normal transition-[color,background-color,border-color,box-shadow,transform] duration-150 active:scale-[0.96]"
          />
        }
      >
        <CalendarIcon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
        <span className="shrink-0 font-medium">{presetLabel}</span>
        {absolute ? (
          <>
            <span className="h-3.5 w-px shrink-0 bg-border" aria-hidden />
            <span className="min-w-0 flex-1 truncate font-mono text-xs tabular-nums text-muted-foreground">
              {absolute}
            </span>
          </>
        ) : null}
        <ChevronDown
          className={cn(
            'size-3.5 shrink-0 text-muted-foreground transition-transform duration-150',
            open && 'rotate-180',
          )}
          aria-hidden
        />
      </PopoverTrigger>
      <PopoverContent align="end" sideOffset={6} className="w-[min(22rem,calc(100vw-1rem))] gap-0 overflow-hidden p-0">
        <Collapsible open={customOpen} onOpenChange={setCustomOpen}>
          <PresetList
            active={filters.preset}
            customOpen={customOpen}
            onSelect={(presetId) => {
              onChange({ ...filters, ...filtersForPreset(presetId) });
              setOpen(false);
            }}
          />
          <CollapsibleContent className="h-[var(--collapsible-panel-height)] border-t border-border opacity-100 transition-[height,opacity] duration-200 ease-out data-[ending-style]:h-0 data-[ending-style]:opacity-0 data-[starting-style]:h-0 data-[starting-style]:opacity-0 motion-reduce:transition-none">
            <CustomRangePanel
              key={`${filters.start}|${filters.end}`}
              filters={filters}
              onApply={(next) => {
                onChange(next);
                setOpen(false);
              }}
            />
          </CollapsibleContent>
        </Collapsible>
      </PopoverContent>
    </Popover>
  );
}

function PresetList({
  active,
  customOpen,
  onSelect,
}: {
  active: ObservabilityFilters['preset'];
  customOpen: boolean;
  onSelect: (preset: (typeof TIME_PRESETS)[number]['id']) => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="p-3">
      <p className="text-xs font-medium text-muted-foreground">
        {msg('observability.filter.quickRanges', 'Quick ranges')}
      </p>
      <div className="mt-2 grid grid-cols-2 gap-1.5 sm:grid-cols-3">
        {TIME_PRESETS.map((item) => {
          const selected = active === item.id;
          return (
            <Button
              key={item.id}
              type="button"
              variant={selected ? 'secondary' : 'ghost'}
              size="sm"
              className="min-w-0 justify-between px-2.5 font-normal transition-[color,background-color,transform] duration-150 active:scale-[0.96]"
              aria-pressed={selected}
              onClick={() => onSelect(item.id)}
            >
              <span className="truncate">{msg(item.labelId, item.fallback)}</span>
              {selected ? <Check className="size-3.5 shrink-0" aria-hidden /> : null}
            </Button>
          );
        })}
      </div>
      <Separator className="my-2.5" />
      <CollapsibleTrigger
        render={
          <Button
            type="button"
            variant={customOpen || active === 'custom' ? 'secondary' : 'ghost'}
            size="sm"
            className="w-full justify-start gap-2 px-2.5 font-normal transition-[color,background-color,transform] duration-150 active:scale-[0.96]"
          />
        }
      >
        <CalendarIcon className="size-3.5 text-muted-foreground" aria-hidden />
        <span>{msg('observability.filter.custom', 'Custom')}</span>
        <ChevronDown
          className={cn(
            'ml-auto size-3.5 text-muted-foreground transition-transform duration-150',
            customOpen && 'rotate-180',
          )}
          aria-hidden
        />
      </CollapsibleTrigger>
    </div>
  );
}

function CustomRangePanel({
  filters,
  onApply,
}: {
  filters: ObservabilityFilters;
  onApply: (next: ObservabilityFilters) => void;
}) {
  const { msg } = useI18n();
  const { locale } = useLocale();
  const startId = useId();
  const endId = useId();
  const startErrorId = useId();
  const endErrorId = useId();
  const startRef = useRef<HTMLInputElement>(null);
  const endRef = useRef<HTMLInputElement>(null);
  const [startText, setStartText] = useState(() => formatDateTimeInput(filters.start));
  const [endText, setEndText] = useState(() => formatDateTimeInput(filters.end));
  const [attempted, setAttempted] = useState(false);
  const start = parseDateTimeInput(startText);
  const end = parseDateTimeInput(endText);
  const messages = {
    format: msg('observability.filter.invalidFormat', 'Use format YYYY-MM-DD HH:mm:ss.'),
    range: msg('observability.filter.invalidRange', 'End must be after start, within 30 days.'),
  };
  const errors = customRangeFieldErrors(start, end, messages);
  const shown = attempted ? errors : {};

  const selectedRange: DateRange | undefined = start
    ? { from: startOfDay(start), to: end && end >= start ? startOfDay(end) : undefined }
    : undefined;

  const onCalendarSelect = (next: DateRange | undefined) => {
    if (!next?.from) {
      setStartText('');
      setEndText('');
      return;
    }
    setStartText(replaceDateKeepingTime(startText, next.from, '00:00:00'));
    setEndText(replaceDateKeepingTime(endText, next.to ?? next.from, '23:59:59'));
  };

  const apply = () => {
    setAttempted(true);
    if (errors.start) {
      startRef.current?.focus();
      return;
    }
    if (errors.end) {
      endRef.current?.focus();
      return;
    }
    if (!start || !end) {
      return;
    }
    onApply({ ...filters, preset: 'custom', start: start.toISOString(), end: end.toISOString() });
  };

  return (
    <div className="flex max-h-[min(28rem,calc(100dvh-10rem))] flex-col">
      <div className="flex min-h-0 flex-1 flex-col gap-2.5 overflow-y-auto p-2.5">
        <p className="text-xs text-muted-foreground">
          {msg('observability.filter.localTime', 'Shown in local time · {offset}', { offset: utcOffsetLabel() })}
        </p>
        <Calendar
          mode="range"
          captionLayout="dropdown"
          selected={selectedRange}
          onSelect={onCalendarSelect}
          defaultMonth={selectedRange?.from}
          disabled={{ after: new Date() }}
          locale={locale === 'zh-CN' ? zhCN : enUS}
          numberOfMonths={1}
          className="mx-auto w-[80%] p-0 [--cell-size:--spacing(8)]"
          classNames={{
            root: 'w-full',
            months: 'relative flex w-full flex-col',
            month: 'flex w-full flex-col gap-2',
          }}
        />
        <Separator />
        <div className="grid gap-1.5">
          <RangeField
            id={startId}
            errorId={startErrorId}
            inputRef={startRef}
            label={msg('observability.filter.start', 'Start')}
            value={startText}
            error={shown.start}
            onChange={setStartText}
            onEnter={apply}
          />
          <RangeField
            id={endId}
            errorId={endErrorId}
            inputRef={endRef}
            label={msg('observability.filter.end', 'End')}
            value={endText}
            error={shown.end}
            onChange={setEndText}
            onEnter={apply}
          />
        </div>
      </div>
      <div className="flex shrink-0 justify-end border-t border-border px-2.5 py-2">
        <Button
          type="button"
          size="sm"
          className="transition-[background-color,transform] duration-150 active:scale-[0.96]"
          onClick={apply}
        >
          {msg('observability.filter.apply', 'Apply')}
        </Button>
      </div>
    </div>
  );
}

function RangeField({
  id,
  errorId,
  inputRef,
  label,
  value,
  error,
  onChange,
  onEnter,
}: {
  id: string;
  errorId: string;
  inputRef: Ref<HTMLInputElement>;
  label: string;
  value: string;
  error?: string;
  onChange: (next: string) => void;
  onEnter: () => void;
}) {
  const invalid = Boolean(error);
  return (
    <Field className="gap-1" data-invalid={invalid || undefined}>
      <FieldLabel htmlFor={id} className="text-xs text-muted-foreground">
        {label}
      </FieldLabel>
      <Input
        ref={inputRef}
        id={id}
        type="text"
        spellCheck={false}
        autoComplete="off"
        placeholder={DATE_TIME_PLACEHOLDER}
        value={value}
        aria-invalid={invalid || undefined}
        aria-describedby={error ? errorId : undefined}
        className="h-8 font-mono tabular-nums"
        onChange={(event) => onChange(event.currentTarget.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter') {
            onEnter();
          }
        }}
      />
      {error ? (
        <FieldError id={errorId} className="text-xs">
          {error}
        </FieldError>
      ) : null}
    </Field>
  );
}

function startOfDay(date: Date) {
  const next = new Date(date);
  next.setHours(0, 0, 0, 0);
  return next;
}

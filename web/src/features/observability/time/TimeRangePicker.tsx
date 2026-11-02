import { CalendarDays, Check, ChevronDown, Clock } from 'lucide-react';
import { useState, type FormEvent } from 'react';
import type { DateRange } from 'react-day-picker';
import { useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Button } from '../../../shared/ui/button';
import { Calendar } from '../../../shared/ui/calendar';
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '../../../shared/ui/command';
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '../../../shared/ui/input-group';
import { Label } from '../../../shared/ui/label';
import { Popover, PopoverContent, PopoverTrigger } from '../../../shared/ui/popover';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { problemText, quickRangeText, rangeText } from './labels';
import { findQuickRange, QUICK_RANGES, type QuickRange } from './quickRanges';
import {
  formatAbsolute,
  inputText,
  parseTime,
  rangeProblem,
  storedText,
  utcOffsetLabel,
  type RangeProblem,
  type RawTimeRange,
  type TimeZoneMode,
} from './range';
import { readRecentRanges, rememberRange } from './recent';

function dayText(date: Date, time: string) {
  const pad = (value: number) => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${time}`;
}

function calendarDay(text: string, now: number, tz: TimeZoneMode) {
  const value = parseTime(text, now, tz);
  if (value == null) return undefined;
  const [year, month, day] = formatAbsolute(value, tz).slice(0, 10).split('-').map(Number);
  return new Date(year, month - 1, day);
}

function TimeField({
  id,
  label,
  value,
  invalid,
  calendarLabel,
  onChange,
  onCalendar,
}: {
  id: string;
  label: string;
  value: string;
  invalid: boolean;
  calendarLabel: string;
  onChange: (value: string) => void;
  onCalendar: () => void;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <InputGroup>
        <InputGroupInput
          id={id}
          value={value}
          aria-invalid={invalid || undefined}
          className="font-mono text-xs"
          onChange={(event) => onChange(event.target.value)}
        />
        <InputGroupAddon align="inline-end">
          <InputGroupButton size="icon-xs" variant="ghost" aria-label={calendarLabel} onClick={onCalendar}>
            <CalendarDays />
          </InputGroupButton>
        </InputGroupAddon>
      </InputGroup>
    </div>
  );
}

function AbsoluteRangeForm({
  range,
  now,
  timeZone,
  onApply,
}: {
  range: RawTimeRange;
  now: number;
  timeZone: TimeZoneMode;
  onApply: (range: RawTimeRange) => void;
}) {
  const { msg } = useI18n();
  const [from, setFrom] = useState(() => inputText(range.from, timeZone));
  const [to, setTo] = useState(() => inputText(range.to, timeZone));
  const [problem, setProblem] = useState<RangeProblem | null>(null);
  const [calendar, setCalendar] = useState(false);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const next = { from: storedText(from, now, timeZone, false), to: storedText(to, now, timeZone, true) };
    const found = rangeProblem(next, now, timeZone);
    setProblem(found);
    if (!found) onApply(next);
  };
  const selectDays = (days: DateRange | undefined) => {
    if (days?.from) setFrom(dayText(days.from, '00:00:00'));
    if (days?.to ?? days?.from) setTo(dayText((days.to ?? days.from)!, '23:59:59'));
  };
  const calendarLabel = msg('observability.time.openCalendar', 'Choose dates on a calendar');
  const invalid = (text: string, roundUp: boolean) =>
    problem === 'invalid' ? parseTime(text, now, timeZone, roundUp) == null : !!problem;
  return (
    <form className="flex min-w-0 flex-col gap-3 p-3" onSubmit={submit}>
      <p className="text-sm font-medium">{msg('observability.time.absolute', 'Absolute time range')}</p>
      <TimeField
        id="observability-time-from"
        label={msg('observability.time.from', 'From')}
        value={from}
        invalid={invalid(from, false)}
        calendarLabel={calendarLabel}
        onChange={setFrom}
        onCalendar={() => setCalendar((open) => !open)}
      />
      <TimeField
        id="observability-time-to"
        label={msg('observability.time.to', 'To')}
        value={to}
        invalid={invalid(to, true)}
        calendarLabel={calendarLabel}
        onChange={setTo}
        onCalendar={() => setCalendar((open) => !open)}
      />
      {calendar && (
        <Calendar
          mode="range"
          className="self-center rounded-md border border-border p-2"
          selected={{ from: calendarDay(from, now, timeZone), to: calendarDay(to, now, timeZone) }}
          onSelect={selectDays}
        />
      )}
      {problem && (
        <p role="alert" className="text-xs text-destructive">
          {problemText(problem, msg)}
        </p>
      )}
      <Button type="submit" className="self-start">
        {msg('observability.time.apply', 'Apply time range')}
      </Button>
    </form>
  );
}

function RecentRanges({
  now,
  timeZone,
  onPick,
}: {
  now: number;
  timeZone: TimeZoneMode;
  onPick: (range: RawTimeRange) => void;
}) {
  const { msg } = useI18n();
  const [recent] = useState(readRecentRanges);
  if (!recent.length) return null;
  return (
    <div className="flex flex-col gap-1 border-t border-border p-3">
      <p className="text-xs font-medium text-muted-foreground">
        {msg('observability.time.recent', 'Recently used absolute ranges')}
      </p>
      {recent.map((range) => (
        <Button
          key={`${range.from}-${range.to}`}
          variant="ghost"
          size="sm"
          className="justify-start font-mono text-xs"
          onClick={() => onPick(range)}
        >
          {rangeText(range, timeZone, now, msg)}
        </Button>
      ))}
    </div>
  );
}

function QuickRanges({ current, onPick }: { current: RawTimeRange; onPick: (range: RawTimeRange) => void }) {
  const { msg } = useI18n();
  const itemValue = (range: QuickRange) => `${quickRangeText(range, msg)} ${range.from}`;
  const active = findQuickRange(current);
  return (
    <Command
      defaultValue={active ? itemValue(active) : undefined}
      className="rounded-none border-t border-border md:border-t-0 md:border-l"
    >
      <CommandInput placeholder={msg('observability.time.search', 'Search quick ranges')} />
      <CommandList className="max-h-80">
        <CommandEmpty>{msg('observability.time.noQuickRange', 'No matching range')}</CommandEmpty>
        <CommandGroup>
          {QUICK_RANGES.map((range) => (
            <CommandItem
              key={range.key}
              value={itemValue(range)}
              onSelect={() => onPick({ from: range.from, to: range.to })}
            >
              <Check className={cn('size-3.5', range === active ? 'opacity-100' : 'opacity-0')} aria-hidden />
              <span className={cn(range === active && 'font-medium')}>{quickRangeText(range, msg)}</span>
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </Command>
  );
}

function TimeZoneSetting({
  timeZone,
  onChange,
}: {
  timeZone: TimeZoneMode;
  onChange: (timeZone: TimeZoneMode) => void;
}) {
  const { msg } = useI18n();
  const [now] = useState(Date.now);
  const labels: Record<TimeZoneMode, string> = {
    browser: msg('observability.time.browserZone', 'Browser time'),
    utc: msg('observability.time.utc', 'Coordinated Universal Time'),
  };
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border px-3 py-2 text-xs">
      <span className="text-muted-foreground">
        {labels[timeZone]} · {utcOffsetLabel(now, timeZone)}
      </span>
      <Select value={timeZone} onValueChange={(next) => next && onChange(next as TimeZoneMode)}>
        <SelectTrigger size="sm" className="w-48" aria-label={msg('observability.time.zone', 'Time zone')}>
          <SelectValue>{labels[timeZone]}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          {(['browser', 'utc'] as const).map((zone) => (
            <SelectItem key={zone} value={zone} label={labels[zone]}>
              {labels[zone]}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

export function TimeRangePicker({
  range,
  now,
  timeZone,
  resolvedTitle,
  onChange,
  onTimeZoneChange,
}: {
  range: RawTimeRange;
  now: number;
  timeZone: TimeZoneMode;
  resolvedTitle: string;
  onChange: (range: RawTimeRange) => void;
  onTimeZoneChange: (timeZone: TimeZoneMode) => void;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const pick = (next: RawTimeRange) => {
    if (!next.from.startsWith('now') && !next.to.startsWith('now')) rememberRange(next);
    onChange(next);
    setOpen(false);
  };
  const formKey = `${range.from}|${range.to}|${timeZone}|${open}`;
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={<Button variant="outline" className="max-w-[min(26rem,100%)] min-w-0" title={resolvedTitle} />}
        aria-label={msg('observability.range', 'Time range')}
      >
        <Clock aria-hidden />
        <span className="truncate">{rangeText(range, timeZone, now, msg)}</span>
        <ChevronDown className="opacity-60" aria-hidden />
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[min(40rem,calc(100vw-2rem))] gap-0 p-0">
        <div className="grid md:grid-cols-[minmax(0,1fr)_15rem]">
          <div className="flex min-w-0 flex-col">
            <AbsoluteRangeForm key={formKey} range={range} now={now} timeZone={timeZone} onApply={pick} />
            <RecentRanges key={`recent|${formKey}`} now={now} timeZone={timeZone} onPick={pick} />
          </div>
          <QuickRanges current={range} onPick={pick} />
        </div>
        <TimeZoneSetting timeZone={timeZone} onChange={onTimeZoneChange} />
      </PopoverContent>
    </Popover>
  );
}

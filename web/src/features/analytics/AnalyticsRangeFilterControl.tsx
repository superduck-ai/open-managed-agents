import { format, parseISO } from 'date-fns';
import { ChevronDown } from 'lucide-react';
import { useState } from 'react';
import { type DateRange } from 'react-day-picker';
import clsx from 'clsx';

import { useFormatters, useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Calendar } from '../../shared/ui/calendar';
import { Popover, PopoverContent, PopoverTrigger } from '../../shared/ui/popover';
import { Separator } from '../../shared/ui/separator';

export type AnalyticsRangePreset = { value: string; label: string };

// Range filter value for analytics views. Presets keep their existing string
// identity (e.g. `last-7-days`); custom ranges carry `yyyy-MM-dd` bounds. The
// analytics dashboards currently render mock metrics, so this value only drives
// the filter UI — no backend mapping is needed yet.
export type AnalyticsRangeFilter = { kind: 'preset'; value: string } | { kind: 'custom'; from: string; to: string };

type AnalyticsRangeFilterControlProps = {
  label: string;
  presets: AnalyticsRangePreset[];
  value: AnalyticsRangeFilter;
  onChange: (filter: AnalyticsRangeFilter) => void;
};

// Range filter with preset shortcuts plus a collapsible Custom range calendar.
// Presets apply on click; Custom range commits on Apply so partial selections
// are not applied. Mirrors the Created filter UX but with data-driven presets.
export function AnalyticsRangeFilterControl({ label, presets, value, onChange }: AnalyticsRangeFilterControlProps) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const [open, setOpen] = useState(false);
  const [rangeDraft, setRangeDraft] = useState<DateRange | undefined>(() => initialDraft(value));
  // The Custom range calendar is collapsed by default and only expands when the
  // user opens it. When the committed value is already a custom range it also
  // starts expanded on open so the existing bounds are editable directly.
  const [customExpanded, setCustomExpanded] = useState<boolean>(() => value.kind === 'custom');

  const handleOpenChange = (nextOpen: boolean) => {
    setOpen(nextOpen);
    if (nextOpen) {
      setRangeDraft(initialDraft(value));
      setCustomExpanded(value.kind === 'custom');
    }
  };

  const selectPreset = (presetValue: string) => {
    onChange({ kind: 'preset', value: presetValue });
    setOpen(false);
  };

  const applyCustomRange = () => {
    const from = rangeDraft?.from;
    const to = rangeDraft?.to;
    if (!from || !to) {
      return;
    }
    onChange({ kind: 'custom', from: format(from, 'yyyy-MM-dd'), to: format(to, 'yyyy-MM-dd') });
    setOpen(false);
  };

  const draftComplete = Boolean(rangeDraft?.from && rangeDraft?.to);
  const valueLabel = rangeFilterLabel(value, presets, msg, formatters);
  const customActive = value.kind === 'custom';

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="outline"
            className="h-9 gap-2 border-border bg-background px-2.5 text-foreground shadow-sm"
          />
        }
      >
        <span className="text-muted-foreground/70">{label}</span>
        <span>{valueLabel}</span>
        <ChevronDown className="size-4 text-muted-foreground/70" aria-hidden />
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={8} className="w-auto p-0">
        <div className="p-0.5">
          <div role="radiogroup" aria-label={label}>
            {presets.map((preset) => {
              const active = value.kind === 'preset' && value.value === preset.value;
              return (
                <button
                  key={preset.value}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  onClick={() => selectPreset(preset.value)}
                  className={clsx(
                    'flex h-11 w-full items-center rounded-md pl-3 pr-8 text-left text-[15px] outline-none',
                    active ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/50',
                  )}
                >
                  {preset.label}
                </button>
              );
            })}
          </div>
          <button
            type="button"
            aria-expanded={customExpanded}
            aria-controls="analytics-range-custom"
            onClick={() => setCustomExpanded((expanded) => !expanded)}
            className={clsx(
              'flex h-11 w-full items-center justify-between rounded-md pl-3 pr-2 text-left text-[15px] outline-none',
              customActive ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/50',
            )}
          >
            <span>{msg('analytics.filter.customRange', 'Custom range')}</span>
            <ChevronDown
              className={clsx('size-4 text-muted-foreground/70 transition-transform', customExpanded && 'rotate-180')}
              aria-hidden
            />
          </button>
        </div>
        {customExpanded ? (
          <>
            <Separator />
            <div id="analytics-range-custom" className="px-0.5 pb-1.5 pt-0.5">
              {rangeLabel(rangeDraft, formatters) ? (
                <div className="mb-1 text-xs text-muted-foreground">{rangeLabel(rangeDraft, formatters)}</div>
              ) : null}
              <Calendar
                mode="range"
                selected={rangeDraft}
                onSelect={setRangeDraft}
                numberOfMonths={1}
                className="p-1"
              />
              <div className="mt-1.5 flex items-center justify-end gap-2">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setRangeDraft(undefined)}
                  disabled={!rangeDraft?.from && !rangeDraft?.to}
                >
                  {msg('common.clear', 'Clear')}
                </Button>
                <Button type="button" size="sm" onClick={applyCustomRange} disabled={!draftComplete}>
                  {msg('common.apply', 'Apply')}
                </Button>
              </div>
            </div>
          </>
        ) : null}
      </PopoverContent>
    </Popover>
  );
}

function initialDraft(value: AnalyticsRangeFilter): DateRange | undefined {
  if (value.kind !== 'custom') {
    return undefined;
  }
  const from = safeParse(value.from);
  const to = safeParse(value.to);
  if (!from || !to) {
    return undefined;
  }
  return { from, to };
}

function safeParse(value: string): Date | undefined {
  const date = parseISO(value);
  return Number.isNaN(date.getTime()) ? undefined : date;
}

function rangeLabel(range: DateRange | undefined, formatters: ReturnType<typeof useFormatters>): string {
  if (!range?.from) {
    return '';
  }
  const fromLabel = formatters.date(range.from, { dateStyle: 'medium' });
  if (!range.to) {
    return fromLabel;
  }
  const toLabel = formatters.date(range.to, { dateStyle: 'medium' });
  return fromLabel === toLabel ? fromLabel : `${fromLabel} – ${toLabel}`;
}

function rangeFilterLabel(
  value: AnalyticsRangeFilter,
  presets: AnalyticsRangePreset[],
  msg: ReturnType<typeof useI18n>['msg'],
  formatters: ReturnType<typeof useFormatters>,
): string {
  if (value.kind === 'custom') {
    const from = safeParse(value.from);
    const to = safeParse(value.to);
    if (from && to) {
      const fromLabel = formatters.date(from, { dateStyle: 'medium' });
      const toLabel = formatters.date(to, { dateStyle: 'medium' });
      return fromLabel === toLabel ? fromLabel : `${fromLabel} – ${toLabel}`;
    }
    return msg('analytics.filter.customRange', 'Custom range');
  }
  return presets.find((preset) => preset.value === value.value)?.label ?? value.value;
}

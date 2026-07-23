import { format, parseISO } from 'date-fns';
import { ChevronDown } from 'lucide-react';
import { useState } from 'react';
import { type DateRange } from 'react-day-picker';
import { Radio as RadioPrimitive } from '@base-ui/react/radio';
import clsx from 'clsx';

import { useI18n, type Locale } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Calendar } from '../../../shared/ui/calendar';
import { Popover, PopoverContent, PopoverTrigger } from '../../../shared/ui/popover';
import { RadioGroup } from '../../../shared/ui/radio-group';
import { Separator } from '../../../shared/ui/separator';
import { createdFilterLabel, formatCreatedRange, formatCreatedRangeDay } from '../labels';
import { type AgentCreatedFilter, type AgentCreatedPreset, type AgentFilterMenu } from '../types';

const PRESETS: Array<{ kind: AgentCreatedPreset; labelKey: string; fallback: string }> = [
  { kind: 'all', labelKey: 'managedAgents.filters.allTime', fallback: 'All time' },
  { kind: 'last7', labelKey: 'managedAgents.filters.last7Days', fallback: 'Last 7 days' },
  { kind: 'last30', labelKey: 'managedAgents.filters.last30Days', fallback: 'Last 30 days' },
];

type CreatedFilterDropdownProps = {
  value: AgentCreatedFilter;
  open: boolean;
  onOpenChange: (menu: AgentFilterMenu | null) => void;
  onChange: (filter: AgentCreatedFilter) => void;
};

// Popover-based replacement for the generic `AgentFilterDropdown` on the
// "Created" filter. The preset list (All time / Last 7 days / Last 30 days)
// applies immediately, while "Custom range" exposes a `react-day-picker`
// range Calendar and commits on Apply so partial selections are not sent.
export function CreatedFilterDropdown({ value, open, onOpenChange, onChange }: CreatedFilterDropdownProps) {
  const { msg, locale } = useI18n();
  const [rangeDraft, setRangeDraft] = useState<DateRange | undefined>(() => initialDraft(value));
  // The Custom range calendar is collapsed by default and only expands when the
  // user opens it. When the committed value is already a custom range it also
  // starts expanded on open so the existing bounds are editable directly.
  const [customExpanded, setCustomExpanded] = useState<boolean>(() => value.kind === 'custom');

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      // Re-seed the draft from the committed value each time the popover opens
      // so reopening after cancelling restores the previously applied range.
      // Done in the open handler (not an effect) to avoid cascading renders.
      setRangeDraft(initialDraft(value));
      setCustomExpanded(value.kind === 'custom');
    }
    onOpenChange(nextOpen ? 'created' : null);
  };

  const selectPreset = (kind: AgentCreatedPreset) => {
    onChange({ kind });
    onOpenChange(null);
  };

  const applyCustomRange = () => {
    const from = rangeDraft?.from;
    const to = rangeDraft?.to;
    if (!from || !to) {
      return;
    }
    onChange({ kind: 'custom', from: format(from, 'yyyy-MM-dd'), to: format(to, 'yyyy-MM-dd') });
    onOpenChange(null);
  };

  const draftComplete = Boolean(rangeDraft?.from && rangeDraft?.to);

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="outline"
            className={clsx('h-9 gap-2 bg-secondary px-3 text-sm', open && 'border-border')}
          />
        }
      >
        <span className="text-muted-foreground">{msg('managedAgents.filters.created', 'Created')}</span>
        <span className="font-medium text-foreground">{createdFilterLabel(value, msg, locale)}</span>
        <ChevronDown className="size-4 text-muted-foreground/70" aria-hidden />
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={8} className="w-auto p-0">
        <div className="p-0.5">
          {/* The preset list uses the shared `RadioGroup` so arrow-key
              navigation and roving tabindex come from Base UI. `Radio.Root`
              is used directly (rather than `RadioGroupItem`) because each
              option is a full-width row with an active background, not the
              default circle + label layout. */}
          <RadioGroup
            aria-label={msg('managedAgents.filters.created', 'Created')}
            value={value.kind === 'custom' ? undefined : value.kind}
            onValueChange={(kind) => selectPreset(kind as AgentCreatedPreset)}
            className="gap-0"
          >
            {PRESETS.map((preset) => {
              const active = value.kind === preset.kind;
              return (
                <RadioPrimitive.Root
                  key={preset.kind}
                  value={preset.kind}
                  className={clsx(
                    'flex h-11 w-full items-center rounded-md pl-3 pr-8 text-left text-[15px] outline-none',
                    active ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/50',
                  )}
                >
                  {msg(preset.labelKey, preset.fallback)}
                </RadioPrimitive.Root>
              );
            })}
          </RadioGroup>
          <button
            type="button"
            aria-expanded={customExpanded}
            aria-controls="created-filter-custom-range"
            onClick={() => setCustomExpanded((expanded) => !expanded)}
            className={clsx(
              'flex h-11 w-full items-center justify-between rounded-md pl-3 pr-2 text-left text-[15px] outline-none',
              value.kind === 'custom' ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/50',
            )}
          >
            <span>{msg('managedAgents.filters.customRange', 'Custom range')}</span>
            <ChevronDown
              className={clsx('size-4 text-muted-foreground/70 transition-transform', customExpanded && 'rotate-180')}
              aria-hidden
            />
          </button>
        </div>
        {customExpanded ? (
          <>
            <Separator />
            <div id="created-filter-custom-range" className="px-0.5 pb-1.5 pt-0.5">
              {rangeLabel(rangeDraft, locale) ? (
                <div className="mb-1 text-xs text-muted-foreground">{rangeLabel(rangeDraft, locale)}</div>
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

function initialDraft(value: AgentCreatedFilter): DateRange | undefined {
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

function rangeLabel(range: DateRange | undefined, locale: Locale = 'en'): string {
  if (!range?.from) {
    return '';
  }
  if (!range.to) {
    return formatCreatedRangeDay(range.from, locale);
  }
  return formatCreatedRange(range.from, range.to, locale);
}

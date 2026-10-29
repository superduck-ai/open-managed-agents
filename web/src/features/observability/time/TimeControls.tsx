import { ChevronDown, ChevronLeft, ChevronRight, RefreshCw, ZoomOut } from 'lucide-react';
import type { ReactElement } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { ButtonGroup } from '../../../shared/ui/button-group';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { Tooltip, TooltipContent, TooltipTrigger } from '../../../shared/ui/tooltip';
import { REFRESH_INTERVALS, type RawTimeRange, type TimeZoneMode } from './range';
import { TimeRangePicker } from './TimeRangePicker';

const OFF = 'off';

function HintButton({ label, hint, children }: { label: string; hint: string; children: ReactElement }) {
  return (
    <Tooltip>
      <TooltipTrigger render={children} aria-label={label} />
      <TooltipContent>
        {label} <kbd className="ml-1 font-mono text-[0.7rem] opacity-70">{hint}</kbd>
      </TooltipContent>
    </Tooltip>
  );
}

function RefreshPicker({
  refresh,
  fetching,
  onRefresh,
  onRefreshChange,
}: {
  refresh: string;
  fetching: boolean;
  onRefresh: () => void;
  onRefreshChange: (refresh: string) => void;
}) {
  const { msg } = useI18n();
  const off = msg('observability.time.autoOff', 'Off');
  return (
    <ButtonGroup>
      <HintButton label={msg('observability.refresh', 'Refresh')} hint="d r">
        <Button variant="outline" size="icon" onClick={onRefresh}>
          <RefreshCw className={fetching ? 'animate-spin' : undefined} />
        </Button>
      </HintButton>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={<Button variant="outline" className="px-2" />}
          aria-label={msg('observability.time.autoRefresh', 'Auto refresh')}
        >
          {refresh ? <span className="text-xs tabular-nums">{refresh}</span> : null}
          <ChevronDown className="opacity-60" aria-hidden />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-32">
          <DropdownMenuRadioGroup
            value={refresh || OFF}
            onValueChange={(value) => onRefreshChange(value === OFF ? '' : String(value))}
          >
            <DropdownMenuRadioItem value={OFF} closeOnClick>
              {off}
            </DropdownMenuRadioItem>
            {REFRESH_INTERVALS.map((interval) => (
              <DropdownMenuRadioItem key={interval} value={interval} closeOnClick>
                {interval}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </ButtonGroup>
  );
}

export function TimeControls({
  range,
  now,
  timeZone,
  refresh,
  fetching,
  resolvedTitle,
  onRangeChange,
  onTimeZoneChange,
  onShift,
  onZoomOut,
  onRefresh,
  onRefreshChange,
}: {
  range: RawTimeRange;
  now: number;
  timeZone: TimeZoneMode;
  refresh: string;
  fetching: boolean;
  resolvedTitle: string;
  onRangeChange: (range: RawTimeRange) => void;
  onTimeZoneChange: (timeZone: TimeZoneMode) => void;
  onShift: (direction: -1 | 1) => void;
  onZoomOut: () => void;
  onRefresh: () => void;
  onRefreshChange: (refresh: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-2">
      <ButtonGroup className="min-w-0">
        <HintButton label={msg('observability.time.back', 'Move time range backwards')} hint="t ←">
          <Button variant="outline" size="icon" onClick={() => onShift(-1)}>
            <ChevronLeft />
          </Button>
        </HintButton>
        <TimeRangePicker
          range={range}
          now={now}
          timeZone={timeZone}
          resolvedTitle={resolvedTitle}
          onChange={onRangeChange}
          onTimeZoneChange={onTimeZoneChange}
        />
        <HintButton label={msg('observability.time.forward', 'Move time range forwards')} hint="t →">
          <Button variant="outline" size="icon" onClick={() => onShift(1)}>
            <ChevronRight />
          </Button>
        </HintButton>
      </ButtonGroup>
      <HintButton label={msg('observability.time.zoomOut', 'Zoom out time range')} hint="t z">
        <Button variant="outline" size="icon" onClick={onZoomOut}>
          <ZoomOut />
        </Button>
      </HintButton>
      <RefreshPicker refresh={refresh} fetching={fetching} onRefresh={onRefresh} onRefreshChange={onRefreshChange} />
    </div>
  );
}

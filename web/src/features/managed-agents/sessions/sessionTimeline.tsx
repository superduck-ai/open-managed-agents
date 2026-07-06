import { useFormatters, useI18n } from '../../../shared/i18n';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import { Tabs, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { Tooltip, TooltipContent, TooltipTrigger } from '../../../shared/ui/tooltip';
import { type DisplayEventType, type IconComponent, type LaneTabGroup, type SessionDetailLane, type SessionEventUsage, type SessionTimelineLane, type SessionTimelineTick, type TimelinePickOptions, type ToolLifecycle } from '../types';
import clsx from 'clsx';
import { Ban, ChevronLeft, ChevronRight, CircleX, Database, Loader2, Timer } from 'lucide-react';
import { type CSSProperties, type MutableRefObject, type MouseEvent as ReactMouseEvent, type ReactElement, type ReactNode, type PointerEvent as ReactPointerEvent, type RefObject, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { formatCompactTokenCount, formatSessionDuration, sessionTokenUsageTitle, truncateLaneLabel } from './sessionDetailModel';
import { outcomeStatusChipClass, outcomeStatusLabel } from './sessionTraceRows';

export const SESSION_MAIN_LANE_ID = '';
const SESSION_MAIN_LANE_TAB_VALUE = '__oma_main_lane__';

export const SESSION_ARCHIVED_LANES_STORAGE_KEY = 'oma.sessionDetail.showArchivedLanes';

export function SessionStatusPill({ status }: { status: string }) {
  const tone = status.toLowerCase();
  return (
    <Badge
      variant="secondary"
      className={clsx(
        'h-6 rounded-md px-2 text-xs font-medium',
        tone.includes('running') || tone.includes('active')
          ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
          : tone.includes('error') || tone.includes('failed')
            ? 'bg-destructive/10 text-destructive'
            : 'bg-secondary text-secondary-foreground'
      )}
    >
      {status}
    </Badge>
  );
}

export function SessionSummaryChip({ icon: Icon, children }: { icon: IconComponent; children: ReactNode }) {
  return (
    <Badge variant="outline" className="h-auto max-w-full items-center gap-1.5 rounded-md bg-card px-2.5 py-1.5 text-sm font-medium text-foreground shadow-sm">
      <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      <span className="truncate">{children}</span>
    </Badge>
  );
}

export function EventsMinimap({
  lanes,
  activeLane,
  selectedEntryId,
  visibleIds,
  scrollerRef,
  suppressScrollSeekUntilRef,
  onLaneChange,
  onSeek
}: {
  lanes: SessionTimelineLane[];
  activeLane: string;
  selectedEntryId: string | null;
  visibleIds?: Set<string>;
  scrollerRef: RefObject<HTMLDivElement | null>;
  suppressScrollSeekUntilRef: MutableRefObject<number>;
  onLaneChange: (laneId: string, targetEntryId?: string | null) => void;
  onSeek: (entryId: string | null) => void;
}) {
  const ticks = useMemo(() => buildTimelineTicks(lanes), [lanes]);
  const trackRef = useRef<HTMLDivElement | null>(null);
  const rowRefs = useRef(new Map<string, HTMLDivElement>());
  const dragRef = useRef<{ startX: number; pointerId: number; laneId: string; dragging: boolean } | null>(null);
  const hoveredLaneRef = useRef<string | null>(null);
  const isMultiLane = lanes.length > 1;
  const [boundaryLock, setBoundaryLock] = useState<'start' | 'end' | null>(null);
  const [hoveredTickId, setHoveredTickId] = useState<string | null>(null);
  const [hoveredLaneId, setHoveredLaneId] = useState<string | null>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [windowRange, setWindowRange] = useState({ leftPct: 1, widthPct: 98 });
  const [playhead, setPlayhead] = useState<{ leftPct: number; topPx: number; label: string; visible: boolean }>({
    leftPct: 1,
    topPx: 14,
    label: '',
    visible: false
  });
  const hoveredTick = hoveredTickId ? ticks.find((tick) => tick.id === hoveredTickId) ?? null : null;
  const activeLaneTicks = useMemo(() => ticks.filter((tick) => tick.lane.id === activeLane), [activeLane, ticks]);
  const timelineMinHeight = isMultiLane ? 32 + 24 + (Math.max(0, lanes.length - 2) * 20) - 4 : undefined;

  const suppressScrollSync = useCallback(() => {
    suppressScrollSeekUntilRef.current = sessionTimelineNow() + 200;
  }, [suppressScrollSeekUntilRef]);

  const updateHoveredLane = useCallback((laneId: string | null) => {
    if (hoveredLaneRef.current === laneId) {
      return;
    }
    hoveredLaneRef.current = laneId;
    setHoveredLaneId(laneId);
  }, []);

  const updatePlayheadFromTick = useCallback((tick: SessionTimelineTick, label = tick.relativeTime) => {
    const row = rowRefs.current.get(tick.lane.id);
    const topPx = row ? row.offsetTop + row.offsetHeight / 2 : 14;
    setBoundaryLock(null);
    setPlayhead({
      leftPct: timelineTickCenterPct(tick),
      topPx,
      label,
      visible: true
    });
  }, []);

  const seekToTick = useCallback((tick: SessionTimelineTick) => {
    suppressScrollSync();
    updatePlayheadFromTick(tick);
    onSeek(tick.id);
    scrollSessionEntryToOffset(scrollerRef.current, tick.rowId);
  }, [onSeek, scrollerRef, suppressScrollSync, updatePlayheadFromTick]);

  const changeLaneToTick = useCallback((tick: SessionTimelineTick) => {
    suppressScrollSync();
    updatePlayheadFromTick(tick);
    onLaneChange(tick.lane.id, tick.id);
  }, [onLaneChange, suppressScrollSync, updatePlayheadFromTick]);

  const seekToBoundary = useCallback((boundary: 'start' | 'end', laneId = activeLane) => {
    const laneTicks = ticks.filter((tick) => tick.lane.id === laneId && isTimelineTickSelectable(tick, visibleIds, true));
    const tick = boundary === 'start' ? laneTicks[0] : laneTicks[laneTicks.length - 1];
    const scroller = scrollerRef.current;
    const row = rowRefs.current.get(laneId);
    const topPx = row ? row.offsetTop + row.offsetHeight / 2 : playhead.topPx;
    suppressScrollSync();
    setBoundaryLock(boundary);
    onSeek(null);
    if (scroller) {
      scroller.scrollTop = boundary === 'start' ? 0 : Math.max(0, scroller.scrollHeight - scroller.clientHeight);
    }
    setPlayhead({
      leftPct: boundary === 'start' ? 1 : 99,
      topPx,
      label: tick?.relativeTime ?? '',
      visible: true
    });
  }, [activeLane, onSeek, playhead.topPx, scrollerRef, suppressScrollSync, ticks, visibleIds]);

  const laneIdAtClientY = useCallback((clientY: number) => {
    if (!isMultiLane) {
      return activeLane;
    }
    for (const lane of lanes) {
      const row = rowRefs.current.get(lane.id);
      if (!row) {
        continue;
      }
      const rect = row.getBoundingClientRect();
      if (clientY >= rect.top - 3 && clientY <= rect.bottom + 3) {
        return lane.id;
      }
    }
    return activeLane;
  }, [activeLane, isMultiLane, lanes]);

  const pickTickAtPoint = useCallback((clientX: number, laneId: string, includeIdle = false, maxDistancePct = 2) =>
    pickTimelineTickAtClientX(clientX, trackRef.current, ticks, {
      laneId,
      includeIdle,
      maxDistancePct,
      visibleIds
    }), [ticks, visibleIds]);

  const handleLanePointerEnter = (laneId: string) => {
    if (!dragRef.current?.dragging) {
      updateHoveredLane(laneId);
    }
  };

  const handleLanePointerLeave = (laneId: string, event: ReactPointerEvent<HTMLDivElement>) => {
    if (dragRef.current?.dragging) {
      return;
    }
    const nextTarget = event.relatedTarget;
    if (nextTarget instanceof Node && event.currentTarget.contains(nextTarget)) {
      return;
    }
    if (hoveredLaneRef.current === laneId) {
      updateHoveredLane(null);
      setHoveredTickId(null);
    }
  };

  useEffect(() => {
    const tick = selectedEntryId ? ticks.find((candidate) => candidate.id === selectedEntryId) : null;
    if (tick) {
      updatePlayheadFromTick(tick);
    }
  }, [selectedEntryId, ticks, updatePlayheadFromTick]);

  useEffect(() => {
    const scroller = scrollerRef.current;
    if (!scroller) {
      return;
    }
    const handleScroll = () => {
      if (sessionTimelineNow() < suppressScrollSeekUntilRef.current) {
        return;
      }
      const visibleEntryIds = visibleSessionEntryIds(scroller);
      const laneTicks = activeLaneTicks.filter((tick) => isTimelineTickSelectable(tick, visibleIds, true));
      if (!laneTicks.length) {
        return;
      }
      const atStart = scroller.scrollTop <= 2;
      const atEnd = scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 2;
      let firstTick = atStart ? laneTicks[0] : atEnd ? laneTicks[laneTicks.length - 1] : null;
      if (!firstTick) {
        firstTick = laneTicks.find((tick) => visibleEntryIds.has(tick.rowId)) ?? laneTicks[0];
      }
      if (!firstTick) {
        return;
      }
      if (isMultiLane) {
        updatePlayheadFromTick(firstTick);
        return;
      }
      const lastVisibleTick = [...laneTicks].reverse().find((tick) => visibleEntryIds.has(tick.rowId)) ?? firstTick;
      const leftPct = clampTimelinePct(firstTick.leftPct);
      const rightPct = clampTimelinePct(lastVisibleTick.leftPct + lastVisibleTick.widthPct);
      setWindowRange({
        leftPct,
        widthPct: Math.max(0.8, rightPct - leftPct)
      });
    };
    handleScroll();
    scroller.addEventListener('scroll', handleScroll, { passive: true });
    return () => scroller.removeEventListener('scroll', handleScroll);
  }, [activeLaneTicks, isMultiLane, scrollerRef, suppressScrollSeekUntilRef, updatePlayheadFromTick, visibleIds]);

  const handlePointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const dragState = dragRef.current;
    const laneId = dragState ? laneIdAtClientY(event.clientY) : hoveredLaneRef.current ?? laneIdAtClientY(event.clientY);
    if (!dragState && hoveredLaneRef.current === null) {
      updateHoveredLane(laneId);
    }
    if (dragState) {
      updateHoveredLane(laneId);
    }
    const hoverTick = pickTickAtPoint(event.clientX, laneId, true, 1.5);
    setHoveredTickId(hoverTick?.id ?? null);
    if (!dragState || dragState.pointerId !== event.pointerId) {
      return;
    }
    if (!dragState.dragging && Math.abs(event.clientX - dragState.startX) < 4) {
      return;
    }
    dragState.dragging = true;
    setIsDragging(true);
    const pct = clientXToTimelinePct(event.clientX, trackRef.current);
    if (isMultiLane && pct <= 1.5) {
      seekToBoundary('start', laneId);
      return;
    }
    if (isMultiLane && pct >= 98.5) {
      seekToBoundary('end', laneId);
      return;
    }
    const dragTick = pickTickAtPoint(event.clientX, laneId, true, 1.5);
    if (dragTick) {
      if (dragTick.lane.id !== activeLane) {
        changeLaneToTick(dragTick);
        return;
      }
      seekToTick(dragTick);
    }
  };

  const handlePointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) {
      return;
    }
    const laneId = laneIdAtClientY(event.clientY);
    dragRef.current = { startX: event.clientX, pointerId: event.pointerId, laneId, dragging: false };
    event.currentTarget.setPointerCapture?.(event.pointerId);
  };

  const handleInactiveLanePointerDown = (laneId: string, event: ReactPointerEvent<HTMLDivElement>) => {
    if (laneId === activeLane || event.button !== 0) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    const tick = pickTickAtPoint(event.clientX, laneId, false, 2.5);
    if (tick) {
      changeLaneToTick(tick);
      return;
    }
    onLaneChange(laneId, null);
  };

  const handlePointerUp = (event: ReactPointerEvent<HTMLDivElement>) => {
    const dragState = dragRef.current;
    dragRef.current = null;
    setIsDragging(false);
    event.currentTarget.releasePointerCapture?.(event.pointerId);
    const laneId = laneIdAtClientY(event.clientY);
    const pct = clientXToTimelinePct(event.clientX, trackRef.current);
    if (dragState?.dragging) {
      if (isMultiLane && pct <= 1.5) {
        seekToBoundary('start', laneId);
      } else if (isMultiLane && pct >= 98.5) {
        seekToBoundary('end', laneId);
      }
      return;
    }
    const tick = pickTickAtPoint(event.clientX, laneId, false, 2.5);
    if (!tick) {
      return;
    }
    if (tick.lane.id !== activeLane) {
      changeLaneToTick(tick);
      return;
    }
    seekToTick(tick);
  };

  const handlePointerLeave = () => {
    if (!dragRef.current?.dragging) {
      setHoveredTickId(null);
      updateHoveredLane(null);
    }
  };
  const handleMouseMove = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (dragRef.current) {
      return;
    }
    const laneId = hoveredLaneRef.current ?? laneIdAtClientY(event.clientY);
    if (hoveredLaneRef.current === null) {
      updateHoveredLane(laneId);
    }
    const hoverTick = pickTickAtPoint(event.clientX, laneId, true, 1.5);
    setHoveredTickId(hoverTick?.id ?? null);
  };
  const handleMouseLeave = () => {
    if (!dragRef.current) {
      setHoveredTickId(null);
      updateHoveredLane(null);
    }
  };
  const handleClick = (event: ReactMouseEvent<HTMLDivElement>) => {
    const laneId = laneIdAtClientY(event.clientY);
    const tick = pickTickAtPoint(event.clientX, laneId, false, 2.5);
    if (!tick) {
      return;
    }
    if (tick.lane.id !== activeLane) {
      changeLaneToTick(tick);
      return;
    }
    seekToTick(tick);
  };

  return (
    <div className="relative z-10 shrink-0 px-8 pb-2" aria-label="Session event timeline" data-testid="events-minimap">
      <div className={clsx(isMultiLane && 'mb-5')} style={timelineMinHeight ? { minHeight: `${timelineMinHeight}px` } : undefined}>
        <div
          ref={trackRef}
          data-boundary-lock={boundaryLock ?? undefined}
          data-dragging={isDragging || undefined}
          className={clsx(
            'relative flex touch-none select-none flex-col',
            isDragging ? 'cursor-grabbing' : 'cursor-grab active:cursor-grabbing'
          )}
          onPointerDown={handlePointerDown}
          onPointerMove={handlePointerMove}
          onPointerUp={handlePointerUp}
          onPointerCancel={handlePointerUp}
          onPointerLeave={handlePointerLeave}
          onMouseMove={handleMouseMove}
          onMouseLeave={handleMouseLeave}
          onClick={handleClick}
        >
          {!isMultiLane ? (
            <div className="pointer-events-none absolute inset-y-0 z-10 rounded bg-foreground/10" style={{ left: `${windowRange.leftPct}%`, width: `${windowRange.widthPct}%` }} aria-hidden />
          ) : null}
          {isMultiLane && playhead.visible ? (
            <div
              className="pointer-events-none absolute z-20 -translate-x-1/2"
              style={{ left: `${playhead.leftPct}%`, top: `${playhead.topPx}px` }}
              aria-hidden
            >
              <div className="absolute left-1/2 top-0 size-2 -translate-x-1/2 -translate-y-1/2 rounded-full bg-muted shadow-sm transition-opacity duration-150" />
              {playhead.label ? (
                <div
                  className={clsx(
                    'pointer-events-auto absolute left-1/2 top-2 -translate-x-1/2 whitespace-nowrap rounded bg-accent px-1.5 py-0.5 font-mono text-[10px] font-medium tabular-nums text-muted-foreground shadow-sm ring-2 ring-muted/80 transition-transform',
                    isDragging ? 'scale-105 cursor-grabbing' : 'cursor-grab active:cursor-grabbing'
                  )}
                >
                  {playhead.label}
                </div>
              ) : null}
            </div>
          ) : null}
          {lanes.map((lane, index) => (
            <div
              key={lane.id || 'main'}
              ref={(node) => {
                if (node) {
                  rowRefs.current.set(lane.id, node);
                } else {
                  rowRefs.current.delete(lane.id);
                }
              }}
              data-lane-index={index}
              style={sessionTimelineLaneSlotStyle(lane.id, activeLane)}
              className={clsx(
                "relative shrink-0 after:pointer-events-none after:absolute after:inset-x-0 after:-bottom-1 after:h-1 after:content-['']",
                lane.id !== activeLane && 'cursor-pointer'
              )}
              onPointerDown={(event) => handleInactiveLanePointerDown(lane.id, event)}
              onPointerEnter={() => handleLanePointerEnter(lane.id)}
              onPointerLeave={(event) => handleLanePointerLeave(lane.id, event)}
            >
              <div
                className={clsx(
                  'absolute inset-x-0 top-1/2 -translate-y-1/2 rounded transition-[height,background-color,opacity] duration-100 ease-out',
                  lane.id === activeLane ? 'bg-accent' : lane.id === hoveredLaneId ? 'bg-accent/50 opacity-100' : 'bg-accent/40 opacity-85'
                )}
                style={sessionTimelineLaneVisualStyle(lane.id, activeLane, hoveredLaneId)}
              >
                {ticks.filter((tick) => tick.lane.id === lane.id).map((tick) => (
                  <SessionTimelineTickMark
                    key={tick.id}
                    tick={tick}
                    selected={tick.id === selectedEntryId}
                    hovered={tick.id === hoveredTickId}
                    hidden={Boolean(visibleIds && !visibleIds.has(tick.id))}
                  />
                ))}
              </div>
            </div>
          ))}
          {hoveredTick ? <SessionTimelineTooltip tick={hoveredTick} row={rowRefs.current.get(hoveredTick.lane.id) ?? null} /> : null}
        </div>
      </div>
    </div>
  );
}

export function SessionTimelineTickMark({ tick, selected, hovered, hidden }: { tick: SessionTimelineTick; selected: boolean; hovered: boolean; hidden: boolean }) {
  const style: CSSProperties = {
    left: `${tick.leftPct}%`,
    width: `${tick.widthPct}%`
  };
  if (tick.type === 'status_idle') {
    style.backgroundColor = 'var(--accent)';
    style.backgroundImage = 'repeating-linear-gradient(-45deg, transparent 0, transparent 6px, var(--card) 6px, var(--card) 12px)';
  }
  return (
    <span
      data-timeline-tick-id={tick.id}
      className={clsx(
        'pointer-events-none absolute bottom-0.5 top-0.5 rounded-sm transition-[left,width,opacity] duration-150',
        sessionTimelineTickClass(tick.type),
        selected && 'z-30 opacity-100 outline outline-[1.5px] outline-offset-1 outline-ring',
        !selected && hovered && 'z-30 opacity-100 outline outline-[1.5px] outline-offset-1 outline-ring/50',
        !selected && !hovered && 'opacity-90',
        hidden && '!opacity-0'
      )}
      style={style}
      aria-hidden
    />
  );
}

export function SessionTimelineTooltip({ tick, row }: { tick: SessionTimelineTick; row: HTMLDivElement | null }) {
  const topPx = row ? row.offsetTop : 0;
  const heightPx = row?.offsetHeight ?? 20;
  const centerPct = timelineTickCenterPct(tick);
  const triggerId = `session-timeline-tooltip-${tick.id}`;
  return (
    <Tooltip open triggerId={triggerId}>
      <TooltipTrigger
        render={
          <span
            id={triggerId}
            aria-hidden
            className="pointer-events-none absolute z-30"
            style={{
              left: `${centerPct}%`,
              top: `${topPx}px`,
              height: `${heightPx}px`,
              width: '1px',
              transform: 'translateX(-50%)'
            }}
          />
        }
      />
      <TooltipContent
        side="top"
        sideOffset={8}
        className="max-w-[280px] flex-col items-start gap-1 px-2.5 py-2 text-left"
      >
        <div className="flex items-center gap-1.5">
          <span
            className={clsx(
              'inline-flex h-4 rounded-sm px-1 text-[10px] font-semibold uppercase leading-4 text-foreground',
              sessionTimelineTickClass(tick.type)
            )}
          >
            {sessionTimelineTypeLabel(tick.type)}
          </span>
          <span className="text-background/70">{tick.relativeTime}</span>
        </div>
        <div className="font-medium text-background">
          {tick.label || tick.preview || sessionTimelineTypeLabel(tick.type)}
        </div>
        {tick.preview && tick.preview !== tick.label ? (
          <div className="line-clamp-2 text-background/70">{tick.preview}</div>
        ) : null}
        {tick.durationMs > 0 ? (
          <div className="text-[11px] text-background/60">{formatTimelineDuration(tick.durationMs)}</div>
        ) : null}
      </TooltipContent>
    </Tooltip>
  );
}

function TimelineTooltip({ label, children }: { label?: string; children: ReactElement }) {
  if (!label) {
    return children;
  }
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="inline-flex min-w-0">{children}</span>} />
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

export function sessionTimelineLaneVisualHeightPx(laneId: string, activeLane: string, hoveredLaneId: string | null) {
  if (laneId === activeLane) {
    return 28;
  }
  if (laneId === hoveredLaneId) {
    return 20;
  }
  return 16;
}

export function sessionTimelineLaneSlotHeightPx(laneId: string, activeLane: string) {
  return laneId === activeLane ? 28 : 20;
}

export function sessionTimelineLaneSlotStyle(laneId: string, activeLane: string): CSSProperties {
  const height = `${sessionTimelineLaneSlotHeightPx(laneId, activeLane)}px`;
  return { height, minHeight: height, maxHeight: height };
}

export function sessionTimelineLaneVisualStyle(laneId: string, activeLane: string, hoveredLaneId: string | null): CSSProperties {
  const height = `${sessionTimelineLaneVisualHeightPx(laneId, activeLane, hoveredLaneId)}px`;
  return { height, minHeight: height, maxHeight: height };
}

export function sessionTimelineTickClass(type: DisplayEventType) {
  switch (type) {
    case 'user':
      return 'bg-destructive/75';
    case 'error':
      return 'bg-destructive';
    case 'agent':
    case 'thinking':
      return 'bg-accent/80';
    case 'subagent':
      return 'bg-emerald-500/80';
    case 'status_idle':
      return 'bg-accent';
    case 'tool_use':
    case 'result':
      return 'bg-muted/70';
    case 'thread':
      return 'bg-emerald-500/70';
    case 'status_rescheduled':
    case 'interrupt':
      return 'bg-amber-500/70';
    case 'model_request':
    case 'outcome':
    case 'status_running':
    case 'status_terminated':
    case 'root':
    case 'system_message':
    case 'unknown':
    default:
      return 'bg-accent';
  }
}

export function sessionTimelineTypeLabel(type: DisplayEventType) {
  switch (type) {
    case 'model_request':
      return 'model';
    case 'status_rescheduled':
    case 'status_running':
    case 'status_idle':
    case 'status_terminated':
      return 'status';
    case 'system_message':
      return 'system';
    default:
      return type.replace(/_/g, ' ');
  }
}

export function formatTimelineDuration(ms: number) {
  if (!Number.isFinite(ms) || ms <= 0) {
    return '';
  }
  if (ms < 1000) {
    return `${Math.round(ms)} ms`;
  }
  if (ms < 60_000) {
    return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)} s`;
  }
  return `${Math.floor(ms / 60_000)}m ${Math.round((ms % 60_000) / 1000)}s`;
}

export function visibleSessionEntryIds(scroller: HTMLDivElement) {
  const ids = new Set<string>();
  const top = scroller.scrollTop;
  const bottom = top + scroller.clientHeight;
  scroller.querySelectorAll<HTMLElement>('[data-event-id]').forEach((node) => {
    const nodeTop = node.offsetTop;
    const nodeBottom = nodeTop + Math.max(node.offsetHeight, 1);
    if (nodeBottom >= top && nodeTop <= bottom) {
      const id = node.getAttribute('data-event-id');
      if (id) {
        ids.add(id);
      }
    }
  });
  return ids;
}

export function scrollSessionEntryToOffset(scroller: HTMLDivElement | null, entryId: string) {
  if (!scroller) {
    return;
  }
  const target = Array.from(scroller.querySelectorAll<HTMLElement>('[data-event-id]')).find(
    (node) => node.getAttribute('data-event-id') === entryId
  );
  if (target) {
    scroller.scrollTop = Math.max(0, target.offsetTop - 16);
  }
}

export function sessionTimelineNow() {
  return typeof performance !== 'undefined' && typeof performance.now === 'function' ? performance.now() : Date.now();
}

export function clientXToTimelinePct(clientX: number, track: HTMLDivElement | null) {
  if (!track) {
    return 1;
  }
  const rect = track.getBoundingClientRect();
  if (!rect.width) {
    return 1;
  }
  return clampTimelinePct(((clientX - rect.left) / rect.width) * 100);
}

export function pickTimelineTickAtClientX(
  clientX: number,
  track: HTMLDivElement | null,
  ticks: SessionTimelineTick[],
  options: TimelinePickOptions = {}
) {
  return pickTimelineTickAtPercent(clientXToTimelinePct(clientX, track), ticks, options);
}

export function pickTimelineTickAtPercent(percent: number, ticks: SessionTimelineTick[], options: TimelinePickOptions = {}) {
  let hit: SessionTimelineTick | null = null;
  let nearest: { tick: SessionTimelineTick; distance: number } | null = null;
  const pct = clampTimelinePct(percent);
  for (const tick of ticks) {
    if (!isTimelineTickSelectable(tick, options.visibleIds, options.includeIdle)) {
      continue;
    }
    if (options.laneId !== undefined && tick.lane.id !== options.laneId) {
      continue;
    }
    const left = tick.leftPct;
    const right = tick.leftPct + tick.widthPct;
    if (pct >= left && pct < right) {
      if (!hit || tick.leftPct > hit.leftPct || (tick.leftPct === hit.leftPct && tick.ms >= hit.ms)) {
        hit = tick;
      }
      continue;
    }
    const center = timelineTickCenterPct(tick);
    const distance = Math.min(Math.abs(pct - left), Math.abs(pct - right), Math.abs(pct - center));
    if (!nearest || distance < nearest.distance) {
      nearest = { tick, distance };
    }
  }
  if (hit) {
    return hit;
  }
  const maxDistance = options.maxDistancePct ?? 2;
  return nearest && nearest.distance <= maxDistance ? nearest.tick : null;
}

export function isTimelineTickSelectable(tick: SessionTimelineTick, visibleIds?: Set<string>, includeIdle = false) {
  if (visibleIds && !visibleIds.has(tick.id)) {
    return false;
  }
  if (!includeIdle && tick.type === 'status_idle') {
    return false;
  }
  return true;
}

export function clampTimelinePct(value: number) {
  if (!Number.isFinite(value)) {
    return 1;
  }
  return Math.max(1, Math.min(99, value));
}

export function timelineTickCenterPct(tick: SessionTimelineTick) {
  return clampTimelinePct(tick.leftPct + tick.widthPct / 2);
}

export function nearestTimelineTickForLane(ticks: SessionTimelineTick[], laneId: string, anchorPct: number, visibleIds?: Set<string>) {
  let nearest: { tick: SessionTimelineTick; distance: number } | null = null;
  for (const tick of ticks) {
    if (tick.lane.id !== laneId || !isTimelineTickSelectable(tick, visibleIds, false)) {
      continue;
    }
    const distance = Math.abs(timelineTickCenterPct(tick) - anchorPct);
    if (!nearest || distance < nearest.distance) {
      nearest = { tick, distance };
    }
  }
  return nearest?.tick ?? null;
}

export function buildTimelineTicks(lanes: SessionTimelineLane[]): SessionTimelineTick[] {
  const flattened = lanes
    .flatMap((lane) => lane.items.map((item) => ({ ...item, lane })))
    .filter((item) => Number.isFinite(item.processedAtMs))
    .sort((left, right) => left.processedAtMs - right.processedAtMs || left.id.localeCompare(right.id));
  if (!flattened.length) {
    return [];
  }

  const starts = flattened.map((item) => item.processedAtMs);
  const renderDurations = flattened
    .map((item) => Math.max(0, item.durationMs ?? 0))
    .map((duration, index) => (index + 1 < starts.length ? Math.min(duration, Math.max(0, starts[index + 1] - starts[index])) : duration));
  const spans: number[] = [];
  for (let index = 0; index < flattened.length; index += 1) {
    if (renderDurations[index] > 0) {
      spans.push(renderDurations[index]);
    }
    if (index + 1 < flattened.length) {
      const gap = starts[index + 1] - (starts[index] + renderDurations[index]);
      if (gap > 0) {
        spans.push(gap);
      }
    }
  }

  if (!spans.length) {
    const totalMs = Math.max(1, starts[starts.length - 1] - starts[0]);
    return flattened.map((item) => {
      const rawLeftPct = starts.length === 1 ? 1 : 1 + ((item.processedAtMs - starts[0]) / totalMs) * 98;
      return {
        ...item,
        leftPct: Math.max(1, Math.min(98.6, rawLeftPct)),
        widthPct: 0.4,
        ms: item.processedAtMs
      };
    });
  }

  spans.sort((left, right) => left - right);
  const threshold = Math.max(1, 4 * spans[Math.floor(spans.length / 2)]);
  const compressMs = (ms: number) => {
    if (ms <= 0) {
      return 0;
    }
    if (ms < threshold) {
      return ms;
    }
    return threshold * (1 + Math.log(ms / threshold));
  };
  const offsets: number[] = [];
  const widths: number[] = [];
  let cursor = 0;
  for (let index = 0; index < flattened.length; index += 1) {
    offsets.push(cursor);
    const width = compressMs(renderDurations[index]);
    widths.push(width);
    cursor += width;
    if (index + 1 < flattened.length) {
      cursor += compressMs(Math.max(0, starts[index + 1] - (starts[index] + renderDurations[index])));
    }
  }
  const total = cursor || 1;

  return flattened.map((item, index) => {
    let leftPct = 1 + (offsets[index] / total) * 98;
    let widthPct = Math.min(98, Math.max(0.4, (widths[index] / total) * 98));
    const overflow = leftPct + widthPct - 99;
    if (overflow > 0) {
      const shrink = Math.min(widthPct - 0.4, overflow);
      widthPct -= shrink;
      leftPct -= overflow - shrink;
    }
    return {
      ...item,
      leftPct: Math.max(1, Math.min(99 - widthPct, leftPct)),
      widthPct,
      ms: item.processedAtMs
    };
  });
}

export function LaneTabStrip({
  lanes,
  activeLane,
  archivedLaneCount,
  isMultiAgent,
  selectedEntryId,
  showArchivedLanes,
  timeline,
  timelineVisibleIds,
  onToggleArchivedLanes,
  onChange
}: {
  lanes: SessionDetailLane[];
  activeLane: string;
  archivedLaneCount: number;
  isMultiAgent: boolean;
  selectedEntryId: string | null;
  showArchivedLanes: boolean;
  timeline: SessionTimelineLane[];
  timelineVisibleIds?: Set<string>;
  onToggleArchivedLanes: () => void;
  onChange: (laneId: string, targetEntryId?: string | null) => void;
}) {
  const { msg } = useI18n();
  const scrollerRef = useRef<HTMLDivElement | null>(null);
  const [scrollState, setScrollState] = useState({ canScroll: false, left: false, right: false });
  const timelineTicks = useMemo(() => buildTimelineTicks(timeline ?? []), [timeline]);
  const laneGroups = useMemo(() => buildLaneTabGroups(lanes, activeLane), [activeLane, lanes]);
  const selectedTick = selectedEntryId ? timelineTicks.find((tick) => tick.id === selectedEntryId) ?? null : null;
  const activeTick = selectedTick ?? timelineTicks.find((tick) => tick.lane.id === activeLane && isTimelineTickSelectable(tick, timelineVisibleIds, false)) ?? null;
  const activeAnchorPct = activeTick ? timelineTickCenterPct(activeTick) : 1;
  const activeLaneTabValue = laneTabValue(activeLane);

  const refreshScrollState = useCallback(() => {
    const scroller = scrollerRef.current;
    if (!scroller) {
      return;
    }
    const canScroll = scroller.scrollWidth > scroller.clientWidth + 1;
    setScrollState({
      canScroll,
      left: canScroll && scroller.scrollLeft > 1,
      right: canScroll && scroller.scrollLeft < scroller.scrollWidth - scroller.clientWidth - 1
    });
  }, []);

  useEffect(() => {
    refreshScrollState();
    const scroller = scrollerRef.current;
    if (!scroller) {
      return;
    }
    scroller.addEventListener('scroll', refreshScrollState, { passive: true });
    window.addEventListener('resize', refreshScrollState);
    return () => {
      scroller.removeEventListener('scroll', refreshScrollState);
      window.removeEventListener('resize', refreshScrollState);
    };
  }, [refreshScrollState, laneGroups.length]);

  useEffect(() => {
    const activeTab = scrollerRef.current?.querySelector<HTMLElement>(`[data-lane-tab-id="${cssEscape(activeLane || 'main')}"]`);
    activeTab?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    refreshScrollState();
  }, [activeLane, refreshScrollState]);

  if (!isMultiAgent) {
    return null;
  }

  const selectLane = (laneId: string) => {
    if (laneId === activeLane) {
      return;
    }
    const targetTick = nearestTimelineTickForLane(timelineTicks, laneId, activeAnchorPct, timelineVisibleIds);
    onChange(laneId, targetTick?.id ?? null);
  };

  const scrollBy = (direction: 'left' | 'right') => {
    const scroller = scrollerRef.current;
    if (!scroller) {
      return;
    }
    scroller.scrollBy({ left: direction === 'left' ? -Math.floor(scroller.clientWidth * 0.8) : Math.floor(scroller.clientWidth * 0.8), behavior: 'smooth' });
  };
  return (
    <div className="flex items-center gap-2 border-b border-border px-8 py-2" data-testid="lane-tab-strip">
      {scrollState.canScroll ? (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          className={clsx(
            'size-6 text-muted-foreground hover:bg-accent hover:text-foreground',
            !scrollState.left && 'pointer-events-none opacity-30'
          )}
          aria-label={msg('managedAgents.sessions.detail.scrollLanesLeft', 'Scroll lane tabs left')}
          disabled={!scrollState.left}
          onClick={() => scrollBy('left')}
        >
          <ChevronLeft className="size-4" aria-hidden />
        </Button>
      ) : null}
      <div
        ref={scrollerRef}
        className="subtle-scrollbar flex min-w-0 flex-1 gap-1 overflow-x-auto"
        style={{ maskImage: scrollState.canScroll ? 'linear-gradient(90deg, transparent 0, #000 24px, #000 calc(100% - 24px), transparent 100%)' : undefined }}
      >
        <Tabs
          value={activeLaneTabValue}
          className="gap-0"
          onValueChange={(nextValue) => selectLane(laneIdFromTabValue(nextValue))}
        >
          <TabsList
            aria-label={msg('managedAgents.sessions.detail.laneTabs', 'Session threads')}
            className="h-auto flex-nowrap gap-1 rounded-none bg-transparent p-0"
          >
            {laneGroups.map((group) => (
              group.collapsed ? (
                <TimelineTooltip key={group.key} label={group.label}>
                  <TabsTrigger
                    value={laneTabValue(group.lanes[0]?.id ?? SESSION_MAIN_LANE_ID)}
                    className="h-8 shrink-0 gap-2 rounded-md bg-transparent px-2 text-sm font-medium text-muted-foreground shadow-none after:hidden hover:bg-accent hover:text-foreground data-active:bg-accent data-active:text-foreground data-active:hover:bg-accent"
                  >
                    <span className="max-w-[88px] truncate">{truncateLaneLabel(group.label)}</span>
                    <span className="rounded bg-secondary px-1.5 py-0.5 text-[10px] text-secondary-foreground">{group.lanes.length}</span>
                  </TabsTrigger>
                </TimelineTooltip>
              ) : (
                group.lanes.map((lane) => (
                  <LaneTabLabel key={lane.id || 'main'} lane={lane} />
                ))
              )
            ))}
          </TabsList>
        </Tabs>
        {archivedLaneCount > 0 ? (
          <Button
            type="button"
            variant="ghost"
            className={clsx(
              'h-8 shrink-0 rounded-md px-2 text-sm font-medium',
              showArchivedLanes ? 'bg-accent text-foreground hover:bg-accent' : 'text-muted-foreground hover:bg-accent hover:text-foreground'
            )}
            aria-pressed={showArchivedLanes}
            onClick={onToggleArchivedLanes}
          >
            {msg('managedAgents.sessions.detail.archivedLanes', '+{count} archived', { count: archivedLaneCount })}
          </Button>
        ) : null}
      </div>
      {scrollState.canScroll ? (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          className={clsx(
            'size-6 text-muted-foreground hover:bg-accent hover:text-foreground',
            !scrollState.right && 'pointer-events-none opacity-30'
          )}
          aria-label={msg('managedAgents.sessions.detail.scrollLanesRight', 'Scroll lane tabs right')}
          disabled={!scrollState.right}
          onClick={() => scrollBy('right')}
        >
          <ChevronRight className="size-4" aria-hidden />
        </Button>
      ) : null}
    </div>
  );
}

function laneTabValue(laneId: string) {
  return laneId || SESSION_MAIN_LANE_TAB_VALUE;
}

function laneIdFromTabValue(tabValue: string) {
  return tabValue === SESSION_MAIN_LANE_TAB_VALUE ? SESSION_MAIN_LANE_ID : tabValue;
}

export function LaneTabLabel({ lane }: { lane: SessionDetailLane }) {
  return (
    <TimelineTooltip label={lane.label}>
      <TabsTrigger
        value={laneTabValue(lane.id)}
        data-lane-tab-id={lane.id || 'main'}
        className={clsx(
          'h-8 shrink-0 rounded-md px-2 text-sm font-medium shadow-none after:hidden',
          'bg-transparent text-muted-foreground hover:bg-accent hover:text-foreground',
          'data-active:bg-accent data-active:text-foreground data-active:hover:bg-accent'
        )}
      >
        <span className="block max-w-[88px] truncate">{truncateLaneLabel(lane.label)}</span>
      </TabsTrigger>
    </TimelineTooltip>
  );
}

export function buildLaneTabGroups(lanes: SessionDetailLane[], activeLane: string): LaneTabGroup[] {
  const groups: LaneTabGroup[] = [];
  lanes.forEach((lane) => {
    const key = lane.group || lane.label || lane.id || 'main';
    const existing = groups.find((group) => group.key === key);
    if (existing) {
      existing.lanes.push(lane);
    } else {
      groups.push({ key, label: key, lanes: [lane], collapsed: false });
    }
  });
  return groups.map((group) => {
    const activeInGroup = group.lanes.some((lane) => lane.id === activeLane);
    return {
      ...group,
      collapsed: group.lanes.length > 8 && !activeInGroup
    };
  });
}

export function cssEscape(value: string) {
  const css = typeof CSS !== 'undefined' ? CSS : undefined;
  if (css && typeof css.escape === 'function') {
    return css.escape(value);
  }
  return value.replace(/["\\]/g, '\\$&');
}

export function HeaderRow({
  isSelected,
  children,
  onSelect
}: {
  isSelected: boolean;
  children: ReactNode;
  onSelect: () => void;
}) {
  return (
    <Button
      render={<div />}
      nativeButton={false}
      variant="ghost"
      data-transcript-header
      aria-pressed={isSelected}
      className={clsx(
        'flex h-9 w-[calc(100%+4rem)] cursor-pointer justify-start rounded-none border-0 bg-transparent px-8 text-left font-normal active:translate-y-0',
        '-mx-8',
        isSelected ? 'bg-accent [[data-panel-focused=true]_&]:bg-accent' : 'hover:bg-accent'
      )}
      onClick={onSelect}
    >
      {children}
    </Button>
  );
}

export function MetaStrip({
  usage,
  inferenceMs,
  executionMs,
  lifecycle,
  isError,
  relativeTime,
  processedAtMs
}: {
  usage?: SessionEventUsage;
  inferenceMs?: number;
  executionMs?: number;
  lifecycle?: ToolLifecycle;
  isError?: boolean;
  relativeTime: string;
  processedAtMs?: number;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const absoluteTime = processedAtMs
    ? formatters.date(processedAtMs, {
      month: '2-digit',
      day: '2-digit',
      year: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
      second: '2-digit'
    })
    : '';
  const inputTokens = usage ? usage.input_tokens + usage.cache_read_input_tokens + usage.cache_creation_input_tokens : 0;
  const outputTokens = usage?.output_tokens ?? 0;
  const hasUsage = Boolean(inputTokens || outputTokens);
  const durationText = executionMs !== undefined ? formatSessionDuration(executionMs, formatters, msg) : null;
  const durationTitle = inferenceMs !== undefined
    ? msg('managedAgents.sessions.trace.modelInferenceDuration', 'Model inference: {duration}', {
      duration: formatSessionDuration(inferenceMs, formatters, msg)
    })
    : undefined;
  return (
    <div className="flex shrink-0 items-center gap-3 text-xs tabular-nums text-muted-foreground" data-testid="session-meta-strip">
      {lifecycle === 'running' ? <InProgressChip label={msg('managedAgents.sessions.trace.running', 'Running')} /> : null}
      <ApprovalChip lifecycle={lifecycle} />
      {isError ? <ErrorStateBadge /> : null}
      {hasUsage && usage ? (
        <TimelineTooltip label={sessionTokenUsageTitle(usage, formatters, msg)}>
          <span className="inline-flex items-center gap-1 font-mono">
            <Database className="size-3.5" aria-hidden />
            <span>
              {formatCompactTokenCount(inputTokens, formatters)}
              <span className="text-muted-foreground"> / </span>
              {formatCompactTokenCount(outputTokens, formatters)}
            </span>
          </span>
        </TimelineTooltip>
      ) : null}
      {durationText ? (
        <TimelineTooltip label={durationTitle}>
          <span className="inline-flex items-center gap-1 font-mono">
            <Timer className="size-3.5" aria-hidden />
            {durationText}
          </span>
        </TimelineTooltip>
      ) : null}
      <TimelineTooltip label={absoluteTime || undefined}>
        <span className="w-16 text-right font-mono text-muted-foreground">
          {relativeTime}
        </span>
      </TimelineTooltip>
    </div>
  );
}

export function ErrorStateBadge() {
  const { msg } = useI18n();
  return (
    <span className="inline-flex items-center gap-1 rounded bg-destructive px-1.5 py-0.5 font-sans text-[10px] font-semibold text-background">
      <CircleX className="size-3" aria-hidden />
      {msg('managedAgents.sessions.trace.error', 'Error')}
    </span>
  );
}

export function ApprovalChip({ lifecycle }: { lifecycle?: ToolLifecycle }) {
  const { msg } = useI18n();
  if (lifecycle === 'denied') {
    return (
      <Badge variant="secondary" className="h-auto items-center gap-1 rounded px-1.5 py-0.5 font-sans text-[10px] font-semibold bg-amber-500/10 text-amber-600 dark:text-amber-400">
        <Ban className="size-3" aria-hidden />
        {msg('managedAgents.sessions.trace.denied', 'denied')}
      </Badge>
    );
  }
  if (lifecycle !== 'awaiting_approval') {
    return null;
  }
  return (
    <Badge variant="secondary" className="h-auto rounded bg-accent px-1.5 py-0.5 font-sans text-[10px] font-semibold text-accent-foreground">
      {msg('managedAgents.sessions.trace.awaitingApproval', 'awaiting approval')}
    </Badge>
  );
}

export function InProgressChip({ label, tooltip }: { label: string; tooltip?: string }) {
  const indicator = (
    <span className="inline-flex shrink-0 items-center" role="status">
      <Loader2 className="size-3.5 animate-spin text-muted-foreground" aria-hidden />
      <span className="sr-only">{label}</span>
    </span>
  );
  if (!tooltip) {
    return indicator;
  }
  return (
    <Tooltip>
      <TooltipTrigger render={indicator} />
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}

export const SESSION_SHIMMER_PERIOD_MS = 3000;

export function SynchronizedShimmerText({
  children,
  variant = 'default',
  className
}: {
  children: ReactNode;
  variant?: 'default' | 'secondary';
  className?: string;
}) {
  const [delay] = useState(() => {
    if (typeof performance === 'undefined') {
      return 0;
    }
    return -(performance.now() % SESSION_SHIMMER_PERIOD_MS);
  });
  return (
    <span
      data-cds="ShimmerText"
      className={clsx(
        'session-shimmer-text bg-clip-text text-transparent motion-reduce:bg-none motion-reduce:text-foreground',
        variant === 'secondary' && 'session-shimmer-text-secondary motion-reduce:text-muted-foreground',
        className
      )}
      style={{ animationDelay: `${delay}ms` }}
    >
      {children}
    </span>
  );
}

export function OutcomeStatusChip({ status }: { status?: string }) {
  const { msg } = useI18n();
  if (!status) {
    return null;
  }
  return (
    <Badge variant="secondary" className={clsx('h-auto shrink-0 rounded px-1.5 py-0.5 text-[10px] font-semibold', outcomeStatusChipClass(status))}>
      {outcomeStatusLabel(status, msg)}
    </Badge>
  );
}

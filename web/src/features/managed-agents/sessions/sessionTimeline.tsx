import { useFormatters, useI18n } from '../../../shared/i18n';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import { Skeleton } from '../../../shared/ui/skeleton';
import { Tabs, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { Toggle } from '../../../shared/ui/toggle';
import { Tooltip, TooltipContent, TooltipTrigger } from '../../../shared/ui/tooltip';
import {
  type DisplayEventType,
  type IconComponent,
  type SessionDetailLane,
  type SessionEventUsage,
  type SessionTimelineLane,
  type SessionTimelineItem,
  type SessionTimelineTick,
  type TimelinePickOptions,
  type ToolLifecycle,
} from '../types';
import clsx from 'clsx';
import { Ban, ChevronLeft, ChevronRight, CircleX, Database, Loader2, Minus, Plus, Timer } from 'lucide-react';
import {
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type MutableRefObject,
  type MouseEvent as ReactMouseEvent,
  type ReactElement,
  type ReactNode,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { createPortal } from 'react-dom';
import {
  formatCompactTokenCount,
  formatSessionDuration,
  sessionTokenUsageTitle,
  truncateLaneLabel,
} from './sessionDetailModel';
import { outcomeStatusChipClass, outcomeStatusLabel } from './sessionTraceRows';

export const SESSION_MAIN_LANE_ID = '';
const SESSION_MAIN_LANE_TAB_VALUE = '__oma_main_lane__';
const SESSION_TIMELINE_SINGLE_LANE_HEIGHT_PX = 28;
const SESSION_TIMELINE_LANE_HEIGHT_PX = 28;
const SESSION_TIMELINE_INACTIVE_LANE_HEIGHT_PX = 20;
const SESSION_TIMELINE_LANE_GAP_PX = 6;
const SESSION_TIMELINE_VERTICAL_PADDING_PX = 12;
const SESSION_TIMELINE_MINIMAP_MIN_HEIGHT_PX = 100;
const SESSION_TIMELINE_MINIMAP_DEFAULT_MAX_HEIGHT_PX = 280;
const SESSION_TIMELINE_MINIMAP_RESIZE_THRESHOLD_PX = 52;
const SESSION_TIMELINE_MIN_ZOOM = 1;
const SESSION_TIMELINE_MAX_ZOOM = 4;
const SESSION_TIMELINE_ZOOM_STEP = 0.25;
const SESSION_TIMELINE_IDLE_WINDOW_MIN_MS = 30_000;

export function sessionTimelineLaneHeight(index: number, activeLaneIndex: number) {
  return index === 0 || index === activeLaneIndex
    ? SESSION_TIMELINE_LANE_HEIGHT_PX
    : SESSION_TIMELINE_INACTIVE_LANE_HEIGHT_PX;
}

export function sessionTimelineLaneTop(index: number, activeLaneIndex: number) {
  let top = 0;
  for (let laneIndex = 0; laneIndex < index; laneIndex += 1) {
    top += sessionTimelineLaneHeight(laneIndex, activeLaneIndex) + SESSION_TIMELINE_LANE_GAP_PX;
  }
  return top;
}

export function sessionTimelineLaneContentHeight(laneCount: number, activeLaneIndex: number) {
  if (laneCount <= 0) {
    return 0;
  }
  let height = (laneCount - 1) * SESSION_TIMELINE_LANE_GAP_PX;
  for (let index = 0; index < laneCount; index += 1) {
    height += sessionTimelineLaneHeight(index, activeLaneIndex);
  }
  return height;
}

export function sessionMinimapLayout(laneCount: number, viewportHeight: number) {
  const laneContentHeight = sessionTimelineLaneContentHeight(laneCount, laneCount > 1 ? 1 : 0);
  const contentHeight =
    Math.max(laneContentHeight, SESSION_TIMELINE_SINGLE_LANE_HEIGHT_PX) + SESSION_TIMELINE_VERTICAL_PADDING_PX;
  const minHeight = Math.min(contentHeight, SESSION_TIMELINE_MINIMAP_MIN_HEIGHT_PX);
  const viewportMaxHeight = viewportHeight > 0 ? viewportHeight * 0.6 : contentHeight;
  const maxHeight = Math.max(minHeight, Math.min(contentHeight, viewportMaxHeight));
  const defaultHeight = clampNumber(
    Math.min(contentHeight, SESSION_TIMELINE_MINIMAP_DEFAULT_MAX_HEIGHT_PX),
    minHeight,
    maxHeight,
  );
  return {
    contentHeight,
    defaultHeight,
    laneContentHeight,
    maxHeight,
    minHeight,
    resizable: contentHeight - minHeight >= SESSION_TIMELINE_MINIMAP_RESIZE_THRESHOLD_PX && maxHeight > minHeight,
  };
}

export function clampSessionTimelineZoom(value: number) {
  return clampNumber(value, SESSION_TIMELINE_MIN_ZOOM, SESSION_TIMELINE_MAX_ZOOM);
}

export function sessionMinimapViewportOverflowClassName(zoom: number) {
  return zoom > SESSION_TIMELINE_MIN_ZOOM ? 'overflow-auto' : 'overflow-x-hidden overflow-y-auto';
}

export const SESSION_ARCHIVED_LANES_STORAGE_KEY = 'oma.sessionDetail.showArchivedLanes';

export function SessionStatusPill({ status }: { status: string }) {
  const tone = status.toLowerCase();
  return (
    <Badge
      variant="secondary"
      className={clsx(
        'h-6 shrink-0 rounded-md px-2 text-xs font-medium',
        tone.includes('running') || tone.includes('active')
          ? 'bg-success-bg text-success'
          : tone.includes('error') || tone.includes('failed')
            ? 'bg-destructive/10 text-destructive'
            : tone.includes('queued') || tone.includes('reschedul')
              ? 'bg-warning-bg text-warning'
              : 'bg-secondary text-secondary-foreground',
      )}
    >
      {status}
    </Badge>
  );
}

export function SessionSummaryChip({
  icon: Icon,
  children,
  className,
  tooltip,
}: {
  icon: IconComponent;
  children: ReactNode;
  className?: string;
  tooltip?: string;
}) {
  const chip = (
    <span
      className={clsx('flex min-w-0 max-w-52 shrink-0 items-center gap-2 text-sm text-muted-foreground', className)}
    >
      <span aria-hidden className="text-border">
        ·
      </span>
      <span className="flex min-w-0 items-center gap-1.5">
        <Icon className="size-3.5 shrink-0" aria-hidden />
        <span className="truncate text-foreground/80">{children}</span>
      </span>
    </span>
  );
  if (!tooltip) {
    return chip;
  }
  return (
    <Tooltip>
      <TooltipTrigger render={chip} />
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}

export function EventsMinimap({
  lanes,
  activeLane,
  selectedEntryId,
  hoveredEventId = null,
  controlsSlot = null,
  visibleIds,
  scrollerRef,
  suppressScrollSeekUntilRef,
  onLaneChange,
  onHoverEvent,
  onSeek,
}: {
  lanes: SessionTimelineLane[];
  activeLane: string;
  selectedEntryId: string | null;
  hoveredEventId?: string | null;
  controlsSlot?: HTMLElement | null;
  visibleIds?: Set<string>;
  scrollerRef: RefObject<HTMLDivElement | null>;
  suppressScrollSeekUntilRef: MutableRefObject<number>;
  onLaneChange: (laneId: string, targetEntryId?: string | null) => void;
  onHoverEvent?: (entryId: string | null) => void;
  onSeek: (entryId: string | null) => void;
}) {
  const { msg } = useI18n();
  const hasOpenTick = useMemo(() => lanes.some((lane) => lane.items.some((item) => item.open)), [lanes]);
  const [timelineNowMs, setTimelineNowMs] = useState(Date.now);
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const trackRef = useRef<HTMLDivElement | null>(null);
  const rowRefs = useRef(new Map<string, HTMLDivElement>());
  const dragRef = useRef<{
    startX: number;
    startScrollLeft: number;
    pointerId: number;
    laneId: string;
    dragging: boolean;
    pannable: boolean;
  } | null>(null);
  const suppressClickRef = useRef(false);
  const resizeRef = useRef<{ startHeight: number; startY: number; pointerId: number } | null>(null);
  const heightWasResizedRef = useRef(false);
  const hoveredLaneRef = useRef<string | null>(null);
  const isMultiLane = lanes.length > 1;
  const activeLaneIndex = Math.max(
    0,
    lanes.findIndex((lane) => lane.id === activeLane),
  );
  const [viewportHeight, setViewportHeight] = useState(sessionTimelineViewportHeight);
  const minimapLayout = useMemo(
    () => sessionMinimapLayout(lanes.length, viewportHeight),
    [lanes.length, viewportHeight],
  );
  const [minimapHeight, setMinimapHeight] = useState(minimapLayout.defaultHeight);
  const [zoom, setZoom] = useState(SESSION_TIMELINE_MIN_ZOOM);
  const [trackBaseWidth, setTrackBaseWidth] = useState(0);
  const ticks = useMemo(
    () => buildTimelineTicks(lanes, timelineNowMs, trackBaseWidth * zoom),
    [lanes, timelineNowMs, trackBaseWidth, zoom],
  );
  const [hoveredTickId, setHoveredTickId] = useState<string | null>(null);
  const [hoveredPointerClientX, setHoveredPointerClientX] = useState<number | null>(null);
  const [hoveredLaneId, setHoveredLaneId] = useState<string | null>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [windowRange, setWindowRange] = useState({ leftPct: 1, widthPct: 98 });
  const hoveredTick = ticks.find((tick) => tick.id === hoveredTickId);
  const pointedTickId = hoveredTickId ?? hoveredEventId;
  const activeLaneTicks = useMemo(() => ticks.filter((tick) => tick.lane.id === activeLane), [activeLane, ticks]);
  const selectableActiveLaneTicks = useMemo(
    () => activeLaneTicks.filter((tick) => isTimelineTickSelectable(tick, visibleIds, true)),
    [activeLaneTicks, visibleIds],
  );
  const selectedTickIndex = selectableActiveLaneTicks.findIndex((tick) => tick.id === selectedEntryId);
  const selectedTick = selectableActiveLaneTicks[selectedTickIndex];
  const messageLinks = useMemo(
    () => buildSessionTimelineMessageLinks(ticks, lanes, activeLaneIndex),
    [activeLaneIndex, lanes, ticks],
  );
  const idleWindows = useMemo(() => buildSessionTimelineIdleWindows(ticks), [ticks]);

  useEffect(() => {
    if (!hasOpenTick) {
      return;
    }
    setTimelineNowMs(Date.now());
    const interval = window.setInterval(() => setTimelineNowMs(Date.now()), 250);
    return () => window.clearInterval(interval);
  }, [hasOpenTick]);

  useEffect(() => {
    const handleResize = () => setViewportHeight(window.innerHeight);
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const updateWidth = () => {
      const width = viewport.clientWidth - 6;
      if (width > 0) setTrackBaseWidth(width);
    };
    updateWidth();
    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(updateWidth);
    observer.observe(viewport);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    setMinimapHeight((currentHeight) =>
      heightWasResizedRef.current
        ? clampNumber(currentHeight, minimapLayout.minHeight, minimapLayout.maxHeight)
        : minimapLayout.defaultHeight,
    );
  }, [minimapLayout.defaultHeight, minimapLayout.maxHeight, minimapLayout.minHeight]);

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

  const updateZoom = useCallback(
    (value: number) => {
      const nextZoom = clampSessionTimelineZoom(value);
      if (nextZoom === zoom) {
        return;
      }
      const viewport = viewportRef.current;
      const centerRatio = viewport
        ? (viewport.scrollLeft + viewport.clientWidth / 2) / Math.max(viewport.scrollWidth, viewport.clientWidth)
        : 0.5;
      setZoom(nextZoom);
      window.requestAnimationFrame(() => {
        const nextViewport = viewportRef.current;
        if (!nextViewport) {
          return;
        }
        nextViewport.scrollLeft = Math.max(0, centerRatio * nextViewport.scrollWidth - nextViewport.clientWidth / 2);
      });
    },
    [zoom],
  );

  const updateMinimapHeight = useCallback(
    (height: number) => {
      heightWasResizedRef.current = true;
      setMinimapHeight(clampNumber(height, minimapLayout.minHeight, minimapLayout.maxHeight));
    },
    [minimapLayout.maxHeight, minimapLayout.minHeight],
  );

  const revealTimelineTick = useCallback(
    (tick: SessionTimelineTick) => {
      const viewport = viewportRef.current;
      if (zoom > SESSION_TIMELINE_MIN_ZOOM && viewport) {
        const center = (timelineTickCenterPct(tick) / 100) * viewport.scrollWidth;
        if (center < viewport.scrollLeft || center > viewport.scrollLeft + viewport.clientWidth) {
          viewport.scrollLeft = Math.max(0, center - viewport.clientWidth / 2);
        }
      }
    },
    [zoom],
  );

  const seekToTick = useCallback(
    (tick: SessionTimelineTick) => {
      suppressScrollSync();
      revealTimelineTick(tick);
      onSeek(tick.id);
      scrollSessionEntryToOffset(scrollerRef.current, tick.rowId);
    },
    [onSeek, revealTimelineTick, scrollerRef, suppressScrollSync],
  );

  const changeLaneToTick = useCallback(
    (tick: SessionTimelineTick) => {
      suppressScrollSync();
      revealTimelineTick(tick);
      onLaneChange(tick.lane.id, tick.id);
    },
    [onLaneChange, revealTimelineTick, suppressScrollSync],
  );

  const laneIdAtClientY = useCallback(
    (clientY: number) => {
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
    },
    [activeLane, isMultiLane, lanes],
  );

  const pickTickAtPoint = useCallback(
    (clientX: number, laneId: string, includeIdle = false, maxDistancePct = 2) =>
      pickTimelineTickAtClientX(clientX, trackRef.current, ticks, {
        laneId,
        includeIdle,
        maxDistancePct,
        visibleIds,
      }),
    [ticks, visibleIds],
  );

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
      onHoverEvent?.(null);
    }
  };

  useEffect(() => {
    const tick = selectedEntryId ? ticks.find((candidate) => candidate.id === selectedEntryId) : null;
    if (tick) {
      revealTimelineTick(tick);
    }
  }, [revealTimelineTick, selectedEntryId, ticks]);

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
        revealTimelineTick(firstTick);
        return;
      }
      const lastVisibleTick = [...laneTicks].reverse().find((tick) => visibleEntryIds.has(tick.rowId)) ?? firstTick;
      const leftPct = clampTimelinePct(firstTick.leftPct);
      const rightPct = clampTimelinePct(lastVisibleTick.leftPct + lastVisibleTick.widthPct);
      setWindowRange({
        leftPct,
        widthPct: Math.max(0.8, rightPct - leftPct),
      });
    };
    handleScroll();
    scroller.addEventListener('scroll', handleScroll, { passive: true });
    return () => scroller.removeEventListener('scroll', handleScroll);
  }, [activeLaneTicks, isMultiLane, revealTimelineTick, scrollerRef, suppressScrollSeekUntilRef, visibleIds]);

  const handlePointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    setHoveredPointerClientX(event.clientX);
    const dragState = dragRef.current;
    const laneId = dragState
      ? laneIdAtClientY(event.clientY)
      : (hoveredLaneRef.current ?? laneIdAtClientY(event.clientY));
    if (!dragState && hoveredLaneRef.current === null) {
      updateHoveredLane(laneId);
    }
    if (dragState) {
      updateHoveredLane(laneId);
    }
    const hoverTick = pickTickAtPoint(event.clientX, laneId, true, 1.5);
    setHoveredTickId(hoverTick?.id ?? null);
    onHoverEvent?.(hoverTick?.id ?? null);
    if (!dragState || dragState.pointerId !== event.pointerId) {
      return;
    }
    if (!dragState.dragging && Math.abs(event.clientX - dragState.startX) < 4) {
      return;
    }
    if (!dragState.dragging) {
      dragState.dragging = true;
      suppressClickRef.current = true;
      setIsDragging(dragState.pannable);
    }
    if (!dragState.pannable) {
      return;
    }
    const viewport = viewportRef.current;
    if (viewport) {
      viewport.scrollLeft = clampNumber(
        dragState.startScrollLeft + dragState.startX - event.clientX,
        0,
        Math.max(0, viewport.scrollWidth - viewport.clientWidth),
      );
    }
  };

  const handlePointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) {
      return;
    }
    const laneId = laneIdAtClientY(event.clientY);
    const viewport = viewportRef.current;
    dragRef.current = {
      startX: event.clientX,
      startScrollLeft: viewport?.scrollLeft ?? 0,
      pointerId: event.pointerId,
      laneId,
      dragging: false,
      pannable: Boolean(
        zoom > SESSION_TIMELINE_MIN_ZOOM && viewport && viewport.scrollWidth > viewport.clientWidth + 1,
      ),
    };
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
    if (dragState?.dragging) {
      window.setTimeout(() => {
        suppressClickRef.current = false;
      }, 0);
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
      setHoveredPointerClientX(null);
      onHoverEvent?.(null);
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
    setHoveredPointerClientX(event.clientX);
    const hoverTick = pickTickAtPoint(event.clientX, laneId, true, 1.5);
    setHoveredTickId(hoverTick?.id ?? null);
    onHoverEvent?.(hoverTick?.id ?? null);
  };
  const handleMouseLeave = () => {
    if (!dragRef.current) {
      setHoveredTickId(null);
      setHoveredPointerClientX(null);
      onHoverEvent?.(null);
      updateHoveredLane(null);
    }
  };
  const handleClick = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (suppressClickRef.current) {
      suppressClickRef.current = false;
      return;
    }
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

  const handleTimelineKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (!selectableActiveLaneTicks.length) {
      return;
    }
    let targetIndex: number | null = null;
    if (event.key === 'Home') {
      targetIndex = 0;
    } else if (event.key === 'End') {
      targetIndex = selectableActiveLaneTicks.length - 1;
    } else if (event.key === 'ArrowLeft' || event.key === 'k') {
      targetIndex = selectedTickIndex < 0 ? selectableActiveLaneTicks.length - 1 : Math.max(0, selectedTickIndex - 1);
    } else if (event.key === 'ArrowRight' || event.key === 'j') {
      targetIndex = selectedTickIndex < 0 ? 0 : Math.min(selectableActiveLaneTicks.length - 1, selectedTickIndex + 1);
    }
    if (targetIndex === null) {
      return;
    }
    event.preventDefault();
    seekToTick(selectableActiveLaneTicks[targetIndex]);
  };

  const handleResizePointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) {
      return;
    }
    resizeRef.current = { pointerId: event.pointerId, startHeight: minimapHeight, startY: event.clientY };
    event.currentTarget.setPointerCapture?.(event.pointerId);
  };

  const handleResizePointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const resizeState = resizeRef.current;
    if (!resizeState || resizeState.pointerId !== event.pointerId) {
      return;
    }
    updateMinimapHeight(resizeState.startHeight + event.clientY - resizeState.startY);
  };

  const handleResizePointerUp = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (resizeRef.current?.pointerId !== event.pointerId) {
      return;
    }
    resizeRef.current = null;
    event.currentTarget.releasePointerCapture?.(event.pointerId);
  };

  const handleResizeKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown') {
      return;
    }
    event.preventDefault();
    updateMinimapHeight(minimapHeight + (event.key === 'ArrowUp' ? -16 : 16));
  };

  const zoomControls = (
    <>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        aria-label={msg('managedAgents.sessions.detail.zoomOut', 'Zoom out')}
        disabled={zoom <= SESSION_TIMELINE_MIN_ZOOM}
        onClick={() => updateZoom(zoom - SESSION_TIMELINE_ZOOM_STEP)}
      >
        <Minus className="size-3" aria-hidden />
      </Button>
      <span
        className="min-w-10 text-center font-mono text-[11px] tabular-nums text-muted-foreground"
        aria-live="polite"
      >
        {zoom.toFixed(2)}×
      </span>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        aria-label={msg('managedAgents.sessions.detail.zoomIn', 'Zoom in')}
        disabled={zoom >= SESSION_TIMELINE_MAX_ZOOM}
        onClick={() => updateZoom(zoom + SESSION_TIMELINE_ZOOM_STEP)}
      >
        <Plus className="size-3" aria-hidden />
      </Button>
    </>
  );

  return (
    <div
      className="group/mm relative z-10 shrink-0 px-8 [overflow:clip_visible]"
      role="group"
      aria-label={msg('managedAgents.sessions.detail.eventTimeline', 'Session event timeline')}
      data-zoom={zoom.toFixed(2)}
      data-testid="events-minimap"
    >
      {controlsSlot ? (
        createPortal(
          <div className="flex items-center gap-1" data-testid="session-minimap-zoom-controls">
            {zoomControls}
          </div>,
          controlsSlot,
        )
      ) : (
        <div
          className="pointer-events-none absolute right-1 top-1 z-50 flex items-center gap-1 rounded-md bg-popover px-1.5 py-px opacity-0 transition-opacity duration-150 group-focus-within/mm:pointer-events-auto group-focus-within/mm:opacity-100 group-hover/mm:pointer-events-auto group-hover/mm:opacity-100"
          data-testid="session-minimap-zoom-controls"
        >
          {zoomControls}
        </div>
      )}
      <div
        ref={viewportRef}
        className={clsx(
          'scrollbar-none relative -mx-[3px] flex-none overscroll-x-contain px-[3px]',
          minimapLayout.contentHeight > minimapHeight && 'session-minimap-bottom-fade',
          sessionMinimapViewportOverflowClassName(zoom),
        )}
        style={
          {
            height: `${minimapHeight}px`,
            maxHeight: `min(60vh, ${minimapLayout.contentHeight}px)`,
            minHeight: `${minimapLayout.minHeight}px`,
            paddingBottom: 4,
            paddingTop: 8,
          } as CSSProperties
        }
        data-testid="session-minimap-viewport"
      >
        <div
          ref={trackRef}
          data-dragging={isDragging || undefined}
          data-testid="session-minimap-track"
          role="slider"
          aria-label={msg('managedAgents.sessions.detail.seekTimeline', 'Seek session event timeline')}
          aria-orientation="horizontal"
          aria-valuemin={0}
          aria-valuemax={Math.max(0, selectableActiveLaneTicks.length - 1)}
          aria-valuenow={Math.max(0, selectedTickIndex)}
          aria-valuetext={selectedTick ? `${selectedTick.relativeTime} · ${selectedTick.label}` : undefined}
          tabIndex={selectableActiveLaneTicks.length ? 0 : -1}
          className={clsx(
            'relative flex touch-none select-none flex-col gap-1.5 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
            sessionMinimapCursorClassName(zoom > SESSION_TIMELINE_MIN_ZOOM, isDragging),
          )}
          style={sessionMinimapTrackStyle(minimapLayout.laneContentHeight, zoom)}
          onKeyDown={handleTimelineKeyDown}
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
            <div
              className="pointer-events-none invisible absolute inset-y-0 rounded"
              style={{ left: `${windowRange.leftPct}%`, width: `${windowRange.widthPct}%` }}
              aria-hidden
            />
          ) : null}
          {!isMultiLane ? (
            <div
              ref={(node) => setSessionTimelineRowRef(rowRefs.current, activeLane, node)}
              className="relative h-7 rounded bg-muted"
              data-minimap-track-state="active"
            >
              <SessionTimelineIdleWash laneId={activeLane} windows={idleWindows} />
              {ticks.map((tick) => (
                <SessionTimelineTickMark
                  key={tick.id}
                  tick={tick}
                  selected={tick.id === selectedEntryId}
                  hovered={tick.id === pointedTickId}
                  hidden={Boolean(visibleIds && !visibleIds.has(tick.id))}
                />
              ))}
            </div>
          ) : (
            lanes.map((lane, index) => (
              <div
                key={lane.id || 'main'}
                ref={(node) => setSessionTimelineRowRef(rowRefs.current, lane.id, node)}
                data-lane-index={index}
                className={clsx(
                  "group/lane relative flex shrink-0 items-center rounded transition-[background-color,opacity,height] duration-100 ease-out after:pointer-events-none after:absolute after:inset-x-0 after:-bottom-1.5 after:h-1.5 after:content-['']",
                  index === 0 || index === activeLaneIndex ? 'h-7' : 'h-5',
                  index === 0 && 'sticky top-0 z-20',
                  lane.id === activeLane
                    ? 'bg-muted'
                    : lane.id === hoveredLaneId
                      ? 'bg-muted/50 opacity-100'
                      : 'bg-muted/40 opacity-[0.85]',
                  lane.id !== activeLane && 'cursor-pointer',
                )}
                data-minimap-track-state={
                  lane.id === activeLane ? 'active' : lane.id === hoveredLaneId ? 'hovered' : 'inactive'
                }
                onPointerDown={(event) => handleInactiveLanePointerDown(lane.id, event)}
                onPointerEnter={() => handleLanePointerEnter(lane.id)}
                onPointerLeave={(event) => handleLanePointerLeave(lane.id, event)}
              >
                <SessionTimelineIdleWash laneId={lane.id} windows={idleWindows} />
                <span className="pointer-events-none sticky left-0 z-10 ml-0.5 inline-flex h-4 max-w-[180px] items-center truncate rounded-sm bg-background/90 px-1 text-[10px] text-foreground shadow-sm transition-opacity duration-150 group-hover/lane:opacity-0">
                  {truncateLaneLabel(lane.label)}
                </span>
                {ticks
                  .filter((tick) => tick.lane.id === lane.id)
                  .map((tick) => (
                    <SessionTimelineTickMark
                      key={tick.id}
                      tick={tick}
                      selected={tick.id === selectedEntryId}
                      hovered={tick.id === pointedTickId}
                      hidden={Boolean(visibleIds && !visibleIds.has(tick.id))}
                    />
                  ))}
              </div>
            ))
          )}
          {messageLinks.length ? (
            <SessionTimelineMessageLinks links={messageLinks} height={minimapLayout.laneContentHeight} />
          ) : null}
          {hoveredTick ? (
            <SessionTimelineTooltip
              tick={hoveredTick}
              row={rowRefs.current.get(hoveredTick.lane.id) ?? null}
              pointerClientX={hoveredPointerClientX}
            />
          ) : null}
        </div>
      </div>
      {minimapLayout.resizable ? (
        <div
          role="separator"
          aria-orientation="horizontal"
          aria-label={msg('managedAgents.sessions.detail.resizeMinimap', 'Resize minimap')}
          aria-valuenow={Math.round(minimapHeight)}
          aria-valuemin={Math.round(minimapLayout.minHeight)}
          aria-valuemax={Math.round(minimapLayout.maxHeight)}
          tabIndex={0}
          className="group/resize relative -mx-8 -mb-[3px] mt-[9px] h-2 touch-none cursor-row-resize outline-none after:absolute after:inset-x-0 after:top-1/2 after:h-px after:bg-border/60 after:transition-colors hover:after:bg-border focus-visible:after:bg-ring"
          onKeyDown={handleResizeKeyDown}
          onPointerDown={handleResizePointerDown}
          onPointerMove={handleResizePointerMove}
          onPointerUp={handleResizePointerUp}
          onPointerCancel={handleResizePointerUp}
        />
      ) : (
        <div
          aria-hidden
          className="relative -mx-8 -mb-[3px] mt-[9px] h-2 after:absolute after:inset-x-0 after:top-1/2 after:h-px after:bg-border/60"
        />
      )}
    </div>
  );
}

export function EventsMinimapSkeleton() {
  return (
    <div className="px-8" data-testid="events-minimap-skeleton" aria-hidden>
      <div style={{ paddingBottom: 4, paddingTop: 8 }}>
        <Skeleton className="h-7 rounded-sm" />
      </div>
      <div className="relative -mx-8 -mb-[3px] mt-[9px] h-2 after:absolute after:inset-x-0 after:top-1/2 after:h-px after:bg-border/60" />
    </div>
  );
}

function sessionTimelineViewportHeight() {
  return typeof window === 'undefined' ? 0 : window.innerHeight;
}

function setSessionTimelineRowRef(rows: Map<string, HTMLDivElement>, laneId: string, node: HTMLDivElement | null) {
  if (node) {
    rows.set(laneId, node);
  } else {
    rows.delete(laneId);
  }
}

function sessionMinimapCursorClassName(pannable: boolean, dragging: boolean) {
  if (!pannable) return 'cursor-default';
  return dragging ? 'cursor-grabbing' : 'cursor-grab active:cursor-grabbing';
}

function sessionMinimapTrackStyle(laneContentHeight: number, zoom: number): CSSProperties {
  const style: CSSProperties = {
    height: `${laneContentHeight || SESSION_TIMELINE_SINGLE_LANE_HEIGHT_PX}px`,
  };
  if (zoom > SESSION_TIMELINE_MIN_ZOOM) style.width = `${zoom * 100}%`;
  return style;
}

type SessionTimelineMessageLink = {
  id: string;
  path: string;
};

type SessionTimelineIdleWindow = {
  id: string;
  laneId: string;
  leftPct: number;
  widthPct: number;
};

export function buildSessionTimelineIdleWindows(ticks: SessionTimelineTick[]): SessionTimelineIdleWindow[] {
  const laneIds = new Set(ticks.map((tick) => tick.lane.id));
  return [...laneIds].flatMap((laneId) => {
    const laneTicks = ticks.filter((tick) => tick.lane.id === laneId).sort((left, right) => left.ms - right.ms);
    return laneTicks.flatMap((tick, index) => {
      if (tick.type !== 'status_idle') return [];
      if ((tick.durationMs ?? 0) >= SESSION_TIMELINE_IDLE_WINDOW_MIN_MS) {
        return [{ id: tick.id, laneId, leftPct: tick.leftPct, widthPct: tick.widthPct }];
      }
      const runningTick = laneTicks.slice(index + 1).find((candidate) => candidate.type === 'status_running');
      if (!runningTick || runningTick.ms - tick.ms < SESSION_TIMELINE_IDLE_WINDOW_MIN_MS) return [];
      return [
        {
          id: `${tick.id}:${runningTick.id}`,
          laneId,
          leftPct: tick.leftPct,
          widthPct: Math.max(0, runningTick.leftPct - tick.leftPct),
        },
      ];
    });
  });
}

function SessionTimelineIdleWash({ laneId, windows }: { laneId: string; windows: SessionTimelineIdleWindow[] }) {
  const laneWindows = windows.filter((window) => window.laneId === laneId);
  if (!laneWindows.length) return null;
  return (
    <div className="pointer-events-none absolute inset-0 overflow-hidden rounded-sm" aria-hidden>
      {laneWindows.map((window) => (
        <span
          key={window.id}
          data-minimap-idle-window={window.id}
          className="absolute inset-y-0 rounded-sm bg-background/80"
          style={{ left: `${window.leftPct}%`, width: `${window.widthPct}%` }}
        />
      ))}
    </div>
  );
}

export function buildSessionTimelineMessageLinks(
  ticks: SessionTimelineTick[],
  lanes: SessionTimelineLane[],
  activeLaneIndex: number,
): SessionTimelineMessageLink[] {
  const laneIndexById = new Map(lanes.map((lane, index) => [lane.id, index]));
  const laneCenter = (index: number) =>
    sessionTimelineLaneTop(index, activeLaneIndex) + sessionTimelineLaneHeight(index, activeLaneIndex) / 2;
  const receivedTicks = ticks.filter((tick) => tick.threadMessage?.direction === 'received');
  const usedReceivedTickIds = new Set<string>();
  return ticks.flatMap((tick) => {
    const message = tick.threadMessage;
    const currentLaneIndex = laneIndexById.get(tick.lane.id);
    const connectedLaneIndex = message ? laneIndexById.get(message.laneId) : undefined;
    if (
      !message ||
      message.direction !== 'sent' ||
      currentLaneIndex === undefined ||
      connectedLaneIndex === undefined ||
      currentLaneIndex === connectedLaneIndex
    ) {
      return [];
    }
    const receivedTick = receivedTicks
      .filter(
        (candidate) =>
          !usedReceivedTickIds.has(candidate.id) &&
          candidate.lane.id === message.laneId &&
          candidate.threadMessage?.laneId === tick.lane.id &&
          candidate.ms >= tick.ms,
      )
      .sort((left, right) => left.ms - right.ms)[0];
    if (receivedTick) usedReceivedTickIds.add(receivedTick.id);
    const fromX = clampTimelinePct(tick.leftPct + tick.widthPct);
    const toX = receivedTick ? receivedTick.leftPct : timelineTickCenterPct(tick);
    return [
      {
        id: receivedTick ? `${receivedTick.id}:${tick.id}` : tick.id,
        path: sessionTimelineMessageLinkPath(fromX, toX, laneCenter(currentLaneIndex), laneCenter(connectedLaneIndex)),
      },
    ];
  });
}

function sessionTimelineMessageLinkPath(fromX: number, toX: number, fromY: number, toY: number) {
  if (toX - fromX < 2) {
    return `M ${toX} ${fromY} V ${toY}`;
  }
  const beforeArrowX = toX - 0.75;
  const controlX = (fromX + beforeArrowX) / 2;
  return `M ${fromX} ${fromY} C ${controlX} ${fromY} ${controlX} ${toY} ${beforeArrowX} ${toY} H ${toX}`;
}

function SessionTimelineMessageLinks({ links, height }: { links: SessionTimelineMessageLink[]; height: number }) {
  return (
    <svg
      className="pointer-events-none absolute inset-0 h-full w-full overflow-visible text-muted-foreground/55"
      data-testid="session-minimap-message-links"
      viewBox={`0 0 100 ${height}`}
      preserveAspectRatio="none"
      aria-hidden
    >
      <defs>
        <marker
          id="session-minimap-arrowhead"
          viewBox="0 0 6 6"
          refX="5"
          refY="3"
          markerWidth="6"
          markerHeight="6"
          orient="auto-start-reverse"
        >
          <path d="M0,0 L6,3 L0,6 z" fill="currentColor" />
        </marker>
      </defs>
      <g fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
        {links.map((link) => (
          <path
            key={link.id}
            data-timeline-message-link={link.id}
            d={link.path}
            markerEnd="url(#session-minimap-arrowhead)"
            vectorEffect="non-scaling-stroke"
          />
        ))}
      </g>
    </svg>
  );
}

export function SessionTimelineTickMark({
  tick,
  selected,
  hovered,
  hidden,
}: {
  tick: SessionTimelineTick;
  selected: boolean;
  hovered: boolean;
  hidden: boolean;
}) {
  if (tick.type === 'status_idle') return null;
  const style: CSSProperties = {
    left: `${tick.leftPct}%`,
    minWidth: 3,
    width: `${tick.widthPct}%`,
  };
  return (
    <span
      data-timeline-tick-id={tick.id}
      data-timeline-tick-type={tick.type}
      className={clsx(
        'pointer-events-none absolute bottom-0.5 top-0.5 rounded-sm transition-[left,width,opacity] duration-150',
        sessionTimelineTickClass(tick.type),
        selected && 'z-30 outline outline-[1.5px] outline-ring outline-offset-1',
        !selected && hovered && 'z-30 outline outline-[1.5px] outline-ring/50 outline-offset-1',
        !selected && !hovered && 'opacity-90',
        tick.open && 'motion-safe:animate-pulse',
        hidden && '!opacity-0',
      )}
      style={style}
      aria-hidden
    />
  );
}

export function SessionTimelineTooltip({
  tick,
  row,
  pointerClientX,
}: {
  tick: SessionTimelineTick;
  row: HTMLDivElement | null;
  pointerClientX: number | null;
}) {
  if (!row || pointerClientX === null || typeof document === 'undefined') {
    return null;
  }
  const rowRect = row.getBoundingClientRect();
  const belowLane = Number(row.dataset.laneIndex ?? 0) > 0;
  const tooltipId = `session-timeline-tooltip-${tick.id}`;
  const title = tick.preview ?? tick.label;
  const duration = formatTimelineDuration(tick.durationMs);
  return createPortal(
    <div
      id={tooltipId}
      role="tooltip"
      className="pointer-events-none fixed left-0 top-0 z-50 flex w-80 max-w-[calc(100vw-1rem)] flex-col gap-0.5 rounded-lg border-[0.5px] border-border bg-popover px-2 py-1.5 text-xs text-popover-foreground shadow-md"
      style={{
        translate: `min(${pointerClientX - 8}px, calc(100vw - 8px - 100%)) ${
          belowLane ? `${rowRect.bottom + 4}px` : `calc(${Math.max(56, rowRect.top) - 4}px - 100%)`
        }`,
      }}
    >
      <div className="flex items-baseline justify-between gap-2 whitespace-nowrap">
        <span
          className={clsx(
            'inline-flex h-4 min-w-0 shrink rounded-sm px-1 text-[10px] font-semibold uppercase leading-4',
            sessionTimelineTickClass(tick.type),
          )}
        >
          {sessionTimelineTypeLabel(tick.type)}
        </span>
        <span className="shrink-0 font-mono text-xs tabular-nums text-muted-foreground">
          {duration ? `${duration} · ` : null}
          {tick.relativeTime}
        </span>
      </div>
      {title ? <div className="truncate text-foreground">{title}</div> : null}
    </div>,
    document.body,
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

export function sessionTimelineTickClass(type: DisplayEventType) {
  switch (type) {
    case 'user':
      return 'bg-session-speaker-user/70';
    case 'error':
      return 'bg-destructive';
    case 'agent':
    case 'thinking':
      return 'bg-session-speaker-agent/70';
    case 'subagent':
      return 'bg-chart-2/80';
    case 'status_idle':
      return 'bg-muted-foreground/40';
    case 'tool_use':
    case 'result':
      return 'bg-muted-foreground/60';
    case 'thread':
      return 'bg-chart-2/80';
    case 'status_rescheduled':
    case 'interrupt':
    case 'model_request':
    case 'outcome':
    case 'status_running':
    case 'status_terminated':
    case 'root':
    case 'system_message':
    case 'unknown':
    default:
      return 'bg-muted-foreground/40';
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
    (node) => node.getAttribute('data-event-id') === entryId,
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
  options: TimelinePickOptions = {},
) {
  return pickTimelineTickAtPercent(clientXToTimelinePct(clientX, track), ticks, options);
}

export function pickTimelineTickAtPercent(
  percent: number,
  ticks: SessionTimelineTick[],
  options: TimelinePickOptions = {},
) {
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

export function clampNumber(value: number, min: number, max: number) {
  return Math.max(min, Math.min(max, value));
}

export function timelineTickCenterPct(tick: SessionTimelineTick) {
  return clampTimelinePct(tick.leftPct + tick.widthPct / 2);
}

export function nearestTimelineTickForLane(
  ticks: SessionTimelineTick[],
  laneId: string,
  anchorPct: number,
  visibleIds?: Set<string>,
) {
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

export function buildTimelineTicks(
  lanes: SessionTimelineLane[],
  nowMs = Date.now(),
  trackWidthPx = 1_000,
): SessionTimelineTick[] {
  const flattened = lanes
    .flatMap((lane) => lane.items.map((item) => ({ ...item, lane })))
    .filter((item) => Number.isFinite(item.processedAtMs));
  if (!flattened.length) {
    return [];
  }

  const renderDurations = flattened.map((item) =>
    Math.max(0, item.durationMs ?? 0, item.open ? nowMs - item.processedAtMs : 0),
  );
  const resolvedTrackWidth = trackWidthPx > 0 ? trackWidthPx : 1_000;
  const timeScale = sessionTimelineTimeScale(flattened, renderDurations, resolvedTrackWidth);
  const minWidthPct = (3 / resolvedTrackWidth) * 100;

  return flattened.map((item, index) => {
    const startOffset = timeScale.offsetByMs.get(item.processedAtMs) ?? 0;
    const endOffset = timeScale.offsetByMs.get(item.processedAtMs + renderDurations[index]) ?? startOffset;
    let leftPct = 1 + (startOffset / timeScale.total) * 98;
    let widthPct = Math.min(98, Math.max(minWidthPct, ((endOffset - startOffset) / timeScale.total) * 98));
    const overflow = leftPct + widthPct - 99;
    if (overflow > 0) {
      const shrink = Math.min(widthPct - minWidthPct, overflow);
      widthPct -= shrink;
      leftPct -= overflow - shrink;
    }
    return {
      ...item,
      leftPct: Math.max(1, Math.min(99 - widthPct, leftPct)),
      widthPct,
      ms: item.processedAtMs,
    };
  });
}

function sessionTimelineTimeScale(
  items: Array<SessionTimelineItem & { lane: SessionTimelineLane }>,
  durations: number[],
  trackWidthPx: number,
) {
  const domainStart = Math.min(...items.map((item) => item.processedAtMs));
  const domainEnd = Math.max(
    domainStart + 10_000,
    ...items.map((item, index) => item.processedAtMs + durations[index]),
  );
  const points = [
    ...new Set([
      domainStart,
      domainEnd,
      ...items.flatMap((item, index) => [item.processedAtMs, item.processedAtMs + durations[index]]),
    ]),
  ].sort((left, right) => left - right);
  const spans = points.slice(1).map((point, index) => point - points[index]);
  const idleRanges = items.flatMap((item, index) =>
    item.type === 'status_idle' && durations[index] > 0
      ? [[item.processedAtMs, item.processedAtMs + durations[index]] as const]
      : [],
  );
  const activeRanges = items.flatMap((item, index) =>
    item.type !== 'status_idle' && durations[index] > 0
      ? [[item.processedAtMs, item.processedAtMs + durations[index]] as const]
      : [],
  );
  const idleSpans = spans.map((span, index) => {
    const midpoint = points[index] + span / 2;
    const insideIdle = idleRanges.some(([start, end]) => start < midpoint && midpoint < end);
    const insideActive = activeRanges.some(([start, end]) => start < midpoint && midpoint < end);
    return insideIdle && !insideActive;
  });
  const fixedIdleWidth = 11;
  const activeDuration = spans.reduce((total, span, index) => total + (idleSpans[index] ? 0 : span), 0);
  const innerWidth = Math.max(1, trackWidthPx * 0.98);
  const activeWidth = Math.max(1, innerWidth - idleSpans.filter(Boolean).length * fixedIdleWidth);
  const activePxPerMs = activeWidth / Math.max(1, activeDuration);
  const offsetByMs = new Map<number, number>([[points[0], 0]]);
  let total = 0;
  spans.forEach((span, index) => {
    total += idleSpans[index] ? fixedIdleWidth : span * activePxPerMs;
    offsetByMs.set(points[index + 1], total);
  });
  return { offsetByMs, total: Math.max(1, total) };
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
  onChange,
}: {
  lanes: SessionDetailLane[];
  activeLane: string;
  archivedLaneCount: number;
  isMultiAgent: boolean;
  selectedEntryId: string | null;
  showArchivedLanes: boolean;
  timeline: SessionTimelineLane[];
  timelineVisibleIds?: Set<string>;
  onToggleArchivedLanes: (nextPressed: boolean) => void;
  onChange: (laneId: string, targetEntryId?: string | null) => void;
}) {
  const { msg } = useI18n();
  const scrollerRef = useRef<HTMLDivElement | null>(null);
  const [scrollState, setScrollState] = useState({ canScroll: false, left: false, right: false });
  const timelineTicks = useMemo(() => buildTimelineTicks(timeline ?? []), [timeline]);
  const selectedTick = selectedEntryId ? (timelineTicks.find((tick) => tick.id === selectedEntryId) ?? null) : null;
  const activeTick =
    selectedTick ??
    timelineTicks.find(
      (tick) => tick.lane.id === activeLane && isTimelineTickSelectable(tick, timelineVisibleIds, false),
    ) ??
    null;
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
      right: canScroll && scroller.scrollLeft < scroller.scrollWidth - scroller.clientWidth - 1,
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
  }, [refreshScrollState, lanes.length]);

  useEffect(() => {
    const activeTab = Array.from(scrollerRef.current?.querySelectorAll<HTMLElement>('[data-lane-tab-id]') ?? []).find(
      (tab) => tab.dataset.laneTabId === (activeLane || 'main'),
    );
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
    scroller.scrollBy({
      left: direction === 'left' ? -Math.floor(scroller.clientWidth * 0.8) : Math.floor(scroller.clientWidth * 0.8),
      behavior: 'smooth',
    });
  };
  return (
    <div className="flex items-center gap-2 border-b border-border px-4 py-2" data-testid="lane-tab-strip">
      {scrollState.canScroll ? (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          className={clsx(
            'size-6 text-muted-foreground hover:bg-accent hover:text-foreground',
            !scrollState.left && 'pointer-events-none opacity-30',
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
        className="scrollbar-none flex min-w-0 flex-1 gap-1 overflow-x-auto"
        style={{
          maskImage: scrollState.canScroll
            ? 'linear-gradient(90deg, transparent 0, #000 24px, #000 calc(100% - 24px), transparent 100%)'
            : undefined,
        }}
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
            {lanes.map((lane) => (
              <LaneTabLabel key={lane.id || 'main'} lane={lane} />
            ))}
          </TabsList>
        </Tabs>
        {archivedLaneCount > 0 ? (
          <Toggle
            type="button"
            size="sm"
            className={clsx(
              'shrink-0 rounded-md text-sm font-medium shadow-none',
              !showArchivedLanes && 'text-muted-foreground',
            )}
            pressed={showArchivedLanes}
            onPressedChange={onToggleArchivedLanes}
          >
            {msg('managedAgents.sessions.detail.archivedLanes', '+{count} archived', { count: archivedLaneCount })}
          </Toggle>
        ) : null}
      </div>
      {scrollState.canScroll ? (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          className={clsx(
            'size-6 text-muted-foreground hover:bg-accent hover:text-foreground',
            !scrollState.right && 'pointer-events-none opacity-30',
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
          'data-active:bg-accent data-active:text-foreground data-active:hover:bg-accent',
        )}
      >
        <span className="block max-w-[88px] truncate">{truncateLaneLabel(lane.label)}</span>
      </TabsTrigger>
    </TimelineTooltip>
  );
}

export function HeaderRow({
  isSelected,
  density = 'default',
  children,
  onSelect,
}: {
  isSelected: boolean;
  density?: 'default' | 'compact';
  children: ReactNode;
  onSelect: () => void;
}) {
  return (
    <Toggle
      render={<div />}
      nativeButton={false}
      data-transcript-header
      pressed={isSelected}
      className={clsx(
        'flex w-full cursor-pointer justify-start rounded-md border-0 bg-transparent text-left font-normal active:translate-y-0 focus-visible:border-transparent focus-visible:ring-1 focus-visible:ring-ring/30',
        density === 'compact' ? 'h-6 gap-1.5 px-1 text-xs' : 'h-9 px-3',
        'hover:bg-session-hover',
        isSelected && 'bg-session-selected',
      )}
      onPressedChange={() => onSelect()}
    >
      {children}
    </Toggle>
  );
}

export function MetaStrip({
  usage,
  inferenceMs,
  executionMs,
  lifecycle,
  isError,
  relativeTime,
  processedAtMs,
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
        second: '2-digit',
      })
    : '';
  const inputTokens = usage
    ? usage.input_tokens + usage.cache_read_input_tokens + usage.cache_creation_input_tokens
    : 0;
  const outputTokens = usage?.output_tokens ?? 0;
  const hasUsage = Boolean(inputTokens || outputTokens);
  const durationText = executionMs !== undefined ? formatSessionDuration(executionMs, formatters, msg) : null;
  const durationTitle =
    inferenceMs !== undefined
      ? msg('managedAgents.sessions.trace.modelInferenceDuration', 'Model inference: {duration}', {
          duration: formatSessionDuration(inferenceMs, formatters, msg),
        })
      : undefined;
  return (
    <div
      className="flex shrink-0 items-center gap-3 text-xs tabular-nums text-muted-foreground"
      data-testid="session-meta-strip"
    >
      {lifecycle === 'running' ? (
        <InProgressChip label={msg('managedAgents.sessions.trace.running', 'Running')} />
      ) : null}
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
        <span className="w-16 text-right font-mono text-muted-foreground">{relativeTime}</span>
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
      <Badge
        variant="secondary"
        className="h-auto items-center gap-1 rounded bg-warning-bg px-1.5 py-0.5 font-sans text-[10px] font-semibold text-warning"
      >
        <Ban className="size-3" aria-hidden />
        {msg('managedAgents.sessions.trace.denied', 'denied')}
      </Badge>
    );
  }
  if (lifecycle !== 'awaiting_approval') {
    return null;
  }
  return (
    <Badge
      variant="secondary"
      className="h-auto rounded bg-accent px-1.5 py-0.5 font-sans text-[10px] font-semibold text-accent-foreground"
    >
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
  className,
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
        className,
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
    <Badge
      variant="secondary"
      className={clsx(
        'h-auto shrink-0 rounded px-1.5 py-0.5 text-[10px] font-semibold',
        outcomeStatusChipClass(status),
      )}
    >
      {outcomeStatusLabel(status, msg)}
    </Badge>
  );
}

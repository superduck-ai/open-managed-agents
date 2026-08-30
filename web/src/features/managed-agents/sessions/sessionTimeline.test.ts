import { describe, expect, test } from 'bun:test';
import { type SessionTimelineLane } from '../types';
import {
  buildSessionTimelineIdleWindows,
  buildSessionTimelineMessageLinks,
  buildTimelineTicks,
  clampSessionTimelineZoom,
  sessionMinimapViewportOverflowClassName,
  sessionMinimapLayout,
} from './sessionTimeline';
import { buildSessionTimeline } from './sessionDetailModel';
import { buildSessionEventEntries, idleGapEntry } from './sessionTraceModel';

describe('session timeline event order', () => {
  test('preserves item order while sharing timestamp geometry across lanes', () => {
    const lanes: SessionTimelineLane[] = [
      {
        id: '',
        label: 'Main',
        isMain: true,
        items: [timelineItem('sevt_first', 2_000), timelineItem('sevt_second', 1_000)],
      },
      {
        id: 'thread-child',
        label: 'Child',
        items: [timelineItem('sevt_child_same_time', 2_000)],
      },
    ];

    const ticks = buildTimelineTicks(lanes);
    expect(ticks.map((tick) => tick.id)).toEqual(['sevt_first', 'sevt_second', 'sevt_child_same_time']);
    expect(ticks[1]?.leftPct).toBeLessThan(ticks[0]?.leftPct ?? 0);
    expect(ticks[2]?.leftPct).toBe(ticks[0]?.leftPct);
  });

  test('grows an open model request and keeps its width when the request completes', () => {
    const startAt = '2026-08-29T01:00:00.000Z';
    const endAt = '2026-08-29T01:00:05.000Z';
    const start = { id: 'model-start', type: 'span.model_request_start', processed_at: startAt };
    const message = {
      id: 'agent-message',
      type: 'agent.message',
      processed_at: '2026-08-29T01:00:01.000Z',
      content: [{ type: 'text', text: 'Streaming answer' }],
    };
    const end = {
      id: 'model-end',
      type: 'span.model_request_end',
      model_request_start_id: start.id,
      processed_at: endAt,
    };
    const idle = { id: 'session-idle', type: 'session.status_idle', processed_at: endAt };

    const earlyWidth = modelRequestTickWidth([start, message], Date.parse(startAt) + 2_000);
    const liveWidthAtEnd = modelRequestTickWidth([start, message], Date.parse(endAt));
    const completedWidth = modelRequestTickWidth([start, message, end], Date.parse(endAt) + 10_000);
    const reconciledFromIdleWidth = modelRequestTickWidth([start, message, idle], Date.parse(endAt) + 10_000);

    expect(earlyWidth).toBeGreaterThan(0.4);
    expect(liveWidthAtEnd).toBeGreaterThan(earlyWidth);
    expect(completedWidth).toBeCloseTo(liveWidthAtEnd, 6);
    expect(reconciledFromIdleWidth).toBeCloseTo(liveWidthAtEnd, 6);
  });
});

describe('Claude minimap geometry', () => {
  test('starts a synthetic idle window at the idle boundary', () => {
    const lane = { id: '', label: 'Main', isMain: true };
    const timeline = buildSessionTimeline([lane], new Map([[lane.id, [idleGapEntry(6_000, 186_000, 0)]]]));

    expect(timeline[0]?.items[0]?.processedAtMs).toBe(6_000);
    expect(timeline[0]?.items[0]?.durationMs).toBe(180_000);
  });

  test('collapses a long idle gap to eleven pixels without squeezing the next turn', () => {
    const lane: SessionTimelineLane = {
      id: '',
      label: 'Main',
      isMain: true,
      items: [
        { ...timelineItem('first-turn', 1_000), durationMs: 5_000 },
        { ...timelineItem('idle-gap', 6_000), type: 'status_idle', durationMs: 180_000 },
        { ...timelineItem('second-turn', 186_000), durationMs: 5_000 },
      ],
    };

    const ticks = buildTimelineTicks([lane], 191_000, 1_000);
    const firstTurn = ticks.find((tick) => tick.id === 'first-turn');
    const idle = ticks.find((tick) => tick.id === 'idle-gap');
    const secondTurn = ticks.find((tick) => tick.id === 'second-turn');

    expect(idle?.widthPct).toBeCloseTo(1.1, 1);
    expect(secondTurn?.widthPct).toBeCloseTo(firstTurn?.widthPct ?? 0, 6);
  });

  test('uses the official default, min, max, and resize thresholds', () => {
    expect(sessionMinimapLayout(1, 800)).toEqual({
      contentHeight: 40,
      defaultHeight: 40,
      laneContentHeight: 28,
      maxHeight: 40,
      minHeight: 40,
      resizable: false,
    });

    const manyLanes = sessionMinimapLayout(10, 800);
    expect(manyLanes).toEqual({
      contentHeight: 282,
      defaultHeight: 280,
      laneContentHeight: 270,
      maxHeight: 282,
      minHeight: 100,
      resizable: true,
    });
    expect(sessionMinimapLayout(10, 300).defaultHeight).toBe(180);
  });

  test('clamps zoom to the official 1x–4x range', () => {
    expect(clampSessionTimelineZoom(0.75)).toBe(1);
    expect(clampSessionTimelineZoom(1.25)).toBe(1.25);
    expect(clampSessionTimelineZoom(4.25)).toBe(4);
  });

  test('only enables horizontal minimap overflow while zoomed', () => {
    expect(sessionMinimapViewportOverflowClassName(1)).toBe('overflow-x-hidden overflow-y-auto');
    expect(sessionMinimapViewportOverflowClassName(1.25)).toBe('overflow-auto');
  });

  test('draws thread-message arrows only between known lanes', () => {
    const lanes: SessionTimelineLane[] = [
      {
        id: '',
        label: 'Main',
        isMain: true,
        items: [
          {
            ...timelineItem('sevt_sent', 1_000),
            threadMessage: { direction: 'sent', laneId: 'thread-child' },
          },
          {
            ...timelineItem('sevt_unknown', 3_000),
            threadMessage: { direction: 'sent', laneId: 'thread-missing' },
          },
        ],
      },
      {
        id: 'thread-child',
        label: 'Child',
        items: [
          {
            ...timelineItem('sevt_received', 2_000),
            threadMessage: { direction: 'received', laneId: '' },
          },
        ],
      },
    ];

    const links = buildSessionTimelineMessageLinks(buildTimelineTicks(lanes), lanes, 0);
    expect(links).toHaveLength(1);
    expect(links[0]?.id).toBe('sevt_received:sevt_sent');
    expect(links[0]?.path).toContain('C');
  });

  test('uses only reliable idle-to-running intervals as lane background windows', () => {
    const lane: SessionTimelineLane = {
      id: '',
      label: 'Main',
      isMain: true,
      items: [
        { ...timelineItem('idle-long', 1_000), type: 'status_idle' },
        { ...timelineItem('running-after-long-idle', 32_000), type: 'status_running' },
        { ...timelineItem('idle-short', 40_000), type: 'status_idle' },
        { ...timelineItem('running-after-short-idle', 50_000), type: 'status_running' },
      ],
    };

    const windows = buildSessionTimelineIdleWindows(buildTimelineTicks([lane]));
    expect(windows).toHaveLength(1);
    expect(windows[0]?.id).toBe('idle-long:running-after-long-idle');
    expect(windows[0]?.widthPct).toBeGreaterThan(0);
  });
});

function timelineItem(id: string, processedAtMs: number) {
  return {
    id,
    rowId: `row-${id}`,
    type: 'agent' as const,
    label: id,
    preview: id,
    relativeTime: '0:00',
    processedAtMs,
    durationMs: 0,
  };
}

function modelRequestTickWidth(events: Array<Record<string, unknown>>, nowMs: number) {
  const lane = { id: 'main', label: 'Agent', isMain: true };
  const entries = buildSessionEventEntries(
    events,
    'transcript',
    Date.parse(String(events[0]?.processed_at)),
    undefined,
    {
      platformTranscriptFiltering: true,
    },
  );
  const timeline = buildSessionTimeline([lane], new Map([[lane.id, entries]]));
  return buildTimelineTicks(timeline, nowMs)[0]?.widthPct ?? 0;
}

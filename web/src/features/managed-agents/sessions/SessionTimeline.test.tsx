import { afterEach, describe, expect, mock, test } from 'bun:test';
import { createRef } from 'react';
import { I18nProvider } from '../../../shared/i18n';
import { resetTestDom } from '../../../test/setup';
import { type SessionTimelineLane } from '../types';
import {
  buildTimelineTicks,
  EventsMinimap,
  EventsMinimapSkeleton,
  SessionStatusPill,
  SessionTimelineTooltip,
} from './sessionTimeline';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');

afterEach(cleanup);

describe('EventsMinimap', () => {
  test('uses theme semantic tokens for session status pills', () => {
    render(
      <>
        <SessionStatusPill status="Running" />
        <SessionStatusPill status="Queued" />
        <SessionStatusPill status="Failed" />
        <SessionStatusPill status="Idle" />
      </>,
    );

    expect(screen.getByText('Running').className).toContain('bg-success-bg');
    expect(screen.getByText('Queued').className).toContain('bg-warning-bg');
    expect(screen.getByText('Failed').className).toContain('bg-destructive/10');
    expect(screen.getByText('Idle').className).toContain('bg-secondary');
  });

  test('supports Claude zoom, keyboard seek, and accessible vertical resize', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/session-test');
    const lanes = minimapLanes();
    const onSeek = mock(() => {});

    render(
      <I18nProvider initialLocale="en">
        <EventsMinimap
          lanes={lanes}
          activeLane="thread-4"
          selectedEntryId="event-4-a"
          scrollerRef={createRef<HTMLDivElement>()}
          suppressScrollSeekUntilRef={{ current: 0 }}
          onLaneChange={() => {}}
          onSeek={onSeek}
        />
      </I18nProvider>,
    );

    const track = screen.getByRole('slider', { name: 'Seek session event timeline' });
    const minimap = screen.getByTestId('events-minimap');
    const viewport = screen.getByTestId('session-minimap-viewport');
    expect(minimap.className).toContain('px-8');
    expect(minimap.className).toContain('overflow:clip_visible');
    expect(viewport.className).toContain('-mx-[3px]');
    expect(viewport.className).toContain('px-[3px]');
    expect(viewport.className).toContain('scroll-fade-y');
    expect(viewport.className).toContain('scroll-fade-size-4');
    expect(viewport.className).toContain('flex-none');
    expect(viewport.className).toContain('overflow-x-hidden');
    expect(viewport.className).toContain('overflow-y-auto');
    expect(viewport.className).not.toContain('overflow-auto');
    expect(viewport.style.minHeight).toBe('100px');
    expect(viewport.style.maxHeight).toBe('min(60vh, 282px)');
    expect(viewport.style.getPropertyValue('--cds-scroll-fade-top')).toBe('0px');
    expect(
      screen.getByTestId('session-minimap-message-links').querySelectorAll('path[data-timeline-message-link]'),
    ).toHaveLength(1);
    fireEvent.keyDown(track, { key: 'ArrowRight' });
    expect(onSeek).toHaveBeenLastCalledWith('event-4-b');

    const zoomIn = screen.getByRole('button', { name: 'Zoom in' });
    fireEvent.click(zoomIn);
    await waitFor(() => expect(screen.getByText('1.25×')).toBeTruthy());
    expect(viewport.className).toContain('overflow-auto');
    expect(viewport.className).not.toContain('overflow-x-hidden');
    expect(screen.getByTestId('session-minimap-track').style.width).toBe('125%');

    const separator = screen.getByRole('separator', { name: 'Resize minimap' });
    expect(separator.getAttribute('aria-valuemin')).toBe('100');
    expect(separator.getAttribute('aria-valuemax')).toBe('282');
    expect(separator.getAttribute('aria-valuenow')).toBe('280');

    fireEvent.keyDown(separator, { key: 'ArrowUp' });
    expect(separator.getAttribute('aria-valuenow')).toBe('264');
    fireEvent.pointerDown(separator, { button: 0, clientY: 100, pointerId: 1 });
    fireEvent.pointerMove(separator, { clientY: 140, pointerId: 1 });
    fireEvent.pointerUp(separator, { clientY: 140, pointerId: 1 });
    expect(separator.getAttribute('aria-valuenow')).toBe('282');
  });

  test('pans an overflowing minimap with a pointer drag', async () => {
    const onSeek = mock(() => {});

    render(
      <I18nProvider initialLocale="en">
        <EventsMinimap
          lanes={minimapLanes().slice(0, 1)}
          activeLane=""
          selectedEntryId={null}
          scrollerRef={createRef<HTMLDivElement>()}
          suppressScrollSeekUntilRef={{ current: 0 }}
          onLaneChange={() => {}}
          onSeek={onSeek}
        />
      </I18nProvider>,
    );

    const viewport = screen.getByTestId('session-minimap-viewport');
    const track = screen.getByRole('slider', { name: 'Seek session event timeline' });
    expect(track.className).toContain('cursor-default');
    Object.defineProperties(viewport, {
      clientWidth: { configurable: true, value: 400 },
      scrollWidth: { configurable: true, value: 600 },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Zoom in' }));
    await waitFor(() => expect(screen.getByText('1.25×')).toBeTruthy());
    expect(track.className).toContain('cursor-grab');
    viewport.scrollLeft = 40;
    fireEvent.pointerDown(track, { button: 0, buttons: 1, clientX: 300, clientY: 14, pointerId: 2 });
    fireEvent.pointerMove(track, { buttons: 1, clientX: 220, clientY: 14, pointerId: 2 });
    fireEvent.pointerUp(track, { button: 0, clientX: 220, clientY: 14, pointerId: 2 });
    fireEvent.click(track, { clientX: 220, clientY: 14 });

    expect(viewport.scrollLeft).toBe(120);
    expect(onSeek).not.toHaveBeenCalled();
  });

  test('uses a skeleton instead of an empty minimap while events load', () => {
    render(<EventsMinimapSkeleton />);

    const skeleton = screen.getByTestId('events-minimap-skeleton');
    expect(skeleton.className).toContain('px-8');
    expect(skeleton.querySelector('[data-slot="skeleton"]')?.className).toContain('h-7');
    expect(screen.queryByTestId('events-minimap')).toBeNull();
  });

  test('portals hover details above the minimap scroll viewport', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/session-test');
    const row = document.createElement('div');
    row.getBoundingClientRect = () => new window.DOMRect(32, 84, 800, 28);
    const tick = buildTimelineTicks([
      {
        id: '',
        label: 'Main',
        isMain: true,
        items: [timelineItem('event-hover', 1_000)],
      },
    ])[0];

    render(<SessionTimelineTooltip tick={tick} row={row} pointerClientX={400} />);

    const tooltip = screen.getByRole('tooltip');
    expect(tooltip.className).toContain('fixed');
    expect(tooltip.parentElement).toBe(document.body);
    expect(tooltip.style.translate).toContain('392px');
    expect(tooltip.textContent).toContain('event-hover');
  });

  test('keeps short ticks visible and pulses only an open request', () => {
    const lanes: SessionTimelineLane[] = [
      {
        id: '',
        label: 'Main',
        isMain: true,
        items: [{ ...timelineItem('event-open', 1_000), open: true }],
      },
    ];

    render(
      <I18nProvider initialLocale="en">
        <EventsMinimap
          lanes={lanes}
          activeLane=""
          selectedEntryId={null}
          scrollerRef={createRef<HTMLDivElement>()}
          suppressScrollSeekUntilRef={{ current: 0 }}
          onLaneChange={() => {}}
          onSeek={() => {}}
        />
      </I18nProvider>,
    );

    const tick = document.querySelector<HTMLElement>('[data-timeline-tick-id="event-open"]');
    expect(tick?.style.minWidth).toBe('3px');
    expect(tick?.className).toContain('motion-safe:animate-pulse');
  });
});

function minimapLanes(): SessionTimelineLane[] {
  return Array.from({ length: 10 }, (_, index) => ({
    id: index === 0 ? '' : `thread-${index}`,
    label: index === 0 ? 'Main' : `Thread ${index}`,
    isMain: index === 0,
    items:
      index === 4
        ? [
            {
              ...timelineItem('event-4-a', 4_000),
              threadMessage: { direction: 'sent' as const, laneId: 'thread-5' },
            },
            timelineItem('event-4-b', 4_500),
          ]
        : [timelineItem(`event-${index}`, index * 1_000)],
  }));
}

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

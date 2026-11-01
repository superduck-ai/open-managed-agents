import { resetTestDom } from '../../../test/setup';
import { afterEach, describe, expect, test } from 'bun:test';
import { I18nProvider } from '../../../shared/i18n';
import { TooltipProvider } from '../../../shared/ui/tooltip';
import { type QuickstartSessionEvent } from '../types';
import { SessionDetailDeltaFramesContext } from './sessionDetailData';
import { BatchDetailPanel, DebugDetailPanel, EventDetailPanel, ToolCallDetailContent } from './SessionTracePanel';
import { MetaStrip } from './sessionTimeline';
import { buildSessionEventEntries, toolBatchEntry } from './sessionTraceModel';

const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');

afterEach(cleanup);

describe('DebugDetailPanel', () => {
  test.each(['user.message', 'agent.message'])('preserves complete %s content in the raw inspector', (type) => {
    resetTestDom();
    const event = {
      id: 'sevt_media',
      type,
      processed_at: '2026-10-29T03:59:14.989319Z',
      content: [
        { type: 'text', text: 'Describe this image' },
        { type: 'image', source: { type: 'file', file_id: 'file_image' } },
        { type: 'document', source: { type: 'file', file_id: 'file_document' } },
        ...(type === 'agent.message'
          ? [
              { type: 'thinking', thinking: 'Inspect the image' },
              { type: 'tool_use', id: 'tool_read', name: 'Read', input: { file_path: '/tmp/image.png' } },
            ]
          : []),
      ],
    };
    const entries = buildSessionEventEntries([event], 'debug');
    expect(entries).toHaveLength(1);
    const entry = entries[0];
    if (!entry || !('traceEntry' in entry)) throw new Error('Expected a debug event entry');
    expect(entry.event).toEqual(event);
    const { container } = render(
      <I18nProvider initialLocale="en">
        <TooltipProvider>
          <DebugDetailPanel entry={entry} />
        </TooltipProvider>
      </I18nProvider>,
    );
    expect(JSON.parse(container.querySelector('code')?.textContent ?? '')).toEqual(event);
  });

  test.each([
    ['en', 'Copy event-localized', 'Close detail panel', 'Event view', 'Deltas'],
    ['zh-CN', '复制 event-localized', '关闭详情面板', '事件视图', '增量帧'],
  ] as const)(
    'localizes event controls and preserves user punctuation in %s',
    (locale, copyLabel, closeLabel, viewLabel, deltaLabel) => {
      resetTestDom('https://oma.duck.ai/');
      const [entry] = buildSessionEventEntries(
        [{ id: 'event-localized', type: 'agent.message', content: [{ type: 'text', text: 'Keep user text A · B.' }] }],
        'debug',
      );
      let closed = false;
      render(
        <I18nProvider initialLocale={locale}>
          <TooltipProvider>
            <EventDetailPanel
              entry={entry!}
              view="debug"
              onClose={() => {
                closed = true;
              }}
            />
          </TooltipProvider>
        </I18nProvider>,
      );
      expect(screen.getByRole('button', { name: copyLabel })).toBeTruthy();
      expect(screen.getByRole('tablist', { name: viewLabel })).toBeTruthy();
      expect(screen.getByRole('tab', { name: deltaLabel }).getAttribute('aria-disabled')).toBe('true');
      expect(screen.getByTestId('session-trace-code-block').textContent).toContain('Keep user text A · B.');
      fireEvent.click(screen.getByRole('button', { name: closeLabel }));
      expect(closed).toBe(true);
    },
  );

  test.each(['running', 'failed'] as const)('shows only one %s state in the metadata strip', (lifecycle) => {
    resetTestDom();
    const { container } = render(
      <I18nProvider initialLocale="en">
        <MetaStrip lifecycle={lifecycle} isError={lifecycle === 'failed'} relativeTime="0:00" />
      </I18nProvider>,
    );
    expect(container.querySelectorAll('[data-execution-state]')).toHaveLength(1);
    expect(container.querySelector('[data-execution-state]')?.getAttribute('data-execution-state')).toBe(lifecycle);
    expect(screen.queryByText('Running')).toBeNull();
    expect(screen.queryByText('Error')).toBeNull();
  });

  test.each(['single', 'batch'])('shows interruption once in a %s tool detail', (mode) => {
    resetTestDom();
    const entries = buildSessionEventEntries(
      [
        { id: 'tool', type: 'agent.tool_use', name: 'Bash', input: {} },
        {
          id: 'result',
          type: 'agent.tool_result',
          tool_use_id: 'tool',
          is_error: true,
          content: 'Tool execution was interrupted by the user.',
        },
      ],
      'transcript',
    );
    const call = entries.find((entry) => entry.kind === 'tool_call');
    if (!call || call.kind !== 'tool_call') throw new Error('Expected a tool call');
    const { container } = render(
      <I18nProvider initialLocale="en">
        {mode === 'single' ? (
          <ToolCallDetailContent entry={call} />
        ) : (
          <BatchDetailPanel entry={toolBatchEntry([call])} />
        )}
      </I18nProvider>,
    );
    expect(container.querySelectorAll('[data-execution-state="interrupted"]')).toHaveLength(1);
    expect(container.querySelector('.text-destructive')).toBeNull();
  });

  test('shows a reported zero duration in a batch call section', () => {
    resetTestDom();
    const entries = buildSessionEventEntries(
      [
        { id: 'write', type: 'agent.tool_use', name: 'Write', input: {}, duration_ms: 300 },
        { id: 'written', type: 'agent.tool_result', tool_use_id: 'write', content: 'Done', duration_ms: 0 },
      ],
      'transcript',
    );
    const call = entries.find((entry) => entry.kind === 'tool_call');
    if (!call || call.kind !== 'tool_call') throw new Error('Expected a tool call');

    render(
      <I18nProvider initialLocale="en">
        <BatchDetailPanel entry={toolBatchEntry([call])} />
      </I18nProvider>,
    );

    expect(screen.getByText('0ms')).toBeTruthy();
  });

  test('switches from the raw event to live delta frames', () => {
    resetTestDom();
    const event: QuickstartSessionEvent = {
      id: 'sevt_message',
      type: 'agent.message',
      created_at: '2026-08-27T08:00:02.000Z',
      processed_at: null,
      content: [{ type: 'text', text: 'Hello' }],
    };
    const [entry] = buildSessionEventEntries([event], 'debug');
    if (!entry || !('traceEntry' in entry)) throw new Error('Expected a debug event entry');

    render(
      <I18nProvider initialLocale="en">
        <TooltipProvider>
          <SessionDetailDeltaFramesContext.Provider
            value={{
              sevt_message: {
                message: event,
                frames: [
                  { type: 'event_start', event },
                  {
                    type: 'event_delta',
                    delta: { index: 0, content: { type: 'text', text: 'Hel\n' } },
                  },
                ],
              },
            }}
          >
            <DebugDetailPanel entry={entry} />
          </SessionDetailDeltaFramesContext.Provider>
        </TooltipProvider>
      </I18nProvider>,
    );

    expect(screen.getByRole('tab', { name: 'Raw' }).getAttribute('data-active')).not.toBeNull();
    fireEvent.click(screen.getByRole('tab', { name: 'Deltas (1)' }));
    const delta = screen.getByRole('button', { name: 'event_delta Hel↵' });
    expect(delta.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(delta);
    expect(delta.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('tabpanel', { name: 'Deltas (1)' }).textContent).toContain('Hel\\n');
    expect(screen.getAllByRole('button', { name: 'Copy code' }).length).toBeGreaterThanOrEqual(1);
  });
});

test('shows captured thinking start and completion as expandable JSON', () => {
  resetTestDom();
  const event: QuickstartSessionEvent = {
    id: 'sevt_thinking',
    type: 'agent.thinking',
    processed_at: '2026-10-30T08:00:00Z',
  };
  const [entry] = buildSessionEventEntries([event], 'debug');
  if (!entry || !('traceEntry' in entry)) throw new Error('Expected a debug event entry');
  render(
    <I18nProvider initialLocale="en">
      <TooltipProvider>
        <SessionDetailDeltaFramesContext.Provider
          value={{
            sevt_thinking: {
              message: event,
              frames: [{ type: 'event_start', event: { id: event.id, type: event.type } }, event],
            },
          }}
        >
          <DebugDetailPanel entry={entry} />
        </SessionDetailDeltaFramesContext.Provider>
      </TooltipProvider>
    </I18nProvider>,
  );
  fireEvent.click(screen.getByRole('tab', { name: 'Deltas' }));
  const start = screen.getByRole('button', { name: 'event_start sevt_thinking' });
  expect(start.getAttribute('aria-expanded')).toBe('true');
  expect(screen.getByRole('button', { name: 'agent.thinking sevt_thinking' })).toBeTruthy();
  expect(document.querySelectorAll('code.language-json').length).toBeGreaterThanOrEqual(2);
  fireEvent.click(start);
  expect(start.getAttribute('aria-expanded')).toBe('false');
  expect(screen.getByRole('button', { name: 'agent.thinking sevt_thinking' }).getAttribute('aria-expanded')).toBe(
    'true',
  );
});

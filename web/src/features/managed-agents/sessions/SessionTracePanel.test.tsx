import { afterEach, describe, expect, test } from 'bun:test';
import { I18nProvider } from '../../../shared/i18n';
import { TooltipProvider } from '../../../shared/ui/tooltip';
import { resetTestDom } from '../../../test/setup';
import { type QuickstartSessionEvent } from '../types';
import { SessionDetailDeltaFramesContext } from './sessionDetailData';
import { BatchDetailPanel, DebugDetailPanel } from './SessionTracePanel';
import { buildSessionEventEntries, toolBatchEntry } from './sessionTraceModel';

const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');

afterEach(cleanup);

describe('DebugDetailPanel', () => {
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
    fireEvent.click(screen.getByRole('tab', { name: 'Deltas' }));
    expect(screen.getByRole('columnheader', { name: 'Frame' })).toBeTruthy();
    expect(screen.getByRole('row', { name: 'event_delta #1' }).textContent).toContain('Hel↵');
  });
});

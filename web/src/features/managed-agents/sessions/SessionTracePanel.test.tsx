import { afterEach, describe, expect, test } from 'bun:test';
import { I18nProvider } from '../../../shared/i18n';
import { TooltipProvider } from '../../../shared/ui/tooltip';
import { resetTestDom } from '../../../test/setup';
import { type QuickstartSessionEvent } from '../types';
import { SessionDetailDeltaFramesContext } from './sessionDetailData';
import { DebugDetailPanel } from './SessionTracePanel';
import { buildSessionEventEntries } from './sessionTraceModel';

const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');

afterEach(cleanup);

describe('DebugDetailPanel', () => {
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

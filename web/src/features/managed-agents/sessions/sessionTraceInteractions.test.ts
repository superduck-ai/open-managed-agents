import { describe, expect, test } from 'bun:test';
import { sessionEventEntrySelectionId } from './sessionDetailModel';
import { buildSessionEventEntries } from './sessionTraceModel';
import { sessionTraceKeyboardTarget } from './sessionTraceInteractions';
import {
  clampSessionTraceInspectorWidth,
  sessionTraceDualPaneMinWidth,
  sessionTraceInspectorDefaultWidth,
  sessionTraceInspectorMaximumWidth,
  sessionTraceInspectorMinWidth,
  sessionTraceInspectorMaxViewportRatio,
  sessionTracePrimaryMinWidth,
} from './SessionTraceWorkspaceLayout';

describe('session trace interactions', () => {
  test('keeps the inspector within its Claude-like minimum, maximum, and primary pane limits', () => {
    expect(sessionTraceDualPaneMinWidth).toBe(1056);
    expect(sessionTraceInspectorDefaultWidth).toBe(480);
    expect(sessionTraceInspectorMinWidth).toBe(360);
    expect(sessionTracePrimaryMinWidth).toBe(360);
    expect(sessionTraceInspectorMaxViewportRatio).toBe(0.7);
    expect(clampSessionTraceInspectorWidth(200, 1400, 1400)).toBe(sessionTraceInspectorMinWidth);
    expect(clampSessionTraceInspectorWidth(sessionTraceInspectorDefaultWidth, 1400, 1400)).toBe(480);
    expect(clampSessionTraceInspectorWidth(1200, 1400, 1400)).toBe(980);
    expect(sessionTraceInspectorMaximumWidth(1056, 1400)).toBe(695);
    expect(clampSessionTraceInspectorWidth(900, 1056, 1400)).toBe(695);
  });

  test('navigates selectable events with arrows, vim keys, home, and end', () => {
    const entries = buildSessionEventEntries(
      [
        { id: 'evt_one', type: 'user.message', created_at: '2026-08-26T10:00:00Z', content: 'One' },
        { id: 'evt_two', type: 'agent.message', created_at: '2026-08-26T10:00:01Z', content: 'Two' },
        { id: 'evt_three', type: 'agent.message', created_at: '2026-08-26T10:00:02Z', content: 'Three' },
      ],
      'debug',
      Date.parse('2026-08-26T10:00:00Z'),
    );
    const firstID = sessionEventEntrySelectionId(entries[0]);
    const secondID = sessionEventEntrySelectionId(entries[1]);
    const thirdID = sessionEventEntrySelectionId(entries[2]);

    expect(sessionTraceKeyboardTarget(entries, null, 'j')?.selectionId).toBe(firstID);
    expect(sessionTraceKeyboardTarget(entries, firstID, 'ArrowDown')?.selectionId).toBe(secondID);
    expect(sessionTraceKeyboardTarget(entries, secondID, 'k')?.selectionId).toBe(firstID);
    expect(sessionTraceKeyboardTarget(entries, firstID, 'End')?.selectionId).toBe(thirdID);
    expect(sessionTraceKeyboardTarget(entries, thirdID, 'Home')?.selectionId).toBe(firstID);
  });
});

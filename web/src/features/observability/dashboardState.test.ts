import { expect, test } from 'bun:test';
import { dashboardSearch, readDashboardState } from './dashboardState';

test('ignores unknown tabs, refresh values, time zones and invalid versions in the URL', () => {
  const state = readDashboardState('?obs_tab=billing&refresh=2s&timezone=mars&agent_version=0&agent_version=x');
  expect(state.tab).toBe('overview');
  expect(state.refresh).toBe('');
  expect(state.timeZone).toBe('browser');
  expect(state.versions).toEqual([]);
  expect(state.range).toEqual({ from: 'now-7d', to: 'now' });
});

test('round-trips dashboard state and keeps unrelated query parameters', () => {
  const state = readDashboardState(
    '?tab=observability&from=now-1h&to=now&refresh=30s&timezone=utc&obs_tab=models&agent_id=agent_1&agent_version=2&agent_version=3&model=m&view_panel=token_trend',
  );
  expect(state).toMatchObject({
    range: { from: 'now-1h', to: 'now' },
    refresh: '30s',
    timeZone: 'utc',
    tab: 'models',
    agentId: 'agent_1',
    versions: [2, 3],
    model: 'm',
    viewPanel: 'token_trend',
  });
  const search = dashboardSearch('?tab=observability&version_id=4', state);
  expect(readDashboardState(search)).toEqual(state);
  expect(new URLSearchParams(search).get('version_id')).toBe('4');
  expect(
    dashboardSearch('?from=now-1h', {
      ...state,
      range: { from: 'now-7d', to: 'now' },
      refresh: '',
      timeZone: 'browser',
      tab: 'overview',
      agentId: '',
      versions: [],
      model: '',
      viewPanel: '',
    }),
  ).toBe('');
});

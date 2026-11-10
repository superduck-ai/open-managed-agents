import { describe, expect, test } from 'bun:test';
import { overviewChartPoints, type WorkspaceOverview } from './overview-api';

const report: WorkspaceOverview = {
  start_time: '2026-10-30T16:00:00Z',
  end_time: '2026-11-06T07:00:00Z',
  timezone: 'Asia/Shanghai',
  agent_count: 2,
  session_count: 5,
  failed_session_count: 1,
  active_agent_count: 2,
  daily: [
    { date: '2026-10-31', agent_id: 'a', session_count: 2 },
    { date: '2026-10-31', agent_id: 'b', session_count: 1 },
    { date: '2026-11-06', agent_id: 'b', session_count: 2 },
  ],
  agents: [],
};
describe('overview chart', () => {
  test('fills empty dates in the requested timezone and sums agents', () => {
    const points = overviewChartPoints(report, 'all');
    expect(points).toHaveLength(7);
    expect(points[0]).toEqual({ date: '10/31', sessions: 3 });
    expect(points[1]).toEqual({ date: '11/01', sessions: 0 });
    expect(points[6]).toEqual({ date: '11/06', sessions: 2 });
    expect(points.reduce((sum, point) => sum + point.sessions, 0)).toBe(report.session_count);
  });
  test('filters only the trend and preserves the report', () => {
    const points = overviewChartPoints(report, 'a');
    expect(points[0]?.sessions).toBe(2);
    expect(points[6]?.sessions).toBe(0);
    expect(report.session_count).toBe(5);
  });
  test('keeps one bucket per calendar date across daylight saving', () => {
    const points = overviewChartPoints(
      {
        ...report,
        start_time: '2026-10-28T04:00:00Z',
        end_time: '2026-11-03T12:00:00Z',
        timezone: 'America/New_York',
        daily: [],
      },
      'all',
    );
    expect(points).toHaveLength(7);
    expect(points.map((point) => point.date)).toEqual(['10/28', '10/29', '10/30', '10/31', '11/01', '11/02', '11/03']);
  });
});

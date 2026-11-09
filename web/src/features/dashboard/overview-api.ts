import { z } from 'zod';
import { consoleApi } from '@/shared/api/client';

const overviewAgentSchema = z.object({
  agent_id: z.string(),
  name: z.string(),
  session_count: z.number().int().nonnegative(),
  failed_session_count: z.number().int().nonnegative(),
  last_run_at: z.string(),
});
const overviewSchema = z.object({
  start_time: z.string(),
  end_time: z.string(),
  timezone: z.string(),
  agent_count: z.number().int().nonnegative(),
  session_count: z.number().int().nonnegative(),
  failed_session_count: z.number().int().nonnegative(),
  active_agent_count: z.number().int().nonnegative(),
  daily: z.array(z.object({ date: z.string(), agent_id: z.string(), session_count: z.number().int().nonnegative() })),
  agents: z.array(overviewAgentSchema),
});
export type OverviewAgent = z.infer<typeof overviewAgentSchema>;
export type WorkspaceOverview = z.infer<typeof overviewSchema>;

export async function loadWorkspaceOverview(
  organizationUuid: string,
  workspaceId: string,
  days: string,
  timezone: string,
  signal?: AbortSignal,
) {
  const params = new URLSearchParams({ days, timezone });
  const response = await consoleApi<WorkspaceOverview>(
    `/api/organizations/${encodeURIComponent(organizationUuid)}/workspaces/${encodeURIComponent(workspaceId)}/overview?${params}`,
    {
      signal,
      context: { organizationUuid, workspaceId },
    },
  );
  return overviewSchema.parse(response);
}

export function overviewChartPoints(report: WorkspaceOverview, selectedAgent: string) {
  const counts = new Map<string, number>();
  for (const day of report.daily) {
    if (selectedAgent === 'all' || day.agent_id === selectedAgent)
      counts.set(day.date, (counts.get(day.date) ?? 0) + day.session_count);
  }
  const dateFormat = new Intl.DateTimeFormat('en-CA', {
    timeZone: report.timezone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  });
  const dateKey = (value: string) => {
    const parts = dateFormat.formatToParts(new Date(value));
    return ['year', 'month', 'day'].map((type) => parts.find((part) => part.type === type)?.value).join('-');
  };
  const start = dateKey(report.start_time);
  const end = dateKey(report.end_time);
  const points: { date: string; sessions: number }[] = [];
  const cursor = new Date(`${start}T00:00:00Z`);
  while (cursor.toISOString().slice(0, 10) <= end) {
    const day = cursor.toISOString().slice(0, 10);
    points.push({ date: day.slice(5).replace('-', '/'), sessions: counts.get(day) ?? 0 });
    cursor.setUTCDate(cursor.getUTCDate() + 1);
  }
  return points;
}

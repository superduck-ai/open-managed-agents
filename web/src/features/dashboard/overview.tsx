import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useWorkspace, getPrimaryOrganizationUuid } from '@/shared/workspaces/context';
import { useAuth } from '@/shared/auth/context';
import { isModelConfigurationUnavailable } from '@/shared/api/client';
import { listCreateAgentModels } from '../managed-agents/agents/create-dialog-api';
import { LLMProviderRequired } from '../llm-providers/LLMProviderRequired';
import { loadWorkspaceOverview, overviewChartPoints, type WorkspaceOverview } from './overview-api';
import { Activity, ArrowUpRight, ChevronDown, CircleAlert, Info, Plus } from 'lucide-react';
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts';
import { productIcons } from '@/shared/icons/product-icons';
import { Button, ButtonLink } from '@/shared/ui/button';
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/shared/ui/chart';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/shared/ui/dropdown-menu';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/shared/ui/tooltip';
import { useI18n } from '@/shared/i18n';
import { documentationHref } from '@/shared/documentation';
import { localizedWorkspaceName } from '@/shared/workspaces/display-name';
import { quickstartModelSetupHref } from '@/shared/workspaces/model-setup-navigation';
import { OverviewOperations } from './overview-operations';
import { useDashboardWorkspaceScope } from './model';

function overviewMetrics(data: WorkspaceOverview | undefined, period: string, zh: boolean) {
  const sessions = data?.session_count ?? 0;
  const errors = data?.failed_session_count ?? 0;
  return [
    {
      label: zh ? '会话数' : 'Sessions',
      value: data ? sessions.toLocaleString() : '—',
      note: zh ? `过去 ${period} 天` : `Past ${period} days`,
      info: zh ? '所选时间范围内创建的会话总数。' : 'Sessions created within the selected period.',
      path: 'sessions',
    },
    {
      label: zh ? '错误率' : 'Error rate',
      value: sessions > 0 ? `${((errors / sessions) * 100).toFixed(1)}%` : '—',
      note: zh ? '过去 ' + period + ' 天' : `Past ${period} days`,
      info: zh
        ? '同期创建的会话中，发生过终止性错误或重试耗尽的会话占比。同一会话只计一次。'
        : 'Share of sessions created in this period with a terminal error or exhausted retries. Each session is counted once.',
      path: 'sessions',
    },
    {
      label: zh ? '活跃智能体' : 'Active agents',
      value: data ? String(data.active_agent_count) : '—',
      note: zh ? `过去 ${period} 天内有会话` : `With sessions in the past ${period} days`,
      info: zh
        ? '所选时间范围内至少创建过一个会话的智能体数。'
        : 'Agents with at least one session created within the selected period.',
      path: 'agents',
    },
  ];
}

function useOverview(workspaceId: string) {
  const { orgUuid } = useWorkspace();
  const { account } = useAuth();
  const organizationUuid = orgUuid || getPrimaryOrganizationUuid(account) || '';
  const [period, setPeriod] = useState('30');
  const [agent, setAgent] = useState('all');
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const overview = useQuery({
    queryKey: ['workspace-overview', account?.uuid, organizationUuid, workspaceId, period, timezone],
    queryFn: ({ signal }) => loadWorkspaceOverview(organizationUuid, workspaceId, period, timezone, signal),
    enabled: Boolean(organizationUuid && workspaceId),
    retry: false,
  });
  const data = overview.isError ? undefined : overview.data;
  const ready = Boolean(data && data.session_count > 0);
  const overviewAgents = data?.agents ?? [];
  const selectedAgent = overviewAgents.some((item) => item.agent_id === agent) ? agent : 'all';
  const points = data ? overviewChartPoints(data, selectedAgent) : [];
  const models = useQuery({
    queryKey: ['overview-models', account?.uuid, organizationUuid, workspaceId],
    queryFn: () => listCreateAgentModels(workspaceId),
    enabled: Boolean(data && data.agent_count === 0 && !ready),
    retry: false,
  });
  const needsModels = isModelConfigurationUnavailable(models.error) || (models.isSuccess && models.data.length === 0);
  return { overview, data, ready, overviewAgents, selectedAgent, points, needsModels, period, setPeriod, setAgent };
}

function OverviewStart({
  data,
  needsModels,
  workspaceId,
  workspacePath,
}: {
  data: WorkspaceOverview | undefined;
  needsModels: boolean;
  workspaceId: string;
  workspacePath: string;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const text = (chinese: string, english: string) => (zh ? chinese : english);
  if (!data || data.session_count > 0) return null;
  return (
    <section className="flex min-h-52 flex-col items-center justify-center rounded-lg border-[0.5px] border-border px-6 py-7 text-center">
      {data.agent_count === 0 && needsModels ? (
        <LLMProviderRequired
          compact
          onConfigure={() => window.location.assign(quickstartModelSetupHref(workspaceId, '?mode=chat'))}
        />
      ) : (
        <>
          <productIcons.agents className="mb-4 size-8 text-muted-foreground" aria-hidden />
          <h2 className="text-sm font-medium">
            {data.agent_count > 0
              ? zh
                ? '当前时段暂无运行记录'
                : 'No sessions in this period'
              : zh
                ? '创建你的第一个智能体'
                : 'Create your first agent'}
          </h2>
          <ButtonLink
            className="mt-5"
            href={`${workspacePath}/${data.agent_count > 0 ? 'agents' : 'agent-quickstart'}`}
          >
            {data.agent_count > 0 ? text('查看智能体', 'View agents') : text('开始使用', 'Get started')}
          </ButtonLink>
        </>
      )}
    </section>
  );
}

export function OverviewPage() {
  const { locale, msg } = useI18n();
  const { workspaceId, workspaceName } = useDashboardWorkspaceScope();
  const workspacePath = `/workspaces/${encodeURIComponent(workspaceId || 'default')}`;
  const { overview, data, ready, overviewAgents, selectedAgent, points, needsModels, period, setPeriod, setAgent } =
    useOverview(workspaceId);
  const zh = locale === 'zh-CN';
  const text = (chinese: string, english: string) => (zh ? chinese : english);

  return (
    <TooltipProvider>
      <section className="space-y-6 pb-20" aria-label={text('概览', 'Overview')} aria-busy={overview.isPending}>
        <header className="flex items-start justify-between gap-4">
          <div>
            <h1 className="text-2xl font-medium leading-8">{text('概览', 'Overview')}</h1>
            <p className="mt-2 text-sm text-muted-foreground">{localizedWorkspaceName(workspaceName, msg)}</p>
          </div>
          <div className="flex flex-wrap justify-end gap-2">
            <Select
              value={period}
              onValueChange={(value) => value && setPeriod(value)}
              items={[
                { value: '7', label: text('过去 7 天', 'Past 7 days') },
                { value: '30', label: text('过去 30 天', 'Past 30 days') },
              ]}
            >
              <SelectTrigger aria-label={text('统计时间范围', 'Time range')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="7">{text('过去 7 天', 'Past 7 days')}</SelectItem>
                <SelectItem value="30">{text('过去 30 天', 'Past 30 days')}</SelectItem>
              </SelectContent>
            </Select>
            <DropdownMenu>
              <DropdownMenuTrigger render={<Button />}>
                <Plus className="size-4" aria-hidden />
                {text('创建', 'Create')}
                <ChevronDown className="size-3.5" aria-hidden />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-44">
                {[
                  { label: text('智能体', 'Agent'), path: 'agent-quickstart', icon: productIcons.agents },
                  { label: text('会话', 'Session'), path: 'sessions', icon: productIcons.sessions },
                  { label: text('部署', 'Deployment'), path: 'deployments', icon: productIcons.deployments },
                  { label: text('环境', 'Environment'), path: 'environments/new', icon: productIcons.environments },
                ].map(({ label, path, icon: Icon }) => (
                  <DropdownMenuItem key={path} render={<a href={`${workspacePath}/${path}`} />}>
                    <Icon className="size-4" aria-hidden />
                    {label}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </header>

        {overview.isError ? (
          <div
            role="alert"
            className="flex flex-wrap items-center gap-3 rounded-lg border-[0.5px] border-border px-4 py-3 text-sm"
          >
            <CircleAlert className="size-4 shrink-0 text-destructive" aria-hidden />
            <span className="flex-1">{text('暂时无法读取运行数据', 'Unable to load activity data')}</span>
            <Button size="sm" variant="outline" onClick={() => void overview.refetch()}>
              {text('重试', 'Retry')}
            </Button>
          </div>
        ) : null}

        <div className="grid grid-cols-3 divide-x-[0.5px] divide-border rounded-lg border-[0.5px] border-border">
          {overviewMetrics(data, period, zh).map(({ label, value, note, info, path }) => (
            <div key={label} className="min-w-0 px-3 py-4 sm:px-5">
              <div className="flex min-h-6 flex-wrap items-center gap-1.5 text-sm">
                <a
                  href={`${workspacePath}/${path}`}
                  className="font-medium hover:underline focus-visible:outline-[0.5px] focus-visible:outline-foreground"
                >
                  {label}
                </a>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <button
                        type="button"
                        aria-label={text(`关于${label}`, `About ${label}`)}
                        className="rounded text-muted-foreground outline-none focus-visible:ring-[0.5px] focus-visible:ring-foreground"
                      />
                    }
                  >
                    <Info className="size-3.5" aria-hidden />
                  </TooltipTrigger>
                  <TooltipContent>{info}</TooltipContent>
                </Tooltip>
              </div>
              <p className="mt-4 text-2xl font-medium tabular-nums leading-8">{value}</p>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">{note}</p>
            </div>
          ))}
        </div>

        <OverviewStart data={data} needsModels={needsModels} workspaceId={workspaceId} workspacePath={workspacePath} />

        <section
          className="rounded-lg border-[0.5px] border-border px-5 pt-4 pb-3"
          aria-label={text('会话趋势', 'Session activity')}
        >
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h2 className="text-sm font-medium">{text('会话趋势', 'Session activity')}</h2>
            {ready ? (
              <Select
                value={selectedAgent}
                onValueChange={(value) => value && setAgent(value)}
                items={[
                  { value: 'all', label: text('全部智能体', 'All agents') },
                  ...overviewAgents.map(({ agent_id, name }) => ({ value: agent_id, label: name })),
                ]}
              >
                <SelectTrigger size="sm" aria-label={text('趋势智能体', 'Trend agent')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">{text('全部智能体', 'All agents')}</SelectItem>
                  {overviewAgents.map(({ agent_id, name }) => (
                    <SelectItem key={agent_id} value={agent_id}>
                      {name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : null}
          </div>
          {ready ? (
            <ChartContainer
              config={{ sessions: { label: text('会话数', 'Sessions'), color: 'var(--chart-1)' } }}
              className="mt-6 h-52 w-full aspect-auto"
              aria-label={text(
                `过去 ${period} 天每日创建的会话数`,
                `Daily sessions created in the past ${period} days`,
              )}
            >
              <AreaChart data={points} accessibilityLayer margin={{ left: -15, right: 8, top: 8, bottom: 0 }}>
                <CartesianGrid vertical={false} stroke="var(--border)" />
                <XAxis dataKey="date" tickLine={false} axisLine={false} minTickGap={36} tickMargin={10} />
                <YAxis tickLine={false} axisLine={false} allowDecimals={false} tickMargin={10} />
                <ChartTooltip content={<ChartTooltipContent />} />
                <Area
                  type="monotone"
                  dataKey="sessions"
                  stroke="var(--chart-1)"
                  strokeWidth={1.5}
                  fill="var(--violet-a3)"
                  isAnimationActive={false}
                />
              </AreaChart>
            </ChartContainer>
          ) : (
            <div className="flex min-h-52 flex-col items-center justify-center gap-3 text-muted-foreground">
              <Activity className="size-6" strokeWidth={1.5} aria-hidden />
              <p className="text-sm">
                {overview.isPending
                  ? text('正在加载运行数据', 'Loading activity data')
                  : overview.isError
                    ? text('运行数据暂不可用', 'Activity data is unavailable')
                    : text('暂无会话数据', 'No session data')}
              </p>
            </div>
          )}
        </section>

        {ready ? (
          <OverviewOperations
            agents={overviewAgents}
            period={period}
            workspacePath={workspacePath}
            selectedAgent={selectedAgent}
            onSelectAgent={setAgent}
          />
        ) : null}

        <section
          aria-label={text('使用资源', 'Resources')}
          className={ready ? 'border-t-[0.5px] border-border pt-5' : ''}
        >
          <h2 className="mb-4 text-sm font-medium">
            {zh ? (ready ? '文档' : '使用资源') : ready ? 'Documentation' : 'Resources'}
          </h2>
          <div className="grid gap-5 sm:grid-cols-3">
            {[
              {
                title: text('技能', 'Skills'),
                text: text('复用指令、工具和文件', 'Reuse instructions, tools and files'),
                path: 'skills',
                icon: productIcons.skills,
              },
              {
                title: text('部署', 'Deployments'),
                text: text('按计划自动运行智能体', 'Run agents on a schedule'),
                path: 'scheduled-deployments',
                icon: productIcons.deployments,
              },
              {
                title: text('记忆', 'Memory'),
                text: text('保留跨会话的上下文', 'Keep context across sessions'),
                path: 'memory',
                icon: productIcons.memory,
              },
            ].map(({ title, text, path, icon: Icon }) => (
              <a
                key={path}
                href={documentationHref(path, locale)}
                target="_blank"
                rel="noreferrer"
                className="group flex items-start gap-3 rounded p-1 outline-none focus-visible:ring-[0.5px] focus-visible:ring-foreground"
              >
                <Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />
                <span className="min-w-0 flex-1">
                  <span className="text-sm font-medium group-hover:underline">{title}</span>
                  {!ready ? <span className="mt-1 block text-xs leading-5 text-muted-foreground">{text}</span> : null}
                </span>
                <ArrowUpRight className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" aria-hidden />
              </a>
            ))}
          </div>
        </section>
      </section>
    </TooltipProvider>
  );
}

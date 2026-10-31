import {
  Bot,
  Check,
  ChevronRight,
  Cloud,
  ShieldCheck,
  CalendarClock,
  Sparkles,
  ArrowDownToLine,
  type LucideIcon,
} from 'lucide-react';
import { useI18n } from '../../../../shared/i18n';
import { Button, ButtonLink } from '../../../../shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '../../../../shared/ui/card';
import { stableStringify } from '../platformQuickstartRequest';
import { downloadTextFile } from '../../utils';
import { builderExportConfig } from './model';
import type { Builder } from './useBuilder';
import { useBuilderPanel } from './panelContext';

export function BuilderInspector({ builder, workspaceId }: { builder: Builder; workspaceId: string }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const { state } = builder;
  const steps = [
    { id: 'agent', label: zh ? '创建智能体' : 'Create agent', done: Boolean(state.agent) },
    { id: 'environment', label: zh ? '配置环境' : 'Environment', done: Boolean(state.environment) },
    { id: 'session', label: zh ? '试跑' : 'Test', done: state.testClosed },
    ...(state.wantsSchedule
      ? [{ id: 'deploy', label: zh ? '定时运行' : 'Schedule', done: Boolean(state.deployment) }]
      : []),
    { id: 'integrate', label: zh ? '接入' : 'Integrate', done: false },
  ];
  const resources = [
    ...(state.savedConfig?.skills ?? []).map((skill) => ({
      id: skill.skill_id,
      name: builder.catalog.data?.skills.find((item) => item.id === skill.skill_id)?.displayTitle ?? skill.skill_id,
      section: 'skills',
      icon: Sparkles,
    })),
    ...(state.environment ? [{ ...state.environment, section: 'environments', icon: Cloud }] : []),
    ...state.vaults.map((vault) => ({ ...vault, section: 'vaults', icon: ShieldCheck })),
    ...(state.deployment ? [{ ...state.deployment, section: 'deployments', icon: CalendarClock }] : []),
  ];
  return (
    <aside
      aria-label={zh ? '构建配置' : 'Build configuration'}
      className="min-h-0 space-y-5 overflow-y-auto pb-6 text-sm"
    >
      <Card className="gap-4 ring-0">
        <CardHeader>
          <CardTitle className="text-sm">{zh ? '进度' : 'Progress'}</CardTitle>
        </CardHeader>
        <CardContent>
          <ol className="space-y-4 text-sm">
            {steps.map((step, index) => (
              <li
                key={step.id}
                aria-current={state.step === step.id ? 'step' : undefined}
                className={`flex items-center gap-3 ${state.step === step.id ? 'font-medium text-foreground' : 'text-muted-foreground'}`}
              >
                <span
                  className={`grid size-6 place-items-center rounded-full border-[0.5px] border-border text-xs ${step.done ? 'bg-[var(--success-bg)] text-success' : state.step === step.id ? 'border-primary bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'}`}
                >
                  {step.done ? <Check aria-hidden="true" className="size-3.5" /> : index + 1}
                </span>
                {step.label}
              </li>
            ))}
          </ol>
        </CardContent>
      </Card>
      <SavedAgent builder={builder} />
      <BuilderResources resources={resources} workspaceId={workspaceId} />
      <SkippedAuthorization builder={builder} />
      <Button
        variant="secondary"
        className="w-full font-normal"
        disabled={builder.busy || Boolean(state.operation)}
        onClick={() => downloadTextFile('agent.json', JSON.stringify(builderExportConfig(state), null, 2))}
      >
        <ArrowDownToLine aria-hidden="true" />
        {state.savedConfig ? (zh ? '导出配置' : 'Export configuration') : zh ? '导出草稿' : 'Export draft'}
      </Button>
    </aside>
  );
}

function BuilderResources({
  resources,
  workspaceId,
}: {
  resources: Array<{ id: string; name: string; section: string; icon: LucideIcon }>;
  workspaceId: string;
}) {
  const openPanel = useBuilderPanel();
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  return (
    <Card className="gap-4 ring-0">
      <CardHeader>
        <CardTitle className="text-sm">{zh ? '资源' : 'Resources'}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {!resources.length && (
          <p className="text-sm text-muted-foreground">{zh ? '暂无关联资源' : 'No resources yet'}</p>
        )}
        {resources.map((resource) => {
          const content = (
            <>
              <span
                className="grid size-8 shrink-0 place-items-center rounded-lg bg-accent text-muted-foreground"
                title={resource.section === 'environments' ? (zh ? '环境' : 'Environment') : resource.section}
              >
                <resource.icon aria-hidden="true" className="size-4" />
              </span>
              <span className="min-w-0 flex-1 truncate text-left">{resource.name}</span>
              <ChevronRight aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
            </>
          );
          const className = 'h-auto w-full min-w-0 justify-start gap-3 px-0 py-2 font-normal';
          return resource.section === 'environments' ? (
            <Button
              key={resource.id}
              variant="ghost"
              className={className}
              aria-label={`${zh ? '环境' : 'Environment'}: ${resource.name}`}
              title={resource.name}
              onClick={() => openPanel({ kind: 'environment', id: resource.id, name: resource.name })}
            >
              {content}
            </Button>
          ) : (
            <ButtonLink
              key={resource.id}
              variant="ghost"
              className={className}
              title={resource.name}
              href={`/workspaces/${encodeURIComponent(workspaceId)}/${resource.section}/${encodeURIComponent(resource.id)}`}
              target="_blank"
              rel="noopener noreferrer"
            >
              {content}
            </ButtonLink>
          );
        })}
      </CardContent>
    </Card>
  );
}

function SkippedAuthorization({ builder }: { builder: Builder }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const { state } = builder;
  return (
    <>
      {state.skippedServers.length > 0 && (
        <div className="space-y-2">
          <p className="text-sm text-warning">
            {zh ? '已跳过授权：' : 'Authorization skipped: '}
            {state.skippedServers
              .map((url) => state.config.mcp_servers.find((server) => server.url === url)?.name ?? url)
              .join(', ')}
          </p>
          <Button
            variant="outline"
            disabled={builder.busy || Boolean(state.sessionID && !state.testClosed)}
            onClick={() =>
              void builder.reply(zh ? '重新授权已跳过的服务。' : 'Authorize the services I skipped earlier.')
            }
          >
            {zh ? '重新授权' : 'Authorize services'}
          </Button>
        </div>
      )}
    </>
  );
}

function SavedAgent({ builder }: { builder: Builder }) {
  const openPanel = useBuilderPanel();
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const { state } = builder;
  return (
    <>
      {state.agent && (
        <Card className="gap-3 ring-0">
          <CardHeader>
            <CardTitle className="text-sm">{zh ? '智能体' : 'Agent'}</CardTitle>
          </CardHeader>
          <CardContent>
            <Button
              variant="ghost"
              aria-label={zh ? '查看智能体配置' : 'View agent configuration'}
              className="h-auto w-full min-w-0 justify-start gap-3 px-0 py-2 font-normal"
              onClick={() => openPanel({ kind: 'agent' })}
            >
              <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-accent text-muted-foreground">
                <Bot className="size-4" />
              </span>
              <span className="min-w-0 flex-1 truncate text-left" title={state.agent.name}>
                {state.agent.name}
              </span>
              <span className="shrink-0 font-mono text-xs text-muted-foreground">v{state.agent.version}</span>
              <ChevronRight aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
            </Button>
            {stableStringify(state.savedConfig) !== stableStringify(state.config) && (
              <p className="text-xs text-muted-foreground">
                {zh
                  ? '有待确认修改，当前版本尚未变更。'
                  : 'Changes await confirmation. The saved version is unchanged.'}
              </p>
            )}
          </CardContent>
        </Card>
      )}
    </>
  );
}

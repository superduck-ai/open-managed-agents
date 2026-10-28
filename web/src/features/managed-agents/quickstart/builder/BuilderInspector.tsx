import { Bot, Check, ChevronRight, Layers, ShieldCheck, CalendarClock, Sparkles } from 'lucide-react';
import { useI18n } from '../../../../shared/i18n';
import { ButtonLink } from '../../../../shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '../../../../shared/ui/card';
import { Badge } from '../../../../shared/ui/badge';
import type { Builder } from './useBuilder';

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
    ...(state.environment ? [{ ...state.environment, section: 'environments', icon: Layers }] : []),
    ...state.vaults.map((vault) => ({ ...vault, section: 'vaults', icon: ShieldCheck })),
    ...(state.deployment ? [{ ...state.deployment, section: 'deployments', icon: CalendarClock }] : []),
  ];
  return (
    <aside aria-label={zh ? '构建配置' : 'Build configuration'} className="min-h-0 space-y-5 overflow-y-auto pt-8 pb-6">
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
      {state.agent && (
        <Card className="gap-3 ring-0">
          <CardHeader>
            <CardTitle className="text-sm">{zh ? '智能体' : 'Agent'}</CardTitle>
          </CardHeader>
          <CardContent>
            <ButtonLink
              variant="ghost"
              aria-label={zh ? '查看配置' : 'View configuration'}
              className="h-auto w-full min-w-0 justify-start gap-3 px-0 py-2 font-normal"
              href={`/workspaces/${encodeURIComponent(workspaceId)}/agents/${encodeURIComponent(state.agent.id)}`}
            >
              <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-accent text-muted-foreground">
                <Bot className="size-4" />
              </span>
              <span className="min-w-0 flex-1 truncate text-left" title={state.config.name}>
                {state.agent.name}
              </span>
              <Badge variant="secondary" className="font-normal">
                {`v${state.agent.version}`}
              </Badge>
              <ChevronRight aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
            </ButtonLink>
            {(state.savedConfig?.skills ?? []).map((skill) => (
              <div key={skill.skill_id} className="mt-3 flex min-w-0 items-center gap-2 text-sm text-muted-foreground">
                <Sparkles aria-hidden="true" className="size-4 shrink-0" />
                <span className="truncate">
                  {builder.catalog.data?.skills.find((item) => item.id === skill.skill_id)?.displayTitle ??
                    skill.skill_id}
                </span>
              </div>
            ))}
          </CardContent>
        </Card>
      )}
      {resources.length > 0 && (
        <Card className="gap-4 ring-0">
          <CardHeader>
            <CardTitle className="text-sm">{zh ? '资源' : 'Resources'}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {resources.map((resource) => (
              <a
                key={resource.id}
                className="flex min-w-0 items-center gap-3 rounded-lg py-2 text-sm hover:bg-accent focus-visible:outline focus-visible:outline-ring"
                title={resource.name}
                href={`/workspaces/${encodeURIComponent(workspaceId)}/${resource.section}/${encodeURIComponent(resource.id)}`}
              >
                <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-accent text-muted-foreground">
                  <resource.icon aria-hidden="true" className="size-4" />
                </span>
                <span className="min-w-0 flex-1 truncate">{resource.name}</span>
                <ChevronRight aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
              </a>
            ))}
          </CardContent>
        </Card>
      )}
    </aside>
  );
}

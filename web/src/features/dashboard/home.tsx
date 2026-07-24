import { BookOpen, Bot, Boxes, BrainCircuit, DatabaseZap, Info, KeyRound, Network, RefreshCw } from 'lucide-react';
import type { ReactNode } from 'react';
import { Badge } from '@/shared/ui/badge';
import { Button, ButtonLink } from '@/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/card';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/shared/ui/tooltip';
import { useAuth } from '../../shared/auth/context';
import { useI18n } from '../../shared/i18n';
import { useWorkspace } from '../../shared/workspaces/context';
import { useModelCatalog, useRefreshModelCatalog } from '../model-catalog/hooks';
import { type ModelCatalogModel, modelCatalogDisplayName } from '../model-catalog/model';
import { PanelCard } from './frame';
import { useDashboardWorkspaceScope, type IconComponent } from './model';

const docsHref = 'https://docs.anthropic.com/';
const modelCardTones = [
  'bg-secondary text-secondary-foreground',
  'bg-amber-500/10 text-amber-700 dark:text-amber-400',
  'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
  'bg-sky-500/10 text-sky-700 dark:text-sky-400',
];

const resourceCards = [
  {
    id: 'advisorTool',
    title: 'Advisor tool',
    badge: 'Beta',
    badgeId: 'dashboard.resources.badge.beta',
    icon: Bot,
    body: 'A fast, lower-cost model consults a more capable advisor mid-task. Get close to advisor-level quality while most tokens run at the cheaper model rate.',
  },
  {
    id: 'batchApi',
    title: 'Batch API',
    icon: Boxes,
    body: 'Move async workloads to the Batch API and save 50% on standard API prices.',
  },
  {
    id: 'promptCaching',
    title: 'Prompt caching',
    icon: DatabaseZap,
    body: 'Reuse prompt prefixes across API calls. Most orgs see input costs drop 50-90%.',
  },
];
export function DashboardHome() {
  const { msg } = useI18n();
  const { account, csrfToken } = useAuth();
  const { orgUuid } = useWorkspace();
  const { workspaceId } = useDashboardWorkspaceScope();
  const modelCatalog = useModelCatalog(orgUuid);
  const refreshModelCatalog = useRefreshModelCatalog(orgUuid, csrfToken);
  const canRefreshModelCatalog =
    account?.memberships?.some(
      (membership) =>
        membership.role?.trim().toLowerCase() === 'admin' &&
        (!membership.organization?.uuid || membership.organization.uuid === orgUuid),
    ) ?? false;
  const apiKeysHref = `/settings/workspaces/${encodeURIComponent(workspaceId || 'default')}/keys`;

  return (
    <TooltipProvider>
      <section className="space-y-6">
        <div className="flex items-start justify-between gap-4">
          <h1 className="text-[28px] font-semibold leading-tight tracking-normal text-foreground">
            {msg('dashboard.greeting', 'Good morning, test')}
          </h1>
          <div className="flex items-center gap-2">
            <Tooltip>
              <TooltipTrigger
                render={
                  <ButtonLink
                    href={docsHref}
                    target="_blank"
                    rel="noreferrer"
                    variant="outline"
                    size="icon-lg"
                    aria-label={msg('dashboard.actions.exploreDocs', 'Explore docs')}
                  >
                    <BookOpen className="size-4" aria-hidden />
                  </ButtonLink>
                }
              />
              <TooltipContent>{msg('dashboard.actions.exploreDocs', 'Explore docs')}</TooltipContent>
            </Tooltip>
            <ButtonLink href={apiKeysHref} variant="outline" size="lg">
              <KeyRound className="size-4" aria-hidden />
              {msg('dashboard.actions.getApiKey', 'Get API key')}
            </ButtonLink>
            <ButtonLink href="/quickstart" size="lg">
              <Network className="size-4" aria-hidden />
              {msg('dashboard.actions.buildAgent', 'Build an agent')}
            </ButtonLink>
          </div>
        </div>

        <div className="grid gap-3 lg:grid-cols-3">
          <DashboardMetricCard title={msg('dashboard.creditBalance.title', 'Credit balance')}>
            <div className="flex items-end justify-between gap-4">
              <div>
                <div className="text-[30px] font-semibold leading-none text-foreground">$1.00</div>
                <ButtonLink href="/billing" variant="link" className="mt-2 h-auto p-0 text-sm text-primary">
                  {msg('dashboard.creditBalance.autoReload', 'Turn on auto-reload')}
                </ButtonLink>
              </div>
              <ButtonLink href="/billing" variant="outline" size="lg">
                {msg('dashboard.creditBalance.addFunds', 'Add funds')}
              </ButtonLink>
            </div>
          </DashboardMetricCard>

          <DashboardMetricCard title={msg('dashboard.spend.title', 'Spend this month')}>
            <div className="flex items-center justify-between gap-4">
              <div>
                <div className="text-[30px] font-semibold leading-none text-foreground">$0.00</div>
                <a href="/cost" className="mt-2 block text-sm text-muted-foreground">
                  {msg('dashboard.spend.limitReset', 'of $500 limit · resets Jul 1')}
                </a>
              </div>
              <div className="size-11 rounded-full border-[5px] border-secondary border-r-border" aria-hidden />
            </div>
          </DashboardMetricCard>

          <DashboardMetricCard
            title={
              <span className="flex items-center gap-2">
                {msg('dashboard.promptCaching.title', 'Prompt caching')}
                <InfoTooltip
                  label={msg(
                    'dashboard.promptCaching.tooltip',
                    'Prompt caching shows how many tokens were reused from cached prompt prefixes.',
                  )}
                />
              </span>
            }
            action={<Badge variant="outline">{msg('dashboard.promptCaching.notEnabled', 'Not enabled')}</Badge>}
          >
            <div className="flex items-end justify-between gap-4">
              <div>
                <div className="text-3xl font-semibold leading-none text-muted-foreground">—</div>
                <div className="mt-4 text-sm text-muted-foreground">
                  {msg('dashboard.promptCaching.tokensReused', 'tokens reused')}
                </div>
              </div>
              <ButtonLink href="/usage/cache" variant="outline" size="lg">
                {msg('dashboard.promptCaching.setUp', 'Set up')}
              </ButtonLink>
            </div>
          </DashboardMetricCard>
        </div>

        <PanelCard
          className="p-5"
          headerClassName="mb-7"
          titleClassName="font-medium"
          title={
            <span className="flex items-center gap-2">
              {msg('dashboard.tokenVolume.title', 'Token volume')}
              <InfoTooltip
                label={msg(
                  'dashboard.tokenVolume.tooltip',
                  'Token volume counts input and output tokens across recent API activity.',
                )}
              />
            </span>
          }
        >
          <div className="flex items-end justify-between gap-4">
            <div>
              <div className="text-3xl font-semibold leading-none text-muted-foreground">—</div>
              <div className="mt-5 text-sm text-muted-foreground">
                {msg('dashboard.tokenVolume.empty', 'No activity in the last 7 days')}
              </div>
            </div>
            <ButtonLink href="/workbench" variant="outline" size="lg">
              {msg('dashboard.tokenVolume.tryPrompt', 'Try a prompt')}
            </ButtonLink>
          </div>
        </PanelCard>

        <section className="space-y-3">
          <div className="flex items-center justify-between px-1">
            <h2 className="text-lg font-semibold text-foreground">{msg('dashboard.models.title', 'Models')}</h2>
            <div className="flex items-center gap-2">
              {modelCatalog.data ? (
                <Badge variant="outline">
                  {modelCatalog.catalogState?.stale
                    ? msg('dashboard.models.stale', 'Stale')
                    : msg('dashboard.models.fresh', 'Fresh')}
                </Badge>
              ) : null}
              {canRefreshModelCatalog ? (
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        aria-label={msg('dashboard.models.refresh', 'Refresh models')}
                        disabled={refreshModelCatalog.isPending}
                        onClick={() => refreshModelCatalog.mutate()}
                      >
                        <RefreshCw
                          className={`size-4 ${refreshModelCatalog.isPending ? 'animate-spin' : ''}`}
                          aria-hidden
                        />
                      </Button>
                    }
                  />
                  <TooltipContent>{msg('dashboard.models.refresh', 'Refresh models')}</TooltipContent>
                </Tooltip>
              ) : null}
              <ButtonLink href="/workbench" variant="link" className="h-auto p-0 text-sm text-muted-foreground">
                {msg('dashboard.models.openWorkbench', 'Open Workbench')}
              </ButtonLink>
            </div>
          </div>
          {modelCatalog.isPending ? (
            <div className="flex min-h-28 items-center justify-center text-sm text-muted-foreground">
              <RefreshCw className="mr-2 size-4 animate-spin" aria-hidden />
              {msg('dashboard.models.loading', 'Loading models')}
            </div>
          ) : modelCatalog.isError ? (
            <p className="rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
              {msg('dashboard.models.unavailable', 'Model catalog is unavailable.')}
            </p>
          ) : modelCatalog.models.length ? (
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
              {modelCatalog.models.map((model, index) => (
                <ModelCard
                  key={model.model_name}
                  model={model}
                  tone={modelCardTones[index % modelCardTones.length]}
                  isDefault={model.model_name === modelCatalog.defaultModelID}
                />
              ))}
            </div>
          ) : (
            <p className="rounded-lg border border-border px-4 py-3 text-sm text-muted-foreground">
              {msg('dashboard.models.empty', 'No models are available from the configured gateway.')}
            </p>
          )}
          {refreshModelCatalog.isError ? (
            <p className="px-1 text-sm text-destructive">
              {msg('dashboard.models.refreshFailed', 'Model refresh failed; the last successful catalog is unchanged.')}
            </p>
          ) : null}
        </section>

        <section className="space-y-3">
          <h2 className="px-1 text-lg font-semibold text-foreground">
            {msg('dashboard.resources.title', 'Resources')}
          </h2>
          <div className="grid gap-3 md:grid-cols-3">
            {resourceCards.map((resource) => (
              <ResourceCard key={resource.title} {...resource} />
            ))}
          </div>
        </section>
      </section>
    </TooltipProvider>
  );
}

function InfoTooltip({ label }: { label: string }) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={label}
            className="-m-1 text-muted-foreground hover:text-foreground"
          >
            <Info className="size-4" aria-hidden />
          </Button>
        }
      />
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function DashboardMetricCard({
  title,
  action,
  children,
}: {
  title: ReactNode;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <PanelCard
      title={title}
      action={action}
      className="min-h-[137px] p-5"
      headerClassName="mb-6"
      titleClassName="font-medium"
    >
      {children}
    </PanelCard>
  );
}

function ModelCard({ model, tone, isDefault }: { model: ModelCatalogModel; tone: string; isDefault: boolean }) {
  const { msg } = useI18n();
  const tags = [
    model.supports_tool_use ? msg('dashboard.models.tags.tools', 'Tools') : '',
    model.supports_thinking ? msg('dashboard.models.tags.thinking', 'Thinking') : '',
    model.max_tokens ? `${model.max_tokens.toLocaleString()} context` : '',
  ].filter(Boolean);

  return (
    <a href="/workbench" className="block rounded-lg outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
      <Card className="gap-0 overflow-hidden rounded-lg p-0 transition-[box-shadow] hover:shadow-sm">
        <div className={`${tone} grid h-[90px] place-items-center`}>
          <BrainCircuit className="size-12 stroke-[1.6]" aria-hidden />
        </div>
        <CardContent className="p-4">
          <div className="mb-3 flex items-center gap-2 text-sm font-medium text-foreground">
            <span className="min-w-0 truncate">{modelCatalogDisplayName(model)}</span>
            {isDefault ? <Badge>{msg('dashboard.models.default', 'Default')}</Badge> : null}
          </div>
          <p className="mb-3 truncate text-xs text-muted-foreground" title={model.model_name}>
            {model.model_name}
          </p>
          <div className="flex flex-wrap gap-1.5">
            {tags.map((tag) => (
              <Badge key={tag} variant="secondary" className="text-muted-foreground">
                {tag}
              </Badge>
            ))}
          </div>
        </CardContent>
      </Card>
    </a>
  );
}

function ResourceCard({
  id,
  title,
  badge,
  badgeId,
  icon: Icon,
  body,
}: {
  id: string;
  title: string;
  badge?: string;
  badgeId?: string;
  icon: IconComponent;
  body: string;
}) {
  const { msg } = useI18n();

  return (
    <a href="/quickstart" className="block rounded-lg outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
      <Card className="min-h-[132px] gap-0 rounded-lg p-4 transition-[box-shadow] hover:shadow-sm">
        <CardHeader className="mb-3 flex-row items-center gap-2 p-0 text-sm font-medium text-foreground">
          <Icon className="size-4 text-muted-foreground" aria-hidden />
          <CardTitle className="text-sm font-medium text-foreground">
            {msg(`dashboard.resources.${id}.title`, title)}
          </CardTitle>
          {badge ? <Badge>{badgeId ? msg(badgeId, badge) : badge}</Badge> : null}
        </CardHeader>
        <CardContent className="p-0">
          <p className="text-sm leading-5 text-muted-foreground">{msg(`dashboard.resources.${id}.body`, body)}</p>
        </CardContent>
      </Card>
    </a>
  );
}

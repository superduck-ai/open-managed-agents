import {
  BookOpen,
  Bot,
  Boxes,
  BrainCircuit,
  DatabaseZap,
  Info,
  KeyRound,
  MousePointer2,
  Network,
  Sparkles
} from 'lucide-react';
import type { ReactNode } from 'react';
import { Badge } from '@/shared/ui/badge';
import { Button, ButtonLink } from '@/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/card';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/shared/ui/tooltip';
import { useI18n } from '../../shared/i18n';
import { PanelCard } from './frame';
import { useDashboardWorkspaceScope, type IconComponent } from './model';

const docsHref = 'https://docs.anthropic.com/';
const modelDocsHref = 'https://docs.anthropic.com/en/docs/about-claude/models/overview';

const modelCards = [
  {
    name: 'Fable 5',
    badge: 'New',
    badgeId: 'nav.new',
    tone: 'bg-secondary text-secondary-foreground',
    icon: BrainCircuit,
    tags: [
      { id: 'dashboard.models.tags.mostCapable', label: 'Most capable' },
      { id: 'dashboard.models.tags.research', label: 'Research' },
      { id: 'dashboard.models.tags.multiDayTasks', label: 'Multi-day tasks' }
    ]
  },
  {
    name: 'Opus 4.8',
    tone: 'bg-amber-500/10 text-amber-600 dark:text-amber-400',
    icon: MousePointer2,
    tags: [
      { id: 'dashboard.models.tags.complexProjects', label: 'Complex projects' },
      { id: 'dashboard.models.tags.agents', label: 'Agents' },
      { id: 'dashboard.models.tags.coding', label: 'Coding' }
    ]
  },
  {
    name: 'Sonnet 4.6',
    tone: 'bg-secondary text-foreground',
    icon: Network,
    tags: [
      { id: 'dashboard.models.tags.everydayTasks', label: 'Everyday tasks' },
      { id: 'dashboard.models.tags.writing', label: 'Writing' },
      { id: 'dashboard.models.tags.costEfficient', label: 'Cost-efficient' }
    ]
  },
  {
    name: 'Haiku 4.5',
    tone: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
    icon: Sparkles,
    tags: [
      { id: 'dashboard.models.tags.fastest', label: 'Fastest' },
      { id: 'dashboard.models.tags.lowestCost', label: 'Lowest cost' },
      { id: 'dashboard.models.tags.highVolume', label: 'High volume' }
    ]
  }
];

const resourceCards = [
  {
    id: 'advisorTool',
    title: 'Advisor tool',
    badge: 'Beta',
    badgeId: 'dashboard.resources.badge.beta',
    icon: Bot,
    body: 'A fast, lower-cost model consults a more capable advisor mid-task. Get close to advisor-level quality while most tokens run at the cheaper model rate.'
  },
  {
    id: 'batchApi',
    title: 'Batch API',
    icon: Boxes,
    body: 'Move async workloads to the Batch API and save 50% on standard API prices.'
  },
  {
    id: 'promptCaching',
    title: 'Prompt caching',
    icon: DatabaseZap,
    body: 'Reuse prompt prefixes across API calls. Most orgs see input costs drop 50-90%.'
  }
];
export function DashboardHome() {
  const { msg } = useI18n();
  const { workspaceId } = useDashboardWorkspaceScope();
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
                <ButtonLink
                  href="/billing"
                  variant="link"
                  className="mt-2 h-auto p-0 text-sm text-primary"
                >
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
              <div
                className="size-11 rounded-full border-[5px] border-secondary border-r-border"
                aria-hidden
              />
            </div>
          </DashboardMetricCard>

          <DashboardMetricCard
            title={
              <span className="flex items-center gap-2">
                {msg('dashboard.promptCaching.title', 'Prompt caching')}
                <InfoTooltip
                  label={msg(
                    'dashboard.promptCaching.tooltip',
                    'Prompt caching shows how many tokens were reused from cached prompt prefixes.'
                  )}
                />
              </span>
            }
            action={<Badge variant="outline">{msg('dashboard.promptCaching.notEnabled', 'Not enabled')}</Badge>}
          >
            <div className="flex items-end justify-between gap-4">
              <div>
                <div className="text-3xl font-semibold leading-none text-muted-foreground">—</div>
                <div className="mt-4 text-sm text-muted-foreground">{msg('dashboard.promptCaching.tokensReused', 'tokens reused')}</div>
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
                  'Token volume counts input and output tokens across recent API activity.'
                )}
              />
            </span>
          }
        >
          <div className="flex items-end justify-between gap-4">
            <div>
              <div className="text-3xl font-semibold leading-none text-muted-foreground">—</div>
              <div className="mt-5 text-sm text-muted-foreground">{msg('dashboard.tokenVolume.empty', 'No activity in the last 7 days')}</div>
            </div>
            <ButtonLink href="/workbench" variant="outline" size="lg">
              {msg('dashboard.tokenVolume.tryPrompt', 'Try a prompt')}
            </ButtonLink>
          </div>
        </PanelCard>

        <section className="space-y-3">
          <div className="flex items-center justify-between px-1">
            <h2 className="text-lg font-semibold text-foreground">{msg('dashboard.models.title', 'Models')}</h2>
            <ButtonLink
              href={modelDocsHref}
              target="_blank"
              rel="noreferrer"
              variant="link"
              className="h-auto p-0 text-sm text-muted-foreground"
            >
              {msg('dashboard.models.compare', 'Compare models')}
            </ButtonLink>
          </div>
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            {modelCards.map((model) => (
              <ModelCard key={model.name} {...model} />
            ))}
          </div>
        </section>

        <section className="space-y-3">
          <h2 className="px-1 text-lg font-semibold text-foreground">{msg('dashboard.resources.title', 'Resources')}</h2>
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
          <Button type="button" variant="ghost" size="icon-xs" aria-label={label} className="-m-1 text-muted-foreground hover:text-foreground">
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
  children
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

function ModelCard({
  name,
  badge,
  badgeId,
  tone,
  icon: Icon,
  tags
}: {
  name: string;
  badge?: string;
  badgeId?: string;
  tone: string;
  icon: IconComponent;
  tags: Array<{ id: string; label: string }>;
}) {
  const { msg } = useI18n();

  return (
    <a href="/workbench" className="block rounded-lg outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
      <Card className="gap-0 overflow-hidden rounded-lg p-0 transition-[box-shadow] hover:shadow-sm">
        <div className={`${tone} grid h-[90px] place-items-center`}>
          <Icon className="size-12 stroke-[1.6]" aria-hidden />
        </div>
        <CardContent className="p-4">
          <div className="mb-3 flex items-center gap-2 text-sm font-medium text-foreground">
            {name}
            {badge ? <Badge>{badgeId ? msg(badgeId, badge) : badge}</Badge> : null}
          </div>
          <div className="flex flex-wrap gap-1.5">
            {tags.map((tag) => (
              <Badge key={tag.id} variant="secondary" className="text-muted-foreground">
                {msg(tag.id, tag.label)}
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
  body
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
          <CardTitle className="text-sm font-medium text-foreground">{msg(`dashboard.resources.${id}.title`, title)}</CardTitle>
          {badge ? <Badge>{badgeId ? msg(badgeId, badge) : badge}</Badge> : null}
        </CardHeader>
        <CardContent className="p-0">
          <p className="text-sm leading-5 text-muted-foreground">{msg(`dashboard.resources.${id}.body`, body)}</p>
        </CardContent>
      </Card>
    </a>
  );
}

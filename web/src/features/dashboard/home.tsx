import {
  ChevronRight,
  BookOpen,
  Bot,
  Boxes,
  Database,
  Folder,
  KeyRound,
  Layers,
  MessagesSquare,
  Server,
  Shield,
  Sparkles,
} from 'lucide-react';
import { ButtonLink } from '@/shared/ui/button';
import { Card, CardContent } from '@/shared/ui/card';
import { ResourcePageHeader } from '@/shared/ui/resource-page-header';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/shared/ui/tooltip';
import { useI18n } from '../../shared/i18n';
import { localizedWorkspaceName } from '../../shared/workspaces/display-name';
import { useDashboardWorkspaceScope } from './model';

const docsHrefByLocale = {
  en: 'https://oma.mintlifysite.com/docs/en/overview',
  'zh-CN': 'https://oma.mintlifysite.com/docs/zh/overview',
} as const;

const workspaceEntries = [
  {
    id: 'agents',
    title: 'Agents',
    description: 'Configure models, tools, and runtime environments.',
    path: 'agents',
    icon: Bot,
  },
  {
    id: 'sessions',
    title: 'Sessions',
    description: 'View running sessions and conversation history.',
    path: 'sessions',
    icon: MessagesSquare,
  },
  {
    id: 'models',
    title: 'LLM models',
    description: 'Manage providers and available models.',
    path: 'llm-models',
    icon: Sparkles,
  },
];

const resourceEntries = [
  { id: 'environments', title: 'Environments', path: 'environments', icon: Server },
  { id: 'deployments', title: 'Deployments', path: 'deployments', icon: Boxes },
  { id: 'memory', title: 'Memory stores', path: 'memory-stores', icon: Database },
  { id: 'vaults', title: 'Credential vaults', path: 'vaults', icon: Shield },
  { id: 'files', title: 'Files', path: 'files', icon: Folder },
  { id: 'skills', title: 'Skills', path: 'skills', icon: Layers },
];

export function DashboardHome() {
  const { locale, msg } = useI18n();
  const { workspaceId, workspaceName } = useDashboardWorkspaceScope();
  const workspacePath = `/workspaces/${encodeURIComponent(workspaceId || 'default')}`;
  const apiKeysHref = `/settings/workspaces/${encodeURIComponent(workspaceId || 'default')}/keys`;

  return (
    <TooltipProvider>
      <section className="space-y-7">
        <ResourcePageHeader
          title={msg('dashboard.overview', 'Overview')}
          description={localizedWorkspaceName(workspaceName, msg)}
          actions={
            <div className="flex flex-wrap items-center gap-2">
              <Tooltip>
                <TooltipTrigger
                  render={
                    <ButtonLink
                      href={docsHrefByLocale[locale]}
                      target="_blank"
                      rel="noreferrer"
                      variant="outline"
                      size="icon"
                      aria-label={msg('dashboard.actions.exploreDocs', 'Explore docs')}
                    >
                      <BookOpen className="size-4" aria-hidden />
                    </ButtonLink>
                  }
                />
                <TooltipContent>{msg('dashboard.actions.exploreDocs', 'Explore docs')}</TooltipContent>
              </Tooltip>
              <ButtonLink href={apiKeysHref} variant="outline">
                <KeyRound className="size-4" aria-hidden />
                {msg('dashboard.actions.getApiKey', 'Get API key')}
              </ButtonLink>
              <ButtonLink href={`${workspacePath}/agents/new`}>
                <Bot className="size-4" aria-hidden />
                {msg('dashboard.actions.createAgent', 'Create agent')}
              </ButtonLink>
            </div>
          }
        />
        <div className="grid gap-4 md:grid-cols-3">
          {workspaceEntries.map(({ id, title, description, path, icon: Icon }) => (
            <a
              key={id}
              href={`${workspacePath}/${path}`}
              className="group block rounded-xl outline-none focus-visible:outline-solid focus-visible:outline-[0.5px] focus-visible:outline-offset-2 focus-visible:outline-foreground"
            >
              <Card className="h-full gap-0 p-5 transition-shadow duration-150 ease-out motion-reduce:transition-none group-hover:shadow-sm">
                <CardContent className="flex h-full flex-col items-start gap-4 p-0">
                  <div className="flex w-full items-center justify-between text-muted-foreground">
                    <Icon className="size-6" aria-hidden />
                    <ChevronRight className="size-4" aria-hidden />
                  </div>
                  <div>
                    <h2 className="text-lg font-medium leading-7 text-foreground">
                      {msg(`dashboard.workspace.${id}.title`, title)}
                    </h2>
                    <p className="mt-1 text-sm leading-5 text-muted-foreground">
                      {msg(`dashboard.workspace.${id}.description`, description)}
                    </p>
                  </div>
                </CardContent>
              </Card>
            </a>
          ))}
        </div>
        <section className="space-y-3">
          <h2 className="text-lg font-medium text-foreground">
            {msg('dashboard.workspace.resources', 'Workspace resources')}
          </h2>
          <Card className="gap-0 overflow-hidden p-0">
            <CardContent className="grid p-0 sm:grid-cols-2">
              {resourceEntries.map(({ id, title, path, icon: Icon }, index) => (
                <a
                  key={id}
                  href={`${workspacePath}/${path}`}
                  className={`flex min-w-0 items-center gap-3 px-5 py-4 text-sm text-foreground outline-none transition-colors duration-150 ease-out hover:bg-muted focus-visible:ring-[0.5px] focus-visible:ring-inset focus-visible:ring-foreground motion-reduce:transition-none ${index > 0 ? 'border-t-[0.5px] border-border' : ''} ${index === 1 ? 'sm:border-t-0' : ''}`}
                >
                  <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                  <span className="min-w-0 flex-1">{msg(`dashboard.workspace.${id}.title`, title)}</span>
                  <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
                </a>
              ))}
            </CardContent>
          </Card>
        </section>
      </section>
    </TooltipProvider>
  );
}

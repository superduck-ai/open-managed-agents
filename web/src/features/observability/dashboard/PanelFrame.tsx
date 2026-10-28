import { ChartNoAxesColumn, Eye, EyeOff, Info, Link, Loader2, MoreVertical, Table2 } from 'lucide-react';
import { useContext, type ReactNode } from 'react';
import { useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Button } from '../../../shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '../../../shared/ui/card';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle } from '../../../shared/ui/empty';
import { Tooltip, TooltipContent, TooltipTrigger } from '../../../shared/ui/tooltip';
import { PanelContext, useDashboardInteractions } from './context';
import { usePanelTitle } from './labels';
import { PANEL_TEXT } from './panelText';

function panelLink(id: string) {
  const url = new URL(window.location.href);
  url.searchParams.set('view_panel', id);
  return url.toString();
}

function PanelMenu({ id, title }: { id: string; title: string }) {
  const { msg } = useI18n();
  const interactions = useDashboardInteractions();
  const panel = useContext(PanelContext);
  if (!interactions || !panel) return null;
  const viewing = interactions.viewPanel === id;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon-xs"
            className="opacity-0 group-hover/card:opacity-100 focus-visible:opacity-100 aria-expanded:opacity-100"
          />
        }
        aria-label={msg('observability.panelMenu', 'Menu for {title}', { title })}
      >
        <MoreVertical />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-44">
        <DropdownMenuItem onClick={() => interactions.onViewPanel(viewing ? '' : id)}>
          {viewing ? <EyeOff /> : <Eye />}
          {viewing ? msg('observability.exitView', 'Exit view') : msg('observability.view', 'View')}
          <DropdownMenuShortcut>{viewing ? 'Esc' : 'v'}</DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => interactions.onInspect(panel)}>
          <Table2 />
          {msg('observability.inspect', 'Inspect data')}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => void navigator.clipboard?.writeText(panelLink(id)).catch(() => undefined)}>
          <Link />
          {msg('observability.copyLink', 'Copy link')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function PanelFrame({ id, children }: { id: string; children: ReactNode }) {
  const { msg } = useI18n();
  const title = usePanelTitle(id);
  const interactions = useDashboardInteractions();
  const [, help] = PANEL_TEXT[id] ?? [id, ''];
  return (
    <Card
      size="sm"
      aria-label={title}
      aria-busy={interactions?.fetching || undefined}
      className="h-full min-w-0 gap-2"
      onMouseEnter={() => interactions?.onHoverPanel(id)}
      onMouseLeave={() => interactions?.onHoverPanel('')}
    >
      <CardHeader className="flex min-h-6 items-center gap-1">
        <CardTitle className="truncate">{title}</CardTitle>
        {help ? (
          <Tooltip>
            <TooltipTrigger type="button" className="cursor-help text-muted-foreground">
              <Info className="size-3.5" aria-hidden />
              <span className="sr-only">{msg(`observability.panel.${id}.help`, help)}</span>
            </TooltipTrigger>
            <TooltipContent className="max-w-72">{msg(`observability.panel.${id}.help`, help)}</TooltipContent>
          </Tooltip>
        ) : null}
        <div className="ml-auto flex items-center gap-1">
          {interactions?.fetching ? (
            <Loader2 className="size-3.5 animate-spin text-muted-foreground" aria-hidden />
          ) : null}
          <PanelMenu id={id} title={title} />
        </div>
      </CardHeader>
      <CardContent className="flex min-h-0 flex-1 flex-col gap-2">{children}</CardContent>
    </Card>
  );
}

export function EmptyPanel({ className }: { className?: string }) {
  const { msg } = useI18n();
  return (
    <Empty role="status" className={cn('min-h-0 flex-1 gap-2 rounded-none border-0 p-2', className)}>
      <EmptyHeader className="gap-1.5">
        <EmptyMedia variant="icon" className="mb-0 size-7">
          <ChartNoAxesColumn className="size-3.5" aria-hidden />
        </EmptyMedia>
        <EmptyTitle className="text-xs font-normal text-muted-foreground">
          {msg('observability.noData', 'No data in this range.')}
        </EmptyTitle>
      </EmptyHeader>
    </Empty>
  );
}

import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table';
import { Archive, Bot, Copy, Eye, Info, MessageSquare, Pencil, X } from 'lucide-react';
import { useMemo, type MouseEvent } from 'react';
import { useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Button } from '../../../shared/ui/button';
import {
  CopyIdCell,
  DataTableCell,
  DataTableRow,
  MoreActionsButton,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
} from '../../../shared/ui/data-table-interactions';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { Skeleton } from '../../../shared/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../../shared/ui/tooltip';
import { toast } from '../../../shared/ui/sonner';
import { useManagedEntityCells } from '../resources/environment-list';
import { entityAgentId, localizedEntityStatusLabel } from '../resources/model';
import { entityActionLabel, managedColumnLabel } from '../labels';
import type { SessionApiResponse } from '../types';
import {
  agentDetailHref,
  copyText,
  handleInternalLinkClick,
  managedEntityDetailHref,
  navigateToInternalHref,
  objectRecord,
} from '../utils';
import { SessionStatusPill } from './sessionTimeline';

export type SessionListActions = {
  onQuickView: (session: SessionApiResponse) => void;
  onEdit: (session: SessionApiResponse) => void;
  onArchive: (session: SessionApiResponse) => void;
  onDelete: (session: SessionApiResponse) => void;
};

function SessionMetricCell({ session, field }: { session: SessionApiResponse; field: string }) {
  return useManagedEntityCells('sessions', session)[field];
}

function SessionAgentCell({
  session,
  workspaceId,
  agentNames,
}: {
  session: SessionApiResponse;
  workspaceId: string;
  agentNames?: ReadonlyMap<string, string>;
}) {
  const id = entityAgentId(session);
  if (!id) return '—';
  const href = agentDetailHref(workspaceId, id);
  const agent = objectRecord(session.agent);
  const name = typeof agent.name === 'string' && agent.name ? agent.name : agentNames?.get(id) || id;
  return (
    <a
      href={href}
      onClick={(event) => handleInternalLinkClick(event, href)}
      className="inline-flex min-w-0 max-w-full items-center gap-1.5 text-muted-foreground hover:text-foreground hover:underline"
    >
      <Bot className="size-3.5 shrink-0" aria-hidden />
      <span className="truncate">{name}</span>
      {typeof agent.version === 'number' ? (
        <span className="shrink-0 text-xs text-muted-foreground/70">v{agent.version}</span>
      ) : null}
    </a>
  );
}

function SessionRowActions({
  session,
  workspaceId,
  busy,
  actions,
}: {
  session: SessionApiResponse;
  workspaceId: string;
  busy: boolean;
  actions: SessionListActions;
}) {
  const { msg } = useI18n();
  const archived = Boolean(session.archived_at);
  const viewOnly = archived || ['terminated', 'deleted'].includes(session.status);
  const entries = [
    {
      key: 'preview',
      icon: Eye,
      label: msg('managedAgents.sessions.quickView', 'Quick view'),
      run: () => actions.onQuickView(session),
    },
    {
      key: 'open',
      icon: MessageSquare,
      label: viewOnly
        ? msg('managedAgents.sessions.viewSession', 'View session')
        : msg('managedAgents.sessions.openConversation', 'Open conversation'),
      run: () =>
        navigateToInternalHref(`${managedEntityDetailHref(workspaceId, 'sessions', session.id)}?interactive=true`),
    },
    {
      key: 'copy',
      icon: Copy,
      label: msg('common.copyId', 'Copy ID'),
      run: () =>
        void copyText(session.id)
          .then(() => toast.success(msg('common.idCopied', 'ID copied')))
          .catch(() => toast.error(msg('common.copyFailed', 'Copy failed'))),
    },
    {
      key: 'edit',
      icon: Pencil,
      label: entityActionLabel('edit', 'sessions', msg),
      run: () => actions.onEdit(session),
      disabled: archived,
    },
    {
      key: 'archive',
      icon: Archive,
      label: entityActionLabel('archive', 'sessions', msg),
      run: () => actions.onArchive(session),
      disabled: archived,
    },
    {
      key: 'delete',
      icon: X,
      label: entityActionLabel('delete', 'sessions', msg),
      run: () => actions.onDelete(session),
    },
  ];
  return (
    <div className="flex justify-end gap-0.5">
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={msg('managedAgents.sessions.quickView', 'Quick view')}
              onClick={() => actions.onQuickView(session)}
            />
          }
        >
          <Eye aria-hidden />
        </TooltipTrigger>
        <TooltipContent>{msg('managedAgents.sessions.quickView', 'Quick view')}</TooltipContent>
      </Tooltip>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={<MoreActionsButton label={msg('managedAgents.common.moreActions', 'More actions')} disabled={busy} />}
        />
        <DropdownMenuContent align="end" className="w-48">
          {entries.map(({ key, icon: Icon, label, run, disabled }) => (
            <DropdownMenuItem
              key={key}
              onClick={run}
              disabled={disabled}
              variant={key === 'delete' ? 'destructive' : undefined}
            >
              <Icon className="size-4" aria-hidden />
              {label}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

export function SessionListTable({
  sessions,
  workspaceId,
  loading,
  busyAction,
  actions,
  agentNames,
}: {
  sessions: SessionApiResponse[];
  agentNames?: ReadonlyMap<string, string>;
  workspaceId: string;
  loading: boolean;
  busyAction: string | null;
  actions: SessionListActions;
}) {
  const { msg } = useI18n();
  const columns = useMemo<ColumnDef<SessionApiResponse>[]>(
    () => [
      {
        id: 'id',
        header: msg('common.id', 'ID'),
        size: 208,
        cell: ({ row }) => {
          const href = managedEntityDetailHref(workspaceId, 'sessions', row.original.id);
          return (
            <CopyIdCell
              value={row.original.id}
              ariaLabel={msg('managedAgents.common.copyIdValue', 'Copy {id}', { id: row.original.id })}
              alwaysVisible
            >
              <a
                href={href}
                className="truncate font-mono text-xs text-foreground hover:underline"
                onClick={(event) => handleInternalLinkClick(event, href)}
              >
                {row.original.id}
              </a>
            </CopyIdCell>
          );
        },
      },
      {
        id: 'name',
        header: msg('common.name', 'Name'),
        size: 180,
        cell: ({ row }) => {
          const href = managedEntityDetailHref(workspaceId, 'sessions', row.original.id);
          return (
            <a
              href={href}
              onClick={(event) => handleInternalLinkClick(event, href)}
              title={row.original.title || undefined}
              className="block truncate hover:underline"
            >
              {row.original.title || '—'}
            </a>
          );
        },
      },
      {
        id: 'status',
        header: msg('common.status', 'Status'),
        size: 112,
        cell: ({ row }) => (
          <SessionStatusPill
            status={
              row.original.archived_at
                ? msg('common.archived', 'Archived')
                : localizedEntityStatusLabel('sessions', row.original, msg)
            }
          />
        ),
      },
      {
        id: 'agent',
        header: msg('managedAgents.common.agent', 'Agent'),
        size: 160,
        cell: ({ row }) => (
          <SessionAgentCell session={row.original} workspaceId={workspaceId} agentNames={agentNames} />
        ),
      },
      {
        id: 'cost',
        size: 100,
        header: () => (
          <span className="inline-flex items-center gap-1">
            {managedColumnLabel('Cost', msg)}
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    className="size-4"
                    aria-label={msg(
                      'managedAgents.sessions.costHint',
                      'Cost is priced at public list rates, before any negotiated discounts.',
                    )}
                  />
                }
              >
                <Info className="size-3" aria-hidden />
              </TooltipTrigger>
              <TooltipContent>
                {msg(
                  'managedAgents.sessions.costHint',
                  'Cost is priced at public list rates, before any negotiated discounts.',
                )}
              </TooltipContent>
            </Tooltip>
          </span>
        ),
        cell: ({ row }) => <SessionMetricCell session={row.original} field="Cost" />,
      },
      {
        id: 'tokens',
        header: managedColumnLabel('Tokens in / out', msg),
        size: 124,
        cell: ({ row }) => <SessionMetricCell session={row.original} field="Tokens in / out" />,
      },
      {
        id: 'created',
        header: msg('common.created', 'Created'),
        size: 132,
        cell: ({ row }) => (
          <span className="text-muted-foreground">
            <SessionMetricCell session={row.original} field="Created" />
          </span>
        ),
      },
      {
        id: 'actions',
        header: '',
        size: 80,
        cell: ({ row }) => (
          <SessionRowActions
            session={row.original}
            workspaceId={workspaceId}
            busy={Boolean(busyAction?.endsWith(`:${row.original.id}`))}
            actions={actions}
          />
        ),
      },
    ],
    [msg, workspaceId, busyAction, actions, agentNames],
  );
  const table = useReactTable({
    data: sessions,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (session) => session.id,
  });
  const previewRow = (event: MouseEvent<HTMLTableRowElement>, session: SessionApiResponse) => {
    if (
      event.target instanceof Element &&
      (!event.currentTarget.contains(event.target) ||
        event.target.closest('a,button,input,select,textarea,[role="checkbox"],[role="button"],[role="menuitem"]'))
    ) {
      return;
    }
    actions.onQuickView(session);
  };
  return (
    <TooltipProvider>
      <Table className={cn(dataTableClassName, 'min-w-[1080px]')} data-testid="session-list-table">
        <TableHeader>
          {table.getHeaderGroups().map((group) => (
            <TableRow key={group.id} className={dataTableHeaderRowClassName}>
              {group.headers.map((header) => (
                <TableHead
                  key={header.id}
                  style={{ width: header.id === 'name' ? undefined : header.getSize() }}
                  className={cn(dataTableHeaderCellClassName, ['cost', 'tokens'].includes(header.id) && 'text-right')}
                  aria-label={
                    header.id === 'actions'
                      ? msg('common.actions', 'Actions')
                      : header.id === 'cost'
                        ? managedColumnLabel('Cost', msg)
                        : undefined
                  }
                >
                  {flexRender(header.column.columnDef.header, header.getContext())}
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {loading
            ? Array.from({ length: 5 }, (_, index) => (
                <TableRow key={index} className="hover:bg-transparent">
                  {columns.map((column) => (
                    <TableCell key={column.id} className="h-11 px-3">
                      <Skeleton className="h-4 w-3/4" />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            : table.getRowModel().rows.map((row) => (
                <DataTableRow
                  key={row.id}
                  className="[&>td]:h-11"
                  clickable
                  onClick={(event) => previewRow(event, row.original)}
                >
                  {row.getVisibleCells().map((cell) => (
                    <DataTableCell
                      key={cell.id}
                      edge={cell.column.id === 'id' ? 'start' : cell.column.id === 'actions' ? 'end' : undefined}
                      className={cn(
                        'min-w-0 truncate',
                        ['cost', 'tokens'].includes(cell.column.id) && 'text-right tabular-nums',
                      )}
                    >
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </DataTableCell>
                  ))}
                </DataTableRow>
              ))}
        </TableBody>
      </Table>
    </TooltipProvider>
  );
}

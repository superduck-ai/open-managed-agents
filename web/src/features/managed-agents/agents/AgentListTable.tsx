import { Archive } from 'lucide-react';
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Checkbox } from '@/shared/ui/checkbox';
import { Badge } from '@/shared/ui/badge';
import { cn } from '@/shared/lib/utils';
import {
  CopyIdCell,
  DataTableCell,
  DataTableResourceLink,
  DataTableRow,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
} from '@/shared/ui/data-table-interactions';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/shared/ui/table';
import { managedColumnLabel } from '../labels';
import type { AgentApiResponse } from '../types';
import { agentDetailHref, handleInternalLinkClick } from '../utils';
import { agentModelName, formatDetailDate } from './model';
import { AgentRowActions } from './AgentRowActions';
import { useAgentListSelection } from './use-agent-list-selection';

export function AgentListTable({
  agents,
  loading,
  workspaceId,
  archivingIds,
  quickViewAgentId,
  onQuickView,
  onStartSession,
  onArchive,
}: {
  agents: AgentApiResponse[];
  loading: boolean;
  workspaceId: string;
  archivingIds: Set<string>;
  quickViewAgentId: string | null;
  onQuickView: (agentId: string) => void;
  onStartSession: (agent: AgentApiResponse) => void;
  onArchive: (agentIds: string[]) => void;
}) {
  const { msg } = useI18n();
  const selection = useAgentListSelection(loading ? [] : agents, workspaceId);
  const columns: ColumnDef<AgentApiResponse>[] = [
    { id: 'selection', size: 36 },
    { id: 'id', header: managedColumnLabel('ID', msg), size: 200 },
    { id: 'name', header: managedColumnLabel('Name', msg) },
    { id: 'model', header: managedColumnLabel('Model', msg), size: 160 },
    { id: 'created', header: managedColumnLabel('Created', msg), size: 88 },
    { id: 'updated', header: managedColumnLabel('Last updated', msg), size: 96 },
    { id: 'actions', size: 136 },
  ];
  const table = useReactTable({
    data: agents,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (agent) => agent.id,
  });
  return (
    <>
      {selection.selectedIds.length ? (
        <div className="mb-2 flex flex-wrap items-center gap-3 rounded-lg bg-muted px-3 py-2 text-sm" role="status">
          <span>
            {msg('managedAgents.agents.selectedCount', '{count} selected', { count: selection.selectedIds.length })}
          </span>
          <Button
            variant="ghost"
            size="sm"
            disabled={selection.selectedIds.some((id) => archivingIds.has(id))}
            onClick={() => onArchive(selection.selectedIds)}
          >
            <Archive aria-hidden />
            {msg('managedAgents.agents.archiveSelected', 'Archive selected')}
          </Button>
        </div>
      ) : null}
      <Table className={cn(dataTableClassName, 'min-w-[900px]')}>
        <TableHeader>
          <TableRow className={dataTableHeaderRowClassName}>
            <TableHead className="h-10 w-9 px-2">
              <Checkbox
                aria-label={msg('managedAgents.agents.selectAll', 'Select all agents')}
                checked={selection.allSelected}
                indeterminate={selection.selectedIds.length > 0 && !selection.allSelected}
                disabled={!selection.selectableIds.length}
                onCheckedChange={selection.toggleAll}
              />
            </TableHead>
            {table
              .getFlatHeaders()
              .filter((header) => header.id !== 'selection' && header.id !== 'actions')
              .map((header) => (
                <TableHead
                  key={header.id}
                  className={cn(dataTableHeaderCellClassName)}
                  style={{ width: header.id === 'name' ? undefined : header.getSize() }}
                >
                  {flexRender(header.column.columnDef.header, header.getContext())}
                </TableHead>
              ))}
            <TableHead className="h-10 w-[136px] px-2" aria-label={managedColumnLabel('Actions', msg)} />
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading ? (
            <TableRow>
              <TableCell colSpan={7} className="h-[280px] text-center text-sm text-muted-foreground">
                {msg('managedAgents.agents.loading', 'Loading agents...')}
              </TableCell>
            </TableRow>
          ) : (
            table
              .getRowModel()
              .rows.map(({ original: agent }) => (
                <AgentListRow
                  key={agent.id}
                  agent={agent}
                  workspaceId={workspaceId}
                  selected={selection.selectedIds.includes(agent.id)}
                  previewed={agent.id === quickViewAgentId}
                  archiving={archivingIds.has(agent.id)}
                  onSelect={(checked) => selection.toggleAgent(agent.id, checked)}
                  onQuickView={() => onQuickView(agent.id)}
                  onStartSession={() => onStartSession(agent)}
                  onArchive={() => onArchive([agent.id])}
                />
              ))
          )}
        </TableBody>
      </Table>
    </>
  );
}

function AgentListRow({
  agent,
  workspaceId,
  selected,
  previewed,
  archiving,
  onSelect,
  onQuickView,
  onStartSession,
  onArchive,
}: {
  agent: AgentApiResponse;
  workspaceId: string;
  selected: boolean;
  previewed: boolean;
  archiving: boolean;
  onSelect: (checked: boolean) => void;
  onQuickView: () => void;
  onStartSession: () => void;
  onArchive: () => void;
}) {
  const { msg, locale } = useI18n();
  const href = agentDetailHref(workspaceId, agent.id);
  const dateFormatter = new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric' });
  const shortDate = (value: string) =>
    Number.isFinite(Date.parse(value)) ? dateFormatter.format(new Date(value)) : '-';
  return (
    <DataTableRow selected={selected || previewed} className="group/agent-row [&>td]:h-11">
      <DataTableCell edge="start" className="px-2">
        <Checkbox
          aria-label={msg('managedAgents.agents.selectAgent', 'Select {name}', { name: agent.name || agent.id })}
          checked={selected}
          disabled={archiving || Boolean(agent.archived_at)}
          onCheckedChange={onSelect}
        />
      </DataTableCell>
      <DataTableCell>
        <CopyIdCell
          value={agent.id}
          ariaLabel={msg('managedAgents.common.copyIdValue', 'Copy {id}', { id: agent.id })}
          className="gap-1.5"
        >
          <DataTableResourceLink
            href={href}
            className="font-mono text-xs text-foreground"
            onClick={(event) => handleInternalLinkClick(event, href)}
          >
            {agent.id}
          </DataTableResourceLink>
        </CopyIdCell>
      </DataTableCell>
      <DataTableCell>
        <div className="flex min-w-0 items-center gap-2">
          <DataTableResourceLink
            href={href}
            className="truncate text-foreground"
            onClick={(event) => handleInternalLinkClick(event, href)}
          >
            {agent.name || msg('managedAgents.agents.untitled', 'Untitled agent')}
          </DataTableResourceLink>
          {agent.archived_at ? <Badge variant="secondary">{msg('common.archived', 'Archived')}</Badge> : null}
        </div>
      </DataTableCell>
      <DataTableCell className="truncate font-mono text-xs text-muted-foreground">
        {agentModelName(agent.model)}
      </DataTableCell>
      <DataTableCell className="text-muted-foreground">
        <time dateTime={agent.created_at} title={formatDetailDate(agent.created_at, locale)}>
          {shortDate(agent.created_at)}
        </time>
      </DataTableCell>
      <DataTableCell className="text-muted-foreground">
        <time dateTime={agent.updated_at} title={formatDetailDate(agent.updated_at, locale)}>
          {shortDate(agent.updated_at)}
        </time>
      </DataTableCell>
      <DataTableCell edge="end" className="px-2">
        <AgentRowActions
          agent={agent}
          archiving={archiving}
          onQuickView={onQuickView}
          onStartSession={onStartSession}
          onArchive={onArchive}
        />
      </DataTableCell>
    </DataTableRow>
  );
}

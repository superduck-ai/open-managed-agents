import { Archive } from 'lucide-react';
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Checkbox } from '@/shared/ui/checkbox';
import { cn } from '@/shared/lib/utils';
import {
  CopyIdCell,
  DataTableCell,
  DataTableResourceLink,
  DataTableRow,
  dataTableActionsColumnClassName,
  dataTableCheckboxColumnClassName,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
  dataTableIdColumnClassName,
  dataTableMetaColumnClassName,
  dataTablePrimaryColumnClassName,
  dataTableTimeColumnClassName,
} from '@/shared/ui/data-table-interactions';
import { ResourceBulkActionBar } from '@/shared/ui/resource-bulk-action-bar';
import { ResourceStatusBadge } from '@/shared/ui/resource-status-badge';
import { TableSkeletonRows } from '@/shared/ui/resource-table';
import { RelativeTimeText } from '@/shared/ui/resource-time-cell';
import { Table, TableBody, TableHead, TableHeader, TableRow } from '@/shared/ui/table';
import { managedColumnLabel } from '../labels';
import type { AgentApiResponse } from '../types';
import { agentDetailHref, handleInternalLinkClick } from '../utils';
import { agentModelName } from './model';
import { AgentRowActions } from './AgentRowActions';
import { useAgentListSelection } from './use-agent-list-selection';

const agentColumnClassNames: Record<string, string> = {
  selection: dataTableCheckboxColumnClassName,
  id: dataTableIdColumnClassName,
  name: dataTablePrimaryColumnClassName,
  model: dataTableMetaColumnClassName,
  created: dataTableTimeColumnClassName,
  updated: dataTableTimeColumnClassName,
  actions: dataTableActionsColumnClassName,
};

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
    { id: 'selection' },
    { id: 'id', header: managedColumnLabel('ID', msg) },
    { id: 'name', header: managedColumnLabel('Name', msg) },
    { id: 'model', header: managedColumnLabel('Model', msg) },
    { id: 'created', header: managedColumnLabel('Created', msg) },
    { id: 'updated', header: managedColumnLabel('Last updated', msg) },
    { id: 'actions' },
  ];
  const table = useReactTable({
    data: agents,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (agent) => agent.id,
  });
  return (
    <>
      <Table className={dataTableClassName}>
        <TableHeader>
          <TableRow className={dataTableHeaderRowClassName}>
            <TableHead className={cn(dataTableHeaderCellClassName, dataTableCheckboxColumnClassName)}>
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
                  className={cn(dataTableHeaderCellClassName, agentColumnClassNames[header.id])}
                >
                  {flexRender(header.column.columnDef.header, header.getContext())}
                </TableHead>
              ))}
            <TableHead
              className={cn(dataTableHeaderCellClassName, dataTableActionsColumnClassName)}
              aria-label={managedColumnLabel('Actions', msg)}
            />
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading ? (
            <TableSkeletonRows colCount={7} label={msg('managedAgents.agents.loading', 'Loading agents...')} />
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
      <ResourceBulkActionBar
        selectedCount={selection.selectedIds.length}
        selectedLabel={msg('managedAgents.agents.selectedCount', '{count} selected', {
          count: selection.selectedIds.length,
        })}
        onClear={() => selection.toggleAll(false)}
      >
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={selection.selectedIds.some((id) => archivingIds.has(id))}
          onClick={() => onArchive(selection.selectedIds)}
        >
          <Archive className="size-4" strokeWidth={1.5} aria-hidden />
          {msg('managedAgents.agents.archiveSelected', 'Archive selected')}
        </Button>
      </ResourceBulkActionBar>
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
  const { msg } = useI18n();
  const href = agentDetailHref(workspaceId, agent.id);
  const modelName = agentModelName(agent.model);
  return (
    <DataTableRow selected={selected || previewed} className="group/agent-row">
      <DataTableCell edge="start" className={dataTableCheckboxColumnClassName}>
        <Checkbox
          aria-label={msg('managedAgents.agents.selectAgent', 'Select {name}', { name: agent.name || agent.id })}
          checked={selected}
          disabled={archiving || Boolean(agent.archived_at)}
          onCheckedChange={onSelect}
        />
      </DataTableCell>
      <DataTableCell className={dataTableIdColumnClassName}>
        <CopyIdCell
          value={agent.id}
          ariaLabel={msg('managedAgents.common.copyIdValue', 'Copy {id}', { id: agent.id })}
          stopPropagation
        />
      </DataTableCell>
      <DataTableCell className={dataTablePrimaryColumnClassName}>
        <div className="flex min-w-0 max-w-[360px] items-center gap-2">
          <DataTableResourceLink
            href={href}
            title={agent.name || undefined}
            className="truncate font-medium text-foreground"
            onClick={(event) => handleInternalLinkClick(event, href)}
          >
            {agent.name || msg('managedAgents.agents.untitled', 'Untitled agent')}
          </DataTableResourceLink>
          {agent.archived_at ? (
            <ResourceStatusBadge tone="neutral">{msg('common.archived', 'Archived')}</ResourceStatusBadge>
          ) : null}
        </div>
      </DataTableCell>
      <DataTableCell className={dataTableMetaColumnClassName}>
        <span title={modelName} className="block max-w-[280px] truncate font-mono text-xs text-muted-foreground">
          {modelName}
        </span>
      </DataTableCell>
      <DataTableCell className={dataTableTimeColumnClassName}>
        <RelativeTimeText value={agent.created_at} />
      </DataTableCell>
      <DataTableCell className={dataTableTimeColumnClassName}>
        <RelativeTimeText value={agent.updated_at} />
      </DataTableCell>
      <DataTableCell edge="end" className={dataTableActionsColumnClassName}>
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

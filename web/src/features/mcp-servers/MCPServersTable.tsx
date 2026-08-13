import { Archive, Pencil, Trash2 } from 'lucide-react';
import type { MouseEvent } from 'react';

import { cn } from '@/shared/lib/utils';
import { type WorkspaceMCPServer, mcpServerErrorMessage } from '@/shared/api/workspaceMCPServers';
import { Badge } from '@/shared/ui/badge';
import {
  CopyIdCell,
  DataTableCell,
  DataTableRow,
  MoreActionsButton,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
} from '@/shared/ui/data-table-interactions';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/shared/ui/dropdown-menu';
import { TableEmptyRow, TableErrorRow, TableLoadingRow } from '@/shared/ui/resource-table';
import { Table, TableBody, TableHead, TableHeader, TableRow } from '@/shared/ui/table';
import { useI18n } from '../../shared/i18n';

type MCPServersTableProps = {
  servers: WorkspaceMCPServer[];
  selectedServerId: string | null;
  isLoading: boolean;
  isFetching: boolean;
  error: unknown;
  formatter: Intl.DateTimeFormat;
  onRetry: () => void;
  onOpen: (serverId: string) => void;
  onEdit: (server: WorkspaceMCPServer) => void;
  onArchive: (server: WorkspaceMCPServer) => void;
  onDelete: (server: WorkspaceMCPServer) => void;
};

export function MCPServersTable(props: MCPServersTableProps) {
  const { msg } = useI18n();
  return (
    <section aria-label={msg('mcpServers.listAria', 'MCP servers list')} className="min-w-0 overflow-x-auto">
      <Table className={cn(dataTableClassName, 'min-w-[840px]')}>
        <colgroup>
          <col className="w-[17%]" />
          <col className="w-[20%]" />
          <col className="w-[32%]" />
          <col className="w-[12%]" />
          <col className="w-[15%]" />
          <col className="w-[4%]" />
        </colgroup>
        <TableHeader>
          <TableRow className={dataTableHeaderRowClassName}>
            <TableHead className={cn(dataTableHeaderCellClassName, 'truncate')}>{msg('common.id', 'ID')}</TableHead>
            <TableHead className={cn(dataTableHeaderCellClassName, 'truncate')}>
              {msg('mcpServers.name', 'Name')}
            </TableHead>
            <TableHead className={cn(dataTableHeaderCellClassName, 'truncate')}>
              {msg('mcpServers.endpoint', 'Endpoint')}
            </TableHead>
            <TableHead className={cn(dataTableHeaderCellClassName, 'truncate')}>
              {msg('mcpServers.status', 'Status')}
            </TableHead>
            <TableHead className={cn(dataTableHeaderCellClassName, 'truncate')}>
              {msg('mcpServers.updated', 'Updated')}
            </TableHead>
            <TableHead className={dataTableHeaderCellClassName} aria-label={msg('common.actions', 'Actions')} />
          </TableRow>
        </TableHeader>
        <TableBody>
          <MCPServersTableBody {...props} />
        </TableBody>
      </Table>
      {props.isFetching && !props.isLoading ? (
        <span className="sr-only">{msg('common.updating', 'Updating...')}</span>
      ) : null}
    </section>
  );
}

function MCPServersTableBody({
  servers,
  selectedServerId,
  isLoading,
  error,
  formatter,
  onRetry,
  onOpen,
  onEdit,
  onArchive,
  onDelete,
}: MCPServersTableProps) {
  const { msg } = useI18n();
  if (isLoading) {
    return <TableLoadingRow colSpan={6} label={msg('mcpServers.loading', 'Loading MCP servers...')} />;
  }
  if (error) {
    return (
      <TableErrorRow
        colSpan={6}
        title={msg('mcpServers.loadError', 'MCP servers could not be loaded.')}
        message={mcpServerErrorMessage(error)}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (servers.length === 0) {
    return <TableEmptyRow colSpan={6}>{msg('mcpServers.empty', 'No MCP servers match this view.')}</TableEmptyRow>;
  }
  return servers.map((server) => (
    <MCPServerRow
      key={server.id}
      server={server}
      selected={selectedServerId === server.id}
      formatter={formatter}
      onOpen={onOpen}
      onEdit={onEdit}
      onArchive={onArchive}
      onDelete={onDelete}
    />
  ));
}

function MCPServerRow({
  server,
  selected,
  formatter,
  onOpen,
  onEdit,
  onArchive,
  onDelete,
}: Pick<MCPServersTableProps, 'formatter' | 'onOpen' | 'onEdit' | 'onArchive' | 'onDelete'> & {
  server: WorkspaceMCPServer;
  selected: boolean;
}) {
  const { msg } = useI18n();
  return (
    <DataTableRow clickable selected={selected} onClick={() => onOpen(server.id)}>
      <DataTableCell edge="start" className="min-w-0">
        <CopyIdCell
          value={server.id}
          displayValue={formatMCPServerId(server.id)}
          ariaLabel={msg('mcpServers.copyAria', 'Copy {serverId}', { serverId: server.id })}
          stopPropagation
        />
      </DataTableCell>
      <DataTableCell className="truncate font-medium" title={server.name}>
        {server.name}
      </DataTableCell>
      <DataTableCell className="truncate font-mono text-xs text-muted-foreground" title={server.url}>
        {server.url}
      </DataTableCell>
      <DataTableCell className="truncate">
        <MCPServerStatusBadge status={server.status} />
      </DataTableCell>
      <DataTableCell className="truncate text-muted-foreground">
        {formatDate(server.updated_at, formatter)}
      </DataTableCell>
      <DataTableCell edge="end" className="px-2 text-right">
        <MCPServerActionsMenu server={server} onEdit={onEdit} onArchive={onArchive} onDelete={onDelete} />
      </DataTableCell>
    </DataTableRow>
  );
}

export function MCPServerActionsMenu({
  server,
  onEdit,
  onArchive,
  onDelete,
}: {
  server: WorkspaceMCPServer;
  onEdit: (server: WorkspaceMCPServer) => void;
  onArchive: (server: WorkspaceMCPServer) => void;
  onDelete: (server: WorkspaceMCPServer) => void;
}) {
  const { msg } = useI18n();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <MoreActionsButton
            label={msg('mcpServers.actionsAria', 'Actions for {name}', { name: server.name })}
            onClick={(event) => event.stopPropagation()}
          />
        }
      />
      <DropdownMenuContent align="end" sideOffset={8} className="min-w-40">
        <DropdownMenuItem onClick={(event) => runMenuAction(event, () => onEdit(server))}>
          <Pencil aria-hidden />
          {msg('common.edit', 'Edit')}
        </DropdownMenuItem>
        {server.status === 'active' ? (
          <DropdownMenuItem onClick={(event) => runMenuAction(event, () => onArchive(server))}>
            <Archive aria-hidden />
            {msg('common.archive', 'Archive')}
          </DropdownMenuItem>
        ) : null}
        <DropdownMenuItem variant="destructive" onClick={(event) => runMenuAction(event, () => onDelete(server))}>
          <Trash2 aria-hidden />
          {msg('common.delete', 'Delete')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function MCPServerStatusBadge({ status }: { status: WorkspaceMCPServer['status'] }) {
  const { msg } = useI18n();
  return (
    <Badge variant="secondary" className="rounded-md">
      {status === 'active' ? msg('mcpServers.active', 'Active') : msg('mcpServers.archived', 'Archived')}
    </Badge>
  );
}

function runMenuAction(event: MouseEvent, action: () => void) {
  event.stopPropagation();
  action();
}

function formatMCPServerId(serverId: string) {
  return serverId.length > 18 ? `${serverId.slice(0, 15)}...` : serverId;
}

function formatDate(value: string, formatter: Intl.DateTimeFormat) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : formatter.format(date);
}

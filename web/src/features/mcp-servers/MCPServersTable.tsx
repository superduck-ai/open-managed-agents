import { Pencil, Trash2 } from 'lucide-react';
import type { MouseEvent } from 'react';

import { cn } from '@/shared/lib/utils';
import { type WorkspaceMCPServer, mcpServerErrorMessage } from '@/shared/api/workspaceMCPServers';
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
  onDelete: (server: WorkspaceMCPServer) => void;
};

export function MCPServersTable(props: MCPServersTableProps) {
  const { msg } = useI18n();
  return (
    <section aria-label={msg('mcpServers.listAria', 'MCP servers list')} className="min-w-0 overflow-x-auto">
      <Table className={cn(dataTableClassName, 'min-w-[840px]')}>
        <colgroup>
          <col className="w-[20%]" />
          <col className="w-[22%]" />
          <col className="w-[36%]" />
          <col className="w-[18%]" />
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
  onDelete,
}: MCPServersTableProps) {
  const { msg } = useI18n();
  if (isLoading) {
    return <TableLoadingRow colSpan={5} label={msg('mcpServers.loading', 'Loading MCP servers...')} />;
  }
  if (error) {
    return (
      <TableErrorRow
        colSpan={5}
        title={msg('mcpServers.loadError', 'MCP servers could not be loaded.')}
        message={mcpServerErrorMessage(error)}
        retryLabel={msg('common.retry', 'Retry')}
        onRetry={onRetry}
      />
    );
  }
  if (servers.length === 0) {
    return <TableEmptyRow colSpan={5}>{msg('mcpServers.empty', 'No MCP servers match this view.')}</TableEmptyRow>;
  }
  return servers.map((server) => (
    <MCPServerRow
      key={server.id}
      server={server}
      selected={selectedServerId === server.id}
      formatter={formatter}
      onOpen={onOpen}
      onEdit={onEdit}
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
  onDelete,
}: Pick<MCPServersTableProps, 'formatter' | 'onOpen' | 'onEdit' | 'onDelete'> & {
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
      <DataTableCell className="truncate text-muted-foreground">
        {formatDate(server.updated_at, formatter)}
      </DataTableCell>
      <DataTableCell edge="end" className="px-2 text-right">
        <MCPServerActionsMenu server={server} onEdit={onEdit} onDelete={onDelete} />
      </DataTableCell>
    </DataTableRow>
  );
}

export function MCPServerActionsMenu({
  server,
  onEdit,
  onDelete,
}: {
  server: WorkspaceMCPServer;
  onEdit: (server: WorkspaceMCPServer) => void;
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
        <DropdownMenuItem variant="destructive" onClick={(event) => runMenuAction(event, () => onDelete(server))}>
          <Trash2 aria-hidden />
          {msg('common.delete', 'Delete')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
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

import { Pencil, Trash2 } from 'lucide-react';
import type { MouseEvent } from 'react';

import { cn } from '@/shared/lib/utils';
import { type WorkspaceMCPServer, mcpServerErrorMessage } from '@/shared/api/workspaceMCPServers';
import {
  CopyIdCell,
  DataTableCell,
  DataTableRow,
  MoreActionsButton,
  dataTableActionsColumnClassName,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
  dataTableIdColumnClassName,
  dataTableMetaColumnClassName,
  dataTablePrimaryColumnClassName,
  dataTableTimeColumnClassName,
} from '@/shared/ui/data-table-interactions';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/shared/ui/dropdown-menu';
import { TableEmptyRow, TableErrorRow, TableSkeletonRows } from '@/shared/ui/resource-table';
import { RelativeTimeText } from '@/shared/ui/resource-time-cell';
import { Table, TableBody, TableHead, TableHeader, TableRow } from '@/shared/ui/table';
import { useI18n } from '../../shared/i18n';

type MCPServersTableProps = {
  servers: WorkspaceMCPServer[];
  hasSearch: boolean;
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
  const showColumns = !props.isLoading && !props.error && props.servers.length > 0;
  return (
    <section aria-label={msg('mcpServers.listAria', 'MCP servers list')} className="min-w-0">
      <Table className={dataTableClassName}>
        {showColumns ? (
          <TableHeader>
            <TableRow className={dataTableHeaderRowClassName}>
              <TableHead className={cn(dataTableHeaderCellClassName, dataTableIdColumnClassName)}>
                {msg('common.id', 'ID')}
              </TableHead>
              <TableHead className={cn(dataTableHeaderCellClassName, dataTablePrimaryColumnClassName)}>
                {msg('mcpServers.name', 'Name')}
              </TableHead>
              <TableHead className={cn(dataTableHeaderCellClassName, dataTableMetaColumnClassName)}>
                {msg('mcpServers.endpoint', 'Endpoint')}
              </TableHead>
              <TableHead className={cn(dataTableHeaderCellClassName, dataTableTimeColumnClassName)}>
                {msg('mcpServers.updated', 'Updated')}
              </TableHead>
              <TableHead
                className={cn(dataTableHeaderCellClassName, dataTableActionsColumnClassName)}
                aria-label={msg('common.actions', 'Actions')}
              />
            </TableRow>
          </TableHeader>
        ) : null}
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
  hasSearch,
  selectedServerId,
  isLoading,
  error,
  onRetry,
  onOpen,
  onEdit,
  onDelete,
}: MCPServersTableProps) {
  const { msg } = useI18n();
  if (isLoading) {
    return <TableSkeletonRows colCount={5} label={msg('mcpServers.loading', 'Loading MCP servers...')} />;
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
    return (
      <TableEmptyRow colSpan={5}>
        {hasSearch
          ? msg('mcpServers.empty', 'No MCP servers match this view.')
          : msg('mcpServers.noServers', 'No MCP servers yet.')}
      </TableEmptyRow>
    );
  }
  return servers.map((server) => (
    <MCPServerRow
      key={server.id}
      server={server}
      selected={selectedServerId === server.id}
      onOpen={onOpen}
      onEdit={onEdit}
      onDelete={onDelete}
    />
  ));
}

function MCPServerRow({
  server,
  selected,
  onOpen,
  onEdit,
  onDelete,
}: Pick<MCPServersTableProps, 'onOpen' | 'onEdit' | 'onDelete'> & {
  server: WorkspaceMCPServer;
  selected: boolean;
}) {
  const { msg } = useI18n();
  return (
    <DataTableRow clickable selected={selected} onClick={() => onOpen(server.id)}>
      <DataTableCell edge="start" className={dataTableIdColumnClassName}>
        <CopyIdCell
          value={server.id}
          ariaLabel={msg('mcpServers.copyAria', 'Copy {serverId}', { serverId: server.id })}
          stopPropagation
        />
      </DataTableCell>
      <DataTableCell className={cn(dataTablePrimaryColumnClassName, 'max-w-[280px] truncate font-medium')}>
        <span title={server.name}>{server.name}</span>
      </DataTableCell>
      <DataTableCell className={cn(dataTableMetaColumnClassName, 'font-mono text-xs text-muted-foreground')}>
        <span title={server.url} className="block max-w-[380px] truncate">
          {server.url}
        </span>
      </DataTableCell>
      <DataTableCell className={dataTableTimeColumnClassName}>
        <RelativeTimeText value={server.updated_at} />
      </DataTableCell>
      <DataTableCell edge="end" className={dataTableActionsColumnClassName}>
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

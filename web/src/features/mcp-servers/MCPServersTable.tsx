import { Archive, ChevronLeft, ChevronRight, Pencil, RefreshCw, Trash2 } from 'lucide-react';
import { useI18n } from '../../shared/i18n';
import { type WorkspaceMCPServer, mcpServerErrorMessage } from '../../shared/api/workspaceMCPServers';
import { Alert, AlertDescription } from '../../shared/ui/alert';
import { Badge } from '../../shared/ui/badge';
import { Button } from '../../shared/ui/button';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';

type MCPServersTableProps = {
  servers: WorkspaceMCPServer[];
  isLoading: boolean;
  error: unknown;
  formatter: Intl.DateTimeFormat;
  onRetry: () => void;
  onEdit: (server: WorkspaceMCPServer) => void;
  onArchive: (server: WorkspaceMCPServer) => void;
  onDelete: (server: WorkspaceMCPServer) => void;
};

export function MCPServersTable(props: MCPServersTableProps) {
  const { msg } = useI18n();
  return (
    <Table aria-label={msg('mcpServers.listAria', 'MCP servers list')}>
      <TableHeader>
        <TableRow>
          <TableHead>{msg('mcpServers.name', 'Name')}</TableHead>
          <TableHead>{msg('mcpServers.endpoint', 'Endpoint')}</TableHead>
          <TableHead>{msg('mcpServers.status', 'Status')}</TableHead>
          <TableHead>{msg('mcpServers.updated', 'Updated')}</TableHead>
          <TableHead className="w-36 text-right">{msg('mcpServers.actions', 'Actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <MCPServersTableBody {...props} />
      </TableBody>
    </Table>
  );
}

export function MCPServerPagination({
  previousLabel,
  nextLabel,
  updatingLabel,
  canPrevious,
  canNext,
  isUpdating,
  onPrevious,
  onNext,
}: {
  previousLabel: string;
  nextLabel: string;
  updatingLabel: string;
  canPrevious: boolean;
  canNext: boolean;
  isUpdating: boolean;
  onPrevious: () => void;
  onNext: () => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        aria-label={previousLabel}
        disabled={!canPrevious}
        onClick={onPrevious}
      >
        <ChevronLeft aria-hidden />
      </Button>
      <Button type="button" variant="ghost" size="icon-sm" aria-label={nextLabel} disabled={!canNext} onClick={onNext}>
        <ChevronRight aria-hidden />
      </Button>
      {isUpdating ? <span className="text-xs text-muted-foreground">{updatingLabel}</span> : null}
    </div>
  );
}

function MCPServersTableBody({
  servers,
  isLoading,
  error,
  formatter,
  onRetry,
  onEdit,
  onArchive,
  onDelete,
}: MCPServersTableProps) {
  const { msg } = useI18n();
  if (isLoading) {
    return (
      <TableRow>
        <TableCell colSpan={5} className="h-28 text-muted-foreground">
          <RefreshCw className="mr-2 inline size-4 animate-spin" aria-hidden />
          {msg('mcpServers.loading', 'Loading MCP servers...')}
        </TableCell>
      </TableRow>
    );
  }
  if (error) {
    return (
      <TableRow>
        <TableCell colSpan={5} className="h-28">
          <Alert variant="destructive">
            <AlertDescription>
              {mcpServerErrorMessage(error)}{' '}
              <Button type="button" variant="outline" size="sm" onClick={onRetry}>
                {msg('common.retry', 'Retry')}
              </Button>
            </AlertDescription>
          </Alert>
        </TableCell>
      </TableRow>
    );
  }
  if (servers.length === 0) {
    return (
      <TableRow>
        <TableCell colSpan={5} className="h-28 text-center text-muted-foreground">
          {msg('mcpServers.empty', 'No MCP servers match this view.')}
        </TableCell>
      </TableRow>
    );
  }
  return servers.map((server) => (
    <MCPServerRow
      key={server.id}
      server={server}
      formatter={formatter}
      onEdit={onEdit}
      onArchive={onArchive}
      onDelete={onDelete}
    />
  ));
}

function MCPServerRow({
  server,
  formatter,
  onEdit,
  onArchive,
  onDelete,
}: Pick<MCPServersTableProps, 'formatter' | 'onEdit' | 'onArchive' | 'onDelete'> & {
  server: WorkspaceMCPServer;
}) {
  const { msg } = useI18n();
  return (
    <TableRow>
      <TableCell className="font-medium">{server.name}</TableCell>
      <TableCell>
        <span className="block max-w-xl truncate font-mono text-xs text-muted-foreground" title={server.url}>
          {server.url}
        </span>
      </TableCell>
      <TableCell>
        <Badge variant={server.status === 'active' ? 'secondary' : 'outline'}>
          {server.status === 'active' ? msg('mcpServers.active', 'Active') : msg('mcpServers.archived', 'Archived')}
        </Badge>
      </TableCell>
      <TableCell className="text-muted-foreground">{formatDate(server.updated_at, formatter)}</TableCell>
      <TableCell>
        <div className="flex justify-end gap-1">
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={msg('mcpServers.editAria', 'Edit {name}', { name: server.name })}
            onClick={() => onEdit(server)}
          >
            <Pencil aria-hidden />
          </Button>
          {server.status === 'active' ? (
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={msg('mcpServers.archiveAria', 'Archive {name}', { name: server.name })}
              onClick={() => onArchive(server)}
            >
              <Archive aria-hidden />
            </Button>
          ) : null}
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={msg('mcpServers.deleteAria', 'Delete {name}', { name: server.name })}
            onClick={() => onDelete(server)}
          >
            <Trash2 aria-hidden />
          </Button>
        </div>
      </TableCell>
    </TableRow>
  );
}

function formatDate(value: string, formatter: Intl.DateTimeFormat) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : formatter.format(date);
}

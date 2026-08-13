import { Plus, Search, Server } from 'lucide-react';
import { useMemo } from 'react';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Card, CardContent } from '../../shared/ui/card';
import { Input } from '../../shared/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { MCPServerDestructiveDialog, MCPServerDetailError, MCPServerEditor } from './MCPServerDialogs';
import { MCPServerPagination, MCPServersTable } from './MCPServersTable';
import { useMCPServerActions, useMCPServerData, useMCPServerWorkspace, type MCPServerScope } from './useMCPServersPage';

export function MCPServersPage({
  initialCreateOpen = false,
  initialServerId,
}: { initialCreateOpen?: boolean; initialServerId?: string } = {}) {
  const { msg, locale } = useI18n();
  const workspace = useMCPServerWorkspace();
  const data = useMCPServerData({ ...workspace, initialServerId });
  const actions = useMCPServerActions({
    ...workspace,
    initialCreateOpen,
    initialServerId,
    detailServer: data.detailQuery.data,
  });

  const servers = data.listQuery.data?.data ?? [];
  const formatter = useMemo(
    () => new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }),
    [locale],
  );

  return (
    <section className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-center gap-3">
            <div className="grid size-10 place-items-center rounded-lg border border-border bg-secondary">
              <Server className="size-5 text-muted-foreground" aria-hidden />
            </div>
            <div>
              <h1 className="text-2xl font-semibold tracking-tight text-foreground">MCP Servers</h1>
              <p className="mt-1 max-w-3xl text-sm leading-5 text-muted-foreground">
                {msg(
                  'mcpServers.description',
                  'Configure reusable custom MCP servers for the {workspaceName} workspace.',
                  { workspaceName: workspace.workspaceName },
                )}
              </p>
            </div>
          </div>
        </div>
        <Button type="button" onClick={() => actions.setEditor({ mode: 'create' })}>
          <Plus className="size-4" aria-hidden />
          {msg('mcpServers.create', 'Create MCP server')}
        </Button>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <div className="relative min-w-64 flex-1 sm:max-w-md">
          <Search
            className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden
          />
          <Input
            value={data.search}
            aria-label={msg('mcpServers.search', 'Search MCP servers')}
            placeholder={msg('mcpServers.searchPlaceholder', 'Search by name or endpoint')}
            className="pl-8"
            onChange={(event) => data.setSearch(event.currentTarget.value)}
          />
        </div>
        <Select value={data.scope} onValueChange={(value) => data.setScope(value as MCPServerScope)}>
          <SelectTrigger aria-label={msg('mcpServers.statusFilter', 'Status filter')} className="w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="active">{msg('mcpServers.filter.active', 'Active')}</SelectItem>
            <SelectItem value="all">{msg('mcpServers.filter.all', 'All statuses')}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <MCPServerDetailError
        enabled={Boolean(initialServerId)}
        error={data.detailQuery.error}
        onRetry={() => void data.detailQuery.refetch()}
      />

      <Card>
        <CardContent className="p-0">
          <MCPServersTable
            servers={servers}
            isLoading={!workspace.workspaceReady || data.listQuery.isLoading}
            error={data.listQuery.error}
            formatter={formatter}
            onRetry={() => void data.listQuery.refetch()}
            onEdit={(server) => actions.setEditor({ mode: 'edit', server })}
            onArchive={(server) => actions.openDestructive('archive', server)}
            onDelete={(server) => actions.openDestructive('delete', server)}
          />
        </CardContent>
      </Card>

      <MCPServerPagination
        previousLabel={msg('pagination.previousPage', 'Previous page')}
        nextLabel={msg('pagination.nextPage', 'Next page')}
        updatingLabel={msg('common.updating', 'Updating...')}
        canPrevious={data.pageIndex > 0 && !data.listQuery.isFetching}
        canNext={Boolean(data.listQuery.data?.next_page) && !data.listQuery.isFetching}
        isUpdating={data.listQuery.isFetching && !data.listQuery.isLoading}
        onPrevious={() => data.setPageIndex((current) => Math.max(0, current - 1))}
        onNext={data.nextPage}
      />

      <MCPServerEditor target={actions.editor} onClose={actions.closeEditor} onSubmit={actions.submitEditor} />
      <MCPServerDestructiveDialog
        target={actions.destructive}
        error={actions.actionError}
        isActing={actions.isActing}
        onClose={actions.closeDestructive}
        onConfirm={() => void actions.confirmDestructive()}
      />
    </section>
  );
}

export function CreateMCPServerPage() {
  return <MCPServersPage initialCreateOpen />;
}

export function MCPServerDetailPage() {
  return <MCPServersPage initialServerId={mcpServerIdFromPath()} />;
}

function mcpServerIdFromPath() {
  return window.location.pathname.match(/\/mcp-servers\/([^/]+)$/)?.[1];
}

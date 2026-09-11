import { Plus } from 'lucide-react';
import { useMemo } from 'react';

import { Button } from '../../shared/ui/button';
import { CursorPagination } from '../../shared/ui/resource-table';
import { useI18n } from '../../shared/i18n';
import { ResourceSearchField } from '../../shared/ui/resource-filters';
import { MCPServerDestructiveDialog, MCPServerEditor, MCPServerPanel } from './MCPServerDialogs';
import { MCPServersTable } from './MCPServersTable';
import { useMCPServerActions, useMCPServerData, useMCPServerWorkspace } from './useMCPServersPage';

export function MCPServersPage({
  initialCreateOpen = false,
  initialServerId,
}: { initialCreateOpen?: boolean; initialServerId?: string } = {}) {
  const { msg, locale } = useI18n();
  const workspace = useMCPServerWorkspace();
  const actions = useMCPServerActions({ ...workspace, initialCreateOpen, initialServerId });
  const data = useMCPServerData({ ...workspace, detailServerId: actions.detailServerId });
  const servers = data.listQuery.data?.data ?? [];
  const searchPlaceholder = msg('mcpServers.searchPlaceholder', 'Search by name or endpoint');
  const formatter = useMemo(
    () => new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }),
    [locale],
  );

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <header className="mb-5 flex items-start justify-between gap-6">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">MCP Servers</h1>
          <p className="mt-1 max-w-3xl text-sm leading-5 text-muted-foreground">
            {msg(
              'mcpServers.description',
              'Manage the reusable custom MCP configuration directory for the {workspaceName} workspace.',
              { workspaceName: workspace.workspaceName },
            )}
          </p>
        </div>
        <Button type="button" className="h-9 shrink-0" onClick={actions.openCreate}>
          <Plus className="size-4" aria-hidden />
          {msg('mcpServers.create', 'Create MCP server')}
        </Button>
      </header>

      <div data-testid="mcp-server-filters" className="mb-7 flex flex-wrap items-center gap-2">
        <ResourceSearchField
          id="mcp-server-search"
          value={data.search}
          placeholder={searchPlaceholder}
          clearLabel={msg('common.clearSearch', 'Clear {placeholder}', {
            placeholder: searchPlaceholder,
          })}
          onChange={data.setSearch}
        />
      </div>

      <MCPServersTable
        servers={servers}
        selectedServerId={actions.selectedServerId}
        isLoading={!workspace.workspaceReady || data.listQuery.isLoading}
        isFetching={data.listQuery.isFetching}
        error={data.listQuery.error}
        formatter={formatter}
        onRetry={() => void data.listQuery.refetch()}
        onOpen={actions.openDetail}
        onEdit={actions.openEdit}
        onDelete={actions.openDestructive}
      />

      <CursorPagination
        previousLabel={msg('pagination.previousPage', 'Previous page')}
        nextLabel={msg('pagination.nextPage', 'Next page')}
        updatingLabel={msg('common.updating', 'Updating...')}
        canPrevious={data.pageIndex > 0 && !data.listQuery.isFetching}
        canNext={Boolean(data.listQuery.data?.next_page) && !data.listQuery.isFetching}
        isUpdating={data.listQuery.isFetching && !data.listQuery.isLoading}
        onPrevious={() => data.setPageIndex((current) => Math.max(0, current - 1))}
        onNext={data.nextPage}
      />

      <MCPServerPanel
        target={actions.panel}
        detailServer={data.detailQuery.data}
        detailLoading={data.detailQuery.isLoading}
        detailError={data.detailQuery.error}
        formatter={formatter}
        onClose={actions.closePanel}
        onRetry={() => void data.detailQuery.refetch()}
        onEdit={actions.openEdit}
        onDelete={actions.openDestructive}
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

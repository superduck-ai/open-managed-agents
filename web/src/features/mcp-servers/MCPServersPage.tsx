import { Plus } from 'lucide-react';
import { useMemo } from 'react';

import { Button } from '../../shared/ui/button';
import { CursorPagination } from '../../shared/ui/resource-table';
import { useI18n } from '../../shared/i18n';
import { ResourcePageHeader } from '../../shared/ui/resource-page-header';
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
      <ResourcePageHeader
        title={msg('nav.mcpServers', 'MCP servers')}
        actions={
          <Button type="button" size="lg" onClick={actions.openCreate}>
            <Plus className="size-4" aria-hidden />
            {msg('mcpServers.create', 'Create MCP server')}
          </Button>
        }
      />

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
        hasSearch={Boolean(data.search.trim())}
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
        currentPage={data.pageIndex + 1}
        totalPages={null}
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

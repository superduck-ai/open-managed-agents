import { Plus, Search } from 'lucide-react';
import { useMemo } from 'react';

import { Button } from '../../shared/ui/button';
import { CursorPagination } from '../../shared/ui/resource-table';
import { Input } from '../../shared/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { useI18n } from '../../shared/i18n';
import { MCPServerDestructiveDialog, MCPServerEditor, MCPServerPanel } from './MCPServerDialogs';
import { MCPServersTable } from './MCPServersTable';
import { useMCPServerActions, useMCPServerData, useMCPServerWorkspace, type MCPServerScope } from './useMCPServersPage';

export function MCPServersPage({
  initialCreateOpen = false,
  initialServerId,
}: { initialCreateOpen?: boolean; initialServerId?: string } = {}) {
  const { msg, locale } = useI18n();
  const workspace = useMCPServerWorkspace();
  const actions = useMCPServerActions({ ...workspace, initialCreateOpen, initialServerId });
  const data = useMCPServerData({ ...workspace, detailServerId: actions.detailServerId });
  const servers = data.listQuery.data?.data ?? [];
  const scopeOptions = [
    { value: 'active' as const, label: msg('mcpServers.filter.active', 'Active') },
    { value: 'all' as const, label: msg('mcpServers.filter.all', 'All statuses') },
  ];
  const scopeLabel = scopeOptions.find((option) => option.value === data.scope)?.label ?? scopeOptions[0].label;
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
            {msg('mcpServers.description', 'Configure reusable custom MCP servers for the {workspaceName} workspace.', {
              workspaceName: workspace.workspaceName,
            })}
          </p>
        </div>
        <Button type="button" className="h-9 shrink-0" onClick={actions.openCreate}>
          <Plus className="size-4" aria-hidden />
          {msg('mcpServers.create', 'Create MCP server')}
        </Button>
      </header>

      <div data-testid="mcp-server-filters" className="mb-7 flex flex-wrap items-center gap-2">
        <div className="relative block h-9 w-[320px] max-w-full">
          <Search
            className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground/70"
            aria-hidden
          />
          <Input
            value={data.search}
            aria-label={msg('mcpServers.search', 'Search MCP servers')}
            placeholder={msg('mcpServers.searchPlaceholder', 'Search by name or endpoint')}
            className="h-9 border-border bg-secondary pl-9 pr-3 text-sm text-foreground placeholder:text-muted-foreground focus-visible:border-border focus-visible:ring-0"
            onChange={(event) => data.setSearch(event.currentTarget.value)}
          />
        </div>
        <Select<MCPServerScope>
          value={data.scope}
          items={scopeOptions}
          onValueChange={(value) => value && data.setScope(value)}
        >
          <SelectTrigger
            aria-label={msg('mcpServers.statusFilter', 'Status filter')}
            className="h-9 gap-2 border-border bg-secondary px-3 text-sm"
          >
            <SelectValue>
              <span className="text-muted-foreground">{msg('mcpServers.status', 'Status')}</span>
              <span className="font-medium text-foreground">{scopeLabel}</span>
            </SelectValue>
          </SelectTrigger>
          <SelectContent align="start" alignItemWithTrigger={false}>
            {scopeOptions.map((option) => (
              <SelectItem key={option.value} value={option.value} label={option.label}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
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
        onArchive={(server) => actions.openDestructive('archive', server)}
        onDelete={(server) => actions.openDestructive('delete', server)}
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
        onArchive={(server) => actions.openDestructive('archive', server)}
        onDelete={(server) => actions.openDestructive('delete', server)}
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

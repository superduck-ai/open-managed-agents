import { useQuery } from '@tanstack/react-query';
import { useLocation } from '@tanstack/react-router';
import { useDeferredValue, useEffect, useState } from 'react';

import { useFormatters, useI18n } from '@/shared/i18n';
import { MetricStrip } from '@/shared/ui/metric-strip';
import { ResourcePageHeader } from '@/shared/ui/resource-page-header';
import { ResourceListPagination, resourceListPageCount } from '@/shared/ui/resource-list-pagination';
import { useWorkspace } from '@/shared/workspaces/context';
import { workspaceIdFromPath } from '@/shared/workspaces/presentation';

import { listIdentities, type BusinessIdentity, type IdentityPage } from './api';
import { IdentityDetailSheet } from './IdentityDetailSheet';
import { IdentityFilters, type IdentitySort, type IdentityStatusFilter } from './IdentityFilters';
import { IdentityTable } from './IdentityTable';

export function IdentitiesPage() {
  const { orgUuid, activeWorkspaceId, selectWorkspace } = useWorkspace();
  const routeWorkspaceId = workspaceIdFromPath(useLocation().pathname);
  const workspaceId = routeWorkspaceId || activeWorkspaceId;
  useEffect(() => {
    if (routeWorkspaceId && routeWorkspaceId !== activeWorkspaceId) selectWorkspace(routeWorkspaceId);
  }, [routeWorkspaceId, activeWorkspaceId, selectWorkspace]);
  return (
    <IdentityDirectory key={JSON.stringify([orgUuid, workspaceId])} orgUuid={orgUuid ?? ''} workspaceId={workspaceId} />
  );
}

function IdentityDirectory({ orgUuid, workspaceId }: { orgUuid: string; workspaceId: string }) {
  const { msg } = useI18n();
  const [search, setSearch] = useState('');
  const [status, setStatus] = useState<IdentityStatusFilter>('all');
  const [page, setPage] = useState(1);
  const [agentId, setAgentId] = useState('');
  const [sort, setSort] = useState<IdentitySort>('recent');
  const [selectedIdentity, setSelectedIdentity] = useState<BusinessIdentity | null>(null);
  const deferredSearch = useDeferredValue(search);
  const currentPage = deferredSearch === search ? page : 1;
  const queryKey = ['identities', orgUuid, workspaceId];
  const ready = Boolean(orgUuid && workspaceId);
  const query = useQuery({
    queryKey: [...queryKey, deferredSearch.trim(), status, agentId, sort, currentPage],
    enabled: ready,
    queryFn: ({ signal }) => {
      const params = new URLSearchParams({
        q: deferredSearch.trim(),
        status,
        agent_id: agentId,
        sort,
        page: String(currentPage),
      });
      return listIdentities(orgUuid, workspaceId, params, signal);
    },
  });
  const title = msg('nav.identities', '身份');
  const agents = query.data?.agent_options ?? [];
  const pages = resourceListPageCount(query.data?.total_count, 20) ?? 1;

  return (
    <section className="min-w-0 text-foreground" aria-label={title}>
      <ResourcePageHeader title={title} />
      <IdentityOverview overview={query.data?.overview} />
      <IdentityFilters
        agents={agents}
        search={search}
        status={status}
        agentId={agentId}
        sort={sort}
        onSearch={(value) => {
          setSearch(value);
          setPage(1);
        }}
        onStatus={(value) => {
          setStatus(value);
          setPage(1);
        }}
        onAgent={(value) => {
          setAgentId(value);
          setPage(1);
        }}
        onSort={(value) => {
          setSort(value);
          setPage(1);
        }}
      />
      <IdentityTable
        rows={query.data?.data ?? []}
        loading={ready && query.isPending}
        filtered={Boolean(deferredSearch.trim() || agentId) || status !== 'all'}
        orgUuid={orgUuid}
        workspaceId={workspaceId}
        agentOptions={agents}
        error={query.error}
        onRetry={() => void query.refetch()}
        onOpen={setSelectedIdentity}
      />
      {Math.max(page, pages) > 1 && (
        <ResourceListPagination
          currentPage={page}
          totalPages={pages}
          canPrevious={page > 1}
          canNext={page < pages && !query.isFetching}
          onPrevious={() => setPage(page - 1)}
          onNext={() => setPage(page + 1)}
        />
      )}
      {selectedIdentity && (
        <IdentityDetailSheet identity={selectedIdentity} onClose={() => setSelectedIdentity(null)} />
      )}
    </section>
  );
}

function IdentityOverview({ overview }: { overview?: IdentityPage['overview'] }) {
  const { msg } = useI18n();
  const format = useFormatters();
  const metrics = [
    [msg('managedAgents.identities.total', '身份总计'), overview?.identities],
    [msg('managedAgents.identities.activeWeek', '近 7 天活跃'), overview?.active],
    [msg('managedAgents.identities.agentTotal', 'Agent 总计'), overview?.agents],
    [msg('managedAgents.identities.sessionTotal', '会话总计'), overview?.sessions],
  ] as const;
  return (
    <MetricStrip
      columns={4}
      className="mb-6"
      items={metrics.map(([label, value]) => ({
        id: label,
        label,
        value: value === undefined ? '—' : format.number(value),
      }))}
    />
  );
}

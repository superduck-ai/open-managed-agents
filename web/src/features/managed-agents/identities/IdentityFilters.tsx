import { useState } from 'react';

import { useI18n } from '@/shared/i18n';
import { ResourceFilterDropdown, ResourceSearchField } from '@/shared/ui/resource-list-controls';

import type { IdentityPage } from './api';

export type IdentitySort = 'recent' | 'sessions';
export type IdentityStatusFilter = 'all' | 'active' | 'enabled' | 'disabled';
type FilterMenu = 'agent' | 'status' | 'sort';

export function IdentityFilters({
  agents,
  search,
  status,
  agentId,
  sort,
  onSearch,
  onStatus,
  onAgent,
  onSort,
}: {
  agents: IdentityPage['agent_options'];
  search: string;
  status: IdentityStatusFilter;
  agentId: string;
  sort: IdentitySort;
  onSearch: (value: string) => void;
  onStatus: (value: IdentityStatusFilter) => void;
  onAgent: (value: string) => void;
  onSort: (value: IdentitySort) => void;
}) {
  const { msg } = useI18n();
  const [filterOpen, setFilterOpen] = useState<FilterMenu | null>(null);
  const options = [
    { value: 'all' as const, label: msg('managedAgents.identities.allStatuses', '全部状态') },
    { value: 'active' as const, label: msg('managedAgents.identities.active', '活跃') },
    { value: 'enabled' as const, label: msg('managedAgents.identities.enabled', '启用') },
    { value: 'disabled' as const, label: msg('managedAgents.identities.disabled', '停用') },
  ];
  const sortOptions = [
    { value: 'recent' as const, label: msg('managedAgents.identities.sortRecent', '按最近活跃排序') },
    { value: 'sessions' as const, label: msg('managedAgents.identities.sortSessions', '按会话数排序') },
  ];
  const agentOptions = [
    { value: '__all__', label: msg('managedAgents.identities.allAgents', '全部 Agent') },
    ...agents.map(({ id, name }) => ({ value: id, label: name })),
  ];
  return (
    <div className="mb-5 flex flex-wrap items-center gap-2">
      <ResourceFilterDropdown
        label="Agent"
        valueLabel={agentOptions.find((item) => item.value === (agentId || '__all__'))?.label ?? agentId}
        value={agentId || '__all__'}
        options={agentOptions}
        menu="agent"
        open={filterOpen === 'agent'}
        menuWidthClass="w-56"
        onOpenChange={setFilterOpen}
        onSelect={(value) => onAgent(value === '__all__' ? '' : value)}
      />
      <ResourceFilterDropdown
        label={msg('common.status', '状态')}
        valueLabel={options.find((item) => item.value === status)!.label}
        value={status}
        options={options}
        menu="status"
        open={filterOpen === 'status'}
        menuWidthClass="w-40"
        onOpenChange={setFilterOpen}
        onSelect={onStatus}
      />
      <ResourceFilterDropdown
        label={msg('managedAgents.identities.sort', '排序')}
        valueLabel={sortOptions.find((item) => item.value === sort)!.label}
        value={sort}
        options={sortOptions}
        menu="sort"
        open={filterOpen === 'sort'}
        menuWidthClass="w-48"
        onOpenChange={setFilterOpen}
        onSelect={onSort}
      />
      <ResourceSearchField
        id="identities-search"
        value={search}
        placeholder={msg('managedAgents.identities.search', '搜索身份 ID、外部 ID 或名称')}
        onChange={onSearch}
      />
    </div>
  );
}

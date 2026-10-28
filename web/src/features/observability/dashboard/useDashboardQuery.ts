import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { observabilityDashboard, type DashboardQuery, type DashboardTab, type QueryScope } from '../api';

export function useDashboardQuery(tab: DashboardTab | null, queryScope: QueryScope, filters: DashboardQuery) {
  return useQuery({
    queryKey: ['observability-dashboard', tab, queryScope, filters],
    queryFn: () => observabilityDashboard(queryScope, tab!, filters),
    enabled: !!tab && !!queryScope.orgUuid && !!queryScope.workspaceId && !!queryScope.startTime,
    placeholderData: keepPreviousData,
    retry: false,
  });
}

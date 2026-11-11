import { useContext, useEffect, useState } from 'react';
import { useInfiniteQuery } from '@tanstack/react-query';
import { AuthContext } from '../../shared/auth/context';
import { useI18n } from '../../shared/i18n';
import { useWorkspace } from '../../shared/workspaces/context';
import { localizedWorkspaceName } from '../../shared/workspaces/display-name';
import { PlatformAdminContext } from '../platform-admin/context';
import { navigationSearchRecords, workspaceSearchRecords, searchMatches } from './search-data';
import { searchResourcePage, resourceContextLabel, type ResourceCursor } from './resource-search';
import { platformNavigationSearchRecords, searchOrganizationPage } from './platform-search';

export function useSearchData(query: string) {
  const { msg } = useI18n();
  const auth = useContext(AuthContext);
  const platform = useContext(PlatformAdminContext);
  const { orgUuid, activeWorkspaceId, activeWorkspace, workspaces } = useWorkspace();
  const [debounced, setDebounced] = useState('');
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(query.trim()), 200);
    return () => window.clearTimeout(timer);
  }, [query]);
  const resources = useInfiniteQuery({
    queryKey: platform
      ? ['platform-admin', 'console-search', platform.bootstrap.account.uuid, debounced]
      : ['console-search', auth?.account?.uuid, orgUuid, activeWorkspaceId, debounced],
    queryFn: ({ pageParam, signal }) =>
      platform
        ? searchOrganizationPage(platform.client, debounced, pageParam, signal)
        : searchResourcePage(orgUuid || '', activeWorkspaceId, debounced, pageParam),
    initialPageParam: {} as ResourceCursor,
    getNextPageParam: (last) => last.next,
    enabled: Boolean(
      debounced &&
      (platform
        ? platform.bootstrap.platform_access.permissions.includes('platform:organizations:read')
        : auth?.account && orgUuid),
    ),
    retry: false,
    staleTime: 30000,
  });
  const scopeName = platform
    ? msg('platformAdmin.platformAdministration', 'Super admin console')
    : localizedWorkspaceName(activeWorkspace.name, msg);
  const pages = platform
    ? platformNavigationSearchRecords(platform.bootstrap.platform_access.permissions, msg)
    : navigationSearchRecords(auth?.account, orgUuid, activeWorkspaceId, msg, scopeName);
  const rawResources = debounced === query.trim() ? resources.data?.pages.flatMap((page) => page.records) || [] : [];
  const resourceRecords = [...new Map(rawResources.map((record) => [record.id, record])).values()]
    .filter((record) => searchMatches(record, query))
    .map((record) => (platform ? record : { ...record, context: resourceContextLabel(record.context, msg) }));
  return {
    resources,
    debounced,
    scopeName,
    pages,
    resourceRecords,
    workspaceRecords: platform ? [] : workspaceSearchRecords(workspaces, msg),
    resourceLabel: platform ? msg('search.organizations', 'Organizations') : msg('nav.resources', 'Resources'),
  };
}

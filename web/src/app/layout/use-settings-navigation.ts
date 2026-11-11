import { useEffect, useState } from 'react';
import type { AuthAccount } from '../../shared/auth/api';
import { navigationHref } from './navigation-paths';
import { isSettingsPath, settingsItem, visibleSettingsGroups } from './settings-navigation';
import {
  initialWorkspaceManagementOrigin,
  saveWorkspaceManagementOrigin,
  workspaceManagementReturnHref,
} from './use-workspace-management-history';

export function useResponsiveSidebar() {
  const [expanded, setExpanded] = useState(() => !window.matchMedia('(max-width: 1040px)').matches);
  useEffect(() => {
    const media = window.matchMedia('(max-width: 1040px)');
    const update = () => setExpanded(!media.matches);
    media.addEventListener('change', update);
    return () => media.removeEventListener('change', update);
  }, []);
  return expanded;
}

export function readNavigationPosition(key: string, fallback: string) {
  try {
    return window.sessionStorage.getItem(key) || fallback;
  } catch {
    return fallback;
  }
}

export function saveNavigationPosition(key: string, value: string) {
  try {
    window.sessionStorage.setItem(key, value);
  } catch {}
}

export function useSettingsHistory(
  account: AuthAccount | null | undefined,
  orgUuid: string | undefined,
  workspaceId: string,
  path: string,
) {
  const settings = isSettingsPath(path);
  const origin = initialWorkspaceManagementOrigin(account?.uuid, orgUuid, workspaceId, path, settings);
  const key = `oma.settings.page:${account?.uuid}:${orgUuid}`;
  const pages = visibleSettingsGroups(account, orgUuid).flatMap((group) => group.children);
  const remembered = readNavigationPosition(key, '/settings/llm');
  const item = settingsItem(path);
  useEffect(() => {
    if (item) saveNavigationPosition(key, item.href);
    if (!settings)
      saveWorkspaceManagementOrigin(account?.uuid, orgUuid, {
        href: path + window.location.search + window.location.hash,
        workspaceId,
      });
  }, [account?.uuid, orgUuid, workspaceId, path, settings, key, item?.href]);
  return {
    settingsHref: navigationHref(
      pages.some((page) => page.href === remembered) ? remembered : '/settings/llm',
      workspaceId,
    ),
    returnHref: workspaceManagementReturnHref(origin, workspaceId),
  };
}

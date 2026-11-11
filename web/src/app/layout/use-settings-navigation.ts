import { useEffect } from 'react';
import type { AuthAccount } from '../../shared/auth/api';
import { navigationHref } from '../../shared/navigation/navigation-paths';
import { isSettingsPath, settingsItem, visibleSettingsGroups } from '../../shared/navigation/settings-navigation';
import {
  initialWorkspaceManagementOrigin,
  saveWorkspaceManagementOrigin,
  workspaceManagementReturnHref,
} from './use-workspace-management-history';

function readStoredPage(key: string, fallback: string) {
  try {
    return window.sessionStorage.getItem(key) || fallback;
  } catch {
    return fallback;
  }
}

function saveStoredPage(key: string, value: string) {
  try {
    window.sessionStorage.setItem(key, value);
  } catch {
    return;
  }
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
  const remembered = readStoredPage(key, '/settings/llm');
  const itemHref = settingsItem(path)?.href;
  useEffect(() => {
    if (itemHref) saveStoredPage(key, itemHref);
    if (!settings)
      saveWorkspaceManagementOrigin(account?.uuid, orgUuid, {
        href: path + window.location.search + window.location.hash,
        workspaceId,
      });
  }, [account?.uuid, orgUuid, workspaceId, path, settings, key, itemHref]);
  return {
    settingsHref: navigationHref(
      pages.some((page) => page.href === remembered) ? remembered : '/settings/llm',
      workspaceId,
    ),
    returnHref: workspaceManagementReturnHref(origin, workspaceId),
  };
}

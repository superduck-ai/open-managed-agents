import { useEffect, useMemo } from 'react';
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
  const suffix = window.location.search + window.location.hash;
  const origin = useMemo(
    () => initialWorkspaceManagementOrigin(account?.uuid, orgUuid, workspaceId, path, settings),
    [account?.uuid, orgUuid, workspaceId, path, suffix, settings],
  );
  const key = `oma.settings.page:${account?.uuid}:${orgUuid}`;
  const itemHref = settingsItem(path)?.href;
  const settingsHref = useMemo(() => {
    const pages = visibleSettingsGroups(account, orgUuid).flatMap((group) => group.children);
    const defaultPage = '/settings/llm';
    const stored = readStoredPage(key, defaultPage);
    const remembered =
      itemHref || (stored === '/llm-models' || stored === '/settings/llm/organization' ? defaultPage : stored);
    return navigationHref(pages.some((page) => page.href === remembered) ? remembered : defaultPage, workspaceId);
  }, [account, orgUuid, key, itemHref, workspaceId]);
  useEffect(() => {
    if (itemHref) saveStoredPage(key, itemHref);
    if (!settings)
      saveWorkspaceManagementOrigin(account?.uuid, orgUuid, {
        href: path + suffix,
        workspaceId,
      });
  }, [account?.uuid, orgUuid, workspaceId, path, suffix, settings, key, itemHref]);
  return {
    settingsHref,
    returnHref: workspaceManagementReturnHref(origin, workspaceId),
  };
}

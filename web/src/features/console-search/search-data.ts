import type { AuthAccount } from '../../shared/auth/api';
import type { I18nContextValue } from '../../shared/i18n/context';
import type { Workspace } from '../../shared/workspaces/api';
import { consoleNavigation, placeholderIcons, type IconComponent } from '../../shared/navigation/navigation';
import { productIcons } from '../../shared/icons/product-icons';
import { localizedWorkspaceName } from '../../shared/workspaces/display-name';
import { visibleSettingsGroups } from '../../shared/navigation/settings-navigation';
import { navigationHref } from '../../shared/navigation/navigation-paths';
import { documentationHref } from '../../shared/documentation';
import documentation from './documentation.json';

export type SearchRecord = {
  id: string;
  name: string;
  context: string;
  href: string;
  kind: 'page' | 'workspace' | 'document' | 'resource';
  workspaceId?: string;
  icon?: IconComponent;
};

export function searchRecordKey(record: SearchRecord) {
  return `${record.kind}:${record.context}:${record.id}`;
}

export function searchMatches(record: SearchRecord, query: string) {
  const value = `${record.name} ${record.context} ${record.id}`.toLocaleLowerCase();
  return query
    .trim()
    .toLocaleLowerCase()
    .split(/\s+/)
    .every((word) => value.includes(word));
}

export function navigationSearchRecords(
  account: AuthAccount | null | undefined,
  orgUuid: string | undefined,
  workspaceId: string,
  msg: I18nContextValue['msg'],
): SearchRecord[] {
  const main = consoleNavigation.flatMap((item) =>
    item.type === 'link'
      ? [{ ...item, context: '' }]
      : item.children.map((child) => ({ ...child, context: msg(item.labelId, item.label) })),
  );
  const settings = visibleSettingsGroups(account, orgUuid).flatMap((group) =>
    group.children.map((item) => ({ ...item, context: msg(group.labelId, group.label) })),
  );
  return [...main, ...settings].map((item) => ({
    id: item.href,
    name: msg(item.labelId, item.label),
    context: item.context,
    icon: item.icon || placeholderIcons[item.href as keyof typeof placeholderIcons],
    href: navigationHref(item.href, workspaceId),
    kind: 'page',
  }));
}

export function workspaceSearchRecords(workspaces: Workspace[], msg: I18nContextValue['msg']): SearchRecord[] {
  return workspaces.map((workspace) => ({
    id: workspace.id,
    name: localizedWorkspaceName(workspace.name, msg),
    context: msg('nav.workspaces', 'Workspaces'),
    href: '',
    kind: 'workspace',
    icon: productIcons.workspace,
    workspaceId: workspace.id,
  }));
}

export function documentationSearchRecords(locale: string): SearchRecord[] {
  const language = locale === 'zh-CN' ? 'zh' : 'en';
  return documentation.map((entry) => ({
    id: `doc:${entry.id}`,
    name: entry[language].title,
    context: entry[language].description,
    href: documentationHref(entry.slug, locale),
    kind: 'document',
  }));
}

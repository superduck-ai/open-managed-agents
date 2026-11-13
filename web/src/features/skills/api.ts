import type { useI18n } from '@/shared/i18n';
import {
  consoleApi,
  consoleRequestHeaders,
  getConsoleRequestContext,
  getScopeSignal,
  reportApiAuthFailure,
} from '@/shared/api/client';
import type { ConsoleSkill } from '@/features/dashboard/model';
import type { SkillImportEntry } from './SkillImportDialog';
import { skillPackageBody } from './files';

export type CatalogSkill = ConsoleSkill & { description: string; icon_key: string };
export type SkillCatalog = {
  data: CatalogSkill[];
  total_count: number;
  counts: { all: number; builtin: number; custom: number };
};
export type SkillContent = { files: string[]; skill_md: string };
export type SkillPreview = SkillContent & {
  id: string;
  display_title: string;
  icon_key?: string;
  source: string;
  description: string;
  latest_version: string;
  updated_at: string;
};
export type SkillFilePreview = { path: string; content: string; previewable: boolean; reason: string };
export function workspaceSkillContentUrl(workspaceId: string, skillId: string, version: string) {
  return workspaceSkillUrl(
    workspaceId,
    `/${encodeURIComponent(skillId)}/versions/${encodeURIComponent(version)}/preview`,
  );
}
function workspaceSkillUrl(workspaceId: string, path: string) {
  const context = getConsoleRequestContext();
  return `/api/console/organizations/${encodeURIComponent(context.organizationUuid || '')}/workspaces/${encodeURIComponent(workspaceId)}/skills${path}`;
}
export async function downloadSkillPackage(url: string, workspaceId?: string) {
  const context = workspaceId ? { ...getConsoleRequestContext(), workspaceId } : {};
  const signal = getScopeSignal();
  const response = await fetch(`${url}?download=1`, {
    credentials: 'include',
    headers: consoleRequestHeaders(undefined, context),
    signal,
  });
  signal.throwIfAborted();
  if (!response.ok) {
    reportApiAuthFailure(response.status, context);
    const body = await response.json().catch(() => null);
    throw new Error(body?.error?.message || `Download failed (${response.status})`);
  }
  const blob = await response.blob();
  signal.throwIfAborted();
  const filename = response.headers.get('Content-Disposition')?.match(/filename="?([^";]+)"?/)?.[1] || 'skill.zip';
  const objectUrl = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  try {
    anchor.href = objectUrl;
    anchor.download = filename;
    document.body.appendChild(anchor);
    anchor.click();
  } finally {
    anchor.remove();
    URL.revokeObjectURL(objectUrl);
  }
}

export function retrieveSkillContent(workspaceId: string, skillId: string, version: string, signal: AbortSignal) {
  return skillConsoleRequest<SkillPreview>(
    workspaceId,
    `/${encodeURIComponent(skillId)}/versions/${encodeURIComponent(version)}/preview`,
    { signal },
  );
}
export function skillConsoleRequest<T>(workspaceId: string, path: string, options: RequestInit = {}) {
  const context = getConsoleRequestContext();
  return consoleApi<T>(workspaceSkillUrl(workspaceId, path), { ...options, context: { ...context, workspaceId } });
}
export function inspectWorkspaceSkill(workspaceId: string, entry: SkillImportEntry, target?: ConsoleSkill) {
  const body = skillPackageBody(entry.files);
  if (target) body.append('skill_id', target.id);
  return skillConsoleRequest<{ name: string; description: string }>(workspaceId, '/inspect', {
    method: 'POST',
    body,
  }).then((preview) => ({
    id: target?.id || preview.name,
    name: target?.display_title || preview.name,
    existing: !!target,
    iconKey: target?.icon_key,
  }));
}
export function importWorkspaceSkill(workspaceId: string, entry: SkillImportEntry, target?: ConsoleSkill) {
  const body = skillPackageBody(entry.files);
  body.append('display_name', entry.name.trim());
  body.append('icon_key', entry.iconKey);
  return skillConsoleRequest<{ id: string; skill_id?: string }>(
    workspaceId,
    target ? `/${encodeURIComponent(target.id)}/versions` : '',
    { method: 'POST', body },
  ).then((result) => ({ id: result.skill_id || result.id }));
}
export function updateWorkspaceSkillIcon(workspaceId: string, id: string, iconKey: string) {
  return skillConsoleRequest(workspaceId, `/${encodeURIComponent(id)}/icon`, {
    method: 'PATCH',
    body: JSON.stringify({ icon_key: iconKey }),
  });
}

export function skillErrorMessage(error: unknown, msg: ReturnType<typeof useI18n>['msg']) {
  const message =
    error instanceof Error ? error.message : String((error as { message?: string } | null)?.message ?? '');
  if (message.startsWith('Skill cannot reuse an existing display_title:')) {
    return msg(
      'skills.import.duplicateName',
      'A skill with this name already exists. Change the name or use Update skill.',
    );
  }
  const inUse = /^Skill is in use by active agent\(s\): (.+)\. Remove it from those agents before deleting\.$/s.exec(
    message,
  );
  if (inUse) {
    return msg(
      'skills.delete.inUseByAgents',
      'Skill is in use by active agent(s): {agents}. Remove it from those agents before deleting.',
      { agents: inUse[1] },
    );
  }
  return message;
}

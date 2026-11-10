import { consoleApi, getConsoleRequestContext } from '@/shared/api/client';
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
export function retrieveSkillContent(workspaceId: string, skillId: string, version: string, signal: AbortSignal) {
  return skillConsoleRequest<SkillContent>(
    workspaceId,
    `/${encodeURIComponent(skillId)}/versions/${encodeURIComponent(version)}/preview`,
    { signal },
  );
}
export function skillConsoleRequest<T>(workspaceId: string, path: string, options: RequestInit = {}) {
  const context = getConsoleRequestContext();
  return consoleApi<T>(
    `/api/console/organizations/${encodeURIComponent(context.organizationUuid || '')}/workspaces/${encodeURIComponent(workspaceId)}/skills${path}`,
    { ...options, context: { ...context, workspaceId } },
  );
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

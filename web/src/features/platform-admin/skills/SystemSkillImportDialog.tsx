import { toast } from 'sonner';
import { useI18n } from '@/shared/i18n';
import { SkillImportDialog, type SkillImportEntry } from '@/features/skills/SkillImportDialog';
import { skillPackageBody } from '@/features/skills/files';
import { usePlatformAdmin } from '../context';
import type { SystemSkill, SystemPackage } from './types';

export function SystemSkillImportDialog({
  target,
  onClose,
  onImported,
}: {
  target?: SystemSkill;
  onClose: () => void;
  onImported: (id: string) => Promise<void>;
}) {
  const { client } = usePlatformAdmin();
  const { msg } = useI18n();
  const inspect = async (entry: SkillImportEntry) => {
    const body = skillPackageBody(entry.files);
    if (target) body.append('skill_id', target.id);
    const preview = await client.request<SystemPackage>('/skills/inspect', { method: 'POST', body });
    const existing = preview.existing ?? target;
    return {
      id: existing?.id || preview.name,
      name: existing?.display_title || preview.name,
      existing: !!existing,
      revision: existing?.revision || 0,
      iconKey: existing?.icon_key,
    };
  };
  const upload = async (entry: SkillImportEntry) => {
    const metadata = entry.metadata!;
    const body = skillPackageBody(entry.files);
    body.append('display_title', entry.name.trim());
    body.append('icon_key', entry.iconKey);
    body.append('revision', String(metadata.revision ?? 0));
    if (metadata.existing) body.append('skill_id', metadata.id);
    const result = await client.request<{ skill: SystemSkill; duplicate: boolean }>('/skills', {
      method: 'POST',
      body,
    });
    if (result.duplicate)
      toast.info(msg('systemSkills.duplicate', 'This version already exists. Its content was not changed.'));
    return { id: result.skill.id, revision: result.skill.revision };
  };
  return (
    <SkillImportDialog
      title={target ? msg('skills.update', 'Update skill') : undefined}
      submitLabel={target ? msg('skills.update.submit', 'Update') : undefined}
      initialIconKey={target?.icon_key}
      multiple={!target}
      onClose={onClose}
      onInspect={inspect}
      onImport={upload}
      onCompleted={onImported}
    />
  );
}

import { useState } from 'react';
import { DeleteSkillDialog } from '@/features/skills/SkillDetail';
import { useI18n } from '@/shared/i18n';
import { usePlatformAdmin } from '../context';
import { systemSkillErrorMessage } from './feedback';
import type { SystemSkill } from './types';

export function SystemSkillDeleteDialog({
  skill,
  onClose,
  onDeleted,
}: {
  skill: SystemSkill;
  onClose: () => void;
  onDeleted: () => Promise<void>;
}) {
  const { client } = usePlatformAdmin();
  const { msg } = useI18n();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const remove = async () => {
    if (busy || skill.bundled) return;
    setBusy(true);
    setError(null);
    try {
      await client.request(`/skills/${encodeURIComponent(skill.id)}`, {
        method: 'DELETE',
        body: { revision: skill.revision },
      });
      await onDeleted();
      onClose();
    } catch (cause) {
      setError(systemSkillErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  };
  return (
    <DeleteSkillDialog
      skill={skill}
      isDeleting={busy}
      error={error}
      description={msg(
        'systemSkills.deleteImpact',
        'Remove this skill from the system catalog. Existing session snapshots are retained.',
      )}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      onConfirm={() => void remove()}
    />
  );
}

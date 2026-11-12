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
  onConflict,
}: {
  skill: SystemSkill;
  onClose: () => void;
  onDeleted: () => Promise<void>;
  onConflict?: () => Promise<void>;
}) {
  const { client } = usePlatformAdmin();
  const { msg } = useI18n();
  const [busy, setBusy] = useState(false);
  const [conflict, setConflict] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const remove = async () => {
    if (busy || conflict) return;
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
      if ((cause as { status?: number } | null)?.status === 409) {
        setConflict(true);
        void onConflict?.();
      }
    } finally {
      setBusy(false);
    }
  };
  return (
    <DeleteSkillDialog
      skill={skill}
      isDeleting={busy}
      confirmDisabled={conflict}
      error={error}
      title={skill.bundled ? msg('systemSkills.restoreBundledTitle', 'Restore bundled version?') : undefined}
      confirmLabel={skill.bundled ? msg('systemSkills.restoreBundled', 'Restore bundled version') : undefined}
      description={
        skill.bundled
          ? msg(
              'systemSkills.restoreBundledImpact',
              'Use the version included with the project. Listing status and existing sessions are retained.',
            )
          : msg(
              'systemSkills.deleteImpact',
              'Remove this skill from the system catalog. Existing session snapshots are retained.',
            )
      }

      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      onConfirm={() => void remove()}
    />
  );
}

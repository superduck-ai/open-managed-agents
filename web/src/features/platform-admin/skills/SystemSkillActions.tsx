import { Eye, EyeOff } from 'lucide-react';
import { SkillActions } from '@/features/skills/SkillActions';
import { downloadSkillPackage } from '@/features/skills/api';
import { useI18n } from '@/shared/i18n';
import { DropdownMenuItem, DropdownMenuSeparator } from '@/shared/ui/dropdown-menu';
import { usePlatformAdmin } from '../context';
import type { SystemSkill } from './types';

export function SystemSkillActions({
  skill,
  onUpdate,
  onDelete,
  onToggle,
  onChanged,
}: {
  skill: SystemSkill;
  onUpdate: () => void;
  onDelete: () => void;
  onToggle: () => void;
  onChanged: () => Promise<void>;
}) {
  const { client } = usePlatformAdmin();
  const { msg } = useI18n();
  const onIcon = async (key: string) => {
    try {
      await client.request(`/skills/${encodeURIComponent(skill.id)}/icon`, {
        method: 'PATCH',
        body: { icon_key: key, revision: skill.revision },
      });
    } finally {
      await onChanged();
    }
  };
  return (
    <SkillActions
      name={skill.display_title}
      iconKey={skill.icon_key}
      onUpdate={skill.bundled ? undefined : onUpdate}
      onIcon={skill.bundled ? undefined : onIcon}
      onDelete={skill.bundled ? undefined : onDelete}
      onDownload={
        skill.default_version
          ? () =>
              downloadSkillPackage(
                `/api/platform-admin/skills/${encodeURIComponent(skill.id)}/versions/${encodeURIComponent(skill.default_version!)}/content`,
              )
          : undefined
      }
    >
      {skill.default_version && (
        <>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={onToggle}>
            {skill.listed ? <EyeOff size={15} aria-hidden /> : <Eye size={15} aria-hidden />}
            {skill.listed ? msg('systemSkills.unlist', 'Unlist skill') : msg('systemSkills.list', 'List skill')}
          </DropdownMenuItem>
        </>
      )}
    </SkillActions>
  );
}

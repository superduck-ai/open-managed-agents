import { Eye, EyeOff, RotateCcw } from 'lucide-react';
import { SkillActions } from '@/features/skills/SkillActions';
import { downloadSkillPackage } from '@/features/skills/api';
import { useI18n } from '@/shared/i18n';
import { DropdownMenuItem, DropdownMenuSeparator } from '@/shared/ui/dropdown-menu';
import { usePlatformAdmin } from '../context';
import { updateSystemSkillIcon } from './feedback';
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
  return (
    <SkillActions
      name={skill.display_title}
      iconKey={skill.icon_key}
      onUpdate={onUpdate}
      onIcon={(key) => updateSystemSkillIcon(client, skill, key, onChanged)}
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
      {skill.bundled_version && skill.default_version !== skill.bundled_version && (
        <DropdownMenuItem onClick={onDelete}>
          <RotateCcw size={15} aria-hidden />
          {msg('systemSkills.restoreBundled', 'Restore bundled version')}
        </DropdownMenuItem>
      )}
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

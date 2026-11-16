import { X } from 'lucide-react';
import { SkillDownloadButton } from '@/features/skills/SkillDownloadButton';
import { SkillIconPicker } from '@/features/skills/icons';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { SheetClose, SheetDescription, SheetHeader, SheetTitle } from '@/shared/ui/sheet';
import { usePlatformAdmin } from '../context';
import { SystemSkillStatus, updateSystemSkillIcon } from './feedback';
import type { SystemSkill } from './types';

export function SystemSkillDrawerHeader({
  skill,
  version,
  pending,
  onChanged,
}: {
  skill?: SystemSkill;
  version: string | null;
  pending: boolean;
  onChanged: () => Promise<void>;
}) {
  const { client } = usePlatformAdmin();
  const { msg } = useI18n();
  return (
    <SheetHeader className="shrink-0 border-b-[0.5px] border-border px-4 py-4 pr-24">
      <div className="absolute right-4 top-4 flex items-center gap-1">
        {skill && version && (
          <SkillDownloadButton
            url={`/api/platform-admin/skills/${encodeURIComponent(skill.id)}/versions/${encodeURIComponent(version)}/content`}
          />
        )}
        <SheetClose
          render={
            <Button variant="ghost" size="icon-sm" disabled={pending} aria-label={msg('common.close', 'Close')} />
          }
        >
          <X className="size-4" aria-hidden />
        </SheetClose>
      </div>
      <div className="flex min-w-0 items-center gap-3">
        {skill && (
          <SkillIconPicker
            value={skill.icon_key || 'blocks'}
            name={skill.display_title}
            onChange={(key) => updateSystemSkillIcon(client, skill, key, onChanged)}
          />
        )}
        <SheetTitle>{skill?.display_title || msg('skills.detail.title', 'Skill details')}</SheetTitle>
      </div>
      <SheetDescription className="text-xs">
        {msg('systemSkills.scope', 'System-wide, available to all workspaces')}
      </SheetDescription>
      {skill && (
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <SystemSkillStatus skill={skill} />
          <code className="break-all text-xs">{skill.id}</code>
        </div>
      )}
    </SheetHeader>
  );
}

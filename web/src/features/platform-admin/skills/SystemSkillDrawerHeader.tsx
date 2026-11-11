import { X } from 'lucide-react';
import { SkillDownloadButton } from '@/features/skills/SkillDownloadButton';
import { SkillIcon, SkillIconPicker } from '@/features/skills/icons';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Badge } from '@/shared/ui/badge';
import { SheetClose, SheetDescription, SheetHeader, SheetTitle } from '@/shared/ui/sheet';
import { usePlatformAdmin } from '../context';
import { SystemSkillStatus } from './feedback';
import type { SystemSkill } from './types';

export function SystemSkillDrawerHeader({
  skill,
  pending,
  onChanged,
}: {
  skill?: SystemSkill;
  pending: boolean;
  onChanged: () => Promise<void>;
}) {
  const { client } = usePlatformAdmin();
  const { msg } = useI18n();
  return (
    <SheetHeader className="shrink-0 border-b-[0.5px] border-border px-4 py-4 pr-24">
      <div className="absolute right-4 top-4 flex items-center gap-1">
        <SystemSkillDownload skill={skill} />
        <SheetClose
          render={
            <Button variant="ghost" size="icon-sm" disabled={pending} aria-label={msg('common.close', 'Close')} />
          }
        >
          <X className="size-4" aria-hidden />
        </SheetClose>
      </div>
      <div className="flex min-w-0 items-center gap-3">
        {skill &&
          (skill.bundled ? (
            <SkillIcon iconKey={skill.icon_key} />
          ) : (
            <SkillIconPicker
              value={skill.icon_key || 'blocks'}
              name={skill.display_title}
              onChange={async (key) => {
                try {
                  await client.request(`/skills/${encodeURIComponent(skill.id)}/icon`, {
                    method: 'PATCH',
                    body: { icon_key: key, revision: skill.revision },
                  });
                } finally {
                  await onChanged();
                }
              }}
            />
          ))}
        <SheetTitle>{skill?.display_title || msg('skills.detail.title', 'Skill details')}</SheetTitle>
      </div>
      <SheetDescription className="text-xs">
        {msg('systemSkills.scope', 'System-wide, available to all workspaces')}
      </SheetDescription>
      {skill && (
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <Badge variant="secondary">
            {skill.bundled ? msg('systemSkills.bundled', 'Project bundled') : msg('systemSkills.uploaded', 'Uploaded')}
          </Badge>
          <SystemSkillStatus skill={skill} />
          <code className="break-all text-xs">{skill.id}</code>
        </div>
      )}
    </SheetHeader>
  );
}

function SystemSkillDownload({ skill }: { skill?: SystemSkill }) {
  if (!skill?.default_version) return null;
  return (
    <SkillDownloadButton
      url={`/api/platform-admin/skills/${encodeURIComponent(skill.id)}/versions/${encodeURIComponent(skill.default_version)}/content`}
    />
  );
}

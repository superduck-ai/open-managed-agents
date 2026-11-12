import type { PlatformAdminClient } from '@/shared/api/platform-admin';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Badge } from '@/shared/ui/badge';
import { ResourceListPagination } from '@/shared/ui/resource-list-pagination';
import type { SystemSkill } from './types';
import { systemSkillPageSize } from './types';

export async function updateSystemSkillIcon(
  client: PlatformAdminClient,
  skill: SystemSkill,
  iconKey: string,
  onChanged: () => Promise<void>,
) {
  try {
    await client.request(`/skills/${encodeURIComponent(skill.id)}/icon`, {
      method: 'PATCH',
      body: { icon_key: iconKey, revision: skill.revision },
    });
  } finally {
    await onChanged();
  }
}

export function systemSkillErrorMessage(error: unknown) {
  return error instanceof Error ? error.message : String((error as { message?: string } | null)?.message ?? '');
}

export function SystemSkillStatus({ skill }: { skill: SystemSkill }) {
  const { msg } = useI18n();
  const label = skill.listed ? msg('systemSkills.listed', 'Listed') : msg('systemSkills.unlisted', 'Unlisted');
  return (
    <Badge variant="secondary" className="rounded-md text-muted-foreground">
      {label}
    </Badge>
  );
}

export function SystemSkillError({ error, retry }: { error: unknown; retry?: () => void }) {
  const { msg } = useI18n();
  return (
    <div role="alert" className="flex flex-wrap items-center gap-3 py-4 text-sm text-destructive">
      <span>{systemSkillErrorMessage(error) || msg('systemSkills.loadError', 'Could not load system skills.')}</span>
      {retry && (
        <Button variant="outline" onClick={retry}>
          {msg('common.retry', 'Retry')}
        </Button>
      )}
    </div>
  );
}

export function SystemSkillPager({
  offset,
  more,
  busy,
  onOffset,
}: {
  offset: number;
  more: boolean;
  busy: boolean;
  onOffset: (offset: number) => void;
}) {
  return (
    <ResourceListPagination
      currentPage={offset / systemSkillPageSize + 1}
      totalPages={null}
      canPrevious={offset > 0 && !busy}
      canNext={more && !busy}
      onPrevious={() => onOffset(Math.max(0, offset - systemSkillPageSize))}
      onNext={() => onOffset(offset + systemSkillPageSize)}
    />
  );
}

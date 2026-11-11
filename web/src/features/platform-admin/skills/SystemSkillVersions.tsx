import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useI18n } from '@/shared/i18n';
import { Badge } from '@/shared/ui/badge';
import { Button } from '@/shared/ui/button';
import { usePlatformAdmin } from '../context';
import { SystemSkillError, SystemSkillPager } from './feedback';
import {
  systemSkillsKey,
  systemSkillPageSize,
  type SystemAction,
  type SystemPage,
  type SystemSkill,
  type SystemVersion,
} from './types';

export function SystemSkillVersions({
  skill,
  onAction,
  onContent,
}: {
  skill: SystemSkill;
  onAction: (action: SystemAction, version: string) => void;
  onContent: (version: string) => void;
}) {
  const { msg, locale } = useI18n();
  const { client } = usePlatformAdmin();
  const [offset, setOffset] = useState(0);
  const query = useQuery({
    queryKey: [...systemSkillsKey, skill.id, 'versions', offset],
    queryFn: ({ signal }) =>
      client.request<SystemPage<SystemVersion>>(
        `/skills/${encodeURIComponent(skill.id)}/versions?limit=${systemSkillPageSize}&offset=${offset}`,
        { signal },
      ),
  });
  if (query.isPending) return <p role="status">{msg('common.loading', 'Loading...')}</p>;
  if (query.isError) return <SystemSkillError error={query.error} retry={() => void query.refetch()} />;
  return (
    <div>
      <p className="mb-3 text-xs leading-5 text-muted-foreground">
        {msg('systemSkills.immutable', 'Uploaded version contents are immutable. Use a new version for changes.')}
      </p>
      {query.data.data.length === 0 && (
        <p className="text-sm text-muted-foreground">
          {msg('systemSkills.noAvailableVersions', 'No available versions')}
        </p>
      )}
      {query.data.data.map((version) => (
        <div key={version.id} className="border-b-[0.5px] border-border py-4">
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant="link"
              className="h-auto p-0 font-mono text-foreground"
              onClick={() => onContent(version.version)}
            >
              {version.version}
            </Button>
            {skill.default_version === version.version && (
              <Badge variant="outline">{msg('systemSkills.default', 'Default')}</Badge>
            )}
          </div>
          <p className="mt-2 text-sm whitespace-pre-wrap break-words">{version.description}</p>
          <p className="mt-2 text-xs text-muted-foreground">{new Date(version.created_at).toLocaleString(locale)}</p>
          <div className="mt-3 flex gap-2">
            {!skill.bundled && skill.default_version !== version.version && (
              <Button size="sm" variant="outline" onClick={() => onAction('set_default', version.version)}>
                {msg('systemSkills.setDefault', 'Set as default')}
              </Button>
            )}
            <Button size="sm" variant="ghost" onClick={() => onContent(version.version)}>
              {msg('systemSkills.viewContent', 'View contents')}
            </Button>
          </div>
        </div>
      ))}
      <SystemSkillPager offset={offset} more={query.data.has_more} busy={query.isFetching} onOffset={setOffset} />
    </div>
  );
}

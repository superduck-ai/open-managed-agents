import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useI18n } from '@/shared/i18n';
import { usePlatformAdmin } from '../context';
import { SystemSkillError, SystemSkillPager } from './feedback';
import { systemSkillPageSize, systemSkillsKey, type SystemEvent, type SystemPage } from './types';

export function SystemSkillEvents({ skillID }: { skillID: string }) {
  const { msg, locale } = useI18n();
  const { client } = usePlatformAdmin();
  const [offset, setOffset] = useState(0);
  const query = useQuery({
    queryKey: [...systemSkillsKey, skillID, 'events', offset],
    queryFn: ({ signal }) =>
      client.request<SystemPage<SystemEvent>>(
        `/skills/${encodeURIComponent(skillID)}/events?limit=${systemSkillPageSize}&offset=${offset}`,
        { signal },
      ),
  });
  if (query.isPending) return <p role="status">{msg('common.loading', 'Loading...')}</p>;
  if (query.isError) return <SystemSkillError error={query.error} retry={() => void query.refetch()} />;
  const actions: Record<string, string> = {
    restore_bundled: msg('systemSkills.restoreBundled', 'Restore bundled version'),
    import: msg('systemSkills.import', 'Import skills'),
    set_icon: msg('skills.icons.change', 'Change icon'),
    import_draft: msg('systemSkills.import', 'Import skills'),
    publish: msg('systemSkills.publish', 'Publish version'),
    set_default: msg('systemSkills.setDefault', 'Set as default'),
    list: msg('systemSkills.relist', 'Relist skill'),
    unlist: msg('systemSkills.unlist', 'Unlist skill'),
  };
  return (
    <div>
      {query.data.data.length === 0 && (
        <p className="text-sm text-muted-foreground">{msg('systemSkills.noEvents', 'No recorded activity yet.')}</p>
      )}
      {query.data.data.map((event) => (
        <div key={event.id} className="space-y-1 border-b-[0.5px] border-border py-3 text-sm">
          <p>
            {actions[event.action] || event.action} {event.version}
          </p>
          <p className="break-all text-xs text-muted-foreground">
            {msg('systemSkills.operator', 'Operator')}: {event.actor_uuid}
          </p>
          <p className="text-xs text-muted-foreground">{new Date(event.created_at).toLocaleString(locale)}</p>
        </div>
      ))}
      <SystemSkillPager offset={offset} more={query.data.has_more} busy={query.isFetching} onOffset={setOffset} />
    </div>
  );
}

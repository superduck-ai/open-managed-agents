import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { FileArchive, Plus, Search, Eye, EyeOff } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Input } from '@/shared/ui/input';
import { Tabs, TabsList, TabsTrigger } from '@/shared/ui/tabs';
import { DropdownMenuItem, DropdownMenuSeparator } from '@/shared/ui/dropdown-menu';
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from '@/shared/ui/alert-dialog';
import { ResourcePageHeader } from '@/shared/ui/resource-page-header';
import { ResourceListState } from '@/shared/ui/resource-list-state';
import { SkillCard } from '@/features/skills/SkillCard';
import { SkillActions } from '@/features/skills/SkillActions';
import { downloadSkillPackage } from '@/features/skills/api';
import { usePlatformAdmin } from '../context';
import { SystemSkillError, SystemSkillPager, SystemSkillStatus, systemSkillErrorMessage } from './feedback';
import { SystemSkillDrawer } from './SystemSkillDrawer';
import { SystemSkillImportDialog } from './SystemSkillImportDialog';
import { systemSkillsKey, systemSkillPageSize, type SystemSkill, type SystemPage } from './types';

export function SystemSkillsPage() {
  const { msg, locale } = useI18n();
  const { client } = usePlatformAdmin();
  const queries = useQueryClient();
  const navigate = useNavigate();
  const selected = useSearch({ strict: false }).skill;
  const [upload, setUpload] = useState<{ target?: SystemSkill } | null>(null);
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState('');
  const [status, setStatus] = useState('all');
  const [offset, setOffset] = useState(0);
  const [toggleTarget, setToggleTarget] = useState<SystemSkill | null>(null);
  const [toggleBusy, setToggleBusy] = useState(false);
  const [toggleError, setToggleError] = useState('');
  const statuses = {
    all: msg('systemSkills.allStatuses', 'All statuses'),
    listed: msg('systemSkills.listed', 'Listed'),
    unlisted: msg('systemSkills.unlisted', 'Unlisted'),
  };
  const query = useQuery({
    queryKey: [...systemSkillsKey, 'list', filter, status, offset],
    queryFn: ({ signal }) =>
      client.request<SystemPage<SystemSkill>>(
        `/skills?${new URLSearchParams({ query: filter, status: status === 'all' ? '' : status, offset: String(offset), limit: String(systemSkillPageSize) })}`,
        { signal },
      ),
  });
  const openSkill = (id?: string) => void navigate({ to: '/platform-admin/skills', search: { skill: id } });
  const changed = async () => {
    await Promise.all([
      queries.invalidateQueries({ queryKey: systemSkillsKey }),
      queries.invalidateQueries({ queryKey: ['skills'] }),
      queries.invalidateQueries({ queryKey: ['skill'] }),
    ]);
  };
  const toggleListing = async () => {
    if (!toggleTarget || toggleBusy) return;
    setToggleBusy(true);
    setToggleError('');
    try {
      await client.request(`/skills/${encodeURIComponent(toggleTarget.id)}/actions`, {
        method: 'POST',
        body: { action: toggleTarget.listed ? 'unlist' : 'list', version: '', revision: toggleTarget.revision },
      });
      setToggleTarget(null);
      await changed();
    } catch (cause) {
      setToggleError(systemSkillErrorMessage(cause));
      void changed();
    } finally {
      setToggleBusy(false);
    }
  };
  return (
    <section>
      <ResourcePageHeader
        contentGap="content"
        title={msg('systemSkills.title', 'System skills')}
        actions={
          <Button size="lg" onClick={() => setUpload({})}>
            <Plus size={16} aria-hidden />
            {msg('skills.import.title', 'Import skills')}
          </Button>
        }
      />
      <Tabs
        value={status}
        onValueChange={(value) => {
          setStatus(value);
          setOffset(0);
        }}
      >
        <TabsList variant="line" className="mb-5">
          {Object.entries(statuses).map(([value, label]) => (
            <TabsTrigger key={value} value={value}>
              {label}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <form
        className="mb-6 flex flex-wrap items-center gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          setOffset(0);
          setFilter(search.trim());
        }}
      >
        <Input
          className="w-full sm:w-80"
          aria-label={msg('systemSkills.search', 'Search name or ID')}
          placeholder={msg('systemSkills.search', 'Search name or ID')}
          maxLength={256}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
        <Button type="submit" variant="outline">
          <Search size={16} aria-hidden />
          {msg('platformAdmin.search', 'Search')}
        </Button>
      </form>
      {query.isPending ? (
        <p role="status" className="py-8 text-sm text-muted-foreground">
          {msg('common.loading', 'Loading...')}
        </p>
      ) : query.isError ? (
        <SystemSkillError error={query.error} retry={() => void query.refetch()} />
      ) : query.data.data.length === 0 ? (
        <ResourceListState icon={FileArchive} title={msg('systemSkills.empty', 'No matching system skills')} />
      ) : (
        <div className="skill-grid">
          {query.data.data.map((record) => (
            <SkillCard
              key={record.id}
              name={record.display_title}
              iconKey={record.icon_key}
              onOpen={() => openSkill(record.id)}
              metadata={
                <code className="mt-1 block truncate text-xs text-muted-foreground" title={record.id}>
                  {record.id}
                </code>
              }
              footer={
                <>
                  <SystemSkillStatus skill={record} />
                  <time dateTime={record.updated_at}>{new Date(record.updated_at).toLocaleDateString(locale)}</time>
                </>
              }
              body={
                <div className="mt-5 flex items-center gap-3 text-xs">
                  <span className="text-muted-foreground">{msg('systemSkills.defaultVersion', 'Default version')}</span>
                  <code className="min-w-0 truncate" title={record.default_version || ''}>
                    {record.default_version || '—'}
                  </code>
                </div>
              }
              actions={
                <SkillActions
                  name={record.display_title}
                  iconKey={record.icon_key}
                  onUpdate={() => setUpload({ target: record })}
                  onDownload={
                    record.default_version
                      ? () =>
                          downloadSkillPackage(
                            `/api/platform-admin/skills/${encodeURIComponent(record.id)}/versions/${encodeURIComponent(record.default_version!)}/content`,
                          )
                      : undefined
                  }
                  onIcon={async (key) => {
                    try {
                      await client.request(`/skills/${encodeURIComponent(record.id)}/icon`, {
                        method: 'PATCH',
                        body: { icon_key: key, revision: record.revision },
                      });
                    } finally {
                      await changed();
                    }
                  }}
                >
                  {record.default_version && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem
                        onClick={() => {
                          setToggleError('');
                          setToggleTarget(record);
                        }}
                      >
                        {record.listed ? <EyeOff size={15} aria-hidden /> : <Eye size={15} aria-hidden />}
                        {record.listed
                          ? msg('systemSkills.unlist', 'Unlist skill')
                          : msg('systemSkills.list', 'List skill')}
                      </DropdownMenuItem>
                    </>
                  )}
                </SkillActions>
              }
            />
          ))}
        </div>
      )}
      {(offset > 0 || query.data?.has_more) && (
        <SystemSkillPager
          offset={offset}
          more={query.data?.has_more ?? false}
          busy={query.isFetching}
          onOffset={setOffset}
        />
      )}
      {selected && (
        <SystemSkillDrawer
          key={selected}
          skillID={selected}
          onClose={() => openSkill()}
          onUpload={(target) => setUpload({ target })}
          onChanged={changed}
        />
      )}
      {upload && (
        <SystemSkillImportDialog
          target={upload.target}
          onClose={() => setUpload(null)}
          onImported={async (id) => {
            await changed();
            if (id) openSkill(id);
          }}
        />
      )}
      <AlertDialog
        open={!!toggleTarget}
        onOpenChange={(open, details) => {
          if (!open && toggleBusy) {
            details.cancel();
            return;
          }
          if (!open) setToggleTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {toggleTarget?.listed
              ? msg('skills.unlistConfirm', 'Unlist skill?')
              : msg('skills.listConfirm', 'List skill?')}
          </AlertDialogTitle>
          <AlertDialogDescription>
            <strong className="block break-words">{toggleTarget?.display_title}</strong>
            {toggleTarget?.listed
              ? msg(
                  'systemSkills.unlistImpact',
                  'Hide this skill from the workspace catalog. Existing Agent references and sessions remain usable. This does not revoke execution.',
                )
              : msg(
                  'systemSkills.listImpact',
                  'Make this skill selectable in all workspaces. It will not be added to Agents automatically.',
                )}
          </AlertDialogDescription>
          {toggleError && (
            <p role="alert" className="text-sm text-destructive">
              {toggleError}
            </p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={toggleBusy}>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              disabled={toggleBusy || !!toggleError}
              onClick={(event) => {
                event.preventDefault();
                void toggleListing();
              }}
            >
              {toggleBusy ? msg('common.saving', 'Saving...') : msg('common.confirm', 'Confirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}

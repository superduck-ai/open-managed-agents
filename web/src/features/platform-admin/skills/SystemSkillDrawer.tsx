import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Upload } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Badge } from '@/shared/ui/badge';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/shared/ui/sheet';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/shared/ui/dialog';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/shared/ui/tabs';
import { CopyIdCell } from '@/shared/ui/data-table-interactions';
import { usePlatformAdmin } from '../context';
import { SystemSkillError, SystemSkillStatus, systemSkillErrorMessage } from './feedback';
import { SystemSkillVersions } from './SystemSkillVersions';
import { SystemSkillEvents } from './SystemSkillEvents';
import { systemSkillsKey, type SystemSkill, type SystemAction, type SystemPackage } from './types';

type Confirmation = { action: SystemAction; version: string; revision: number };

export function SystemSkillDrawer({
  skillID,
  onClose,
  onUpload,
  onChanged,
}: {
  skillID: string;
  onClose: () => void;
  onUpload: (skill: SystemSkill) => void;
  onChanged: () => Promise<void>;
}) {
  const { client } = usePlatformAdmin();
  const { msg } = useI18n();
  const [tab, setTab] = useState('overview');
  const [contentVersion, setContentVersion] = useState<string | null>(null);
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  const query = useQuery({
    queryKey: [...systemSkillsKey, skillID],
    queryFn: ({ signal }) => client.request<SystemSkill>(`/skills/${encodeURIComponent(skillID)}`, { signal }),
  });
  const skill = query.data;
  const confirm = (action: SystemAction, version = '') => {
    if (!skill) return;
    setError('');
    setConfirmation({ action, version, revision: skill.revision });
  };
  const change = async () => {
    if (!confirmation || pending) return;
    setPending(true);
    setError('');
    try {
      await client.request(`/skills/${encodeURIComponent(skillID)}/actions`, { method: 'POST', body: confirmation });
      await onChanged();
      setConfirmation(null);
    } catch (cause) {
      setError(systemSkillErrorMessage(cause));
      void query.refetch();
    } finally {
      setPending(false);
    }
  };
  return (
    <>
      <Sheet
        open
        onOpenChange={(open) => {
          if (!open && !pending) onClose();
        }}
      >
        <SheetContent showCloseButton={!pending} showOverlay={false} className="gap-0">
          <SheetHeader className="shrink-0 border-b-[0.5px] border-border px-4 py-4 pr-12">
            <SheetTitle>{skill?.display_title || msg('skills.detail.title', 'Skill details')}</SheetTitle>
            <SheetDescription className="text-xs">
              {msg('systemSkills.scope', 'System-wide, available to all workspaces')}
            </SheetDescription>
            {skill && (
              <div className="mt-2 flex flex-wrap items-center gap-2">
                <Badge variant="secondary">{msg('systemSkills.builtin', 'System built-in')}</Badge>
                <SystemSkillStatus skill={skill} />
                <CopyIdCell
                  value={skill.id}
                  ariaLabel={msg('skills.copyAria', 'Copy {skillId}', { skillId: skill.id })}
                />
              </div>
            )}
          </SheetHeader>
          <div className="min-h-0 flex-1 overflow-y-auto p-4">
            {query.isPending ? (
              <p role="status">{msg('common.loading', 'Loading...')}</p>
            ) : query.isError ? (
              <SystemSkillError error={query.error} retry={() => void query.refetch()} />
            ) : (
              skill && (
                <>
                  <div className="mb-5 flex flex-wrap gap-2">
                    <Button size="sm" onClick={() => onUpload(skill)}>
                      <Upload className="size-4" aria-hidden />
                      {msg('systemSkills.uploadVersion', 'Upload new version')}
                    </Button>
                    {skill.default_version && (
                      <Button size="sm" variant="outline" onClick={() => confirm(skill.listed ? 'unlist' : 'list')}>
                        {skill.listed
                          ? msg('systemSkills.unlist', 'Unlist skill')
                          : msg('systemSkills.relist', 'Relist skill')}
                      </Button>
                    )}
                  </div>
                  <Tabs value={tab} onValueChange={setTab}>
                    <TabsList variant="line" className="mb-4">
                      <TabsTrigger value="overview">{msg('systemSkills.overview', 'Overview')}</TabsTrigger>
                      <TabsTrigger value="versions">{msg('systemSkills.versions', 'Versions')}</TabsTrigger>
                      <TabsTrigger value="content">{msg('systemSkills.content', 'Package contents')}</TabsTrigger>
                      <TabsTrigger value="events">{msg('systemSkills.events', 'Activity')}</TabsTrigger>
                    </TabsList>
                    <TabsContent value="overview">
                      <SystemSkillOverview skill={skill} />
                    </TabsContent>
                    <TabsContent value="versions">
                      <SystemSkillVersions
                        skill={skill}
                        onAction={confirm}
                        onContent={(version) => {
                          setContentVersion(version);
                          setTab('content');
                        }}
                      />
                    </TabsContent>
                    <TabsContent value="content">
                      <SystemSkillContent skillID={skillID} version={contentVersion ?? skill.default_version} />
                    </TabsContent>
                    <TabsContent value="events">
                      <SystemSkillEvents skillID={skillID} />
                    </TabsContent>
                  </Tabs>
                </>
              )
            )}
          </div>
        </SheetContent>
      </Sheet>
      <Dialog
        open={confirmation !== null}
        onOpenChange={(open) => {
          if (!open && !pending) setConfirmation(null);
        }}
      >
        <DialogContent closeButtonDisabled={pending}>
          <DialogHeader>
            <DialogTitle>{msg('systemSkills.confirmAction', 'Confirm system skill change')}</DialogTitle>
            <DialogDescription>
              {skillID}
              {confirmation?.version ? ` / ${confirmation.version}` : ''}
            </DialogDescription>
          </DialogHeader>
          {confirmation && <SystemSkillImpact action={confirmation.action} />}{' '}
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button variant="outline" disabled={pending} onClick={() => setConfirmation(null)}>
              {msg('common.cancel', 'Cancel')}
            </Button>
            <Button
              variant={confirmation?.action === 'unlist' ? 'destructive' : 'default'}
              disabled={pending || !!error}
              onClick={() => void change()}
            >
              {pending ? msg('common.saving', 'Saving...') : msg('common.confirm', 'Confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

function SystemSkillImpact({ action }: { action: SystemAction }) {
  const { msg } = useI18n();
  const messages = {
    unlist: msg(
      'systemSkills.unlistImpact',
      'Hide this skill from the workspace catalog. Existing Agent references and sessions remain usable. This does not revoke execution.',
    ),
    list: msg(
      'systemSkills.listImpact',
      'Make this skill selectable in all workspaces. It will not be added to Agents automatically.',
    ),
    publish: msg(
      'systemSkills.publishImpact',
      'Published content cannot be overwritten. Keep the current default; the first published version becomes the default and is listed.',
    ),
    set_default: msg(
      'systemSkills.defaultImpact',
      'Use this version when a future new session first resolves latest. Pinned Agent versions and existing sessions will not change.',
    ),
  };
  return <p className="text-sm leading-6 text-muted-foreground">{messages[action]}</p>;
}

function SystemSkillOverview({ skill }: { skill: SystemSkill }) {
  const { msg } = useI18n();
  const fields = [
    [msg('systemSkills.publisher', 'Publisher'), skill.publisher || msg('systemSkills.unattributed', 'Unattributed')],
    [msg('systemSkills.defaultVersion', 'Default version'), skill.default_version || '—'],
    [msg('systemSkills.sourceURL', 'Source or original URL'), skill.source_url || '—'],
    [msg('systemSkills.license', 'License'), skill.license || '—'],
  ];
  return (
    <>
      <dl className="grid gap-5 sm:grid-cols-2">
        {fields.map(([label, value]) => (
          <div key={label}>
            <dt className="mb-1 text-xs text-muted-foreground">{label}</dt>
            <dd className="text-sm whitespace-pre-wrap break-words">{value}</dd>
          </div>
        ))}
      </dl>
      <p className="mt-6 text-sm leading-6 text-muted-foreground">
        {msg(
          'systemSkills.runtimeNote',
          'Skills contain instructions and supporting files. Prepare CLI tools, libraries, browsers and model credentials in the sandbox separately. Package validation is not a runtime acceptance test.',
        )}
      </p>
      <p className="mt-4 text-xs leading-5 text-muted-foreground">
        {msg(
          'systemSkills.publisherNote',
          'System built-in describes distribution scope. Publisher describes provenance, not official certification. Workspaces use system skills read-only.',
        )}
      </p>
    </>
  );
}

function SystemSkillContent({ skillID, version }: { skillID: string; version: string | null }) {
  const { msg } = useI18n();
  const { client } = usePlatformAdmin();
  const query = useQuery({
    queryKey: [...systemSkillsKey, skillID, 'content', version],
    queryFn: ({ signal }) =>
      client.request<SystemPackage>(
        `/skills/${encodeURIComponent(skillID)}/versions/${encodeURIComponent(version!)}/content`,
        { signal },
      ),
    enabled: !!version,
  });
  if (!version)
    return (
      <p className="text-sm text-muted-foreground">
        {msg('systemSkills.chooseVersion', 'Choose a version in the Versions tab to inspect its contents.')}
      </p>
    );
  if (query.isPending) return <p role="status">{msg('common.loading', 'Loading...')}</p>;
  if (query.isError) return <SystemSkillError error={query.error} retry={() => void query.refetch()} />;
  return (
    <div className="space-y-4">
      <p className="text-xs text-muted-foreground">{version}</p>
      <p className="text-sm">{query.data.description}</p>
      <pre className="max-h-48 overflow-auto rounded-lg border-[0.5px] border-border p-3 text-xs">
        {query.data.files.join('\n')}
      </pre>
      <h3 className="text-sm font-medium">SKILL.md</h3>
      <pre className="overflow-auto rounded-lg bg-muted p-3 font-mono text-xs leading-5 whitespace-pre-wrap break-words">
        {query.data.skill_md}
      </pre>
    </div>
  );
}

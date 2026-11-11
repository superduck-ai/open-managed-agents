import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { SkillFilePreview } from '@/features/skills/api';
import { SkillContent } from '@/features/skills/SkillContent';
import { Upload } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Sheet, SheetContent } from '@/shared/ui/sheet';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/shared/ui/dialog';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/shared/ui/tabs';
import { usePlatformAdmin } from '../context';
import { SystemSkillError, systemSkillErrorMessage } from './feedback';
import { SystemSkillVersions } from './SystemSkillVersions';
import { SystemSkillEvents } from './SystemSkillEvents';
import { SystemSkillDrawerHeader } from './SystemSkillDrawerHeader';
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
  const [open, setOpen] = useState(true);
  const [tab, setTab] = useState('content');
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
        open={open}
        modal={false}
        onOpenChange={(nextOpen) => {
          if (!pending) setOpen(nextOpen);
        }}
        onOpenChangeComplete={(nextOpen) => !nextOpen && onClose()}
      >
        <SheetContent
          showCloseButton={false}
          showOverlay={false}
          className="inset-y-2 right-2 h-auto w-[calc(100%-1rem)] max-w-[480px] min-h-0 gap-0 overflow-hidden rounded-xl border-[0.5px] p-0 sm:max-w-[480px]"
        >
          <SystemSkillDrawerHeader skill={skill} pending={pending} onChanged={onChanged} />
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
                      <TabsTrigger value="content">{msg('systemSkills.content', 'Package contents')}</TabsTrigger>
                      <TabsTrigger value="versions">{msg('systemSkills.versions', 'Versions')}</TabsTrigger>
                      <TabsTrigger value="events">{msg('systemSkills.events', 'Activity')}</TabsTrigger>
                    </TabsList>
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
    set_default: msg(
      'systemSkills.defaultImpact',
      'Use this version when a future new session first resolves latest. Pinned Agent versions and existing sessions will not change.',
    ),
  };
  return <p className="text-sm leading-6 text-muted-foreground">{messages[action]}</p>;
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
      <SkillContent
        key={`${skillID}/${version}`}
        content={query.data}
        contentKey={`platform-admin/${skillID}/${version}`}
        loadFile={(path, signal) =>
          client.request<SkillFilePreview>(
            `/skills/${encodeURIComponent(skillID)}/versions/${encodeURIComponent(version)}/content?path=${encodeURIComponent(path)}`,
            { signal },
          )
        }
      />
    </div>
  );
}

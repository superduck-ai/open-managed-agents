import { useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { AlertCircle, RefreshCw, Trash2, X } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '@/shared/ui/alert';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/shared/ui/alert-dialog';
import { Badge } from '@/shared/ui/badge';
import { Button } from '@/shared/ui/button';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/shared/ui/sheet';
import {
  errorMessage,
  formatRelativeTime,
  formatSkillSource,
  listSkillVersions,
  retrieveSkill,
  type ConsoleSkill,
} from '../dashboard/model';
import { SkillIcon, SkillIconPicker } from './icons';
import {
  retrieveSkillContent,
  updateWorkspaceSkillIcon,
  skillConsoleRequest,
  workspaceSkillContentUrl,
  type SkillFilePreview,
} from './api';
import { SkillDownloadButton } from './SkillDownloadButton';
import { SkillContent } from './SkillContent';
type I18nMsg = ReturnType<typeof useI18n>['msg'];
function SkillDrawerHeader({
  skill,
  onClose,
  onIcon,
  workspaceId,
}: {
  workspaceId: string;
  skill?: ConsoleSkill;
  onClose: () => void;
  onIcon: (key: string) => Promise<void>;
}) {
  const { locale, msg } = useI18n();
  const title = skill ? skill.display_title || skill.id : msg('skills.detail.title', 'Skill details');
  return (
    <SheetHeader className="shrink-0 border-b-[0.5px] border-border px-4 py-4 pr-24">
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            {skill &&
              (skill.source === 'custom' ? (
                <SkillIconPicker value={skill.icon_key || 'blocks'} name={title} onChange={onIcon} />
              ) : (
                <SkillIcon iconKey={skill.icon_key} />
              ))}
            <SheetTitle className="line-clamp-2 min-w-0 [overflow-wrap:anywhere]" title={title}>
              {title}
            </SheetTitle>
            {skill ? <SkillSourceBadge source={skill.source} /> : null}
          </div>
          {skill ? (
            <SheetDescription className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
              <span>{formatSkillUpdatedAt(skill.updated_at || skill.created_at, locale, msg)}</span>
              <span className="font-mono">{formatSkillId(skill.id)}</span>
            </SheetDescription>
          ) : null}
        </div>
        <div className="absolute right-4 top-4 flex items-center gap-1">
          {skill?.latest_version && (
            <SkillDownloadButton
              url={workspaceSkillContentUrl(workspaceId, skill.id, skill.latest_version)}
              workspaceId={workspaceId}
            />
          )}
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={msg('common.close', 'Close')}
            onClick={onClose}
          >
            <X className="size-4" aria-hidden />
          </Button>
        </div>
      </div>
    </SheetHeader>
  );
}

export function SkillDrawer({
  skillId,
  workspaceId,
  onClose,
  onDeleteSkill,
  onVersionChanged,
}: {
  skillId: string | null;
  workspaceId: string;
  onClose: () => void;
  onDeleteSkill: (skill: ConsoleSkill) => void;
  onVersionChanged: (skillId: string) => Promise<void>;
}) {
  const { locale, msg } = useI18n();
  const [open, setOpen] = useState(Boolean(skillId));
  useEffect(() => setOpen(Boolean(skillId)), [skillId]);
  const skillQuery = useQuery({
    queryKey: ['skill', workspaceId, skillId],
    queryFn: () => retrieveSkill(skillId || '', workspaceId),
    enabled: Boolean(skillId),
    retry: false,
  });
  const versionsQuery = useQuery({
    queryKey: ['skillVersions', workspaceId, skillId],
    queryFn: () => listSkillVersions(skillId || '', workspaceId),
    enabled: Boolean(skillId),
    retry: false,
  });
  const skill = skillQuery.data;
  const versions = versionsQuery.data?.data ?? [];
  const latestVersion = useMemo(() => {
    if (!skill) {
      return versions[0];
    }
    return versions.find((version) => version.version === skill.latest_version) ?? versions[0];
  }, [skill, versions]);
  const contentVersion = skill?.latest_version;
  const contentQuery = useQuery({
    queryKey: ['skillContent', workspaceId, skillId, contentVersion],
    queryFn: ({ signal }) => retrieveSkillContent(workspaceId, skillId!, contentVersion!, signal),
    enabled: Boolean(skillId && contentVersion),
    retry: false,
  });

  return (
    <>
      <Sheet
        open={open}
        modal={false}
        onOpenChange={setOpen}
        onOpenChangeComplete={(nextOpen) => !nextOpen && onClose()}
      >
        <SheetContent
          showCloseButton={false}
          showOverlay={false}
          side="right"
          className="inset-y-2 right-2 h-auto w-[calc(100%-1rem)] max-w-[480px] min-h-0 gap-0 overflow-hidden rounded-xl border-[0.5px] p-0 sm:max-w-[480px]"
        >
          <SkillDrawerHeader
            skill={skill}
            workspaceId={workspaceId}
            onClose={() => setOpen(false)}
            onIcon={async (key) => {
              if (!skill || skill.source !== 'custom') return;
              await updateWorkspaceSkillIcon(workspaceId, skill.id, key);
              await onVersionChanged(skill.id);
            }}
          />
          {skillQuery.isLoading ? (
            <div className="p-4 text-sm text-muted-foreground">
              <RefreshCw className="mr-2 inline size-4 animate-spin" aria-hidden />
              {msg('skills.detail.loading', 'Loading skill...')}
            </div>
          ) : skillQuery.error || !skill ? (
            <div className="subtle-scrollbar min-h-0 overflow-y-auto p-4">
              <Alert variant="destructive" className="[overflow-wrap:anywhere]">
                <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
                <AlertTitle>{msg('skills.detail.error', 'Skill could not be loaded.')}</AlertTitle>
                <AlertDescription>
                  <p>{errorMessage(skillQuery.error)}</p>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    className="mt-3"
                    onClick={() => void skillQuery.refetch()}
                  >
                    <RefreshCw className="size-3.5" aria-hidden />
                    {msg('common.retry', 'Retry')}
                  </Button>
                </AlertDescription>
              </Alert>
            </div>
          ) : (
            <>
              <div className="subtle-scrollbar min-h-0 flex-1 space-y-5 overflow-y-auto px-4 py-4">
                {latestVersion ? (
                  <section className="space-y-2">
                    <h2 className="text-sm font-medium text-foreground">{msg('common.description', 'Description')}</h2>
                    <p className="text-sm leading-6 text-muted-foreground [overflow-wrap:anywhere]">
                      {latestVersion?.description || msg('common.noDescription', 'No description provided.')}
                    </p>
                  </section>
                ) : null}

                {contentVersion &&
                  (contentQuery.isPending ? (
                    <p role="status" className="text-sm text-muted-foreground">
                      {msg('common.loading', 'Loading...')}
                    </p>
                  ) : contentQuery.isError ? (
                    <Alert variant="destructive">
                      <AlertTitle>{msg('skills.content.error', 'Skill contents could not be loaded.')}</AlertTitle>
                      <AlertDescription>
                        <p>{errorMessage(contentQuery.error)}</p>
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={contentQuery.isFetching}
                          onClick={() => void contentQuery.refetch()}
                        >
                          {msg('common.retry', 'Retry')}
                        </Button>
                      </AlertDescription>
                    </Alert>
                  ) : (
                    <SkillContent
                      key={`${skill.id}/${contentVersion}`}
                      content={contentQuery.data}
                      contentKey={workspaceSkillContentUrl(workspaceId, skill.id, contentVersion)}
                      loadFile={(path, signal) =>
                        skillConsoleRequest<SkillFilePreview>(
                          workspaceId,
                          `/${encodeURIComponent(skill.id)}/versions/${encodeURIComponent(contentVersion)}/preview?path=${encodeURIComponent(path)}`,
                          { signal },
                        )
                      }
                    />
                  ))}

                <section className="space-y-3 border-t-[0.5px] border-border pt-4">
                  <h2 className="text-sm font-medium text-foreground">{msg('skills.versions.title', 'Versions')}</h2>
                  {versionsQuery.isLoading ? (
                    <div className="text-sm text-muted-foreground">
                      <RefreshCw className="mr-2 inline size-4 animate-spin" aria-hidden />
                      {msg('skills.versions.loading', 'Loading versions...')}
                    </div>
                  ) : versionsQuery.error ? (
                    <Alert variant="destructive" className="[overflow-wrap:anywhere]">
                      <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
                      <AlertTitle>{msg('skills.versions.error', 'Versions could not be loaded.')}</AlertTitle>
                      <AlertDescription>
                        <p>{errorMessage(versionsQuery.error)}</p>
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          className="mt-3"
                          disabled={versionsQuery.isFetching}
                          onClick={() => void versionsQuery.refetch()}
                        >
                          {msg('common.retry', 'Retry')}
                        </Button>
                      </AlertDescription>
                    </Alert>
                  ) : versions.length === 0 ? (
                    <p className="text-sm text-muted-foreground">
                      {msg('skills.versions.empty', 'No versions have been created for this skill.')}
                    </p>
                  ) : (
                    <div className="space-y-2">
                      {versions.map((version) => (
                        <div key={version.version} className="flex min-w-0 flex-wrap items-center gap-2">
                          <code className="min-w-0 break-all text-xs">{version.version}</code>
                          <span className="text-xs text-muted-foreground">
                            {formatSkillUpdatedAt(version.created_at, locale, msg)}
                          </span>
                          {version.version === skill.latest_version ? (
                            <Badge variant="secondary" className="rounded-md">
                              {msg('skills.versions.latest', 'Latest')}
                            </Badge>
                          ) : null}
                        </div>
                      ))}
                    </div>
                  )}
                </section>
              </div>
            </>
          )}
          {skill?.source === 'custom' && (
            <div className="border-t-[0.5px] border-border p-4">
              <Button variant="ghost" size="sm" className="text-destructive" onClick={() => onDeleteSkill(skill)}>
                <Trash2 size={14} aria-hidden />
                {msg('skills.delete.action', 'Delete skill')}
              </Button>
            </div>
          )}
        </SheetContent>
      </Sheet>
    </>
  );
}

export function DeleteSkillDialog({
  skill,
  isDeleting,
  error,
  onOpenChange,
  onConfirm,
  description,
  title,
  confirmLabel,
}: {
  skill: Pick<ConsoleSkill, 'id' | 'display_title'> | null;
  isDeleting: boolean;
  error: string | null;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  description?: string;
  title?: string;
  confirmLabel?: string;
}) {
  const { msg } = useI18n();
  const errorRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);
  const [content, setContent] = useState({ skill, error });
  if (skill && (skill !== content.skill || error !== content.error)) setContent({ skill, error });
  const name = content.skill?.display_title || content.skill?.id || '';
  return (
    <AlertDialog
      open={Boolean(skill)}
      onOpenChange={(nextOpen, details) => {
        if (!nextOpen && isDeleting) {
          details.cancel();
          return;
        }
        onOpenChange(nextOpen);
      }}
    >
      <AlertDialogContent
        size="default"
        className="max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden"
      >
        <AlertDialogHeader>
          <AlertDialogTitle>{title || msg('skills.delete.heading', 'Delete skill?')}</AlertDialogTitle>
        </AlertDialogHeader>
        <div className="subtle-scrollbar min-h-0 space-y-4 overflow-y-auto">
          <AlertDialogDescription className="space-y-3 [overflow-wrap:anywhere]">
            <span className="block font-medium text-foreground">{name}</span>
            <span className="block">
              {description ||
                msg(
                  'skills.delete.warning',
                  'Existing code references will stop working immediately. This action is permanent and cannot be undone.',
                )}
            </span>
          </AlertDialogDescription>
          {content.error ? (
            <Alert ref={errorRef} tabIndex={-1} variant="destructive" className="outline-none [overflow-wrap:anywhere]">
              <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
              <AlertTitle>
                {title
                  ? msg('common.error', 'Something went wrong')
                  : msg('skills.delete.error', 'Skill could not be deleted.')}
              </AlertTitle>
              <AlertDescription>{content.error}</AlertDescription>
            </Alert>
          ) : null}
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel variant="secondary" disabled={isDeleting}>
            {msg('common.cancel', 'Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={isDeleting}
            onClick={(event) => {
              event.preventDefault();
              onConfirm();
            }}
          >
            {isDeleting ? msg('common.saving', 'Saving...') : confirmLabel || msg('common.delete', 'Delete')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function SkillSourceBadge({ source }: { source: string }) {
  const { msg } = useI18n();
  const label = formatSkillSource(source, msg);
  return (
    <Badge variant="secondary" className="max-w-full rounded-md text-muted-foreground" title={label}>
      <span className="truncate">{label}</span>
    </Badge>
  );
}

function formatSkillId(skillId: string) {
  if (skillId.length <= 16) {
    return skillId;
  }
  return `${skillId.slice(0, 9)}...${skillId.slice(-6)}`;
}

function formatSkillUpdatedAt(value: string, locale: string, msg: I18nMsg) {
  const timestamp = Date.parse(value);
  if (Number.isFinite(timestamp) && Math.abs(Date.now() - timestamp) < 60_000) {
    return msg('common.justNow', 'just now');
  }
  return formatRelativeTime(value, locale, msg('common.justNow', 'just now'));
}

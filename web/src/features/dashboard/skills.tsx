import { AlertCircle, Copy, Download, Plus, RefreshCw, ToggleLeft, Upload } from 'lucide-react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Alert, AlertDescription, AlertTitle } from '@/shared/ui/alert';
import { Badge } from '@/shared/ui/badge';
import { Button } from '@/shared/ui/button';
import { Field, FieldDescription, FieldError } from '@/shared/ui/field';
import { Input } from '@/shared/ui/input';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/shared/ui/table';
import { useI18n } from '../../shared/i18n';
import {
  BackLink,
  ConsolePageFrame,
  CursorPagination,
  DataTableCard,
  PanelCard,
  PrimaryAction,
  TableEmptyRow,
  TableErrorRow,
  TableLoadingRow
} from './frame';
import {
  copyText,
  createSkillHref,
  createSkillPackage,
  downloadSkillVersion,
  errorMessage,
  formatRelativeTime,
  formatSkillSource,
  listSkillVersions,
  listSkills,
  retrieveSkill,
  skillDetailHref,
  skillsIndexHref,
  useDashboardWorkspaceScope,
  type ConsoleSkill,
  type ConsoleSkillVersion,
  type EnrichedConsoleSkill
} from './model';

export function SkillsPage() {
  const { msg } = useI18n();
  const { workspaceId, workspaceName } = useDashboardWorkspaceScope();
  const [pageIndex, setPageIndex] = useState(0);
  const [pageTokens, setPageTokens] = useState<Array<string | undefined>>([undefined]);
  const paginationWorkspaceIdRef = useRef(workspaceId);
  const pageToken = paginationWorkspaceIdRef.current === workspaceId ? pageTokens[pageIndex] : undefined;
  const skillsQuery = useQuery({
    queryKey: ['skills', workspaceId, pageToken ?? ''],
    queryFn: () => listSkills(pageToken, workspaceId),
    retry: false
  });
  const response = skillsQuery.data;
  const skills = response?.data ?? [];
  const nextPage = response?.next_page ?? undefined;

  useEffect(() => {
    if (paginationWorkspaceIdRef.current === workspaceId) {
      return;
    }
    paginationWorkspaceIdRef.current = workspaceId;
    setPageIndex(0);
    setPageTokens([undefined]);
  }, [workspaceId]);

  const goNext = () => {
    if (!nextPage) {
      return;
    }
    setPageTokens((current) => {
      const next = current.slice(0, pageIndex + 1);
      next.push(nextPage);
      return next;
    });
    setPageIndex((current) => current + 1);
  };

  const goPrevious = () => {
    setPageIndex((current) => Math.max(0, current - 1));
  };

  return (
    <ConsolePageFrame
      title={msg('skills.title', 'Skills')}
      icon={ToggleLeft}
      description={msg(
        'skills.description',
        "Skills are repeatable and customizable instructions that Claude API can follow. Only skills from the {workspaceName} workspace are shown. To see another workspace's skills, select a workspace.",
        { workspaceName }
      )}
      actions={<PrimaryAction href={createSkillHref()} icon={Plus} label={msg('skills.create', 'Create skill')} />}
    >
      <SkillsList
        skills={skills}
        workspaceName={workspaceName}
        isLoading={skillsQuery.isLoading}
        isFetching={skillsQuery.isFetching}
        error={skillsQuery.error}
        canPrevious={pageIndex > 0 && !skillsQuery.isFetching}
        canNext={Boolean(response?.has_more && nextPage) && !skillsQuery.isFetching}
        onRetry={() => void skillsQuery.refetch()}
        onPrevious={goPrevious}
        onNext={goNext}
      />
    </ConsolePageFrame>
  );
}

function SkillsList({
  skills,
  workspaceName,
  isLoading,
  isFetching,
  error,
  canPrevious,
  canNext,
  onRetry,
  onPrevious,
  onNext
}: {
  skills: EnrichedConsoleSkill[];
  workspaceName: string;
  isLoading: boolean;
  isFetching: boolean;
  error: unknown;
  canPrevious: boolean;
  canNext: boolean;
  onRetry: () => void;
  onPrevious: () => void;
  onNext: () => void;
}) {
  const { msg } = useI18n();

  return (
    <section aria-label={msg('skills.listAria', 'Skills list')}>
      <DataTableCard>
        <Table className="min-w-[860px] table-fixed text-left">
          <colgroup>
            <col className="w-[48%]" />
            <col className="w-[22%]" />
            <col className="w-[14%]" />
            <col className="w-[16%]" />
          </colgroup>
          <TableHeader className="text-xs text-muted-foreground/70">
            <TableRow className="border-b border-border hover:bg-transparent">
              <TableHead className="px-3 py-3 text-muted-foreground/70">{msg('skills.table.skill', 'Skill')}</TableHead>
              <TableHead className="px-3 py-3 text-muted-foreground/70">{msg('skills.versions.directory', 'Directory')}</TableHead>
              <TableHead className="px-3 py-3 text-muted-foreground/70">{msg('skills.table.source', 'Source')}</TableHead>
              <TableHead className="px-3 py-3 text-muted-foreground/70">{msg('common.updated', 'Updated')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableLoadingRow colSpan={4} label={msg('skills.loading', 'Loading skills...')} />
            ) : error ? (
              <TableErrorRow
                colSpan={4}
                title={msg('skills.error', 'Skills could not be loaded.')}
                message={errorMessage(error)}
                retryLabel={msg('common.retry', 'Retry')}
                onRetry={onRetry}
              />
            ) : skills.length === 0 ? (
              <TableEmptyRow colSpan={4}>
                {msg('skills.empty', 'No skills have been created in the {workspaceName} workspace.', { workspaceName })}
              </TableEmptyRow>
            ) : (
              skills.map((skill) => (
                <TableRow key={skill.id} className="border-b border-border text-foreground hover:bg-secondary/50">
                  <TableCell className="min-w-0 px-3 py-4 align-top">
                    <a
                      href={skillDetailHref(skill.id)}
                      className="inline-flex max-w-full text-[15px] font-semibold leading-6 text-foreground transition hover:text-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                    >
                      <span className="truncate">{skill.display_title || skill.versionName || skill.id}</span>
                    </a>
                    {skill.description ? (
                      <p className="mt-1 max-w-[900px] overflow-hidden text-sm leading-5 text-muted-foreground [display:-webkit-box] [-webkit-box-orient:vertical] [-webkit-line-clamp:2]">
                        {skill.description}
                      </p>
                    ) : null}
                  </TableCell>
                  <TableCell className="px-3 py-4 align-top">
                    <Badge variant="outline" className="max-w-full rounded-md bg-secondary font-mono text-[11px] text-muted-foreground">
                      <span className="truncate">{skill.directory || skill.id}</span>
                    </Badge>
                  </TableCell>
                  <TableCell className="px-3 py-4 align-top text-muted-foreground">{formatSkillSource(skill.source, msg)}</TableCell>
                  <TableCell className="px-3 py-4 align-top text-muted-foreground">
                    {formatRelativeTime(skill.updated_at || skill.created_at)}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </DataTableCard>

      <CursorPagination
        previousLabel={msg('pagination.previousPage', 'Previous page')}
        nextLabel={msg('pagination.nextPage', 'Next page')}
        updatingLabel={msg('common.updating', 'Updating...')}
        canPrevious={canPrevious}
        canNext={canNext}
        isUpdating={isFetching && !isLoading}
        onPrevious={onPrevious}
        onNext={onNext}
      />
    </section>
  );
}

export function CreateSkillPage() {
  const { msg } = useI18n();
  const { workspaceId, workspaceName } = useDashboardWorkspaceScope();
  const [createdSkill, setCreatedSkill] = useState<ConsoleSkill | null>(null);

  const handleUploaded = (resource: ConsoleSkill | ConsoleSkillVersion) => {
    if (resource.type !== 'skill') {
      return;
    }
    setCreatedSkill(resource);
    window.location.assign(skillDetailHref(resource.id));
  };

  return (
    <section className="space-y-5">
      <BackLink href={skillsIndexHref()} label={msg('skills.title', 'Skills')} />
      <ConsolePageFrame
        title={msg('skills.create', 'Create skill')}
        icon={ToggleLeft}
        description={msg('skills.create.description', 'Upload a skill package to the {workspaceName} workspace.', {
          workspaceName
        })}
      >
        <section className="border-t border-border pt-5">
          <SkillPackageUpload
            workspaceId={workspaceId}
            chooseLabel={msg('skills.upload.chooseDirectory', 'Choose directory')}
            submitLabel={msg('skills.create', 'Create skill')}
            onUploaded={handleUploaded}
          />
          {createdSkill ? (
            <a href={skillDetailHref(createdSkill.id)} className="mt-4 inline-flex text-sm text-primary underline underline-offset-2">
              {msg('skills.created.view', 'View {name}', { name: createdSkill.display_title || createdSkill.id })}
            </a>
          ) : null}
        </section>
      </ConsolePageFrame>
    </section>
  );
}

export function SkillDetailPage({ skillId }: { skillId: string }) {
  const { msg } = useI18n();
  const queryClient = useQueryClient();
  const { workspaceId } = useDashboardWorkspaceScope();
  const [selectedVersion, setSelectedVersion] = useState<string | null>(null);
  const [downloadingVersion, setDownloadingVersion] = useState<string | null>(null);
  const skillQuery = useQuery({
    queryKey: ['skill', workspaceId, skillId],
    queryFn: () => retrieveSkill(skillId, workspaceId),
    enabled: Boolean(skillId),
    retry: false
  });
  const versionsQuery = useQuery({
    queryKey: ['skillVersions', workspaceId, skillId],
    queryFn: () => listSkillVersions(skillId, workspaceId),
    enabled: Boolean(skillId),
    retry: false
  });
  const skill = skillQuery.data;
  const versions = versionsQuery.data?.data ?? [];
  const selectedVersionRecord = versions.find((version) => version.version === selectedVersion) ?? versions[0];

  useEffect(() => {
    if (!versions.length) {
      setSelectedVersion(null);
      return;
    }
    setSelectedVersion((current) => {
      if (current && versions.some((version) => version.version === current)) {
        return current;
      }
      return versions.find((version) => version.version === skill?.latest_version)?.version ?? versions[0]?.version ?? null;
    });
  }, [skill?.latest_version, versions]);

  const refreshSkill = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['skill', workspaceId, skillId] }),
      queryClient.invalidateQueries({ queryKey: ['skillVersions', workspaceId, skillId] }),
      queryClient.invalidateQueries({ queryKey: ['skills', workspaceId] })
    ]);
  };

  const handleUploadedVersion = (resource: ConsoleSkill | ConsoleSkillVersion) => {
    if (resource.type === 'skill_version') {
      setSelectedVersion(resource.version);
    }
    void refreshSkill();
  };

  const handleDownloadVersion = async (version: ConsoleSkillVersion) => {
    setDownloadingVersion(version.version);
    try {
      await downloadSkillVersion(skillId, version.version, version.directory, workspaceId);
    } finally {
      setDownloadingVersion(null);
    }
  };

  if (!skillId) {
    return (
      <section className="space-y-4">
        <BackLink href={skillsIndexHref()} label={msg('skills.title', 'Skills')} />
        <div className="border-t border-border pt-6 text-sm text-muted-foreground">{msg('skills.detail.notFound', 'Skill not found.')}</div>
      </section>
    );
  }

  return (
    <section className="space-y-5">
      <BackLink href={skillsIndexHref()} label={msg('skills.title', 'Skills')} />

      {skillQuery.isLoading ? (
        <div className="border-t border-border px-3 py-6 text-sm text-muted-foreground">
          <span className="inline-flex items-center gap-2">
            <RefreshCw className="size-3.5 animate-spin" aria-hidden />
            {msg('skills.detail.loading', 'Loading skill...')}
          </span>
        </div>
      ) : skillQuery.error || !skill ? (
        <div className="border-t border-border px-3 py-6">
          <Alert variant="destructive" className="max-w-xl">
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
        <ConsolePageFrame
          title={skill.display_title || selectedVersionRecord?.name || skill.id}
          icon={ToggleLeft}
          meta={
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground/70">
              <Badge variant="outline" className="rounded-md bg-secondary font-mono text-[11px] text-muted-foreground">
                {skill.id}
              </Badge>
              <span aria-hidden>•</span>
              <span>{formatSkillSource(skill.source, msg)}</span>
              <span aria-hidden>•</span>
              <span>{msg('skills.detail.latestVersion', 'Latest version {version}', { version: formatVersionLabel(skill.latest_version) })}</span>
              <span aria-hidden>•</span>
              <span>{formatRelativeTime(skill.updated_at || skill.created_at)}</span>
            </div>
          }
          actions={
            <Button
              type="button"
              variant="outline"
              size="lg"
              onClick={() => void copyText(skill.id)}
            >
              <Copy className="size-4" aria-hidden />
              {msg('common.copyId', 'Copy ID')}
            </Button>
          }
        >
          <PanelCard title={msg('common.description', 'Description')}>
            <p className="max-w-[900px] text-sm leading-6 text-muted-foreground">
              {selectedVersionRecord?.description || msg('common.noDescription', 'No description provided.')}
            </p>
          </PanelCard>

          <section className="space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h2 className="text-sm font-semibold text-foreground">{msg('skills.versions.title', 'Versions')}</h2>
              {skill.source === 'custom' ? (
                <span className="text-xs text-muted-foreground/70">{msg('skills.versions.customHint', 'Custom skills can receive new versions.')}</span>
              ) : (
                <span className="text-xs text-muted-foreground/70">{msg('skills.versions.builtInHint', 'Built-in Anthropic skills are read-only.')}</span>
              )}
            </div>
            <SkillVersionsTable
              versions={versions}
              latestVersion={skill.latest_version}
              selectedVersion={selectedVersionRecord?.version ?? null}
              isLoading={versionsQuery.isLoading}
              error={versionsQuery.error}
              downloadingVersion={downloadingVersion}
              onRetry={() => void versionsQuery.refetch()}
              onSelectVersion={setSelectedVersion}
              onDownloadVersion={(version) => void handleDownloadVersion(version)}
            />
          </section>

          {skill.source === 'custom' ? (
            <PanelCard title={msg('skills.versions.create', 'Create version')}>
              <SkillPackageUpload
                skillId={skill.id}
                workspaceId={workspaceId}
                chooseLabel={msg('skills.upload.chooseDirectory', 'Choose directory')}
                submitLabel={msg('skills.versions.create', 'Create version')}
                onUploaded={handleUploadedVersion}
              />
            </PanelCard>
          ) : null}
        </ConsolePageFrame>
      )}
    </section>
  );
}

function SkillVersionsTable({
  versions,
  latestVersion,
  selectedVersion,
  isLoading,
  error,
  downloadingVersion,
  onRetry,
  onSelectVersion,
  onDownloadVersion
}: {
  versions: ConsoleSkillVersion[];
  latestVersion: string;
  selectedVersion: string | null;
  isLoading: boolean;
  error: unknown;
  downloadingVersion: string | null;
  onRetry: () => void;
  onSelectVersion: (version: string) => void;
  onDownloadVersion: (version: ConsoleSkillVersion) => void;
}) {
  const { msg } = useI18n();

  return (
    <section aria-label={msg('skills.versions.listAria', 'Skill versions')}>
      <DataTableCard>
        <Table className="min-w-[860px] table-fixed text-left">
          <colgroup>
            <col className="w-[18%]" />
            <col className="w-[28%]" />
            <col className="w-[28%]" />
            <col className="w-[18%]" />
            <col className="w-[8%]" />
          </colgroup>
          <TableHeader className="text-xs text-muted-foreground/70">
            <TableRow className="border-b border-border hover:bg-transparent">
              <TableHead className="px-3 py-3 text-muted-foreground/70">{msg('skills.versions.version', 'Version')}</TableHead>
              <TableHead className="px-3 py-3 text-muted-foreground/70">{msg('common.name', 'Name')}</TableHead>
              <TableHead className="px-3 py-3 text-muted-foreground/70">{msg('skills.versions.directory', 'Directory')}</TableHead>
              <TableHead className="px-3 py-3 text-muted-foreground/70">{msg('common.created', 'Created')}</TableHead>
              <TableHead className="px-3 py-3 text-muted-foreground/70" aria-label={msg('common.actions', 'Actions')} />
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableLoadingRow colSpan={5} label={msg('skills.versions.loading', 'Loading versions...')} />
            ) : error ? (
              <TableErrorRow
                colSpan={5}
                title={msg('skills.versions.error', 'Versions could not be loaded.')}
                message={errorMessage(error)}
                retryLabel={msg('common.retry', 'Retry')}
                onRetry={onRetry}
              />
            ) : versions.length === 0 ? (
              <TableEmptyRow colSpan={5}>
                {msg('skills.versions.empty', 'No versions have been created for this skill.')}
              </TableEmptyRow>
            ) : (
              versions.map((version) => {
                const selected = version.version === selectedVersion;
                return (
                  <TableRow
                    key={version.version}
                    className={selected ? 'border-b border-border bg-secondary/60' : 'border-b border-border'}
                  >
                    <TableCell className="px-3 py-3 align-middle">
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="justify-start px-1 text-left font-medium text-foreground hover:text-primary"
                        onClick={() => onSelectVersion(version.version)}
                      >
                        {formatVersionLabel(version.version)}
                        {version.version === latestVersion ? (
                          <Badge variant="secondary" className="rounded-md bg-secondary text-[11px] text-secondary-foreground">
                            {msg('skills.versions.latest', 'Latest')}
                          </Badge>
                        ) : null}
                      </Button>
                    </TableCell>
                    <TableCell className="truncate px-3 py-3 align-middle text-foreground">
                      {version.name || msg('common.untitled', 'Untitled')}
                    </TableCell>
                    <TableCell className="truncate px-3 py-3 align-middle font-mono text-[13px] text-muted-foreground">{version.directory}</TableCell>
                    <TableCell className="px-3 py-3 align-middle text-muted-foreground">{formatRelativeTime(version.created_at)}</TableCell>
                    <TableCell className="px-3 py-3 text-right align-middle">
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        aria-label={msg('skills.versions.downloadAria', 'Download version {version}', { version: version.version })}
                        className="text-muted-foreground/70"
                        disabled={downloadingVersion === version.version}
                        onClick={() => onDownloadVersion(version)}
                      >
                        <Download className="size-3.5" aria-hidden />
                      </Button>
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </DataTableCard>
    </section>
  );
}

function SkillPackageUpload({
  skillId,
  workspaceId,
  chooseLabel,
  submitLabel,
  onUploaded
}: {
  skillId?: string;
  workspaceId: string;
  chooseLabel: string;
  submitLabel: string;
  onUploaded: (resource: ConsoleSkill | ConsoleSkillVersion) => void;
}) {
  const { msg } = useI18n();
  const inputRef = useRef<HTMLInputElement | null>(null);
  const [files, setFiles] = useState<File[]>([]);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const directoryInputProps = { webkitdirectory: '', directory: '' };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!files.length || isSubmitting) {
      return;
    }
    setError(null);
    setIsSubmitting(true);
    try {
      const resource = await createSkillPackage(skillId, files, workspaceId);
      setFiles([]);
      if (inputRef.current) {
        inputRef.current.value = '';
      }
      onUploaded(resource);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <form className="flex flex-wrap items-start gap-3" onSubmit={handleSubmit}>
      <Input
        {...directoryInputProps}
        ref={inputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(event) => {
          setFiles(Array.from(event.currentTarget.files ?? []));
          setError(null);
        }}
      />
      <Button
        type="button"
        variant="outline"
        size="lg"
        onClick={() => inputRef.current?.click()}
      >
        <Upload className="size-4" aria-hidden />
        {chooseLabel}
      </Button>
      <Field data-invalid={Boolean(error)} className="min-w-[220px] flex-1 gap-1 pt-1">
        <FieldDescription className="text-muted-foreground">
          {files.length
            ? msg('skills.upload.filesSelected', '{count, plural, one {# file selected} other {# files selected}}', {
                count: files.length
              })
            : msg('skills.upload.noFilesSelected', 'No files selected')}
        </FieldDescription>
        <FieldError className="text-amber-600 dark:text-amber-400">{error}</FieldError>
      </Field>
      <Button
        type="submit"
        size="lg"
        disabled={!files.length || isSubmitting}
      >
        {isSubmitting ? msg('skills.upload.uploading', 'Uploading...') : submitLabel}
      </Button>
    </form>
  );
}

function formatVersionLabel(version: string) {
  if (!version) {
    return 'Unknown';
  }
  return version.startsWith('v') ? version : `v${version}`;
}

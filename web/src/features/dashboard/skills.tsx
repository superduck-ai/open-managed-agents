import { AlertCircle, Check, FileArchive, FolderPlus, Plus, RefreshCw, Trash2, Upload, X } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from "react";
import { cn } from "@/shared/lib/utils";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/shared/ui/alert-dialog";
import { Alert, AlertDescription, AlertTitle } from "@/shared/ui/alert";
import { Badge } from "@/shared/ui/badge";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/shared/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/shared/ui/dropdown-menu";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/shared/ui/sheet";
import { Table, TableBody, TableHead, TableHeader, TableRow } from "@/shared/ui/table";
import {
  CopyIdCell,
  DataTableCell,
  DataTableRow,
  MoreActionsButton,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
} from "@/shared/ui/data-table-interactions";
import { useI18n } from "../../shared/i18n";
import { CursorPagination, TableEmptyRow, TableErrorRow, TableLoadingRow } from "./frame";
import {
  createSkillPackage,
  deleteSkill,
  deleteSkillVersion,
  errorMessage,
  formatBytes,
  formatRelativeTime,
  formatSkillSource,
  listSkillVersions,
  listSkills,
  retrieveSkill,
  skillsIndexHref,
  useDashboardWorkspaceScope,
  type ConsoleSkill,
  type ConsoleSkillVersion,
} from "./model";

const maxUploadBytes = 8 * 1024 * 1024;

type SkillsPageProps = {
  initialCreateOpen?: boolean;
  initialSkillId?: string;
};

type UploadMode = "create" | "update";

type UploadSelection = {
  files: File[];
  label: string;
  displayTitle: string;
  fileCount: number;
  size: number;
};

type SkillFile = File & {
  __skillPath?: string;
};

type I18nMsg = ReturnType<typeof useI18n>["msg"];

type SkillFileSystemEntry = {
  isFile: boolean;
  isDirectory: boolean;
  name: string;
  file?: (success: (file: File) => void, error?: (err: DOMException) => void) => void;
  createReader?: () => {
    readEntries: (success: (entries: SkillFileSystemEntry[]) => void, error?: (err: DOMException) => void) => void;
  };
};

export function SkillsPage({ initialCreateOpen = false, initialSkillId }: SkillsPageProps = {}) {
  const { msg } = useI18n();
  const queryClient = useQueryClient();
  const { workspaceId, workspaceName } = useDashboardWorkspaceScope();
  const [selectedSkillId, setSelectedSkillId] = useSkillSearchParam(initialSkillId);
  const [createOpen, setCreateOpen] = useState(initialCreateOpen);
  const [updateSkillTarget, setUpdateSkillTarget] = useState<ConsoleSkill | null>(null);
  const [deleteSkillTarget, setDeleteSkillTarget] = useState<ConsoleSkill | null>(null);
  const [deletingSkillId, setDeletingSkillId] = useState<string | null>(null);
  const [deleteSkillError, setDeleteSkillError] = useState<string | null>(null);
  const [pageIndex, setPageIndex] = useState(0);
  const [pageTokens, setPageTokens] = useState<Array<string | undefined>>([undefined]);
  const paginationWorkspaceIdRef = useRef(workspaceId);
  const pageToken = paginationWorkspaceIdRef.current === workspaceId ? pageTokens[pageIndex] : undefined;

  const skillsQuery = useQuery({
    queryKey: ["skills", workspaceId, pageToken ?? ""],
    queryFn: () => listSkills(pageToken, workspaceId),
    retry: false,
  });
  const skills = skillsQuery.data?.data ?? [];
  const nextPage = skillsQuery.data?.next_page ?? undefined;

  useEffect(() => {
    if (!initialSkillId) {
      return;
    }
    setSelectedSkillId(initialSkillId, true);
  }, [initialSkillId, setSelectedSkillId]);

  useEffect(() => {
    if (paginationWorkspaceIdRef.current === workspaceId) {
      return;
    }
    paginationWorkspaceIdRef.current = workspaceId;
    setPageIndex(0);
    setPageTokens([undefined]);
  }, [workspaceId]);

  const invalidateSkills = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey: ["skills", workspaceId] });
  }, [queryClient, workspaceId]);

  const invalidateSkill = useCallback(
    async (skillId: string) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["skills", workspaceId] }),
        queryClient.invalidateQueries({ queryKey: ["skill", workspaceId, skillId] }),
        queryClient.invalidateQueries({ queryKey: ["skillVersions", workspaceId, skillId] }),
      ]);
    },
    [queryClient, workspaceId],
  );

  const handleCreateUploaded = (resource: ConsoleSkill | ConsoleSkillVersion) => {
    const uploadedSkillId = "skill_id" in resource ? resource.skill_id : resource.id;

    setCreateOpen(false);
    void invalidateSkills();
    if (uploadedSkillId) {
      setSelectedSkillId(uploadedSkillId);
      void invalidateSkill(uploadedSkillId);
    }
  };

  const handleVersionUploaded = (skillId: string) => {
    void invalidateSkill(skillId);
  };

  const handleDeleteSkill = async () => {
    if (!deleteSkillTarget || deletingSkillId) {
      return;
    }
    const target = deleteSkillTarget;
    setDeletingSkillId(target.id);
    setDeleteSkillError(null);
    try {
      await deleteSkill(target.id, workspaceId);
      setDeleteSkillTarget(null);
      if (selectedSkillId === target.id) {
        setSelectedSkillId(null);
      }
      await invalidateSkills();
    } catch (err) {
      setDeleteSkillError(errorMessage(err));
    } finally {
      setDeletingSkillId(null);
    }
  };

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
    <section className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">{msg("skills.title", "Skills")}</h1>
          <p className="mt-1 max-w-3xl text-sm leading-5 text-muted-foreground">
            {msg(
              "skills.description",
              "Skills are repeatable and customizable instructions that Claude API can follow.",
              {
                workspaceName,
              },
            )}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button type="button" onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" aria-hidden />
            {msg("skills.create", "Create skill")}
          </Button>
        </div>
      </div>

      <SkillsTable
        skills={skills}
        workspaceName={workspaceName}
        selectedSkillId={selectedSkillId}
        isLoading={skillsQuery.isLoading}
        isFetching={skillsQuery.isFetching}
        error={skillsQuery.error}
        onRetry={() => void skillsQuery.refetch()}
        onOpenSkill={(skillId) => setSelectedSkillId(skillId)}
        onUpdateSkill={setUpdateSkillTarget}
        onDeleteSkill={(skill) => {
          setDeleteSkillError(null);
          setDeleteSkillTarget(skill);
        }}
      />

      <CursorPagination
        previousLabel={msg("pagination.previousPage", "Previous page")}
        nextLabel={msg("pagination.nextPage", "Next page")}
        updatingLabel={msg("common.updating", "Updating...")}
        canPrevious={pageIndex > 0 && !skillsQuery.isFetching}
        canNext={Boolean(skillsQuery.data?.has_more && nextPage) && !skillsQuery.isFetching}
        isUpdating={skillsQuery.isFetching && !skillsQuery.isLoading}
        onPrevious={goPrevious}
        onNext={goNext}
      />

      <SkillDrawer
        skillId={selectedSkillId}
        workspaceId={workspaceId}
        onClose={() => setSelectedSkillId(null)}
        onUpdateSkill={setUpdateSkillTarget}
        onDeleteSkill={(skill) => {
          setDeleteSkillError(null);
          setDeleteSkillTarget(skill);
        }}
        onVersionChanged={invalidateSkill}
      />

      <SkillUploadDialog
        mode="create"
        open={createOpen}
        workspaceId={workspaceId}
        onOpenChange={setCreateOpen}
        onUploaded={handleCreateUploaded}
      />

      <SkillUploadDialog
        key={updateSkillTarget?.id ?? "update-skill-dialog"}
        mode="update"
        open={Boolean(updateSkillTarget)}
        skill={updateSkillTarget ?? undefined}
        workspaceId={workspaceId}
        onOpenChange={(open) => {
          if (!open) {
            setUpdateSkillTarget(null);
          }
        }}
        onUploaded={(resource) => {
          const uploadedSkillId = "skill_id" in resource ? resource.skill_id : updateSkillTarget?.id;
          if (uploadedSkillId) {
            handleVersionUploaded(uploadedSkillId);
          }
          setUpdateSkillTarget(null);
        }}
      />

      <DeleteSkillDialog
        skill={deleteSkillTarget}
        isDeleting={Boolean(deleteSkillTarget && deletingSkillId === deleteSkillTarget.id)}
        error={deleteSkillError}
        onOpenChange={(open) => {
          if (!open && !deletingSkillId) {
            setDeleteSkillTarget(null);
            setDeleteSkillError(null);
          }
        }}
        onConfirm={() => void handleDeleteSkill()}
      />
    </section>
  );
}

export function CreateSkillPage() {
  return <SkillsPage initialCreateOpen />;
}

export function SkillDetailPage({ skillId }: { skillId: string }) {
  return <SkillsPage initialSkillId={skillId} />;
}

function SkillsTable({
  skills,
  workspaceName,
  selectedSkillId,
  isLoading,
  isFetching,
  error,
  onRetry,
  onOpenSkill,
  onUpdateSkill,
  onDeleteSkill,
}: {
  skills: ConsoleSkill[];
  workspaceName: string;
  selectedSkillId: string | null;
  isLoading: boolean;
  isFetching: boolean;
  error: unknown;
  onRetry: () => void;
  onOpenSkill: (skillId: string) => void;
  onUpdateSkill: (skill: ConsoleSkill) => void;
  onDeleteSkill: (skill: ConsoleSkill) => void;
}) {
  const { locale, msg } = useI18n();

  return (
    <section aria-label={msg("skills.listAria", "Skills list")} className="min-w-0">
      <Table className={dataTableClassName}>
        <colgroup>
          <col className="w-[16%]" />
          <col className="w-[43%]" />
          <col className="w-[12%]" />
          <col className="w-[14%]" />
          <col className="w-[11%]" />
          <col className="w-[4%]" />
        </colgroup>
        <TableHeader>
          <TableRow className={dataTableHeaderRowClassName}>
            <TableHead className={cn(dataTableHeaderCellClassName, "truncate")}>
              {msg("skills.table.id", "ID")}
            </TableHead>
            <TableHead className={cn(dataTableHeaderCellClassName, "truncate")}>{msg("common.name", "Name")}</TableHead>
            <TableHead className={cn(dataTableHeaderCellClassName, "truncate")}>
              {msg("skills.table.source", "Source")}
            </TableHead>
            <TableHead className={cn(dataTableHeaderCellClassName, "truncate")}>
              {msg("skills.table.latest", "Latest version")}
            </TableHead>
            <TableHead className={cn(dataTableHeaderCellClassName, "truncate")}>
              {msg("common.updated", "Updated")}
            </TableHead>
            <TableHead
              className={cn(dataTableHeaderCellClassName, "truncate")}
              aria-label={msg("common.actions", "Actions")}
            />
          </TableRow>
        </TableHeader>
        <TableBody>
          {isLoading ? (
            <TableLoadingRow colSpan={6} label={msg("skills.loading", "Loading skills...")} />
          ) : error ? (
            <TableErrorRow
              colSpan={6}
              title={msg("skills.error", "Skills could not be loaded.")}
              message={errorMessage(error)}
              retryLabel={msg("common.retry", "Retry")}
              onRetry={onRetry}
            />
          ) : skills.length === 0 ? (
            <TableEmptyRow colSpan={6}>
              {msg("skills.empty", "No skills have been created in the {workspaceName} workspace.", { workspaceName })}
            </TableEmptyRow>
          ) : (
            skills.map((skill) => (
              <DataTableRow
                key={skill.id}
                clickable
                selected={selectedSkillId === skill.id}
                onClick={() => onOpenSkill(skill.id)}
              >
                <DataTableCell edge="start" className="min-w-0">
                  <CopyIdCell
                    value={skill.id}
                    displayValue={formatSkillId(skill.id)}
                    ariaLabel={msg("skills.copyAria", "Copy {skillId}", { skillId: skill.id })}
                    stopPropagation
                  />
                </DataTableCell>
                <DataTableCell className="truncate font-medium" title={skill.display_title || skill.id}>
                  {skill.display_title || skill.id}
                </DataTableCell>
                <DataTableCell className="truncate">
                  <SkillSourceBadge source={skill.source} />
                </DataTableCell>
                <DataTableCell className="truncate text-muted-foreground">
                  {formatSkillVersionDate(skill.latest_version, locale)}
                </DataTableCell>
                <DataTableCell className="truncate text-muted-foreground">
                  {formatSkillUpdatedAt(skill.updated_at || skill.created_at, locale, msg)}
                </DataTableCell>
                <DataTableCell edge="end" className="px-2 text-right">
                  {skill.source === "custom" ? (
                    <SkillActionsMenu skill={skill} onUpdateSkill={onUpdateSkill} onDeleteSkill={onDeleteSkill} />
                  ) : null}
                </DataTableCell>
              </DataTableRow>
            ))
          )}
        </TableBody>
      </Table>
      {isFetching && !isLoading ? <span className="sr-only">{msg("common.updating", "Updating...")}</span> : null}
    </section>
  );
}

function SkillDrawer({
  skillId,
  workspaceId,
  onClose,
  onUpdateSkill,
  onDeleteSkill,
  onVersionChanged,
}: {
  skillId: string | null;
  workspaceId: string;
  onClose: () => void;
  onUpdateSkill: (skill: ConsoleSkill) => void;
  onDeleteSkill: (skill: ConsoleSkill) => void;
  onVersionChanged: (skillId: string) => Promise<void>;
}) {
  const { locale, msg } = useI18n();
  const [versionToDelete, setVersionToDelete] = useState<ConsoleSkillVersion | null>(null);
  const [deletingVersion, setDeletingVersion] = useState(false);
  const [deleteVersionError, setDeleteVersionError] = useState<string | null>(null);
  const skillQuery = useQuery({
    queryKey: ["skill", workspaceId, skillId],
    queryFn: () => retrieveSkill(skillId || "", workspaceId),
    enabled: Boolean(skillId),
    retry: false,
  });
  const versionsQuery = useQuery({
    queryKey: ["skillVersions", workspaceId, skillId],
    queryFn: () => listSkillVersions(skillId || "", workspaceId),
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

  const handleDeleteVersion = async () => {
    if (!skill || !versionToDelete || deletingVersion) {
      return;
    }
    setDeletingVersion(true);
    setDeleteVersionError(null);
    try {
      await deleteSkillVersion(skill.id, versionToDelete.version, workspaceId);
      setVersionToDelete(null);
      await onVersionChanged(skill.id);
    } catch (err) {
      setDeleteVersionError(errorMessage(err));
    } finally {
      setDeletingVersion(false);
    }
  };

  return (
    <>
      <Sheet open={Boolean(skillId)} onOpenChange={(open) => !open && onClose()}>
        <SheetContent showCloseButton={false} showOverlay={false} side="right" className="gap-0 p-0 sm:!max-w-md">
          {skillQuery.isLoading ? (
            <div className="p-4 text-sm text-muted-foreground">
              <RefreshCw className="mr-2 inline size-4 animate-spin" aria-hidden />
              {msg("skills.detail.loading", "Loading skill...")}
            </div>
          ) : skillQuery.error || !skill ? (
            <div className="p-4">
              <Alert variant="destructive">
                <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
                <AlertTitle>{msg("skills.detail.error", "Skill could not be loaded.")}</AlertTitle>
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
                    {msg("common.retry", "Retry")}
                  </Button>
                </AlertDescription>
              </Alert>
            </div>
          ) : (
            <>
              <SheetHeader className="border-b border-border px-4 py-4 pr-12">
                <div className="flex min-w-0 items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex min-w-0 flex-wrap items-center gap-2">
                      <SheetTitle className="truncate">
                        {skill.display_title || latestVersion?.name || skill.id}
                      </SheetTitle>
                      <SkillSourceBadge source={skill.source} />
                    </div>
                    <SheetDescription className="mt-1 font-mono">
                      {formatSkillUpdatedAt(skill.updated_at || skill.created_at, locale, msg)}
                      <span className="mx-2 font-sans" aria-hidden>
                        ·
                      </span>
                      {formatSkillId(skill.id)}
                    </SheetDescription>
                  </div>
                  <div className="absolute right-4 top-4 flex items-center gap-1">
                    {skill.source === "custom" ? (
                      <SkillActionsMenu skill={skill} onUpdateSkill={onUpdateSkill} onDeleteSkill={onDeleteSkill} />
                    ) : null}
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      aria-label={msg("common.close", "Close")}
                      onClick={onClose}
                    >
                      <X className="size-4" aria-hidden />
                    </Button>
                  </div>
                </div>
              </SheetHeader>

              <div className="subtle-scrollbar flex-1 space-y-5 overflow-y-auto px-4 py-4">
                <section className="space-y-2">
                  <h2 className="text-sm font-medium text-foreground">{msg("common.description", "Description")}</h2>
                  <p className="text-sm leading-6 text-muted-foreground">
                    {latestVersion?.description || msg("common.noDescription", "No description provided.")}
                  </p>
                </section>

                <section className="space-y-3 border-t border-border pt-4">
                  <h2 className="text-sm font-medium text-foreground">{msg("skills.versions.title", "Versions")}</h2>
                  {versionsQuery.isLoading ? (
                    <div className="text-sm text-muted-foreground">
                      <RefreshCw className="mr-2 inline size-4 animate-spin" aria-hidden />
                      {msg("skills.versions.loading", "Loading versions...")}
                    </div>
                  ) : versionsQuery.error ? (
                    <Alert variant="destructive">
                      <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
                      <AlertTitle>{msg("skills.versions.error", "Versions could not be loaded.")}</AlertTitle>
                      <AlertDescription>{errorMessage(versionsQuery.error)}</AlertDescription>
                    </Alert>
                  ) : (
                    <div className="space-y-2">
                      {versions.map((version) => (
                        <div key={version.version} className="flex min-w-0 items-center gap-2">
                          <Badge variant="secondary" className="max-w-[180px] rounded-md font-mono">
                            <span className="truncate">{version.version}</span>
                          </Badge>
                          <span className="text-sm text-muted-foreground">
                            {formatSkillUpdatedAt(version.created_at, locale, msg)}
                          </span>
                          {version.version === skill.latest_version ? (
                            <Badge className="rounded-md bg-primary/20 text-primary">
                              {msg("skills.versions.latest", "Latest")}
                            </Badge>
                          ) : null}
                          {skill.source === "custom" ? (
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon-sm"
                              className="ml-auto text-muted-foreground"
                              aria-label={msg("skills.versions.deleteAria", "Delete version {version}", {
                                version: version.version,
                              })}
                              onClick={() => {
                                setDeleteVersionError(null);
                                setVersionToDelete(version);
                              }}
                            >
                              <Trash2 className="size-4" aria-hidden />
                            </Button>
                          ) : null}
                        </div>
                      ))}
                    </div>
                  )}
                </section>
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>

      <AlertDialog
        open={Boolean(versionToDelete)}
        onOpenChange={(open) => {
          if (!open && !deletingVersion) {
            setVersionToDelete(null);
            setDeleteVersionError(null);
          }
        }}
      >
        <AlertDialogContent size="default">
          <AlertDialogHeader>
            <AlertDialogTitle>
              {msg("skills.deleteVersion.title", "Confirm deleting version {version}", {
                version: versionToDelete?.version ?? "",
              })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {msg(
                "skills.deleteVersion.description",
                "This version will no longer be available. This action is permanent and cannot be undone.",
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {deleteVersionError ? (
            <Alert variant="destructive">
              <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
              <AlertTitle>{msg("skills.deleteVersion.error", "Version could not be deleted.")}</AlertTitle>
              <AlertDescription>{deleteVersionError}</AlertDescription>
            </Alert>
          ) : null}
          <AlertDialogFooter>
            <AlertDialogCancel variant="secondary" disabled={deletingVersion}>
              {msg("common.cancel", "Cancel")}
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={deletingVersion}
              onClick={(event) => {
                event.preventDefault();
                void handleDeleteVersion();
              }}
            >
              {deletingVersion ? msg("common.deleting", "Deleting...") : msg("common.delete", "Delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function SkillUploadDialog({
  mode,
  open,
  workspaceId,
  skill,
  onOpenChange,
  onUploaded,
}: {
  mode: UploadMode;
  open: boolean;
  workspaceId: string;
  skill?: ConsoleSkill;
  onOpenChange: (open: boolean) => void;
  onUploaded: (resource: ConsoleSkill | ConsoleSkillVersion) => void;
}) {
  const { msg } = useI18n();
  const inputRef = useRef<HTMLInputElement | null>(null);
  const directoryInputRef = useRef<HTMLInputElement | null>(null);
  const [selection, setSelection] = useState<UploadSelection | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);

  useEffect(() => {
    if (!open) {
      setSelection(null);
      setError(null);
      setIsDragging(false);
      setIsSubmitting(false);
      if (inputRef.current) {
        inputRef.current.value = "";
      }
      if (directoryInputRef.current) {
        directoryInputRef.current.value = "";
      }
    }
  }, [open]);

  useEffect(() => {
    directoryInputRef.current?.setAttribute("webkitdirectory", "");
    directoryInputRef.current?.setAttribute("directory", "");
  }, [open]);

  const selectFiles = async (files: File[]) => {
    const result = validateUploadFiles(files, msg);
    setSelection(result.selection);
    setError(result.error);
  };

  const handleDrop = async (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault();
    setIsDragging(false);
    const files = await filesFromDataTransfer(event.dataTransfer);
    await selectFiles(files);
  };

  const handleSubmit = async () => {
    if (!selection || error || isSubmitting) {
      return;
    }
    setIsSubmitting(true);
    try {
      const resource = await createSkillPackage(skill?.id, selection.files, workspaceId, selection.displayTitle);
      onUploaded(resource);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setIsSubmitting(false);
    }
  };

  const title = mode === "create" ? msg("skills.create", "Create skill") : msg("skills.update.title", "Update Skill");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[425px]" showCloseButton>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {mode === "update" ? (
            <DialogDescription>
              {msg(
                "skills.update.description",
                "Upload new files to create a new version of this skill. The version number will be automatically generated.",
              )}
            </DialogDescription>
          ) : null}
        </DialogHeader>

        <input
          ref={inputRef}
          type="file"
          accept=".zip,.skill"
          className="hidden"
          onChange={(event) => void selectFiles(Array.from(event.currentTarget.files ?? []))}
        />
        <input
          ref={directoryInputRef}
          type="file"
          multiple
          className="hidden"
          onChange={(event) => void selectFiles(Array.from(event.currentTarget.files ?? []))}
        />

        {selection ? (
          <div className="rounded-lg border border-border bg-background p-3">
            <div className="flex items-center gap-3">
              <span className="grid size-10 shrink-0 place-items-center rounded-md bg-secondary">
                <FileArchive className="size-5 text-muted-foreground" aria-hidden />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate text-sm font-medium text-primary">{selection.label}</span>
                  <Check className="size-4 shrink-0 text-primary" aria-hidden />
                </div>
                <p className="mt-1 text-xs leading-tight text-muted-foreground">
                  {msg("skills.upload.summary", "{count, plural, one {# file} other {# files}}", {
                    count: selection.fileCount,
                  })}
                  <span className="mx-2" aria-hidden>
                    •
                  </span>
                  {formatBytes(selection.size)}
                </p>
              </div>
              <Button
                type="button"
                variant="ghost"
                size="icon-lg"
                aria-label={msg("skills.upload.remove", "Remove upload")}
                onClick={() => {
                  setSelection(null);
                  setError(null);
                  if (inputRef.current) {
                    inputRef.current.value = "";
                  }
                  if (directoryInputRef.current) {
                    directoryInputRef.current.value = "";
                  }
                }}
              >
                <Trash2 className="size-4" aria-hidden />
              </Button>
            </div>
          </div>
        ) : (
          <div
            role="button"
            tabIndex={0}
            className={cn(
              "grid min-h-32 cursor-pointer place-items-center rounded-lg border border-dashed border-border bg-background/30 p-4 text-center outline-none transition hover:bg-secondary/40 focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/40",
              isDragging && "border-primary bg-primary/5",
            )}
            onClick={() => inputRef.current?.click()}
            onKeyDown={(event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                inputRef.current?.click();
              }
            }}
            onDragEnter={(event) => {
              event.preventDefault();
              setIsDragging(true);
            }}
            onDragOver={(event) => {
              event.preventDefault();
              setIsDragging(true);
            }}
            onDragLeave={() => setIsDragging(false)}
            onDrop={(event) => void handleDrop(event)}
          >
            <div className="space-y-3 text-muted-foreground">
              <FolderPlus className="mx-auto size-8" aria-hidden />
              <p className="text-sm leading-5">
                {msg("skills.upload.dropzone", "Drag and drop a .zip, .skill file, or directory to upload")}
              </p>
              <div className="flex justify-center gap-2">
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  onClick={(event) => {
                    event.stopPropagation();
                    inputRef.current?.click();
                  }}
                >
                  {msg("skills.upload.chooseFile", "Choose file")}
                </Button>
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  onClick={(event) => {
                    event.stopPropagation();
                    directoryInputRef.current?.click();
                  }}
                >
                  {msg("skills.upload.chooseDirectory", "Choose directory")}
                </Button>
              </div>
            </div>
          </div>
        )}

        {error ? (
          <Alert variant="destructive">
            <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
            <AlertTitle>{msg("skills.upload.error", "Upload could not continue.")}</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        <DialogFooter className="items-start sm:items-center sm:justify-between">
          <p className="max-w-[260px] text-xs leading-5 text-muted-foreground">
            {msg("skills.upload.limit", "Total file size limit: 8MB.")}{" "}
            <a href="https://docs.anthropic.com/" className="underline underline-offset-4">
              {msg("skills.upload.fileFormat", "File format")}
            </a>
            {" · "}
            <a href="https://docs.anthropic.com/" className="underline underline-offset-4">
              {msg("skills.upload.example", "download an example.")}
            </a>
          </p>
          <Button
            type="button"
            disabled={!selection || Boolean(error) || isSubmitting}
            onClick={() => void handleSubmit()}
          >
            {isSubmitting ? msg("skills.upload.uploading", "Uploading...") : msg("common.continue", "Continue")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SkillActionsMenu({
  skill,
  onUpdateSkill,
  onDeleteSkill,
}: {
  skill: ConsoleSkill;
  onUpdateSkill: (skill: ConsoleSkill) => void;
  onDeleteSkill: (skill: ConsoleSkill) => void;
}) {
  const { msg } = useI18n();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <MoreActionsButton label={msg("common.actions", "Actions")} onClick={(event) => event.stopPropagation()} />
        }
      />
      <DropdownMenuContent align="end" sideOffset={8} className="min-w-40">
        <DropdownMenuItem
          onClick={(event) => {
            event.stopPropagation();
            onUpdateSkill(skill);
          }}
        >
          <Upload className="size-4" aria-hidden />
          {msg("skills.update.action", "Update")}
        </DropdownMenuItem>
        <DropdownMenuItem
          variant="destructive"
          onClick={(event) => {
            event.stopPropagation();
            onDeleteSkill(skill);
          }}
        >
          <Trash2 className="size-4" aria-hidden />
          {msg("common.delete", "Delete")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function DeleteSkillDialog({
  skill,
  isDeleting,
  error,
  onOpenChange,
  onConfirm,
}: {
  skill: ConsoleSkill | null;
  isDeleting: boolean;
  error: string | null;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  const { msg } = useI18n();
  const name = skill?.display_title || skill?.id || "";
  return (
    <AlertDialog open={Boolean(skill)} onOpenChange={onOpenChange}>
      <AlertDialogContent size="default">
        <AlertDialogHeader>
          <AlertDialogTitle>{msg("skills.delete.title", "Confirm deleting {name}", { name })}</AlertDialogTitle>
          <AlertDialogDescription>
            {msg(
              "skills.delete.description",
              "Are you sure you want to delete {name} skill? Existing code references will break immediately. This action is permanent and cannot be undone.",
              { name },
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error ? (
          <Alert variant="destructive">
            <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
            <AlertTitle>{msg("skills.delete.error", "Skill could not be deleted.")}</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel variant="secondary" disabled={isDeleting}>
            {msg("common.cancel", "Cancel")}
          </AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={isDeleting}
            onClick={(event) => {
              event.preventDefault();
              onConfirm();
            }}
          >
            {isDeleting ? msg("common.deleting", "Deleting...") : msg("common.delete", "Delete")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function SkillSourceBadge({ source }: { source: string }) {
  const { msg } = useI18n();
  const label = formatSkillSource(source, msg);
  const anthropic = source.trim().toLowerCase() === "anthropic";
  return (
    <Badge
      variant="secondary"
      className={cn("rounded-md", anthropic ? "bg-blue-500/20 text-blue-300" : "bg-secondary text-muted-foreground")}
    >
      {label}
    </Badge>
  );
}

function useSkillSearchParam(
  initialSkillId?: string,
): [string | null, (skillId: string | null, replace?: boolean) => void] {
  const readCurrent = useCallback(() => {
    if (typeof window === "undefined") {
      return initialSkillId || null;
    }
    return new URLSearchParams(window.location.search).get("skill") || initialSkillId || null;
  }, [initialSkillId]);
  const [skillId, setSkillId] = useState<string | null>(() => readCurrent());

  useEffect(() => {
    const handlePopState = () => setSkillId(readCurrent());
    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, [readCurrent]);

  const updateSkillId = useCallback((nextSkillId: string | null, replace = false) => {
    const params = new URLSearchParams(window.location.search);
    if (nextSkillId) {
      params.set("skill", nextSkillId);
    } else {
      params.delete("skill");
    }
    const search = params.toString();
    const basePath = skillsIndexHref();
    const nextURL = search ? `${basePath}?${search}` : basePath;
    if (replace) {
      window.history.replaceState(null, "", nextURL);
    } else {
      window.history.pushState(null, "", nextURL);
    }
    setSkillId(nextSkillId);
  }, []);

  return [skillId, updateSkillId];
}

function validateUploadFiles(files: File[], msg: I18nMsg): { selection: UploadSelection | null; error: string | null } {
  if (!files.length) {
    return { selection: null, error: null };
  }
  if (files.some((file) => file.size === 0)) {
    return { selection: null, error: msg("skills.upload.errors.emptyFile", "Skill package files cannot be empty.") };
  }
  const size = files.reduce((total, file) => total + file.size, 0);
  if (size > maxUploadBytes) {
    return { selection: null, error: msg("skills.upload.errors.tooLarge", "Skill package exceeds maximum size.") };
  }
  if (files.length === 1 && isSkillArchive(files[0].name)) {
    const label = files[0].name;
    return {
      selection: {
        files,
        label,
        displayTitle: label.replace(/\.(zip|skill)$/i, ""),
        fileCount: 1,
        size,
      },
      error: null,
    };
  }

  const paths = files.map(skillFilePath);
  const topLevel = new Set(paths.map((path) => path.split("/")[0]).filter(Boolean));
  if (topLevel.size !== 1) {
    return {
      selection: null,
      error: msg("skills.upload.errors.singleTopLevel", "All skill files must be under a single top-level directory."),
    };
  }
  const directory = Array.from(topLevel)[0];
  if (!paths.includes(`${directory}/SKILL.md`)) {
    return {
      selection: null,
      error: msg(
        "skills.upload.errors.missingSkillMd",
        "SKILL.md not found. File name must be in all caps (SKILL.md) and located in the top-level folder.",
      ),
    };
  }
  return {
    selection: {
      files,
      label: directory,
      displayTitle: directory,
      fileCount: files.length,
      size,
    },
    error: null,
  };
}

async function filesFromDataTransfer(dataTransfer: DataTransfer) {
  const entries = Array.from(dataTransfer.items ?? [])
    .map((item) => {
      const entry = (item as DataTransferItem & { webkitGetAsEntry?: () => unknown }).webkitGetAsEntry?.();
      return entry as SkillFileSystemEntry | null | undefined;
    })
    .filter((entry): entry is SkillFileSystemEntry => Boolean(entry));
  if (entries.length) {
    const nested = await Promise.all(entries.map((entry) => filesFromEntry(entry, "")));
    return nested.flat();
  }
  return Array.from(dataTransfer.files ?? []);
}

async function filesFromEntry(entry: SkillFileSystemEntry, parentPath: string): Promise<File[]> {
  const entryPath = `${parentPath}${entry.name}`;
  if (entry.isFile && entry.file) {
    const file = await new Promise<File>((resolve, reject) => {
      entry.file?.(resolve, reject);
    });
    (file as SkillFile).__skillPath = entryPath;
    return [file];
  }
  if (!entry.isDirectory || !entry.createReader) {
    return [];
  }
  const reader = entry.createReader();
  const children: SkillFileSystemEntry[] = [];
  for (;;) {
    const batch = await new Promise<SkillFileSystemEntry[]>((resolve, reject) => {
      reader.readEntries(resolve, reject);
    });
    if (!batch.length) {
      break;
    }
    children.push(...batch);
  }
  const nested = await Promise.all(children.map((child) => filesFromEntry(child, `${entryPath}/`)));
  return nested.flat();
}

function skillFilePath(file: File) {
  return ((file as SkillFile).__skillPath || file.webkitRelativePath || file.name).replace(/\\/g, "/");
}

function isSkillArchive(name: string) {
  return /\.(zip|skill)$/i.test(name.trim());
}

function formatSkillId(skillId: string) {
  if (skillId.length <= 16) {
    return skillId;
  }
  return `${skillId.slice(0, 9)}...${skillId.slice(-6)}`;
}

function formatSkillVersionDate(version: string, locale: string) {
  if (/^\d{8}$/.test(version)) {
    const year = Number(version.slice(0, 4));
    const month = Number(version.slice(4, 6));
    const day = Number(version.slice(6, 8));
    return formatDate(new Date(Date.UTC(year, month - 1, day)), locale);
  }
  if (/^\d{13,16}$/.test(version)) {
    return formatDate(new Date(Number(version) / 1000), locale);
  }
  return version || "-";
}

function formatDate(date: Date, locale: string) {
  if (!Number.isFinite(date.getTime())) {
    return "-";
  }
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    year: "numeric",
  }).format(date);
}

function formatSkillUpdatedAt(value: string, locale: string, msg: I18nMsg) {
  const timestamp = Date.parse(value);
  if (Number.isFinite(timestamp) && Math.abs(Date.now() - timestamp) < 60_000) {
    return msg("common.justNow", "just now");
  }
  return formatRelativeTime(value, locale, msg("common.justNow", "just now"));
}

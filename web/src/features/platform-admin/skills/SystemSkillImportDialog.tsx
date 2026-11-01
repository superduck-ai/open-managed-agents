import { useRef, useState } from 'react';
import { FileArchive, FolderOpen, Upload, X } from 'lucide-react';
import { toast } from 'sonner';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Input } from '@/shared/ui/input';
import { Checkbox } from '@/shared/ui/checkbox';
import { Badge } from '@/shared/ui/badge';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/shared/ui/dialog';
import { usePlatformAdmin } from '../context';
import { systemSkillErrorMessage } from './feedback';
import type { SystemPackage, SystemSkill } from './types';

type ImportItem = {
  key: string;
  label: string;
  files: File[];
  directory: boolean;
  preview?: SystemPackage;
  version: string;
  title?: string;
  error?: string;
  versionError?: string;
  done: boolean;
};
const maxUploadSize = 8 * 1024 * 1024;

export function SystemSkillImportDialog({
  target,
  onClose,
  onImported,
}: {
  target?: SystemSkill;
  onClose: () => void;
  onImported: (id: string) => Promise<void>;
}) {
  const { msg } = useI18n();
  const { client } = usePlatformAdmin();
  const input = useRef<HTMLInputElement>(null);
  const directoryInput = useRef<HTMLInputElement>(null);
  const [items, setItems] = useState<ImportItem[]>([]);
  const [step, setStep] = useState<'select' | 'review' | 'confirm'>('select');
  const [busy, setBusy] = useState(false);
  const [publish, setPublish] = useState(!target);
  const [error, setError] = useState('');
  const [finished, setFinished] = useState(false);
  const update = (key: string, patch: Partial<ImportItem>) =>
    setItems((current) => current.map((item) => (item.key === key ? { ...item, ...patch } : item)));
  const inspect = async (item: ImportItem): Promise<ImportItem> => {
    if (
      (!item.directory && !item.label.toLowerCase().endsWith('.zip')) ||
      item.files.reduce((size, file) => size + file.size, 0) > maxUploadSize
    )
      return {
        ...item,
        error: msg('systemSkills.uploadLimit', 'Choose a ZIP package or skill folder totaling at most 8 MiB.'),
      };
    if (item.directory && !item.files.some((file) => file.webkitRelativePath === `${item.label}/SKILL.md`))
      return {
        ...item,
        error: msg('systemSkills.folderRoot', 'Choose one skill folder containing SKILL.md at its root.'),
      };
    const body = systemPackageBody(item);
    if (target) body.append('skill_id', target.id);
    try {
      const preview = await client.request<SystemPackage>('/skills/inspect', { method: 'POST', body });
      return {
        ...item,
        preview,
        title: preview.existing?.display_title ?? preview.name,
        version: item.version || (preview.existing || target ? '' : '1.0.0'),
        error: undefined,
      };
    } catch (cause) {
      return { ...item, preview: undefined, error: systemSkillErrorMessage(cause) };
    }
  };
  const select = async (files: File[], directory = false) => {
    if (!files.length || busy) return;
    const selected: ImportItem[] = directory
      ? [
          {
            key: crypto.randomUUID(),
            label: (files[0].webkitRelativePath || files[0].name).split('/')[0],
            files,
            directory: true,
            version: '',
            done: false,
          },
        ]
      : files.map((file) => ({
          key: crypto.randomUUID(),
          label: file.name,
          files: [file],
          directory: false,
          version: '',
          done: false,
        }));
    if (selected.length > 20) {
      setError(msg('systemSkills.batchLimit', 'Import at most 20 ZIP packages at a time.'));
      return;
    }
    setBusy(true);
    setError('');
    setStep('review');
    setItems(selected);
    try {
      for (const item of selected) update(item.key, await inspect(item));
    } finally {
      setBusy(false);
    }
  };
  const retry = async (item: ImportItem) => {
    setBusy(true);
    try {
      update(item.key, await inspect(item));
    } finally {
      setBusy(false);
    }
  };
  const submit = async () => {
    if (busy || finished) return;
    setBusy(true);
    setError('');
    const revisions = new Map<string, number>();
    let failed = false;
    let lastID = '';
    try {
      for (const item of items.filter((entry) => !entry.done && entry.preview)) {
        const preview = item.preview!;
        const body = systemImportBody(item, preview, publish, revisions, target);
        try {
          const result = await client.request<{ skill: SystemSkill; duplicate: boolean }>('/skills', {
            method: 'POST',
            body,
          });
          if (result.duplicate)
            toast.info(
              msg('systemSkills.alreadyExists', '{name} {version} already exists. No changes were made.', {
                name: preview.name,
                version: item.version,
              }),
            );
          revisions.set(result.skill.id, result.skill.revision);
          update(item.key, { done: true, error: undefined });
          lastID = result.skill.id;
        } catch (cause) {
          failed = true;
          update(item.key, { error: systemSkillErrorMessage(cause) });
        }
      }
      if (failed) {
        await onImported('');
        setStep('review');
        return;
      }
      if (lastID) {
        setFinished(true);
        await onImported(lastID);
        onClose();
      }
    } catch (cause) {
      setError(systemSkillErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  };
  const continueImport = () => {
    const invalid = items.find((item) => !item.done && !validSystemVersion(item.version));
    if (invalid) {
      update(invalid.key, {
        versionError: invalid.version.trim()
          ? msg('systemSkills.invalidVersion', 'Invalid version identifier')
          : msg('systemSkills.requiredVersion', 'Enter a version'),
      });
      document.getElementById(`system-skill-version-${invalid.key}`)?.focus();
      return;
    }
    setStep('confirm');
  };
  const canContinue = systemImportReady(items, busy);
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <DialogContent className="flex max-h-[90dvh] flex-col sm:max-w-2xl" closeButtonDisabled={busy}>
        <DialogHeader>
          <DialogTitle>
            {target
              ? msg('systemSkills.uploadVersion', 'Upload new version')
              : msg('systemSkills.import', 'Import skills')}
          </DialogTitle>
          {target && <DialogDescription>{target.display_title}</DialogDescription>}
        </DialogHeader>
        <div className="min-h-0 flex-1 overflow-y-auto pr-1">
          <p className="mb-4 text-xs text-muted-foreground">
            {step === 'select'
              ? msg('systemSkills.stepSelect', '1. Select files')
              : step === 'review'
                ? msg('systemSkills.stepReview', '2. Check packages and versions')
                : msg('systemSkills.stepConfirm', '3. Confirm import')}
          </p>
          {step === 'select' ? (
            <div className="grid min-h-48 place-items-center rounded-lg border-[0.5px] border-dashed border-border p-6 text-center">
              <div>
                <FileArchive className="mx-auto mb-3 size-9 text-muted-foreground" aria-hidden />
                <div className="flex flex-wrap justify-center gap-2">
                  <Button variant="outline" onClick={() => input.current?.click()}>
                    <Upload aria-hidden className="size-4" />
                    {msg('systemSkills.chooseZip', 'Choose ZIP packages')}
                  </Button>
                  <Button variant="outline" onClick={() => directoryInput.current?.click()}>
                    <FolderOpen aria-hidden className="size-4" />
                    {msg('systemSkills.chooseFolder', 'Choose skill folder')}
                  </Button>
                </div>
                <p className="mt-3 text-xs text-muted-foreground">
                  {msg('systemSkills.uploadLimit', 'Choose a ZIP package or skill folder totaling at most 8 MiB.')}
                </p>
              </div>
            </div>
          ) : (
            <div className="space-y-3">
              {items.map((item) => (
                <SystemImportItem
                  key={item.key}
                  item={item}
                  busy={busy}
                  step={step}
                  onRemove={() => setItems(items.filter((entry) => entry.key !== item.key))}
                  onChange={(patch) => update(item.key, patch)}
                  onRetry={() => void retry(item)}
                />
              ))}
            </div>
          )}
          {step === 'confirm' && (
            <>
              <label className="my-4 flex items-center gap-2 text-sm">
                <Checkbox checked={publish} disabled={busy} onCheckedChange={(value) => setPublish(!!value)} />
                {msg('systemSkills.publishOnImport', 'Publish immediately after import')}
              </label>
              <p className="text-xs text-muted-foreground">
                {publish
                  ? msg('systemSkills.importImpact', 'Available to all workspaces after publishing.')
                  : msg('systemSkills.draftImpact', 'Drafts are hidden from workspaces.')}
              </p>
            </>
          )}
          <input
            ref={input}
            type="file"
            accept=".zip,application/zip"
            multiple={!target}
            className="hidden"
            aria-label={msg('systemSkills.chooseZip', 'Choose ZIP packages')}
            onChange={(event) => void select(Array.from(event.target.files ?? []))}
          />
          <input
            ref={(node) => {
              directoryInput.current = node;
              node?.setAttribute('webkitdirectory', '');
              node?.setAttribute('directory', '');
            }}
            type="file"
            multiple
            className="hidden"
            aria-label={msg('systemSkills.chooseFolder', 'Choose skill folder')}
            onChange={(event) => void select(Array.from(event.target.files ?? []), true)}
          />
          {error && (
            <p role="alert" className="mt-4 text-sm text-destructive">
              {error}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => (step === 'select' ? onClose() : setStep(step === 'confirm' ? 'review' : 'select'))}
          >
            {step === 'select' ? msg('common.cancel', 'Cancel') : msg('common.previous', 'Previous')}
          </Button>
          {step === 'review' && (
            <Button disabled={!canContinue} onClick={continueImport}>
              {msg('common.next', 'Next')}
            </Button>
          )}
          {step === 'confirm' && (
            <Button disabled={busy || finished} onClick={() => void submit()}>
              {busy
                ? msg('common.saving', 'Saving...')
                : publish
                  ? msg('systemSkills.confirmImport', 'Import and publish')
                  : msg('systemSkills.saveDraft', 'Save drafts')}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SystemImportItem({
  item,
  busy,
  step,
  onRemove,
  onChange,
  onRetry,
}: {
  item: ImportItem;
  busy: boolean;
  step: 'select' | 'review' | 'confirm';
  onRemove: () => void;
  onChange: (patch: Partial<ImportItem>) => void;
  onRetry: () => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="border-b-[0.5px] border-border py-3">
      <div className="flex items-center gap-2">
        <FileArchive aria-hidden className="size-4 shrink-0" />
        <span className="min-w-0 flex-1 truncate text-sm" title={item.label}>
          {item.label}
        </span>
        <Badge variant={item.error ? 'destructive' : 'secondary'}>
          {item.done
            ? msg('systemSkills.imported', 'Imported')
            : item.error
              ? msg('systemSkills.failed', 'Failed')
              : item.preview
                ? msg('systemSkills.checked', 'Checked')
                : msg('systemSkills.checking', 'Checking...')}
        </Badge>
        {step === 'review' && !item.done && (
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={busy}
            aria-label={msg('systemSkills.removeFile', 'Remove {file}', { file: item.label })}
            onClick={onRemove}
          >
            <X />
          </Button>
        )}
      </div>
      {item.preview && (
        <div className="mt-3 flex flex-wrap items-center gap-3">
          <span className="text-xs text-muted-foreground">
            {item.preview.name} /{' '}
            {item.preview.existing
              ? msg('systemSkills.newVersion', 'New version')
              : msg('systemSkills.newSkill', 'New skill')}
          </span>
          {step === 'review' && (
            <label className="flex items-center gap-2 text-xs">
              {msg('common.name', 'Name')}
              <Input
                className="h-7 w-44"
                disabled={busy || item.done || !!item.preview.existing}
                maxLength={256}
                value={item.title ?? ''}
                onChange={(event) => onChange({ title: event.target.value })}
              />
            </label>
          )}
          <div className="space-y-1">
            <label className="flex items-center gap-2 text-xs">
              {msg('systemSkills.version', 'Version')}
              <Input
                id={`system-skill-version-${item.key}`}
                className="h-7 w-36"
                aria-label={msg('systemSkills.versionFor', 'Version for {file}', { file: item.label })}
                aria-required
                aria-invalid={!!item.versionError}
                aria-describedby={item.versionError ? `system-skill-version-error-${item.key}` : undefined}
                disabled={busy || item.done || step === 'confirm'}
                value={item.version}
                placeholder={msg('systemSkills.requiredVersion', 'Enter a version')}
                onChange={(event) =>
                  onChange({ version: event.target.value, error: undefined, versionError: undefined })
                }
              />
            </label>
            {item.versionError && (
              <p id={`system-skill-version-error-${item.key}`} role="alert" className="text-xs text-destructive">
                {item.versionError}
              </p>
            )}
          </div>
        </div>
      )}
      {item.error && (
        <div className="mt-2 text-sm text-destructive" role="alert">
          {item.error}
          <Button size="sm" variant="link" disabled={busy} onClick={onRetry}>
            {msg('systemSkills.recheck', 'Check again')}
          </Button>
        </div>
      )}
    </div>
  );
}

function systemImportBody(
  item: ImportItem,
  preview: SystemPackage,
  publish: boolean,
  revisions: Map<string, number>,
  target?: SystemSkill,
) {
  const body = systemPackageBody(item);
  const existing = preview.existing ?? target;
  body.append('version', item.version.trim());
  body.append('display_title', item.title?.trim() || preview.name);
  body.append('publish', String(publish));
  body.append('revision', String(revisions.get(existing?.id ?? preview.name) ?? existing?.revision ?? 0));
  if (existing) body.append('skill_id', existing.id);
  return body;
}

function systemPackageBody(item: ImportItem) {
  const body = new FormData();
  for (const file of item.files) body.append('files[]', file, file.webkitRelativePath || file.name);
  return body;
}

function systemImportReady(items: ImportItem[], busy: boolean) {
  const pendingItems = items.filter((item) => !item.done);
  return !busy && pendingItems.length > 0 && pendingItems.every((item) => item.preview && !item.error);
}

function validSystemVersion(version: string) {
  const value = version.trim();
  return /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/.test(value) && value !== 'latest';
}

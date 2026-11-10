import { useEffect, useRef, useState } from 'react';
import { FileArchive, FolderOpen, Upload, X } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Input } from '@/shared/ui/input';
import { Dialog, DialogContent, DialogTitle, DialogDescription, DialogFooter } from '@/shared/ui/dialog';
import { SkillIconPicker } from './icons';
import { filesFromDataTransfer, skillFilePath } from './files';
import { skillErrorMessage } from './errors';

export type SkillImportMetadata = { id: string; name: string; existing: boolean; iconKey?: string; revision?: number };
export type SkillImportEntry = {
  key: string;
  label: string;
  files: File[];
  directory: boolean;
  name: string;
  iconKey: string;
  metadata?: SkillImportMetadata;
  error?: string;
  done: boolean;
};
type ImportResult = { id: string; revision?: number };

export function SkillImportDialog({
  onClose,
  onInspect,
  onImport,
  onCompleted,
  multiple = true,
  title,
  submitLabel,
  initialIconKey = 'blocks',
}: {
  onClose: () => void;
  onInspect: (entry: SkillImportEntry) => Promise<SkillImportMetadata>;
  onImport: (entry: SkillImportEntry) => Promise<ImportResult>;
  onCompleted: (id: string) => Promise<void>;
  multiple?: boolean;
  title?: string;
  submitLabel?: string;
  initialIconKey?: string;
}) {
  const { msg } = useI18n();
  const active = useRef(true);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  const fileInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  const [phase, setPhase] = useState<'select' | 'configure'>('select');
  const [entries, setEntries] = useState<SkillImportEntry[]>([]);
  const [busy, setBusy] = useState(false);
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState('');
  const [open, setOpen] = useState(true);
  const completedID = useRef('');
  const revisions = useRef(new Map<string, number>());
  const update = (key: string, patch: Partial<SkillImportEntry>) =>
    setEntries((current) => current.map((entry) => (entry.key === key ? { ...entry, ...patch } : entry)));
  const inspect = async (entry: SkillImportEntry) => {
    update(entry.key, { error: undefined, metadata: undefined });
    try {
      validateSelection(entry);
      const metadata = await onInspect(entry);
      if (!active.current) return;
      update(entry.key, {
        metadata,
        name: entry.name || metadata.name,
        iconKey: entry.iconKey === initialIconKey ? metadata.iconKey || initialIconKey : entry.iconKey,
      });
      if (metadata.revision !== undefined) revisions.current.set(metadata.id, metadata.revision);
    } catch (cause) {
      update(entry.key, { error: skillErrorMessage(cause, msg) });
    }
  };
  function validateSelection(entry: SkillImportEntry) {
    if (!entry.directory && !/\.(zip|skill)$/i.test(entry.label))
      throw new Error(msg('skills.import.invalidType', 'Choose a ZIP or SKILL package.'));
    if (!entry.directory && entry.files.some((file) => file.size === 0))
      throw new Error(msg('skills.upload.errors.emptyFile', 'Skill package files cannot be empty.'));
    if (entry.files.reduce((sum, file) => sum + file.size, 0) > 8 * 1024 * 1024)
      throw new Error(msg('skills.upload.errors.tooLarge', 'Skill package exceeds maximum size.'));
    if (entry.directory && !entry.files.some((file) => skillFilePath(file) === `${entry.label}/SKILL.md`))
      throw new Error(msg('skills.import.missingSkill', 'The skill folder must contain SKILL.md at its root.'));
  }
  const select = async (files: File[], directory = false) => {
    if (!files.length || busy || checking) return;
    setError('');
    if (!directory && files.length > (multiple ? 20 : 1)) {
      setError(msg('skills.import.limit', 'Choose at most {count} skill packages.', { count: multiple ? 20 : 1 }));
      return;
    }
    const selected: SkillImportEntry[] = (directory ? [files] : files.map((file) => [file])).map((group) => ({
      key: crypto.randomUUID(),
      label: directory ? skillFilePath(group[0]).split('/')[0] : group[0].name,
      files: group,
      directory,
      name: '',
      iconKey: initialIconKey,
      done: false,
    }));
    revisions.current.clear();
    setEntries(selected);
    setPhase('configure');
    setChecking(true);
    try {
      for (const entry of selected) {
        if (!active.current) return;
        await inspect(entry);
      }
    } finally {
      setChecking(false);
    }
  };
  const submit = async () => {
    if (busy || checking) return;
    setBusy(true);
    setError('');
    let lastID = '';
    let failed = false;
    try {
      for (const entry of entries.filter((item) => !item.done)) {
        if (!active.current) return;
        try {
          const metadata = entry.metadata!;
          const result = await onImport({
            ...entry,
            metadata: { ...metadata, revision: revisions.current.get(metadata.id) ?? metadata.revision },
          });
          if (result.revision !== undefined) revisions.current.set(result.id, result.revision);
          update(entry.key, { done: true });
          lastID = result.id;
        } catch (cause) {
          failed = true;
          update(entry.key, { error: skillErrorMessage(cause, msg) });
        }
      }
      if (!active.current) return;
      if (failed) await onCompleted('');
      else {
        completedID.current = lastID;
        setOpen(false);
      }
    } catch (cause) {
      setError(skillErrorMessage(cause, msg));
    } finally {
      setBusy(false);
    }
  };
  const pending = entries.filter((entry) => !entry.done);
  const ready = pending.length > 0 && pending.every((entry) => entry.metadata && !entry.error && entry.name.trim());
  return (
    <Dialog
      open={open}
      onOpenChange={(next, details) => {
        if (!next && (busy || checking)) {
          details.cancel();
          return;
        }
        setOpen(next);
      }}
      onOpenChangeComplete={(next) => {
        if (!next) {
          if (completedID.current) void onCompleted(completedID.current);
          onClose();
        }
      }}
    >
      <DialogContent
        closeButtonDisabled={busy || checking}
        className="flex max-h-[calc(100dvh-2rem)] flex-col gap-5 sm:max-w-lg"
      >
        <DialogTitle>{title || msg('skills.import.title', 'Import skills')}</DialogTitle>
        <DialogDescription className="sr-only">
          {msg(
            'skills.import.accessibleDescription',
            'Choose a skill package, check its contents, then configure its icon and name.',
          )}
        </DialogDescription>
        <div className="min-h-0 overflow-y-auto">
          {phase === 'select' ? (
            <div
              className="skill-import-selection"
              onDragOver={(event) => event.preventDefault()}
              onDrop={async (event) => {
                event.preventDefault();
                try {
                  const files = await filesFromDataTransfer(event.dataTransfer);
                  await select(
                    files,
                    files.some((file) => skillFilePath(file).includes('/')),
                  );
                } catch (cause) {
                  setError(skillErrorMessage(cause, msg));
                }
              }}
            >
              <FileArchive size={24} strokeWidth={1.5} aria-hidden />
              <div className="flex flex-wrap justify-center gap-2">
                <Button variant="outline" size="sm" onClick={() => fileInput.current?.click()}>
                  <Upload size={14} aria-hidden />
                  {msg('skills.import.choosePackage', 'Choose skill package')}
                </Button>
                <Button variant="outline" size="sm" onClick={() => folderInput.current?.click()}>
                  <FolderOpen size={14} aria-hidden />
                  {msg('systemSkills.chooseFolder', 'Choose skill folder')}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                {msg('skills.import.formats', 'ZIP, SKILL or skill folder, up to 8 MiB')}
              </p>
            </div>
          ) : (
            entries.map((entry) => (
              <div className="skill-import-row" key={entry.key}>
                <div className="skill-import-file">
                  <FileArchive size={15} className="shrink-0" aria-hidden />
                  <span className="min-w-0 flex-1 truncate" title={entry.label}>
                    {entry.label}
                  </span>
                  <span role="status" className={entry.error ? 'text-destructive' : 'text-muted-foreground'}>
                    {entry.done
                      ? msg('systemSkills.imported', 'Imported')
                      : entry.error
                        ? msg('systemSkills.failed', 'Failed')
                        : entry.metadata
                          ? msg('systemSkills.checked', 'Checked')
                          : msg('systemSkills.checking', 'Checking...')}
                  </span>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    disabled={busy || checking || entry.done}
                    aria-label={msg('systemSkills.removeFile', 'Remove {file}', { file: entry.label })}
                    onClick={() => {
                      const next = entries.filter((item) => item.key !== entry.key);
                      setEntries(next);
                      if (!next.length) setPhase('select');
                    }}
                  >
                    <X size={13} aria-hidden />
                  </Button>
                </div>
                <div className="skill-import-fields">
                  <span className="text-xs text-muted-foreground">{msg('skills.icons.field', 'Icon')}</span>
                  <SkillIconPicker
                    value={entry.iconKey}
                    name={entry.name || msg('skills.detail.title', 'Skill')}
                    disabled={busy || checking || entry.done}
                    onChange={(iconKey) => update(entry.key, { iconKey })}
                  />
                  <Input
                    className="min-w-0 flex-1"
                    aria-label={msg('skills.import.nameFor', 'Skill name for {file}', { file: entry.label })}
                    placeholder={msg('skills.import.name', 'Skill name')}
                    maxLength={64}
                    value={entry.name}
                    disabled={busy || checking || entry.done || entry.metadata?.existing}
                    onChange={(event) => update(entry.key, { name: event.target.value })}
                  />
                </div>
                {entry.error && (
                  <p role="alert" className="mt-2 text-xs text-destructive">
                    {entry.error}
                    <Button
                      variant="link"
                      size="sm"
                      disabled={busy || checking}
                      onClick={async () => {
                        setChecking(true);
                        try {
                          await inspect(entry);
                        } finally {
                          setChecking(false);
                        }
                      }}
                    >
                      {msg('common.retry', 'Retry')}
                    </Button>
                  </p>
                )}
              </div>
            ))
          )}
        </div>
        <input
          ref={fileInput}
          className="hidden"
          type="file"
          accept=".zip,.skill"
          multiple={multiple}
          aria-label={msg('skills.import.choosePackage', 'Choose skill package')}
          onChange={(event) => {
            void select(Array.from(event.target.files ?? []));
            event.target.value = '';
          }}
        />
        <input
          ref={(node) => {
            folderInput.current = node;
            node?.setAttribute('webkitdirectory', '');
            node?.setAttribute('directory', '');
          }}
          className="hidden"
          type="file"
          multiple
          aria-label={msg('systemSkills.chooseFolder', 'Choose skill folder')}
          onChange={(event) => {
            void select(Array.from(event.target.files ?? []), true);
            event.target.value = '';
          }}
        />
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        <DialogFooter>
          <Button
            variant="outline"
            disabled={busy || checking}
            onClick={() => (phase === 'select' ? setOpen(false) : setPhase('select'))}
          >
            {phase === 'select' ? msg('common.cancel', 'Cancel') : msg('common.previous', 'Previous')}
          </Button>
          {phase === 'configure' && (
            <Button disabled={!ready || busy || checking} onClick={() => void submit()}>
              {busy ? msg('common.saving', 'Saving...') : submitLabel || msg('skills.import.submit', 'Import')}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

import { Ban, Copy, Eye, Info, KeyRound, MoreVertical, Plus, RotateCcw, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { useI18n } from '../../shared/i18n';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle
} from '../../shared/ui/alert-dialog';
import { Alert, AlertDescription, AlertTitle } from '../../shared/ui/alert';
import { Badge } from '../../shared/ui/badge';
import { Button } from '../../shared/ui/button';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader
} from '../../shared/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '../../shared/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '../../shared/ui/dropdown-menu';
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle
} from '../../shared/ui/empty';
import { Field, FieldDescription, FieldLabel } from '../../shared/ui/field';
import { Input } from '../../shared/ui/input';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '../../shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { Textarea } from '../../shared/ui/textarea';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../shared/ui/tooltip';

type AdminKeyStatus = 'active' | 'revoked';

type AdminKeyRecord = {
  id: string;
  sequence: number;
  revision: number;
  name: string;
  description: string;
  token: string;
  createdAt: number;
  lastUsedAt: number | null;
  status: AdminKeyStatus;
};

type AdminKeyConfirmation =
  | { kind: 'rotate'; record: AdminKeyRecord }
  | { kind: 'revoke'; record: AdminKeyRecord }
  | { kind: 'delete'; record: AdminKeyRecord }
  | null;

export function AdminKeysSettingsPage() {
  const { locale, msg } = useI18n();
  const nextIdRef = useRef(1);
  const copiedResetRef = useRef<number | null>(null);
  const [activeTab, setActiveTab] = useState<AdminKeyStatus>('active');
  const [records, setRecords] = useState<AdminKeyRecord[]>([]);
  const [createOpen, setCreateOpen] = useState(false);
  const [draftName, setDraftName] = useState('');
  const [draftDescription, setDraftDescription] = useState('');
  const [pendingRevealRecord, setPendingRevealRecord] = useState<AdminKeyRecord | null>(null);
  const [revealRecord, setRevealRecord] = useState<AdminKeyRecord | null>(null);
  const [copiedRecordId, setCopiedRecordId] = useState<string | null>(null);
  const [confirmation, setConfirmation] = useState<AdminKeyConfirmation>(null);

  const visibleRecords = useMemo(
    () => records.filter((record) => record.status === activeTab),
    [activeTab, records]
  );

  const resetDraft = () => {
    setDraftName('');
    setDraftDescription('');
  };

  const openCreateDialog = (nextOpen: boolean) => {
    setCreateOpen(nextOpen);
    if (!nextOpen) {
      resetDraft();
    }
  };

  const openRevealDialog = (record: AdminKeyRecord | null) => {
    setRevealRecord(record);
    setCopiedRecordId(null);
    if (copiedResetRef.current !== null) {
      window.clearTimeout(copiedResetRef.current);
      copiedResetRef.current = null;
    }
  };

  useEffect(() => {
    if (!pendingRevealRecord || createOpen || confirmation !== null) {
      return;
    }

    const frame = window.requestAnimationFrame(() => {
      openRevealDialog(pendingRevealRecord);
      setPendingRevealRecord(null);
    });

    return () => {
      window.cancelAnimationFrame(frame);
    };
  }, [confirmation, createOpen, pendingRevealRecord]);

  const handleCreate = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmedName = draftName.trim();
    if (!trimmedName) {
      return;
    }
    const sequence = nextIdRef.current++;
    const nextRecord: AdminKeyRecord = {
      id: `adminkey_local_${String(sequence).padStart(4, '0')}`,
      sequence,
      revision: 1,
      name: trimmedName,
      description: draftDescription.trim(),
      token: buildAdminKeyToken(sequence, 1),
      createdAt: Date.now(),
      lastUsedAt: null,
      status: 'active'
    };
    setRecords((current) => [nextRecord, ...current]);
    setActiveTab('active');
    openCreateDialog(false);
    setPendingRevealRecord(nextRecord);
  };

  const handleRotate = (record: AdminKeyRecord) => {
    let rotatedRecord: AdminKeyRecord | null = null;
    setRecords((current) =>
      current.map((item) => {
        if (item.id !== record.id) {
          return item;
        }
        rotatedRecord = {
          ...item,
          revision: item.revision + 1,
          token: buildAdminKeyToken(item.sequence, item.revision + 1),
          lastUsedAt: Date.now()
        };
        return rotatedRecord;
      })
    );
    setConfirmation(null);
    if (rotatedRecord) {
      setPendingRevealRecord(rotatedRecord);
    }
  };

  const handleRevoke = (record: AdminKeyRecord) => {
    setRecords((current) =>
      current.map((item) =>
        item.id === record.id
          ? {
              ...item,
              status: 'revoked'
            }
          : item
      )
    );
    setConfirmation(null);
  };

  const handleDelete = (record: AdminKeyRecord) => {
    setRecords((current) => current.filter((item) => item.id !== record.id));
    setConfirmation(null);
  };

  const handleCopy = async () => {
    if (!revealRecord) {
      return;
    }
    try {
      await navigator.clipboard?.writeText(revealRecord.token);
      setCopiedRecordId(revealRecord.id);
      if (copiedResetRef.current !== null) {
        window.clearTimeout(copiedResetRef.current);
      }
      copiedResetRef.current = window.setTimeout(() => {
        setCopiedRecordId(null);
        copiedResetRef.current = null;
      }, 1600);
    } catch {
      setCopiedRecordId(null);
    }
  };

  const confirmationTitle =
    confirmation?.kind === 'rotate'
      ? msg('adminKeys.rotateConfirm.title', 'Rotate admin key?')
      : confirmation?.kind === 'revoke'
        ? msg('adminKeys.revokeConfirm.title', 'Revoke admin key?')
        : msg('adminKeys.deleteConfirm.title', 'Delete preview row?');
  const confirmationBody =
    confirmation?.kind === 'rotate'
      ? msg(
          'adminKeys.rotateConfirm.body',
          'Rotate {name}? A new local preview token will be generated for this admin key.',
          { name: confirmation?.record.name ?? '' }
        )
      : confirmation?.kind === 'revoke'
        ? msg(
            'adminKeys.revokeConfirm.body',
            'Revoke {name}? It will move to the revoked tab and stop appearing in the active list.',
            { name: confirmation?.record.name ?? '' }
          )
        : msg(
            'adminKeys.deleteConfirm.body',
            'Delete the local preview row for {name}? This only removes it from the settings demo.',
            { name: confirmation?.record.name ?? '' }
          );
  const confirmationActionLabel =
    confirmation?.kind === 'rotate'
      ? msg('adminKeys.rotate', 'Rotate key')
      : confirmation?.kind === 'revoke'
        ? msg('adminKeys.revoke', 'Revoke key')
        : msg('adminKeys.deletePreview', 'Delete preview row');

  return (
    <TooltipProvider>
      <section className="mx-auto w-full max-w-[1100px] space-y-4" data-testid="settings-admin-keys-page">
        <Card>
          <CardHeader className="space-y-3">
            <CardAction>
              <Button type="button" size="sm" onClick={() => openCreateDialog(true)}>
                <Plus className="size-4" aria-hidden />
                {msg('adminKeys.create', 'Create admin key')}
              </Button>
            </CardAction>
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-xl font-semibold tracking-normal text-foreground">
                {msg('nav.adminKeys', 'Admin keys')}
              </h1>
              <Tooltip>
                <TooltipTrigger>
                  <Badge variant="secondary" className="rounded-full px-2.5 py-1">
                    {msg('adminKeys.organizationWide', 'Organization-wide access')}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent>
                  {msg(
                    'adminKeys.organizationWideTooltip',
                    'Admin keys apply across every workspace in this organization.'
                  )}
                </TooltipContent>
              </Tooltip>
            </div>
            <CardDescription>
              {msg(
                'adminKeys.description',
                'Create organization-level keys for automation that needs broader access than a single workspace API key.'
              )}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <Alert>
              <Info className="size-4" aria-hidden />
              <AlertTitle>{msg('adminKeys.notice.title', 'Admin keys should stay tightly scoped')}</AlertTitle>
              <AlertDescription>
                {msg(
                  'adminKeys.notice.body',
                  'Use admin keys only for trusted automation that needs to manage organization settings, audit activity, or coordinate across multiple workspaces.'
                )}
              </AlertDescription>
            </Alert>

            <Tabs
              value={activeTab}
              onValueChange={(nextValue) => nextValue && setActiveTab(nextValue as AdminKeyStatus)}
              className="gap-4"
            >
              <TabsList>
                <TabsTrigger value="active">{msg('common.active', 'Active')}</TabsTrigger>
                <TabsTrigger value="revoked">{msg('adminKeys.revoked', 'Revoked')}</TabsTrigger>
              </TabsList>
              <TabsContent value="active">
                {visibleRecords.length ? (
                  <AdminKeysTable
                    locale={locale}
                    records={visibleRecords}
                    onReveal={openRevealDialog}
                    onRotate={(record) => setConfirmation({ kind: 'rotate', record })}
                    onRevoke={(record) => setConfirmation({ kind: 'revoke', record })}
                    onDelete={(record) => setConfirmation({ kind: 'delete', record })}
                  />
                ) : (
                  <AdminKeysEmptyState
                    icon={KeyRound}
                    title={msg('adminKeys.empty.activeTitle', 'No admin keys yet')}
                    body={msg(
                      'adminKeys.empty.activeBody',
                      'Create an admin key when trusted automation needs organization-wide access.'
                    )}
                    action={
                      <Button type="button" onClick={() => openCreateDialog(true)}>
                        <Plus className="size-4" aria-hidden />
                        {msg('adminKeys.createFirst', 'Create first admin key')}
                      </Button>
                    }
                  />
                )}
              </TabsContent>
              <TabsContent value="revoked">
                {visibleRecords.length ? (
                  <AdminKeysTable
                    locale={locale}
                    records={visibleRecords}
                    onReveal={openRevealDialog}
                    onRotate={(record) => setConfirmation({ kind: 'rotate', record })}
                    onRevoke={(record) => setConfirmation({ kind: 'revoke', record })}
                    onDelete={(record) => setConfirmation({ kind: 'delete', record })}
                  />
                ) : (
                  <AdminKeysEmptyState
                    icon={Ban}
                    title={msg('adminKeys.empty.revokedTitle', 'No revoked admin keys')}
                    body={msg('adminKeys.empty.revokedBody', 'Revoked admin keys appear here.')}
                    action={
                      <Button type="button" variant="outline" onClick={() => setActiveTab('active')}>
                        {msg('adminKeys.showActive', 'Show active admin keys')}
                      </Button>
                    }
                  />
                )}
              </TabsContent>
            </Tabs>
          </CardContent>
        </Card>

        <Dialog open={createOpen} onOpenChange={openCreateDialog}>
          <DialogContent className="sm:max-w-[520px]">
            <DialogHeader>
              <DialogTitle>{msg('adminKeys.createDialog.title', 'Create admin key')}</DialogTitle>
              <DialogDescription>
                {msg(
                  'adminKeys.createDialog.description',
                  'Create a local preview admin key for trusted automation that needs organization-level access.'
                )}
              </DialogDescription>
            </DialogHeader>
            <form className="space-y-5" onSubmit={handleCreate}>
              <Field className="gap-2">
                <FieldLabel htmlFor="admin-key-name">{msg('common.name', 'Name')}</FieldLabel>
                <Input
                  id="admin-key-name"
                  value={draftName}
                  onChange={(event) => setDraftName(event.target.value)}
                  placeholder={msg('adminKeys.createDialog.namePlaceholder', 'Build pipeline')}
                />
                <FieldDescription>
                  {msg(
                    'adminKeys.createDialog.nameHelp',
                    'Use a short label that identifies which automation system owns this key.'
                  )}
                </FieldDescription>
              </Field>
              <Field className="gap-2">
                <FieldLabel htmlFor="admin-key-description">{msg('common.description', 'Description')}</FieldLabel>
                <Textarea
                  id="admin-key-description"
                  value={draftDescription}
                  onChange={(event) => setDraftDescription(event.target.value)}
                  placeholder={msg(
                    'adminKeys.createDialog.descriptionPlaceholder',
                    'Used by the release coordinator to create workspaces and rotate deployment credentials.'
                  )}
                  className="min-h-[112px] resize-y"
                />
                <FieldDescription>
                  {msg(
                    'adminKeys.createDialog.descriptionHelp',
                    'Document when this key should be used so reviewers can audit access later.'
                  )}
                </FieldDescription>
              </Field>
              <DialogFooter>
                <Button type="button" variant="outline" onClick={() => openCreateDialog(false)}>
                  {msg('common.cancel', 'Cancel')}
                </Button>
                <Button type="submit" disabled={!draftName.trim()}>
                  {msg('common.create', 'Create')}
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>

        <Dialog open={revealRecord !== null} onOpenChange={(nextOpen) => !nextOpen && openRevealDialog(null)}>
          <DialogContent className="sm:max-w-[560px]">
            <DialogHeader>
              <DialogTitle>{msg('adminKeys.revealDialog.title', 'Admin key value')}</DialogTitle>
              <DialogDescription>
                {msg(
                  'adminKeys.revealDialog.description',
                  'Copy the key value for {name}. This local preview dialog lets you verify the shared settings flow.',
                  { name: revealRecord?.name ?? '' }
                )}
              </DialogDescription>
            </DialogHeader>
            <Field className="gap-2">
              <FieldLabel htmlFor="admin-key-token">
                {msg('adminKeys.revealDialog.label', 'Admin key')}
              </FieldLabel>
              <div className="flex gap-2">
                <Input
                  id="admin-key-token"
                  value={revealRecord?.token ?? ''}
                  readOnly
                  aria-readonly="true"
                  className="font-mono text-xs"
                />
                <Button type="button" variant="outline" onClick={handleCopy}>
                  <Copy className="size-4" aria-hidden />
                  {copiedRecordId === revealRecord?.id ? msg('common.copied', 'Copied') : msg('common.copy', 'Copy')}
                </Button>
              </div>
              <FieldDescription>
                {msg(
                  'adminKeys.revealDialog.help',
                  'This preview key is stored only in local page state and is not persisted to the backend.'
                )}
              </FieldDescription>
            </Field>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => openRevealDialog(null)}>
                {msg('common.close', 'Close')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <AlertDialog open={confirmation !== null} onOpenChange={(nextOpen) => !nextOpen && setConfirmation(null)}>
          <AlertDialogContent size="sm">
            <AlertDialogHeader>
              <AlertDialogMedia>
                {confirmation?.kind === 'rotate' ? (
                  <RotateCcw aria-hidden />
                ) : confirmation?.kind === 'revoke' ? (
                  <Ban aria-hidden />
                ) : (
                  <Trash2 aria-hidden />
                )}
              </AlertDialogMedia>
              <AlertDialogTitle>{confirmationTitle}</AlertDialogTitle>
              <AlertDialogDescription>{confirmationBody}</AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel onClick={() => setConfirmation(null)}>
                {msg('common.cancel', 'Cancel')}
              </AlertDialogCancel>
              <AlertDialogAction
                variant={confirmation?.kind === 'rotate' ? 'default' : 'destructive'}
                onClick={() => {
                  if (!confirmation) {
                    return;
                  }
                  if (confirmation.kind === 'rotate') {
                    handleRotate(confirmation.record);
                    return;
                  }
                  if (confirmation.kind === 'revoke') {
                    handleRevoke(confirmation.record);
                    return;
                  }
                  handleDelete(confirmation.record);
                }}
              >
                {confirmationActionLabel}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </section>
    </TooltipProvider>
  );
}

function AdminKeysTable({
  locale,
  records,
  onReveal,
  onRotate,
  onRevoke,
  onDelete
}: {
  locale: string;
  records: AdminKeyRecord[];
  onReveal: (record: AdminKeyRecord) => void;
  onRotate: (record: AdminKeyRecord) => void;
  onRevoke: (record: AdminKeyRecord) => void;
  onDelete: (record: AdminKeyRecord) => void;
}) {
  const { msg } = useI18n();

  return (
    <Card className="overflow-hidden py-0">
      <CardContent className="p-0">
        <Table aria-label={msg('adminKeys.table.ariaLabel', 'Admin keys')}>
          <TableHeader className="text-muted-foreground">
            <TableRow className="hover:bg-transparent">
              <TableHead className="px-5 py-3">{msg('common.name', 'Name')}</TableHead>
              <TableHead className="px-5 py-3">{msg('adminKeys.table.id', 'ID')}</TableHead>
              <TableHead className="px-5 py-3">{msg('adminKeys.table.created', 'Created')}</TableHead>
              <TableHead className="px-5 py-3">{msg('adminKeys.table.lastUsed', 'Last used')}</TableHead>
              <TableHead className="px-5 py-3">{msg('adminKeys.table.status', 'Status')}</TableHead>
              <TableHead className="w-14 px-5 py-3" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {records.map((record) => (
              <TableRow key={record.id} className="text-foreground last:border-0">
                <TableCell className="px-5 py-4 align-top">
                  <div className="min-w-0">
                    <div className="font-medium text-foreground">{record.name}</div>
                    {record.description ? (
                      <p className="mt-1 max-w-[420px] text-sm leading-6 text-muted-foreground">
                        {record.description}
                      </p>
                    ) : null}
                  </div>
                </TableCell>
                <TableCell className="px-5 py-4 align-top font-mono text-xs text-muted-foreground">
                  {record.id}
                </TableCell>
                <TableCell className="px-5 py-4 align-top text-sm text-muted-foreground">
                  {formatDateTime(record.createdAt, locale)}
                </TableCell>
                <TableCell className="px-5 py-4 align-top text-sm text-muted-foreground">
                  {record.lastUsedAt
                    ? formatDateTime(record.lastUsedAt, locale)
                    : msg('adminKeys.table.neverUsed', 'Never')}
                </TableCell>
                <TableCell className="px-5 py-4 align-top">
                  <Badge variant={record.status === 'active' ? 'secondary' : 'outline'}>
                    {record.status === 'active'
                      ? msg('common.active', 'Active')
                      : msg('adminKeys.revoked', 'Revoked')}
                  </Badge>
                </TableCell>
                <TableCell className="px-5 py-4 text-right align-top">
                  <AdminKeyActionsMenu
                    record={record}
                    onReveal={onReveal}
                    onRotate={onRotate}
                    onRevoke={onRevoke}
                    onDelete={onDelete}
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

function AdminKeyActionsMenu({
  record,
  onReveal,
  onRotate,
  onRevoke,
  onDelete
}: {
  record: AdminKeyRecord;
  onReveal: (record: AdminKeyRecord) => void;
  onRotate: (record: AdminKeyRecord) => void;
  onRevoke: (record: AdminKeyRecord) => void;
  onDelete: (record: AdminKeyRecord) => void;
}) {
  const { msg } = useI18n();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="text-muted-foreground"
            aria-label={msg('adminKeys.moreActions', 'More actions for {name}', { name: record.name })}
          />
        }
      >
        <MoreVertical className="size-4" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        <DropdownMenuItem onClick={() => onReveal(record)}>
          <Eye className="size-4" aria-hidden />
          <span>{msg('adminKeys.reveal', 'Reveal key')}</span>
        </DropdownMenuItem>
        {record.status === 'active' ? (
          <>
            <DropdownMenuItem onClick={() => onRotate(record)}>
              <RotateCcw className="size-4" aria-hidden />
              <span>{msg('adminKeys.rotate', 'Rotate key')}</span>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onClick={() => onRevoke(record)}>
              <Ban className="size-4" aria-hidden />
              <span>{msg('adminKeys.revoke', 'Revoke key')}</span>
            </DropdownMenuItem>
          </>
        ) : (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onClick={() => onDelete(record)}>
              <Trash2 className="size-4" aria-hidden />
              <span>{msg('adminKeys.deletePreview', 'Delete preview row')}</span>
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function AdminKeysEmptyState({
  icon: Icon,
  title,
  body,
  action
}: {
  icon: typeof KeyRound;
  title: string;
  body: string;
  action?: ReactNode;
}) {
  return (
    <Empty className="min-h-[280px] rounded-lg border-border bg-card">
      <EmptyHeader>
        <EmptyMedia variant="icon" className="size-10 rounded-full border border-border bg-secondary text-muted-foreground">
          <Icon className="size-5" aria-hidden />
        </EmptyMedia>
        <EmptyTitle>
          <h2 className="text-[20px] font-semibold leading-7 text-foreground">{title}</h2>
        </EmptyTitle>
        <EmptyDescription className="max-w-[520px]">{body}</EmptyDescription>
      </EmptyHeader>
      {action ? <EmptyContent>{action}</EmptyContent> : null}
    </Empty>
  );
}

function buildAdminKeyToken(sequence: number, revision: number) {
  return `sk-ant-admin-local-${String(sequence).padStart(4, '0')}-${String(revision).padStart(2, '0')}`;
}

function formatDateTime(value: number, locale: string) {
  return new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short'
  }).format(value);
}

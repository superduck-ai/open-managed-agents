import {
  Archive,
  LockKeyhole,
  MoreVertical,
  Plus,
  RotateCcw,
  Trash2
} from 'lucide-react';
import { useMemo, useRef, useState, type FormEvent, type ReactNode } from 'react';
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
} from '@/shared/ui/alert-dialog';
import { Badge } from '@/shared/ui/badge';
import { Button } from '@/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/shared/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/shared/ui/dropdown-menu';
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle
} from '@/shared/ui/empty';
import { Field, FieldDescription, FieldLabel } from '@/shared/ui/field';
import { Input } from '@/shared/ui/input';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/shared/ui/tabs';
import { Textarea } from '@/shared/ui/textarea';
import { useI18n } from '../../shared/i18n';
import { ConsolePageFrame, DataTableCard } from './frame';

type ServiceAccountStatus = 'active' | 'archived';

type ServiceAccountRecord = {
  id: string;
  name: string;
  description: string;
  createdAt: number;
  status: ServiceAccountStatus;
};

type ServiceAccountConfirmation =
  | { kind: 'archive'; record: ServiceAccountRecord }
  | { kind: 'delete'; record: ServiceAccountRecord }
  | null;

export function ServiceAccountsPage() {
  const { locale, msg } = useI18n();
  const nextIdRef = useRef(1);
  const [activeTab, setActiveTab] = useState<ServiceAccountStatus>('active');
  const [createOpen, setCreateOpen] = useState(false);
  const [draftName, setDraftName] = useState('');
  const [draftDescription, setDraftDescription] = useState('');
  const [records, setRecords] = useState<ServiceAccountRecord[]>([]);
  const [confirmation, setConfirmation] = useState<ServiceAccountConfirmation>(null);

  const visibleRecords = useMemo(
    () => records.filter((record) => record.status === activeTab),
    [activeTab, records]
  );

  const resetDraft = () => {
    setDraftName('');
    setDraftDescription('');
  };

  const handleCreateOpenChange = (nextOpen: boolean) => {
    setCreateOpen(nextOpen);
    if (!nextOpen) {
      resetDraft();
    }
  };

  const handleCreate = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmedName = draftName.trim();
    if (!trimmedName) {
      return;
    }
    const sequence = nextIdRef.current++;
    const nextRecord: ServiceAccountRecord = {
      id: `svcacc_local_${String(sequence).padStart(4, '0')}`,
      name: trimmedName,
      description: draftDescription.trim(),
      createdAt: Date.now(),
      status: 'active'
    };
    setRecords((current) => [nextRecord, ...current]);
    setActiveTab('active');
    handleCreateOpenChange(false);
  };

  const handleArchive = (record: ServiceAccountRecord) => {
    setRecords((current) =>
      current.map((item) =>
        item.id === record.id
          ? {
              ...item,
              status: 'archived'
            }
          : item
      )
    );
    setConfirmation(null);
  };

  const handleRestore = (record: ServiceAccountRecord) => {
    setRecords((current) =>
      current.map((item) =>
        item.id === record.id
          ? {
              ...item,
              status: 'active'
            }
          : item
      )
    );
  };

  const handleDelete = (record: ServiceAccountRecord) => {
    setRecords((current) => current.filter((item) => item.id !== record.id));
    setConfirmation(null);
  };

  const confirmTitle =
    confirmation?.kind === 'archive'
      ? msg('serviceAccounts.confirmArchive.title', 'Archive service account?')
      : msg('serviceAccounts.confirmDelete.title', 'Delete service account?');
  const confirmBody =
    confirmation?.kind === 'archive'
      ? msg(
          'serviceAccounts.confirmArchive.body',
          'Archive {name}? You can restore it later from the archived tab.',
          {
            name: confirmation?.record.name ?? ''
          }
        )
      : msg(
          'serviceAccounts.confirmDelete.body',
          'Delete {name}? This removes the local preview record from the list.',
          {
            name: confirmation?.record.name ?? ''
          }
        );
  const confirmActionLabel =
    confirmation?.kind === 'archive'
      ? msg('common.archive', 'Archive')
      : msg('common.delete', 'Delete');

  return (
    <ConsolePageFrame
      title={msg('serviceAccounts.title', 'Service accounts')}
      icon={LockKeyhole}
      description={msg('serviceAccounts.description', 'Named non-human identities for workload and CI federation.')}
      actions={
        <Button type="button" size="lg" onClick={() => setCreateOpen(true)}>
          <Plus className="size-4" aria-hidden />
          {msg('serviceAccounts.create', 'Create service account')}
        </Button>
      }
    >
      <Tabs
        value={activeTab}
        onValueChange={(nextValue) => nextValue && setActiveTab(nextValue as ServiceAccountStatus)}
        className="gap-4"
      >
        <TabsList>
          <TabsTrigger value="active">{msg('common.active', 'Active')}</TabsTrigger>
          <TabsTrigger value="archived">{msg('common.archived', 'Archived')}</TabsTrigger>
        </TabsList>
        <TabsContent value="active">
          {visibleRecords.length ? (
            <ServiceAccountsTable locale={locale} records={visibleRecords} onArchive={(record) => setConfirmation({ kind: 'archive', record })} onRestore={handleRestore} onDelete={(record) => setConfirmation({ kind: 'delete', record })} />
          ) : (
            <ServiceAccountsEmptyState
              icon={LockKeyhole}
              title={msg('serviceAccounts.empty.activeTitle', 'No service accounts yet')}
              body={msg(
                'serviceAccounts.empty.activeBody',
                'Create a service account for CI, workload identity, and other automation flows.'
              )}
            />
          )}
        </TabsContent>
        <TabsContent value="archived">
          {visibleRecords.length ? (
            <ServiceAccountsTable locale={locale} records={visibleRecords} onArchive={(record) => setConfirmation({ kind: 'archive', record })} onRestore={handleRestore} onDelete={(record) => setConfirmation({ kind: 'delete', record })} />
          ) : (
            <ServiceAccountsEmptyState
              icon={Archive}
              title={msg('serviceAccounts.empty.archivedTitle', 'No archived service accounts')}
              body={msg('serviceAccounts.empty.archivedBody', 'Archived service accounts appear here.')}
              action={
                <Button type="button" variant="outline" onClick={() => setActiveTab('active')}>
                  {msg('serviceAccounts.showActive', 'Show active service accounts')}
                </Button>
              }
            />
          )}
        </TabsContent>
      </Tabs>

      <Dialog open={createOpen} onOpenChange={handleCreateOpenChange}>
        <DialogContent className="sm:max-w-[520px]">
          <DialogHeader>
            <DialogTitle>{msg('serviceAccounts.createDialog.title', 'Create service account')}</DialogTitle>
            <DialogDescription>
              {msg(
                'serviceAccounts.createDialog.description',
                'Create a named non-human identity for CI, workload federation, and other automation flows.'
              )}
            </DialogDescription>
          </DialogHeader>
          <form className="space-y-5" onSubmit={handleCreate}>
            <Field className="gap-2">
              <FieldLabel htmlFor="service-account-name">{msg('common.name', 'Name')}</FieldLabel>
              <Input
                id="service-account-name"
                value={draftName}
                onChange={(event) => setDraftName(event.target.value)}
                placeholder={msg('serviceAccounts.createDialog.namePlaceholder', 'CI deploy bot')}
              />
              <FieldDescription>
                {msg(
                  'serviceAccounts.createDialog.nameHelp',
                  'Use a short, descriptive name so automation owners can identify this account quickly.'
                )}
              </FieldDescription>
            </Field>
            <Field className="gap-2">
              <FieldLabel htmlFor="service-account-description">{msg('common.description', 'Description')}</FieldLabel>
              <Textarea
                id="service-account-description"
                value={draftDescription}
                onChange={(event) => setDraftDescription(event.target.value)}
                placeholder={msg(
                  'serviceAccounts.createDialog.descriptionPlaceholder',
                  'Used by deployment pipelines to publish production builds.'
                )}
                className="min-h-[112px] resize-y"
              />
            </Field>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => handleCreateOpenChange(false)}>
                {msg('common.cancel', 'Cancel')}
              </Button>
              <Button type="submit" disabled={!draftName.trim()}>
                {msg('common.create', 'Create')}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={confirmation !== null} onOpenChange={(nextOpen) => !nextOpen && setConfirmation(null)}>
        <AlertDialogContent size="sm">
          <AlertDialogHeader>
            <AlertDialogMedia>
              {confirmation?.kind === 'archive' ? <Archive aria-hidden /> : <Trash2 aria-hidden />}
            </AlertDialogMedia>
            <AlertDialogTitle>{confirmTitle}</AlertDialogTitle>
            <AlertDialogDescription>{confirmBody}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => setConfirmation(null)}>
              {msg('common.cancel', 'Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant={confirmation?.kind === 'delete' ? 'destructive' : 'default'}
              onClick={() => {
                if (!confirmation) {
                  return;
                }
                if (confirmation.kind === 'archive') {
                  handleArchive(confirmation.record);
                  return;
                }
                handleDelete(confirmation.record);
              }}
            >
              {confirmActionLabel}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </ConsolePageFrame>
  );
}

function ServiceAccountsTable({
  locale,
  records,
  onArchive,
  onRestore,
  onDelete
}: {
  locale: string;
  records: ServiceAccountRecord[];
  onArchive: (record: ServiceAccountRecord) => void;
  onRestore: (record: ServiceAccountRecord) => void;
  onDelete: (record: ServiceAccountRecord) => void;
}) {
  const { msg } = useI18n();

  return (
    <DataTableCard>
      <Table aria-label={msg('serviceAccounts.table.ariaLabel', 'Service accounts')}>
        <TableHeader className="text-muted-foreground">
          <TableRow className="hover:bg-transparent">
            <TableHead className="px-5 py-3">{msg('common.name', 'Name')}</TableHead>
            <TableHead className="px-5 py-3">{msg('serviceAccounts.table.id', 'ID')}</TableHead>
            <TableHead className="px-5 py-3">{msg('serviceAccounts.table.created', 'Created')}</TableHead>
            <TableHead className="px-5 py-3">{msg('serviceAccounts.table.lastUsed', 'Last used')}</TableHead>
            <TableHead className="px-5 py-3">{msg('serviceAccounts.table.status', 'Status')}</TableHead>
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
                    <p className="mt-1 max-w-[360px] text-sm leading-6 text-muted-foreground">
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
                {msg('serviceAccounts.table.neverUsed', 'Never')}
              </TableCell>
              <TableCell className="px-5 py-4 align-top">
                <Badge variant={record.status === 'active' ? 'secondary' : 'outline'}>
                  {record.status === 'active'
                    ? msg('common.active', 'Active')
                    : msg('common.archived', 'Archived')}
                </Badge>
              </TableCell>
              <TableCell className="px-5 py-4 text-right align-top">
                <ServiceAccountActionsMenu
                  record={record}
                  onArchive={onArchive}
                  onRestore={onRestore}
                  onDelete={onDelete}
                />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </DataTableCard>
  );
}

function ServiceAccountActionsMenu({
  record,
  onArchive,
  onRestore,
  onDelete
}: {
  record: ServiceAccountRecord;
  onArchive: (record: ServiceAccountRecord) => void;
  onRestore: (record: ServiceAccountRecord) => void;
  onDelete: (record: ServiceAccountRecord) => void;
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
            aria-label={msg('managedAgents.common.moreActions', 'More actions')}
          />
        }
      >
        <MoreVertical className="size-4" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-44">
        {record.status === 'active' ? (
          <DropdownMenuItem onClick={() => onArchive(record)}>
            <Archive className="size-4" aria-hidden />
            <span>{msg('common.archive', 'Archive')}</span>
          </DropdownMenuItem>
        ) : (
          <>
            <DropdownMenuItem onClick={() => onRestore(record)}>
              <RotateCcw className="size-4" aria-hidden />
              <span>{msg('serviceAccounts.restore', 'Restore')}</span>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onClick={() => onDelete(record)}>
              <Trash2 className="size-4" aria-hidden />
              <span>{msg('common.delete', 'Delete')}</span>
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function ServiceAccountsEmptyState({
  icon: Icon,
  title,
  body,
  action
}: {
  icon: typeof LockKeyhole;
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

function formatDateTime(value: number, locale: string) {
  return new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short'
  }).format(value);
}

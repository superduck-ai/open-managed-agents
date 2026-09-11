import { useWorkspace } from '../../../shared/workspaces/context';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Button } from '../../../shared/ui/button';
import {
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
} from '../../../shared/ui/data-table-interactions';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { AlertCircle, Archive, LockKeyhole, MoreVertical, Pencil, Plus, X } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import {
  archiveVaultCredential,
  createVaultCredential,
  deleteVaultCredential,
  listVaultCredentials,
  updateVaultCredential,
} from '../api';
import { AgentFilterDropdown, AgentStatusBadge, ConfirmEntityDialog, ManagedSearchField } from '../components/common';
import { managedColumnLabel } from '../labels';
import { type CredentialFormValues, type VaultApiResponse, type VaultCredentialApiResponse } from '../types';
import { compactEntityId, errorMessage } from '../utils';
import { CredentialDialog } from './dialogs';
import { credentialAuthLabel } from './model';

type CredentialStatusFilter = 'all' | 'active';

function vaultCredentialDateLabel(
  value: string,
  formatDate: (value: string | number | Date, options?: Intl.DateTimeFormatOptions) => string,
): string {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) {
    return '—';
  }
  return formatDate(value, { month: 'short', day: 'numeric' });
}

function credentialMatchesSearch(credential: VaultCredentialApiResponse, search: string): boolean {
  const needle = search.trim().toLowerCase();
  if (!needle) {
    return true;
  }
  return credential.id.toLowerCase().includes(needle);
}

export function VaultCredentialsPanel({
  vault,
  workspaceId,
  refreshKey,
  onRefresh,
  dialog: controlledDialog,
  onDialogChange,
}: {
  vault: VaultApiResponse;
  workspaceId: string;
  refreshKey: number;
  onRefresh: () => void;
  dialog?: {
    mode: 'create' | 'edit';
    credential?: VaultCredentialApiResponse;
    firstCredential?: boolean;
  } | null;
  onDialogChange?: (
    dialog: {
      mode: 'create' | 'edit';
      credential?: VaultCredentialApiResponse;
      firstCredential?: boolean;
    } | null,
  ) => void;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const { orgUuid } = useWorkspace();
  const [state, setState] = useState<{ loading: boolean; error: string | null; data: VaultCredentialApiResponse[] }>({
    loading: true,
    error: null,
    data: [],
  });
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<CredentialStatusFilter>('all');
  const [openFilterMenu, setOpenFilterMenu] = useState<'status' | null>(null);
  const [internalDialog, setInternalDialog] = useState<{
    mode: 'create' | 'edit';
    credential?: VaultCredentialApiResponse;
    firstCredential?: boolean;
  } | null>(null);
  const dialog = onDialogChange ? (controlledDialog ?? null) : internalDialog;
  const setDialog = onDialogChange ?? setInternalDialog;
  const [confirmAction, setConfirmAction] = useState<{
    action: 'archive' | 'delete';
    credential: VaultCredentialApiResponse;
  } | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);

  useEffect(() => {
    if (onDialogChange) {
      return;
    }
    const params = new URLSearchParams(window.location.search);
    if (params.get('addCredential') !== '1') {
      return;
    }
    setInternalDialog({ mode: 'create', firstCredential: true });
    params.delete('addCredential');
    const searchParams = params.toString();
    window.history.replaceState(
      null,
      '',
      `${window.location.pathname}${searchParams ? `?${searchParams}` : ''}${window.location.hash}`,
    );
  }, [onDialogChange]);

  useEffect(() => {
    let active = true;
    setState((current) => ({ ...current, loading: true, error: null }));
    void listVaultCredentials(vault.id, workspaceId)
      .then((page) => active && setState({ loading: false, error: null, data: page.data ?? [] }))
      .catch((error) => active && setState({ loading: false, error: errorMessage(error), data: [] }));
    return () => {
      active = false;
    };
  }, [refreshKey, vault.id, workspaceId]);

  const statusOptions = useMemo(
    () => [
      { value: 'all' as const, label: msg('common.all', 'All') },
      { value: 'active' as const, label: msg('common.active', 'Active') },
    ],
    [msg],
  );
  const statusValueLabel = statusOptions.find((option) => option.value === statusFilter)?.label ?? statusFilter;

  const visibleCredentials = useMemo(
    () =>
      // listVaultCredentials always uses include_archived: false, so All/Active are equivalent today.
      state.data.filter((credential) => !credential.archived_at && credentialMatchesSearch(credential, search)),
    [search, state.data],
  );

  const columns = ['ID', 'Name', 'Auth', 'Status', 'Updated'].map((column) => managedColumnLabel(column, msg));

  const submit = async (values: CredentialFormValues, credential?: VaultCredentialApiResponse) => {
    const updated = credential
      ? await updateVaultCredential(vault.id, credential.id, values, workspaceId)
      : await createVaultCredential(vault.id, values, workspaceId);
    setState((current) => ({ ...current, data: [updated, ...current.data.filter((item) => item.id !== updated.id)] }));
    setDialog(null);
    onRefresh();
  };

  const remove = async (credential: VaultCredentialApiResponse, action: 'archive' | 'delete') => {
    setBusyId(credential.id);
    try {
      if (action === 'archive') {
        await archiveVaultCredential(vault.id, credential.id, workspaceId);
      } else {
        await deleteVaultCredential(vault.id, credential.id, workspaceId);
      }
      // List API omits archived credentials; drop locally after archive or delete.
      setState((current) => ({ ...current, data: current.data.filter((item) => item.id !== credential.id) }));
      setConfirmAction(null);
      onRefresh();
    } catch (error) {
      setState((current) => ({ ...current, error: errorMessage(error) }));
      setConfirmAction(null);
    } finally {
      setBusyId(null);
    }
  };

  return (
    <>
      {confirmAction ? (
        <ConfirmEntityDialog
          action={confirmAction.action}
          section="credential-vaults"
          entity={confirmAction.credential}
          labelOverride={msg('managedAgents.credentialVaults.credentialKind', 'credential')}
          busy={busyId === confirmAction.credential.id}
          onCancel={() => {
            if (!busyId) {
              setConfirmAction(null);
            }
          }}
          onConfirm={() => {
            void remove(confirmAction.credential, confirmAction.action);
          }}
        />
      ) : null}

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <ManagedSearchField
          id="vault-credential-search"
          value={search}
          placeholder={msg('managedAgents.credentialVaults.credentials.searchPlaceholder', 'Find credential by ID')}
          onChange={setSearch}
        />
        <AgentFilterDropdown
          label={msg('managedAgents.filters.status', 'Status')}
          valueLabel={statusValueLabel}
          options={statusOptions}
          value={statusFilter}
          menu="status"
          open={openFilterMenu === 'status'}
          menuWidthClass="w-[220px]"
          onOpenChange={setOpenFilterMenu}
          onSelect={(value) => {
            setStatusFilter(value);
            setOpenFilterMenu(null);
          }}
        />
      </div>

      {state.error ? (
        <Alert variant="destructive" className="mb-4 max-w-xl">
          <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
          <AlertDescription>{state.error}</AlertDescription>
        </Alert>
      ) : null}

      <Table className={cn(dataTableClassName, 'min-w-[960px]')}>
        <TableHeader>
          <TableRow className={dataTableHeaderRowClassName}>
            {columns.map((column) => (
              <TableHead
                key={column}
                className={cn(
                  dataTableHeaderCellClassName,
                  column === managedColumnLabel('ID', msg) && 'w-[200px]',
                  column === managedColumnLabel('Name', msg) && 'w-[220px]',
                  column === managedColumnLabel('Auth', msg) && 'w-[180px]',
                  column === managedColumnLabel('Status', msg) && 'w-[110px]',
                  column === managedColumnLabel('Updated', msg) && 'w-[120px]',
                )}
              >
                {column}
              </TableHead>
            ))}
            <TableHead
              className={cn(dataTableHeaderCellClassName, 'w-[48px] px-2')}
              aria-label={managedColumnLabel('Actions', msg)}
            />
          </TableRow>
        </TableHeader>
        <TableBody>
          {state.loading ? (
            <TableRow className="border-0 hover:bg-transparent">
              <TableCell colSpan={columns.length + 1} className="h-[280px] text-center text-sm text-muted-foreground">
                {msg('common.loading', 'Loading...')}
              </TableCell>
            </TableRow>
          ) : visibleCredentials.length === 0 ? (
            <TableRow className="border-0 hover:bg-transparent">
              <TableCell colSpan={columns.length + 1} className="h-[320px] p-0">
                <div className="grid h-full place-items-center text-center">
                  <div className="max-w-[360px] px-4 py-10">
                    <LockKeyhole className="mx-auto mb-4 size-12 stroke-[1.3] text-foreground" aria-hidden />
                    <div className="text-sm font-semibold text-foreground">
                      {search.trim() || statusFilter !== 'all'
                        ? msg(
                            'managedAgents.credentialVaults.credentials.searchEmpty',
                            'No credentials match your filters.',
                          )
                        : msg('managedAgents.credentialVaults.credentials.empty', 'No credentials yet')}
                    </div>
                    {!search.trim() && statusFilter === 'all' ? (
                      <>
                        <p className="mt-3 text-sm leading-5 text-muted-foreground">
                          {msg(
                            'managedAgents.credentialVaults.credentials.emptyBody',
                            'Add a credential to give agents access through this vault.',
                          )}
                        </p>
                        <Button
                          type="button"
                          variant="outline"
                          className="mt-4"
                          onClick={() => setDialog({ mode: 'create' })}
                        >
                          <Plus className="size-4" aria-hidden />
                          {msg('managedAgents.credentialVaults.credentialDialog.add', 'Add credential')}
                        </Button>
                      </>
                    ) : null}
                  </div>
                </div>
              </TableCell>
            </TableRow>
          ) : (
            visibleCredentials.map((credential) => (
              <TableRow key={credential.id} className="bg-card text-foreground hover:bg-card">
                <TableCell className="h-11 truncate px-3 font-mono text-[13px]">
                  {compactEntityId(credential.id)}
                </TableCell>
                <TableCell className="h-11 truncate px-3">{credential.display_name}</TableCell>
                <TableCell className="h-11 truncate px-3">{credentialAuthLabel(credential.auth, msg)}</TableCell>
                <TableCell className="h-11 px-3">
                  <AgentStatusBadge archived={Boolean(credential.archived_at)} />
                </TableCell>
                <TableCell className="h-11 truncate px-3">
                  {vaultCredentialDateLabel(credential.updated_at, formatters.date)}
                </TableCell>
                <TableCell className="h-11 px-2">
                  <div className="flex justify-end">
                    <DropdownMenu>
                      <DropdownMenuTrigger
                        render={
                          <Button
                            type="button"
                            variant="outline"
                            size="icon-sm"
                            aria-label={msg('managedAgents.common.moreActions', 'More actions')}
                            className="text-foreground"
                            disabled={busyId === credential.id}
                          />
                        }
                      >
                        <MoreVertical className="size-4" aria-hidden />
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" className="w-[164px]">
                        <DropdownMenuItem className="h-9" onClick={() => setDialog({ mode: 'edit', credential })}>
                          <Pencil className="size-4" aria-hidden />
                          {msg('common.edit', 'Edit')}
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          className="h-9"
                          disabled={busyId === credential.id || Boolean(credential.archived_at)}
                          onClick={() => setConfirmAction({ action: 'archive', credential })}
                        >
                          <Archive className="size-4" aria-hidden />
                          {msg('common.archive', 'Archive')}
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          className="h-9"
                          variant="destructive"
                          disabled={busyId === credential.id}
                          onClick={() => setConfirmAction({ action: 'delete', credential })}
                        >
                          <X className="size-4" aria-hidden />
                          {msg('common.delete', 'Delete')}
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>

      {dialog ? (
        <CredentialDialog
          credential={dialog.credential}
          vaultId={vault.id}
          workspaceId={workspaceId}
          orgUuid={orgUuid}
          firstCredential={dialog.firstCredential}
          onClose={() => setDialog(null)}
          onSubmit={(values) => submit(values, dialog.credential)}
          onOAuthComplete={() => {
            setDialog(null);
            onRefresh();
          }}
        />
      ) : null}
    </>
  );
}

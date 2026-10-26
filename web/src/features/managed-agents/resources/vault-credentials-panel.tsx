import { useWorkspace } from '../../../shared/workspaces/context';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Button } from '../../../shared/ui/button';
import { ResourceListState } from '../../../shared/ui/resource-list-state';
import { ResourceSearchField } from '../../../shared/ui/resource-list-controls';
import {
  DataTableRow,
  DataTableCell,
  CopyIdCell,
  MoreActionsButton,
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
import { AlertCircle, Archive, LockKeyhole, Pencil, X } from 'lucide-react';
import { type RefObject, useEffect, useMemo, useRef, useState } from 'react';
import {
  archiveVaultCredential,
  createVaultCredential,
  deleteVaultCredential,
  listVaultCredentials,
  updateVaultCredential,
} from '../api';
import { AgentStatusBadge, ConfirmEntityDialog } from '../components/common';
import { managedColumnLabel } from '../labels';
import { type CredentialFormValues, type VaultApiResponse, type VaultCredentialApiResponse } from '../types';
import { compactEntityId, errorMessage } from '../utils';
import { CredentialDialog } from './dialogs';
import { credentialAuthLabel } from './model';

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
  returnFocus,
  onDialogChange,
}: {
  vault: VaultApiResponse;
  workspaceId: string;
  refreshKey: number;
  onRefresh: () => void;
  returnFocus?: RefObject<HTMLElement | null>;
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
  const [internalDialog, setInternalDialog] = useState<{
    mode: 'create' | 'edit';
    credential?: VaultCredentialApiResponse;
    firstCredential?: boolean;
  } | null>(null);
  const refreshAfterCloseRef = useRef(false);
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

  const visibleCredentials = useMemo(
    () => state.data.filter((credential) => !credential.archived_at && credentialMatchesSearch(credential, search)),
    [search, state.data],
  );

  const columns = ['ID', 'Name', 'Auth', 'Status', 'Updated'].map((column) => managedColumnLabel(column, msg));

  const submit = async (values: CredentialFormValues, credential?: VaultCredentialApiResponse) => {
    const updated = credential
      ? await updateVaultCredential(vault.id, credential.id, values, workspaceId)
      : await createVaultCredential(vault.id, values, workspaceId);
    setState((current) => ({ ...current, data: [updated, ...current.data.filter((item) => item.id !== updated.id)] }));
    refreshAfterCloseRef.current = true;
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

      {state.data.length > 0 ? (
        <div className="mb-7 flex flex-wrap items-center gap-2">
          <ResourceSearchField
            id="vault-credential-search"
            value={search}
            placeholder={msg('managedAgents.credentialVaults.credentials.searchPlaceholder', 'Find credential by ID')}
            onChange={setSearch}
          />
        </div>
      ) : null}

      {state.error ? (
        <Alert variant="destructive" className="mb-4 max-w-xl [overflow-wrap:anywhere]">
          <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
          <AlertDescription>
            <p>{state.error}</p>
            <Button type="button" variant="outline" size="sm" className="mt-2" onClick={onRefresh}>
              {msg('common.retry', 'Retry')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}

      {state.loading || visibleCredentials.length > 0 ? (
        <Table className={cn(dataTableClassName, state.loading ? 'min-w-full' : 'min-w-[900px]')}>
          {!state.loading ? (
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
          ) : null}
          <TableBody>
            {state.loading ? (
              <TableRow className="border-0 hover:bg-transparent">
                <TableCell colSpan={columns.length + 1} className="h-[280px] text-center text-sm text-muted-foreground">
                  {msg('common.loading', 'Loading...')}
                </TableCell>
              </TableRow>
            ) : (
              visibleCredentials.map((credential) => (
                <DataTableRow key={credential.id} className="[&>td]:h-11">
                  <DataTableCell edge="start" title={credential.id}>
                    <CopyIdCell
                      value={credential.id}
                      displayValue={compactEntityId(credential.id)}
                      ariaLabel={msg('common.copyValue', 'Copy {value}', { value: credential.id })}
                    />
                  </DataTableCell>
                  <DataTableCell className="h-11 truncate px-3" title={credential.display_name}>
                    {credential.display_name}
                  </DataTableCell>
                  <DataTableCell className="h-11 truncate px-3">
                    {credentialAuthLabel(credential.auth, msg)}
                  </DataTableCell>
                  <DataTableCell className="h-11 px-3">
                    <AgentStatusBadge archived={Boolean(credential.archived_at)} />
                  </DataTableCell>
                  <DataTableCell className="h-11 truncate px-3">
                    {vaultCredentialDateLabel(credential.updated_at, formatters.date)}
                  </DataTableCell>
                  <DataTableCell edge="end" className="px-2">
                    <div className="flex justify-end">
                      <DropdownMenu>
                        <DropdownMenuTrigger
                          render={
                            <MoreActionsButton
                              label={msg('managedAgents.common.moreActions', 'More actions')}
                              disabled={busyId === credential.id}
                            />
                          }
                        />
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
                  </DataTableCell>
                </DataTableRow>
              ))
            )}
          </TableBody>
        </Table>
      ) : null}

      {!state.loading && !state.error && visibleCredentials.length === 0 ? (
        <ResourceListState
          icon={LockKeyhole}
          title={
            search.trim()
              ? msg('managedAgents.credentialVaults.credentials.searchEmpty', 'No credentials match your filters.')
              : msg('managedAgents.credentialVaults.credentials.empty', 'No credentials yet')
          }
        />
      ) : null}

      {dialog ? (
        <CredentialDialog
          credential={dialog.credential}
          returnFocus={returnFocus}
          vaultId={vault.id}
          workspaceId={workspaceId}
          orgUuid={orgUuid}
          firstCredential={dialog.firstCredential}
          onClose={() => {
            setDialog(null);
            if (refreshAfterCloseRef.current) {
              refreshAfterCloseRef.current = false;
              onRefresh();
            }
          }}
          onSubmit={(values) => submit(values, dialog.credential)}
          onOAuthComplete={() => {
            refreshAfterCloseRef.current = true;
          }}
        />
      ) : null}
    </>
  );
}

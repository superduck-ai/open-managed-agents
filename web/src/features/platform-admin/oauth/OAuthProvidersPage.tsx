import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Archive, ArchiveRestore, KeyRound, Pencil, MoreVertical, Plus, Power, PowerOff, Trash2 } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { OAuthProviderAvatar } from '../../../shared/icons/oauth-provider-avatar';
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '../../../shared/ui/dropdown-menu';
import { Button } from '../../../shared/ui/button';
import { Badge } from '../../../shared/ui/badge';
import { ResourceFilterDropdown } from '../../../shared/ui/resource-list-controls';
import { ResourcePageHeader } from '../../../shared/ui/resource-page-header';
import { ResourceListState } from '../../../shared/ui/resource-list-state';
import { Table, TableBody, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import {
  DataTableCell,
  DataTableRow,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
} from '../../../shared/ui/data-table-interactions';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../../shared/ui/dialog';
import { deleteOAuthProvider, listOAuthProviders, type OAuthProviderRecord } from '../../../shared/api/platform-admin';
import { usePlatformAdmin } from '../context';
import { usePlatformAdminError } from '../usePlatformAdminError';
import { OAuthProviderDialog } from './OAuthProviderDialog';
import { OAuthProviderStatusDialog, type OAuthProviderStatusAction } from './OAuthProviderStatusDialog';
import type { ApiError } from '../../../shared/api/client';

type OAuthProviderFilter = 'active' | 'enabled' | 'disabled' | 'archived' | 'all';

const oauthProvidersKey = ['platform-oauth-providers'] as const;

export function OAuthProvidersPage() {
  const { msg } = useI18n();
  const { client } = usePlatformAdmin();
  const queryClient = useQueryClient();
  const errorMessage = usePlatformAdminError();
  const [editing, setEditing] = useState<OAuthProviderRecord | null | undefined>(undefined);
  const [deleting, setDeleting] = useState<OAuthProviderRecord | null>(null);
  const [statusChange, setStatusChange] = useState<{
    provider: OAuthProviderRecord;
    action: OAuthProviderStatusAction;
  } | null>(null);
  const [status, setStatus] = useState<OAuthProviderFilter>('active');
  const [filterOpen, setFilterOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const query = useQuery({
    queryKey: oauthProvidersKey,
    queryFn: () => listOAuthProviders(client),
  });
  const remove = useMutation({
    mutationFn: (provider: OAuthProviderRecord) => deleteOAuthProvider(client, provider.id, provider.version),
    onSuccess: async () => {
      setDeleting(null);
      await queryClient.invalidateQueries({ queryKey: oauthProvidersKey });
    },
    onError: async (mutationError: unknown) => {
      setError(errorMessage(mutationError));
      if ((mutationError as ApiError).code === 'oauth_provider_version_conflict')
        await queryClient.invalidateQueries({ queryKey: oauthProvidersKey });
    },
  });
  const reload = async () => {
    setEditing(undefined);
    await queryClient.invalidateQueries({ queryKey: oauthProvidersKey });
  };
  const providers = (query.data ?? []).filter((provider) => matchesOAuthProviderStatus(provider, status));
  const statusOptions: Array<{ value: OAuthProviderFilter; label: string }> = [
    { value: 'active', label: msg('common.active', 'Active') },
    { value: 'enabled', label: msg('platformAdmin.oauthManagement.enabledState', 'Enabled') },
    { value: 'disabled', label: msg('platformAdmin.oauthManagement.disabled', 'Disabled') },
    { value: 'archived', label: msg('platformAdmin.oauthManagement.archived', 'Archived') },
    { value: 'all', label: msg('platformAdmin.oauthManagement.all', 'All') },
  ];
  return (
    <section>
      <ResourcePageHeader
        title={msg('platformAdmin.oauthManagement.title', 'OAuth management')}
        description={msg(
          'platformAdmin.oauthManagement.description',
          'Manage OAuth providers and client configuration.',
        )}
        actions={
          <Button size="lg" onClick={() => setEditing(null)}>
            <Plus aria-hidden className="size-4" />
            {msg('platformAdmin.oauthManagement.create', 'Register OAuth client')}
          </Button>
        }
      />
      <div className="mb-7 flex flex-wrap items-center gap-2">
        <ResourceFilterDropdown
          label={msg('managedAgents.filters.status', 'Status')}
          valueLabel={statusOptions.find((option) => option.value === status)!.label}
          options={statusOptions}
          value={status}
          menu="status"
          open={filterOpen}
          menuWidthClass="w-[230px]"
          onOpenChange={(menu) => setFilterOpen(menu !== null)}
          onSelect={setStatus}
        />
      </div>
      {query.isPending ? (
        <p role="status">{msg('common.loading', 'Loading...')}</p>
      ) : query.isError ? (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(query.error)}
        </p>
      ) : providers.length === 0 ? (
        <ResourceListState
          icon={KeyRound}
          title={
            query.data?.length
              ? msg('platformAdmin.oauthManagement.noMatches', 'No matching OAuth clients')
              : msg('platformAdmin.oauthManagement.empty', 'No OAuth clients yet')
          }
          body={
            query.data?.length
              ? undefined
              : msg(
                  'platformAdmin.oauthManagement.emptyBody',
                  'Register a client to let workspaces authorize ENV OAuth credentials.',
                )
          }
        />
      ) : (
        <OAuthProvidersTable
          providers={providers}
          onEdit={setEditing}
          onStatusChange={(provider, action) => setStatusChange({ provider, action })}
          onDelete={(provider) => {
            setError(null);
            setDeleting(provider);
          }}
        />
      )}
      {editing !== undefined ? (
        <OAuthProviderDialog
          provider={editing ?? undefined}
          onClose={() => setEditing(undefined)}
          onSaved={reload}
          onConflict={() => queryClient.invalidateQueries({ queryKey: oauthProvidersKey })}
        />
      ) : null}
      {statusChange ? (
        <OAuthProviderStatusDialog
          provider={statusChange.provider}
          action={statusChange.action}
          onClose={() => setStatusChange(null)}
          onSaved={async () => {
            setStatusChange(null);
            await queryClient.invalidateQueries({ queryKey: oauthProvidersKey });
          }}
          onConflict={() => queryClient.invalidateQueries({ queryKey: oauthProvidersKey })}
        />
      ) : null}
      {deleting ? (
        <Dialog open onOpenChange={(open) => !open && !remove.isPending && setDeleting(null)}>
          <DialogContent className="sm:max-w-md" closeButtonDisabled={remove.isPending}>
            <DialogHeader>
              <DialogTitle>{msg('platformAdmin.oauthManagement.deleteTitle', 'Delete OAuth client')}</DialogTitle>
              <DialogDescription>
                {msg(
                  'platformAdmin.oauthManagement.deleteBody',
                  'Existing credentials keep working until their token expires; refresh will require reauthorization.',
                  { name: deleting.name },
                )}
              </DialogDescription>
            </DialogHeader>
            {error ? (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            ) : null}
            <DialogFooter>
              <DialogClose render={<Button type="button" variant="outline" disabled={remove.isPending} />}>
                {msg('common.cancel', 'Cancel')}
              </DialogClose>
              <Button
                type="button"
                variant="destructive"
                disabled={remove.isPending}
                onClick={() => remove.mutate(deleting)}
              >
                {remove.isPending
                  ? msg('common.saving', 'Saving...')
                  : msg('platformAdmin.oauthManagement.delete', 'Delete')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      ) : null}
    </section>
  );
}

function matchesOAuthProviderStatus(provider: OAuthProviderRecord, status: OAuthProviderFilter) {
  if (status === 'all') return true;
  if (status === 'active') return !provider.archived;
  if (status === 'archived') return provider.archived;
  return !provider.archived && (provider.enabled ? status === 'enabled' : status === 'disabled');
}

function OAuthProvidersTable({
  providers,
  onEdit,
  onDelete,
  onStatusChange,
}: {
  providers: OAuthProviderRecord[];
  onEdit: (provider: OAuthProviderRecord) => void;
  onDelete: (provider: OAuthProviderRecord) => void;
  onStatusChange: (provider: OAuthProviderRecord, action: OAuthProviderStatusAction) => void;
}) {
  const { msg, locale } = useI18n();
  return (
    <Table className={`${dataTableClassName} min-w-[900px]`}>
      <TableHeader>
        <TableRow className={dataTableHeaderRowClassName}>
          <TableHead className={dataTableHeaderCellClassName}>
            {msg('platformAdmin.oauthManagement.name', 'Name')}
          </TableHead>
          <TableHead className={dataTableHeaderCellClassName}>
            {msg('managedAgents.filters.status', 'Status')}
          </TableHead>
          <TableHead className={dataTableHeaderCellClassName}>
            {msg('platformAdmin.oauthManagement.authorizeEndpoint', 'Authorize endpoint')}
          </TableHead>
          <TableHead className={dataTableHeaderCellClassName}>
            {msg('platformAdmin.oauthManagement.tokenEndpoint', 'Token endpoint')}
          </TableHead>
          <TableHead className={dataTableHeaderCellClassName}>
            {msg('platformAdmin.oauthManagement.clientId', 'Client ID')}
          </TableHead>
          <TableHead className={dataTableHeaderCellClassName}>
            {msg('platformAdmin.oauthManagement.updatedAt', 'Updated')}
          </TableHead>
          <TableHead className={dataTableHeaderCellClassName} />
        </TableRow>
      </TableHeader>
      <TableBody>
        {providers.map((provider) => (
          <DataTableRow key={provider.id} clickable onClick={() => onEdit(provider)}>
            <DataTableCell edge="start" className="font-medium">
              <span className="flex items-center gap-2">
                <OAuthProviderAvatar providerKey={provider.provider_key} logoUrl={provider.logo_url} />
                <span>{provider.name}</span>
              </span>
            </DataTableCell>
            <DataTableCell>
              <Badge variant="secondary">
                {provider.archived
                  ? msg('platformAdmin.oauthManagement.archived', 'Archived')
                  : provider.enabled
                    ? msg('platformAdmin.oauthManagement.enabledState', 'Enabled')
                    : msg('platformAdmin.oauthManagement.disabled', 'Disabled')}
              </Badge>
            </DataTableCell>
            <DataTableCell className="max-w-[280px] truncate font-mono text-xs">
              {provider.authorize_endpoint}
            </DataTableCell>
            <DataTableCell className="max-w-[280px] truncate font-mono text-xs">
              {provider.token_endpoint}
            </DataTableCell>
            <DataTableCell className="font-mono text-xs">{provider.client_id}</DataTableCell>
            <DataTableCell>{new Date(provider.updated_at).toLocaleDateString(locale)}</DataTableCell>
            <DataTableCell edge="end">
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={msg('skills.moreFor', 'More actions for {name}', { name: provider.name })}
                      onClick={(event) => event.stopPropagation()}
                    />
                  }
                >
                  <MoreVertical className="size-4" strokeWidth={1.5} aria-hidden />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-44" onClick={(event) => event.stopPropagation()}>
                  <DropdownMenuItem onClick={() => onEdit(provider)}>
                    <Pencil size={15} aria-hidden />
                    {msg('common.edit', 'Edit')}
                  </DropdownMenuItem>
                  {!provider.archived ? (
                    <DropdownMenuItem onClick={() => onStatusChange(provider, provider.enabled ? 'disable' : 'enable')}>
                      {provider.enabled ? <PowerOff size={15} aria-hidden /> : <Power size={15} aria-hidden />}
                      {provider.enabled
                        ? msg('platformAdmin.oauthManagement.disable', 'Disable')
                        : msg('platformAdmin.oauthManagement.enable', 'Enable')}
                    </DropdownMenuItem>
                  ) : null}
                  <DropdownMenuItem onClick={() => onStatusChange(provider, provider.archived ? 'restore' : 'archive')}>
                    {provider.archived ? <ArchiveRestore size={15} aria-hidden /> : <Archive size={15} aria-hidden />}
                    {provider.archived
                      ? msg('platformAdmin.oauthManagement.restore', 'Restore')
                      : msg('platformAdmin.oauthManagement.archive', 'Archive')}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="destructive" onClick={() => onDelete(provider)}>
                    <Trash2 size={15} aria-hidden />
                    {msg('platformAdmin.oauthManagement.delete', 'Delete')}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </DataTableCell>
          </DataTableRow>
        ))}
      </TableBody>
    </Table>
  );
}

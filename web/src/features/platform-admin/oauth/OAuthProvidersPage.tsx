import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { KeyRound, MoreVertical, Plus, Power, PowerOff, Trash2 } from 'lucide-react';
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
import { OAuthProviderStatusDialog } from './OAuthProviderStatusDialog';
import type { ApiError } from '../../../shared/api/client';

const oauthProvidersKey = ['platform-oauth-providers'] as const;

export function OAuthProvidersPage() {
  const { msg, locale } = useI18n();
  const { client } = usePlatformAdmin();
  const queryClient = useQueryClient();
  const errorMessage = usePlatformAdminError();
  const [editing, setEditing] = useState<OAuthProviderRecord | null | undefined>(undefined);
  const [deleting, setDeleting] = useState<OAuthProviderRecord | null>(null);
  const [toggling, setToggling] = useState<OAuthProviderRecord | null>(null);
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
  const providers = query.data ?? [];
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
      {query.isPending ? (
        <p role="status">{msg('common.loading', 'Loading...')}</p>
      ) : query.isError ? (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(query.error)}
        </p>
      ) : providers.length === 0 ? (
        <ResourceListState
          icon={KeyRound}
          title={msg('platformAdmin.oauthManagement.empty', 'No OAuth clients yet')}
          body={msg(
            'platformAdmin.oauthManagement.emptyBody',
            'Register a client to let workspaces authorize ENV OAuth credentials.',
          )}
        />
      ) : (
        <Table className={`${dataTableClassName} min-w-[900px]`}>
          <TableHeader>
            <TableRow className={dataTableHeaderRowClassName}>
              <TableHead className={dataTableHeaderCellClassName}>
                {msg('platformAdmin.oauthManagement.name', 'Name')}
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
              <DataTableRow key={provider.id} clickable onClick={() => setEditing(provider)}>
                <DataTableCell edge="start" className="font-medium">
                  <span className="flex items-center gap-2">
                    <OAuthProviderAvatar providerKey={provider.provider_key} logoUrl={provider.logo_url} />
                    <span>{provider.name}</span>
                  </span>
                  <span className="mt-1 block text-xs text-muted-foreground">
                    {!provider.enabled
                      ? msg('platformAdmin.oauthManagement.disabled', 'Disabled')
                      : provider.configured
                        ? msg('platformAdmin.oauthManagement.configured', 'Configured')
                        : msg('platformAdmin.oauthManagement.unconfigured', 'Not configured')}
                  </span>
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
                      <DropdownMenuItem onClick={() => setToggling(provider)}>
                        {provider.enabled ? <PowerOff size={15} aria-hidden /> : <Power size={15} aria-hidden />}
                        {provider.enabled
                          ? msg('platformAdmin.oauthManagement.disable', 'Disable')
                          : msg('platformAdmin.oauthManagement.enable', 'Enable')}
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem
                        variant="destructive"
                        onClick={() => {
                          setError(null);
                          setDeleting(provider);
                        }}
                      >
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
      )}
      {editing !== undefined ? (
        <OAuthProviderDialog
          provider={editing ?? undefined}
          onClose={() => setEditing(undefined)}
          onSaved={reload}
          onConflict={() => queryClient.invalidateQueries({ queryKey: oauthProvidersKey })}
        />
      ) : null}
      {toggling ? (
        <OAuthProviderStatusDialog
          provider={toggling}
          onClose={() => setToggling(null)}
          onSaved={async () => {
            setToggling(null);
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

import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  useReactTable,
  type Table as TableInstance,
} from '@tanstack/react-table';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertCircle, MoreVertical, Plus, Send, Trash2, Users } from 'lucide-react';
import { useMemo, useRef, useState, type FormEvent } from 'react';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '../../shared/ui/alert-dialog';
import { Alert, AlertDescription, AlertTitle } from '../../shared/ui/alert';
import { Badge } from '../../shared/ui/badge';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { ResourceListState } from '../../shared/ui/resource-list-state';
import { ResourcePageHeader } from '../../shared/ui/resource-page-header';
import type { ApiError } from '../../shared/api/client';
import { useI18n } from '../../shared/i18n';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../shared/ui/dropdown-menu';
import { Label } from '../../shared/ui/label';
import { RoleSelect } from '../../shared/permissions/RoleSelect';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { Textarea } from '../../shared/ui/textarea';
import { Skeleton } from '../../shared/ui/skeleton';
import { toast } from '../../shared/ui/sonner';
import { useAuth } from '../../shared/auth/context';
import { useWorkspace } from '../../shared/workspaces/context';
import { localizedWorkspaceName } from '../../shared/workspaces/display-name';
import { notifyInvitationDelivery } from './invitationDelivery';
import { MembersAccessDenied } from './MembersAccessDenied';
import { canManageMembers, canViewMembers } from '../../shared/permissions/members';
import { platformRoleLabel, roleOptions, type PlatformRole } from '../../shared/permissions/roles';
import { MemberDirectoryFilters } from './MemberDirectoryFilters';
import { OrganizationMemberRemoval } from './OrganizationMemberRemoval';
import {
  createOrganizationInvite,
  deleteOrganizationInvite,
  listOrganizationInvites,
  listOrganizationMembers,
  resendOrganizationInvite,
  updateOrganizationMemberRole,
  type OrganizationInvite,
  type OrganizationMember,
} from './membersApi';

type OrganizationMemberRow = OrganizationMember | OrganizationInvite;
type InviteValidationError = { type: 'invalid-email'; emails: string[] } | { type: 'too-many' };

const memberColumnHelper = createColumnHelper<OrganizationMemberRow>();

export function OrganizationMembersPage() {
  const { msg } = useI18n();
  const { account } = useAuth();
  const { orgUuid } = useWorkspace();
  const bootstrapOrganization = account?.memberships?.find((membership) => membership.organization?.uuid)?.organization;
  const activeOrgUuid = orgUuid ?? bootstrapOrganization?.uuid;

  if (!activeOrgUuid) {
    return (
      <section>
        <ResourcePageHeader contentGap="content" title={msg('members.title', 'Members')} />
        <p className="text-sm leading-5 text-muted-foreground">
          {msg('members.noOrganization', 'No organization is available for this session.')}
        </p>
      </section>
    );
  }

  if (!canViewMembers(account, activeOrgUuid)) {
    return <OrganizationMembersAccessDenied />;
  }

  return (
    <OrganizationMembersAdminPage
      key={`${activeOrgUuid}:${canManageMembers(account, activeOrgUuid)}`}
      activeOrgUuid={activeOrgUuid}
    />
  );
}

function OrganizationMembersAdminPage({ activeOrgUuid }: { activeOrgUuid: string }) {
  const { msg } = useI18n();
  const { account, csrfToken } = useAuth();
  const canManage = canManageMembers(account, activeOrgUuid);
  const { activeWorkspace } = useWorkspace();
  const queryClient = useQueryClient();
  const bootstrapOrganization = account?.memberships?.find((membership) => membership.organization?.uuid)?.organization;
  const activeOrganization =
    account?.memberships?.find((membership) => membership.organization?.uuid === activeOrgUuid)?.organization ??
    bootstrapOrganization;
  const [memberSearch, setMemberSearch] = useState('');
  const [roleFilter, setRoleFilter] = useState('all');
  const [inviteOpen, setInviteOpen] = useState(false);
  const [inviteActionError, setInviteActionError] = useState<string | null>(null);
  const [inviteToRevoke, setInviteToRevoke] = useState<OrganizationInvite | null>(null);
  const [pendingRoleMemberId, setPendingRoleMemberId] = useState<string | null>(null);
  const membersQueryKey = ['console', 'organization-members', activeOrgUuid] as const;
  const invitesQueryKey = ['console', 'organization-invites', activeOrgUuid, 'pending'] as const;

  const membersQuery = useQuery({
    queryKey: membersQueryKey,
    queryFn: () => listOrganizationMembers(activeOrgUuid ?? ''),
    enabled: Boolean(activeOrgUuid),
    retry: false,
  });

  const invitesQuery = useQuery({
    queryKey: invitesQueryKey,
    queryFn: () => listOrganizationInvites(activeOrgUuid ?? '', 'pending'),
    enabled: canManage,
    retry: false,
  });

  const updateRoleMutation = useMutation({
    mutationFn: ({ member, role }: { member: OrganizationMember; role: PlatformRole }) =>
      updateOrganizationMemberRole(activeOrgUuid ?? '', member.id, role, csrfToken),
    onMutate: ({ member }) => {
      setPendingRoleMemberId(member.id);
    },
    onSuccess: (updatedMember) => {
      queryClient.setQueryData<OrganizationMember[]>(
        membersQueryKey,
        (current) =>
          current?.map((member) => (member.id === updatedMember.id ? updatedMember : member)) ?? [updatedMember],
      );
    },
    onSettled: () => {
      setPendingRoleMemberId(null);
    },
  });

  const resendInviteMutation = useMutation({
    mutationFn: (invite: OrganizationInvite) => resendOrganizationInvite(activeOrgUuid ?? '', invite.id, csrfToken),
    onSuccess: (updatedInvite) => {
      queryClient.setQueryData<OrganizationInvite[]>(
        invitesQueryKey,
        (current) =>
          current?.map((invite) => (invite.id === updatedInvite.id ? updatedInvite : invite)) ?? [updatedInvite],
      );
      setInviteActionError(null);
      notifyInvitationDelivery([updatedInvite], msg);
    },
    onError: (error) => {
      setInviteActionError(errorMessage(error));
    },
  });

  const deleteInviteMutation = useMutation({
    mutationFn: (invite: OrganizationInvite) => deleteOrganizationInvite(activeOrgUuid ?? '', invite.id, csrfToken),
    onSuccess: (_deletedInvite, invite) => {
      queryClient.setQueryData<OrganizationInvite[]>(
        invitesQueryKey,
        (current) => current?.filter((existingInvite) => existingInvite.id !== invite.id) ?? [],
      );
      setInviteToRevoke(null);
      setInviteActionError(null);
      toast.success(msg('members.revoked', 'Invitation revoked.'));
    },
    onError: (error) => {
      setInviteActionError(errorMessage(error));
    },
  });

  const tableRows = useMemo<OrganizationMemberRow[]>(
    () => [...(canManage ? (invitesQuery.data ?? []) : []), ...(membersQuery.data ?? [])],
    [canManage, invitesQuery.data, membersQuery.data],
  );
  const filteredRows = useMemo(
    () =>
      tableRows.filter((row) => {
        if (roleFilter !== 'all' && normalizePlatformRole(row.role) !== roleFilter) return false;
        const keyword = memberSearch.trim().toLowerCase();
        return (
          !keyword ||
          (!isInviteRow(row) && row.name?.toLowerCase().includes(keyword)) ||
          row.email.toLowerCase().includes(keyword)
        );
      }),
    [tableRows, roleFilter, memberSearch],
  );
  const isInitialLoading = membersQuery.isPending || (canManage && invitesQuery.isPending);
  const inviteError = canManage ? invitesQuery.error : null;
  const hasTableError = membersQuery.isError || Boolean(inviteError);
  const handleRetry = () => {
    membersQuery.refetch();
    if (canManage) invitesQuery.refetch();
  };

  const columns = useMemo(
    () => [
      memberColumnHelper.display({
        id: 'name',
        header: msg('common.name', 'Name'),
        cell: ({ row }) => {
          if (isInviteRow(row.original)) {
            return (
              <div className="flex min-w-0 items-center gap-2">
                <span className="text-muted-foreground">–</span>
                <Badge variant="secondary" className="rounded-md px-1.5">
                  {msg('members.pending', 'Pending')}
                </Badge>
              </div>
            );
          }

          const name = row.original.name;
          return (
            <div className="flex min-w-0 items-center gap-3">
              <MemberAvatar member={row.original} />
              <span className="truncate text-foreground" title={name || row.original.email}>
                {name || row.original.email}
              </span>
            </div>
          );
        },
      }),
      memberColumnHelper.display({
        id: 'email',
        header: msg('members.email', 'Email'),
        cell: ({ row }) => (
          <span className="block truncate text-muted-foreground" title={row.original.email}>
            {row.original.email}
          </span>
        ),
      }),
      memberColumnHelper.display({
        id: 'role',
        header: msg('members.role', 'Role'),
        cell: ({ row }) => {
          if (isInviteRow(row.original)) {
            return (
              <span className="text-foreground">
                {platformRoleLabel(normalizePlatformRole(row.original.role), msg)}
              </span>
            );
          }

          const member = row.original;
          const role = normalizePlatformRole(member.role);
          const isSelf = isCurrentAccountMember(account, member);

          if (!canManage || isSelf) {
            return <span className="text-foreground">{platformRoleLabel(role, msg)}</span>;
          }

          return (
            <RoleSelect
              ariaLabel={msg('members.role.for', 'Role for {name}', { name: displayMemberName(member) })}
              value={role}
              disabled={pendingRoleMemberId === member.id || updateRoleMutation.isPending}
              className="w-full min-w-0"
              contentClassName="min-w-[300px]"
              onChange={(nextRole) => {
                if (nextRole !== role) {
                  updateRoleMutation.mutate({ member, role: nextRole });
                }
              }}
            />
          );
        },
      }),
      memberColumnHelper.display({
        id: 'actions',
        header: '',
        cell: ({ row }) =>
          !canManage ? null : isInviteRow(row.original) ? (
            <InviteActionsMenu
              invite={row.original}
              disabled={resendInviteMutation.isPending || deleteInviteMutation.isPending}
              onResend={(invite) => {
                setInviteActionError(null);
                resendInviteMutation.mutate(invite);
              }}
              onRevoke={(invite) => {
                setInviteActionError(null);
                setInviteToRevoke(invite);
              }}
            />
          ) : isCurrentAccountMember(account, row.original) ? (
            <span aria-hidden className="block h-8 w-8" />
          ) : (
            <OrganizationMemberRemoval
              orgUuid={activeOrgUuid}
              organizationName={activeOrganization?.name ?? msg('members.fallbackOrgName', 'this organization')}
              member={row.original}
              csrfToken={csrfToken}
              disabled={updateRoleMutation.isPending}
            />
          ),
      }),
    ],
    [
      account,
      canManage,
      activeOrgUuid,
      activeOrganization,
      csrfToken,
      deleteInviteMutation,
      msg,
      pendingRoleMemberId,
      resendInviteMutation,
      updateRoleMutation,
    ],
  );

  // TanStack Table returns callback-heavy instance methods; this table instance stays local to the page.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: filteredRows,
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  if ([membersQuery.error, inviteError].some(isPermissionDenied)) {
    return <OrganizationMembersAccessDenied />;
  }

  return (
    <section data-testid="organization-members-page">
      <ResourcePageHeader
        contentGap="content"
        title={msg('members.title', 'Members')}
        description={memberScopeLabel(activeOrganization, activeWorkspace.name, msg)}
        titleAdornment={
          !isInitialLoading && !hasTableError ? (
            <Badge variant="secondary" className="min-w-5 rounded-full px-1.5">
              {tableRows.length}
            </Badge>
          ) : null
        }
        actions={
          canManage ? (
            <Button
              size="lg"
              onClick={() => {
                setInviteActionError(null);
                setInviteOpen(true);
              }}
            >
              <Plus className="size-4" aria-hidden />
              {msg('members.invite', 'Invite')}
            </Button>
          ) : null
        }
      />

      <MemberDirectoryFilters
        search={memberSearch}
        onSearch={setMemberSearch}
        role={roleFilter}
        onRole={setRoleFilter}
        roles={roleOptions.map((role) => ({ value: role.value, label: platformRoleLabel(role.value, msg) }))}
        roleLabelPrefix="members.role"
      />

      {updateRoleMutation.isError ? <InlineNotice>{errorMessage(updateRoleMutation.error)}</InlineNotice> : null}
      {inviteActionError ? <InlineNotice>{inviteActionError}</InlineNotice> : null}

      <OrganizationMemberDirectory
        table={table}
        loading={isInitialLoading}
        hasError={hasTableError}
        filtered={Boolean(memberSearch.trim()) || roleFilter !== 'all'}
        canManage={canManage}
        onRetry={handleRetry}
      />

      <InviteMembersDialog
        open={inviteOpen}
        activeOrgUuid={activeOrgUuid}
        csrfToken={csrfToken}
        onOpenChange={setInviteOpen}
        onInvited={(createdInvites) => {
          queryClient.setQueryData<OrganizationInvite[]>(invitesQueryKey, (current) => [
            ...createdInvites,
            ...(current ?? []),
          ]);
          notifyInvitationDelivery(createdInvites, msg);
        }}
      />
      <InviteRevokeDialog
        invite={inviteToRevoke}
        isSubmitting={deleteInviteMutation.isPending}
        onCancel={() => {
          if (!deleteInviteMutation.isPending) {
            setInviteToRevoke(null);
          }
        }}
        onConfirm={() => {
          if (inviteToRevoke) {
            deleteInviteMutation.mutate(inviteToRevoke);
          }
        }}
      />
    </section>
  );
}

function OrganizationMemberDirectory({
  table,
  loading,
  hasError,
  filtered,
  canManage,
  onRetry,
}: {
  table: TableInstance<OrganizationMemberRow>;
  loading: boolean;
  hasError: boolean;
  filtered: boolean;
  canManage: boolean;
  onRetry: () => void;
}) {
  const { msg } = useI18n();
  const showRows = !loading && !hasError && table.getRowModel().rows.length > 0;
  return (
    <>
      <Table
        className={`table-fixed text-left ${showRows ? 'min-w-[720px]' : 'min-w-full'}`}
        aria-label={msg('members.tableAria', 'Members')}
      >
        {showRows ? (
          <TableHeader className="text-muted-foreground">
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id} className="border-border hover:bg-transparent">
                {headerGroup.headers.map((header) => (
                  <TableHead key={header.id} className={headerClassName(header.column.id)}>
                    {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
        ) : null}
        <TableBody>
          {loading ? <MembersSkeletonRows /> : null}
          {!loading && hasError ? (
            <TableRow>
              <TableCell colSpan={4} className="px-3 py-10">
                <Alert variant="destructive" className="mx-auto max-w-xl">
                  <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
                  <AlertTitle>{msg('members.loadError', 'Members could not be loaded.')}</AlertTitle>
                  <AlertDescription>
                    <p>{msg('members.tryAgainBody', 'Try again.')}</p>
                    <Button type="button" variant="outline" size="sm" className="mt-3" onClick={onRetry}>
                      {msg('members.tryAgain', 'Try again')}
                    </Button>
                  </AlertDescription>
                </Alert>
              </TableCell>
            </TableRow>
          ) : null}
          {!loading && !hasError
            ? table.getRowModel().rows.map((row) => (
                <TableRow key={row.id} className="border-border last:border-b-0">
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id} className={cellClassName(cell.column.id)}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            : null}
        </TableBody>
      </Table>

      {!loading && !hasError && table.getRowModel().rows.length === 0 ? (
        <ResourceListState
          icon={Users}
          title={
            filtered ? msg('members.noMatches', 'No matching members') : msg('members.emptyTitle', 'No members yet')
          }
          body={
            !filtered && canManage ? msg('members.emptyBody', 'Invite people to join this organization.') : undefined
          }
        />
      ) : null}
    </>
  );
}

function OrganizationMembersAccessDenied() {
  const { msg } = useI18n();

  return (
    <section data-testid="organization-members-page">
      <ResourcePageHeader contentGap="content" title={msg('members.title', 'Members')} />
      <MembersAccessDenied
        description={msg(
          'members.accessDeniedDescription',
          'Organization members can view this directory. Only organization administrators can manage organization members. Contact your organization administrator.',
        )}
      />
    </section>
  );
}

function isPermissionDenied(error: unknown) {
  return Boolean(error && typeof error === 'object' && (error as Partial<ApiError>).status === 403);
}

function InviteMembersDialog({
  open,
  activeOrgUuid,
  csrfToken,
  onOpenChange,
  onInvited,
}: {
  open: boolean;
  activeOrgUuid: string;
  csrfToken?: string;
  onOpenChange: (open: boolean) => void;
  onInvited: (createdInvites: OrganizationInvite[]) => void;
}) {
  const { msg } = useI18n();
  const [emailsValue, setEmailsValue] = useState('');
  const [role, setRole] = useState<PlatformRole>('user');
  const [submitValidationError, setSubmitValidationError] = useState<InviteValidationError | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const parsedEmails = parseEmailAddresses(emailsValue);

  const inviteMutation = useMutation({
    mutationFn: async ({ emails, inviteRole }: { emails: string[]; inviteRole: PlatformRole }) => {
      const createdInvites: OrganizationInvite[] = [];
      for (const email of emails) {
        createdInvites.push(await createOrganizationInvite(activeOrgUuid, email, inviteRole, csrfToken));
      }
      return createdInvites;
    },
    onSuccess: (createdInvites) => {
      onOpenChange(false);
      onInvited(createdInvites);
    },
  });

  const canSubmit = parsedEmails.length > 0 && !inviteMutation.isPending;

  const resetInvite = () => {
    setEmailsValue('');
    setRole('user');
    setSubmitValidationError(null);
    inviteMutation.reset();
  };

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    if (parsedEmails.length > 50) {
      setSubmitValidationError({ type: 'too-many' });
      return;
    }
    const invalidEmails = parsedEmails.filter((email) => !isEmailLike(email));
    if (invalidEmails.length > 0) {
      setSubmitValidationError({ type: 'invalid-email', emails: invalidEmails });
      return;
    }
    setSubmitValidationError(null);
    inviteMutation.mutate({ emails: parsedEmails, inviteRole: role });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen, details) => {
        if (!nextOpen && inviteMutation.isPending) {
          details.cancel();
          return;
        }
        onOpenChange(nextOpen);
      }}
      onOpenChangeComplete={(nextOpen) => {
        if (!nextOpen) resetInvite();
      }}
    >
      <DialogContent
        closeButtonDisabled={inviteMutation.isPending}
        className="grid-rows-[auto_minmax(0,1fr)] max-h-[min(720px,calc(100dvh-2rem))] overflow-clip sm:max-w-[520px]"
        initialFocus={textareaRef}
      >
        <DialogHeader>
          <DialogTitle className="text-xl font-medium leading-7 text-foreground">
            {msg('members.invite', 'Invite')}
          </DialogTitle>
        </DialogHeader>
        <form className="flex min-h-0 flex-col gap-5" onSubmit={handleSubmit}>
          <div className="subtle-scrollbar min-h-0 space-y-5 overflow-y-auto">
            <div>
              <Label htmlFor="invite-emails" className="mb-2">
                {msg('members.emails', 'Email addresses')}
              </Label>
              <Textarea
                ref={textareaRef}
                id="invite-emails"
                value={emailsValue}
                onChange={(event) => {
                  setEmailsValue(event.target.value);
                  inviteMutation.reset();
                }}
                placeholder={'claude.shannon@example.com\nshannon.claude@example.com'}
                className="min-h-[132px] resize-y"
                disabled={inviteMutation.isPending}
                aria-invalid={submitValidationError ? true : undefined}
                aria-describedby="invite-emails-help"
              />
              <p id="invite-emails-help" className="mt-2 text-xs leading-5 text-muted-foreground">
                {msg('members.emails.help', 'Separate addresses with commas, spaces, or new lines. Up to 50 at once.')}
              </p>
            </div>

            <div>
              <Label className="mb-2">{msg('members.role', 'Role')}</Label>
              <RoleSelect
                ariaLabel={msg('members.role', 'Role')}
                value={role}
                disabled={inviteMutation.isPending}
                className="w-full"
                contentClassName="min-w-[300px]"
                onChange={(nextRole) => {
                  setRole(nextRole);
                  inviteMutation.reset();
                }}
              />
            </div>

            {submitValidationError ? <InviteValidationNotice error={submitValidationError} /> : null}
            {inviteMutation.isError ? <InlineNotice>{errorMessage(inviteMutation.error)}</InlineNotice> : null}
          </div>
          <DialogFooter className="shrink-0 sm:justify-end">
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={inviteMutation.isPending}
            >
              {msg('common.cancel', 'Cancel')}
            </Button>
            <Button type="submit" size="lg" disabled={!canSubmit}>
              {inviteMutation.isPending ? msg('members.inviting', 'Inviting...') : msg('members.invite', 'Invite')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function MembersSkeletonRows() {
  const { msg } = useI18n();
  return (
    <>
      {[0, 1, 2].map((index) => (
        <TableRow
          key={index}
          className="border-border last:border-b-0"
          aria-label={msg('members.loadingRow', 'Loading member')}
        >
          <TableCell className="px-3 py-3">
            <Skeleton className="h-5 w-44 max-w-full" />
          </TableCell>
          <TableCell className="px-3 py-3">
            <Skeleton className="h-5 w-56 max-w-full" />
          </TableCell>
          <TableCell className="px-3 py-3">
            <Skeleton className="h-5 w-24 max-w-full" />
          </TableCell>
          <TableCell className="px-3 py-3" />
        </TableRow>
      ))}
    </>
  );
}

function InviteActionsMenu({
  invite,
  disabled,
  onResend,
  onRevoke,
}: {
  invite: OrganizationInvite;
  disabled: boolean;
  onResend: (invite: OrganizationInvite) => void;
  onRevoke: (invite: OrganizationInvite) => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="flex justify-end">
      <DropdownMenu>
        <DropdownMenuTrigger
          disabled={disabled}
          render={
            <Button
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
          <DropdownMenuItem onClick={() => onResend(invite)}>
            <Send className="size-4" aria-hidden />
            <span>{msg('members.resend', 'Resend invite')}</span>
          </DropdownMenuItem>
          <DropdownMenuItem variant="destructive" onClick={() => onRevoke(invite)}>
            <Trash2 className="size-4" aria-hidden />
            <span>{msg('members.revokeInvitation', 'Revoke invitation')}</span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

function InviteRevokeDialog({
  invite,
  isSubmitting,
  onCancel,
  onConfirm,
}: {
  invite: OrganizationInvite | null;
  isSubmitting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { msg } = useI18n();
  const [content, setContent] = useState(invite);
  if (invite && invite !== content) setContent(invite);

  return (
    <AlertDialog
      open={Boolean(invite)}
      onOpenChange={(nextOpen, details) => {
        if (!nextOpen && isSubmitting) {
          details.cancel();
          return;
        }
        if (!nextOpen) onCancel();
      }}
    >
      <AlertDialogContent className="grid-rows-[auto_minmax(0,1fr)_auto] max-h-[calc(100dvh-2rem)] overflow-clip">
        <AlertDialogHeader>
          <AlertDialogTitle>{msg('members.revokeTitle', 'Revoke invitation?')}</AlertDialogTitle>
        </AlertDialogHeader>
        <div className="subtle-scrollbar min-h-0 min-w-0 overflow-y-auto">
          <AlertDialogDescription className="[overflow-wrap:anywhere]">
            {msg('members.revokeBody', 'Are you sure you want to revoke the invitation for {email}?', {
              email: content?.email ?? '',
            })}
          </AlertDialogDescription>
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={isSubmitting}>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={isSubmitting} onClick={onConfirm}>
            {isSubmitting ? msg('members.revoking', 'Revoking...') : msg('members.revoke', 'Revoke')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function InviteValidationNotice({ error }: { error: InviteValidationError }) {
  const { msg } = useI18n();
  if (error.type === 'too-many') {
    return (
      <Alert variant="destructive">
        <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
        <AlertDescription>{msg('members.tooMany', 'Invite up to 50 email addresses at once.')}</AlertDescription>
      </Alert>
    );
  }

  return (
    <Alert variant="destructive">
      <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
      <AlertTitle>
        {error.emails.length === 1
          ? msg('members.invalidEmail', 'Invalid email address:')
          : msg('members.invalidEmails', 'Invalid email addresses:')}
      </AlertTitle>
      <AlertDescription>
        {error.emails.map((email) => (
          <p key={email}>{email}</p>
        ))}
        <p>{msg('members.fixEmails', 'Please remove or fix before sending invitations.')}</p>
      </AlertDescription>
    </Alert>
  );
}

function InlineNotice({ children }: { children: string }) {
  return (
    <Alert variant="destructive" className="mb-4">
      <AlertCircle className="size-4 shrink-0" aria-hidden />
      <AlertDescription>{children}</AlertDescription>
    </Alert>
  );
}

function MemberAvatar({ member }: { member: OrganizationMember }) {
  const label = initials(displayMemberName(member));
  return (
    <span className="grid size-7 shrink-0 place-items-center rounded-full bg-secondary text-xs font-medium uppercase text-muted-foreground">
      {label}
    </span>
  );
}

function isInviteRow(row: OrganizationMemberRow): row is OrganizationInvite {
  return row.type === 'invite';
}

function memberScopeLabel(
  organization: { name?: string } | undefined,
  workspaceName: string,
  msg: ReturnType<typeof useI18n>['msg'],
) {
  return organization?.name || localizedWorkspaceName(workspaceName, msg);
}

function headerClassName(columnId: string) {
  switch (columnId) {
    case 'name':
      return 'w-[34%] px-3 py-2.5 text-muted-foreground';
    case 'email':
      return 'w-[38%] px-3 py-2.5 text-muted-foreground';
    case 'role':
      return 'w-[22%] px-3 py-2.5 text-muted-foreground';
    default:
      return 'w-14 px-3 py-2.5 text-muted-foreground';
  }
}

function cellClassName(columnId: string) {
  switch (columnId) {
    case 'name':
    case 'email':
      return 'min-w-0 whitespace-normal px-3 py-2.5';
    case 'role':
      return 'px-3 py-2.5';
    default:
      return 'px-3 py-2.5';
  }
}

function normalizePlatformRole(role: unknown): PlatformRole {
  const value = typeof role === 'string' ? role.trim().toLowerCase() : '';
  return roleOptions.some((option) => option.value === value) ? (value as PlatformRole) : 'user';
}

function displayMemberName(member: OrganizationMember) {
  return member.name || member.email || 'member';
}

function isCurrentAccountMember(account: ReturnType<typeof useAuth>['account'], member: OrganizationMember) {
  if (!account) {
    return false;
  }
  return (
    account.memberships?.some((membership) => member.id === membership.user_id || member.id === membership.user_uuid) ||
    member.id === account.uuid ||
    member.id === account.tagged_id ||
    (member.email !== '' && member.email.toLowerCase() === account.email_address.toLowerCase())
  );
}

function parseEmailAddresses(value: string) {
  return value
    .split(/[,\s]+/)
    .map((email) => email.trim())
    .filter(Boolean);
}

function isEmailLike(value: string) {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value);
}

function initials(value: string) {
  const parts = value.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) {
    return '?';
  }
  if (parts.length === 1) {
    return parts[0].slice(0, 2);
  }
  return `${parts[0][0]}${parts[1][0]}`;
}

function errorMessage(error: unknown) {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') {
    return error.message;
  }
  return 'Something went wrong.';
}

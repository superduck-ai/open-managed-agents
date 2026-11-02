import { RoleSelect } from '../../shared/permissions/RoleSelect';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertCircle, MoreVertical, Plus, Send, Trash2 } from 'lucide-react';
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
import { Textarea } from '../../shared/ui/textarea';
import { toast } from '../../shared/ui/sonner';
import { useAuth } from '../../shared/auth/context';
import { useWorkspace } from '../../shared/workspaces/context';
import { localizedWorkspaceName } from '../../shared/workspaces/display-name';
import { notifyInvitationDelivery } from './invitationDelivery';
import { MembersAccessDenied } from './MembersAccessDenied';
import { canManageMembers, canViewMembers } from '../../shared/permissions/members';
import { roleOptions, type PlatformRole } from '../../shared/permissions/roles';
import { OrganizationMemberList } from './OrganizationMemberList';
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
  const { account, csrfToken, organizationAccess } = useAuth();
  const canManage = canManageMembers(account, activeOrgUuid);
  const { activeWorkspace } = useWorkspace();
  const queryClient = useQueryClient();
  const bootstrapOrganization = account?.memberships?.find((membership) => membership.organization?.uuid)?.organization;
  const activeOrganization =
    organizationAccess?.organization.uuid === activeOrgUuid
      ? organizationAccess.organization
      : (account?.memberships?.find((membership) => membership.organization?.uuid === activeOrgUuid)?.organization ??
        bootstrapOrganization);
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

      {updateRoleMutation.isError ? <InlineNotice>{errorMessage(updateRoleMutation.error)}</InlineNotice> : null}
      {inviteActionError ? <InlineNotice>{inviteActionError}</InlineNotice> : null}

      <OrganizationMemberList
        rows={filteredRows}
        canManage={canManage}
        canEdit={(member) => !isCurrentAccountMember(account, member)}
        onRole={(member, role) => updateRoleMutation.mutate({ member, role })}
        pendingRoleMemberId={pendingRoleMemberId}
        rolePending={updateRoleMutation.isPending}
        renderActions={(row) =>
          !canManage ? null : isInviteRow(row) ? (
            <InviteActionsMenu
              invite={row}
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
          ) : isCurrentAccountMember(account, row) ? (
            <span aria-hidden className="block h-8 w-8" />
          ) : (
            <OrganizationMemberRemoval
              orgUuid={activeOrgUuid}
              organizationName={activeOrganization?.name ?? msg('members.fallbackOrgName', 'this organization')}
              member={row}
              csrfToken={csrfToken}
              disabled={updateRoleMutation.isPending}
            />
          )
        }
        loading={isInitialLoading}
        hasError={hasTableError}
        filtered={Boolean(memberSearch.trim()) || roleFilter !== 'all'}
        onRetry={handleRetry}
        search={memberSearch}
        onSearch={setMemberSearch}
        role={roleFilter}
        onFilterRole={setRoleFilter}
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

function normalizePlatformRole(role: unknown): PlatformRole {
  const value = typeof role === 'string' ? role.trim().toLowerCase() : '';
  return roleOptions.some((option) => option.value === value) ? (value as PlatformRole) : 'user';
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

function errorMessage(error: unknown) {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') {
    return error.message;
  }
  return 'Something went wrong.';
}

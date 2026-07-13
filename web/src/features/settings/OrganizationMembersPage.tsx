import { createColumnHelper, flexRender, getCoreRowModel, useReactTable } from "@tanstack/react-table";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, MoreVertical, Plus, Send, Trash2 } from "lucide-react";
import { useMemo, useRef, useState, type FormEvent, type ReactNode } from "react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "../../shared/ui/alert-dialog";
import { Alert, AlertDescription, AlertTitle } from "../../shared/ui/alert";
import { Badge } from "../../shared/ui/badge";
import { Button } from "../../shared/ui/button";
import { Card, CardContent, CardHeader } from "../../shared/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "../../shared/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../shared/ui/dropdown-menu";
import { Label } from "../../shared/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../../shared/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../../shared/ui/table";
import { Textarea } from "../../shared/ui/textarea";
import { Skeleton } from "../../shared/ui/skeleton";
import { toast, Toaster } from "../../shared/ui/sonner";
import { useAuth } from "../../shared/auth/context";
import { canManageMembers } from "../../shared/permissions/members";
import { roleOptions, type PlatformRole } from "../../shared/permissions/roles";
import { useWorkspace } from "../../shared/workspaces/context";
import {
  createOrganizationInvite,
  deleteOrganizationInvite,
  listOrganizationInvites,
  listOrganizationMembers,
  resendOrganizationInvite,
  updateOrganizationMemberRole,
  type OrganizationInvite,
  type OrganizationMember,
} from "./membersApi";

type OrganizationMemberRow = OrganizationMember | OrganizationInvite;
type SelectOption<T extends string> = {
  value: T;
  label: string;
  description?: ReactNode;
  disabled?: boolean;
};
type InviteValidationError = { type: "invalid-email"; emails: string[] } | { type: "too-many" };

const memberColumnHelper = createColumnHelper<OrganizationMemberRow>();
const roleSelectOptions = roleOptions.map<SelectOption<PlatformRole>>((role) => ({
  value: role.value,
  label: role.label,
  description: role.description,
}));

export function OrganizationMembersPage() {
  const { account, csrfToken } = useAuth();
  const { orgUuid } = useWorkspace();
  const queryClient = useQueryClient();
  const bootstrapOrganization = account?.memberships?.find((membership) => membership.organization?.uuid)?.organization;
  const activeOrgUuid = orgUuid ?? bootstrapOrganization?.uuid;
  const canManage = canManageMembers(account);
  const [inviteOpen, setInviteOpen] = useState(false);
  const [inviteActionError, setInviteActionError] = useState<string | null>(null);
  const [inviteToRevoke, setInviteToRevoke] = useState<OrganizationInvite | null>(null);
  const [pendingRoleMemberId, setPendingRoleMemberId] = useState<string | null>(null);
  const membersQueryKey = ["console", "organization-members", activeOrgUuid] as const;
  const invitesQueryKey = ["console", "organization-invites", activeOrgUuid, "pending"] as const;

  const membersQuery = useQuery({
    queryKey: membersQueryKey,
    queryFn: () => listOrganizationMembers(activeOrgUuid ?? ""),
    enabled: Boolean(activeOrgUuid),
    retry: false,
  });

  const invitesQuery = useQuery({
    queryKey: invitesQueryKey,
    queryFn: () => listOrganizationInvites(activeOrgUuid ?? "", "pending"),
    enabled: Boolean(activeOrgUuid) && canManage,
    retry: false,
  });

  const updateRoleMutation = useMutation({
    mutationFn: ({ member, role }: { member: OrganizationMember; role: PlatformRole }) =>
      updateOrganizationMemberRole(activeOrgUuid ?? "", member.id, role, csrfToken),
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
    mutationFn: (invite: OrganizationInvite) => resendOrganizationInvite(activeOrgUuid ?? "", invite.id, csrfToken),
    onSuccess: (updatedInvite) => {
      queryClient.setQueryData<OrganizationInvite[]>(
        invitesQueryKey,
        (current) =>
          current?.map((invite) => (invite.id === updatedInvite.id ? updatedInvite : invite)) ?? [updatedInvite],
      );
      setInviteActionError(null);
      toast.success("Invite reminder sent.");
    },
    onError: (error) => {
      setInviteActionError(errorMessage(error));
    },
  });

  const deleteInviteMutation = useMutation({
    mutationFn: (invite: OrganizationInvite) => deleteOrganizationInvite(activeOrgUuid ?? "", invite.id, csrfToken),
    onSuccess: (_deletedInvite, invite) => {
      queryClient.setQueryData<OrganizationInvite[]>(
        invitesQueryKey,
        (current) => current?.filter((existingInvite) => existingInvite.id !== invite.id) ?? [],
      );
      setInviteToRevoke(null);
      setInviteActionError(null);
      toast.success("Invitation revoked.");
    },
    onError: (error) => {
      setInviteActionError(errorMessage(error));
    },
  });

  const tableRows = useMemo<OrganizationMemberRow[]>(
    () => [...(canManage ? (invitesQuery.data ?? []) : []), ...(membersQuery.data ?? [])],
    [canManage, invitesQuery.data, membersQuery.data],
  );
  const isInitialLoading =
    (membersQuery.isLoading && !membersQuery.data) || (canManage && invitesQuery.isLoading && !invitesQuery.data);
  const hasTableError = membersQuery.isError || (canManage && invitesQuery.isError);
  const titleCount = isInitialLoading ? 0 : tableRows.length;
  const handleRetry = () => {
    membersQuery.refetch();
    if (canManage) {
      invitesQuery.refetch();
    }
  };

  const columns = useMemo(
    () => [
      memberColumnHelper.display({
        id: "name",
        header: "Name",
        cell: ({ row }) => {
          if (isInviteRow(row.original)) {
            return (
              <div className="flex min-w-0 items-center gap-2">
                <span className="text-muted-foreground">–</span>
                <Badge variant="secondary" className="rounded-md px-1.5">
                  Pending
                </Badge>
              </div>
            );
          }

          const name = row.original.name;
          return (
            <div className="flex min-w-0 items-center gap-3">
              <MemberAvatar member={row.original} />
              <span className="truncate text-foreground">{name || row.original.email}</span>
            </div>
          );
        },
      }),
      memberColumnHelper.display({
        id: "email",
        header: "Email",
        cell: ({ row }) => <span className="truncate text-muted-foreground">{row.original.email}</span>,
      }),
      memberColumnHelper.display({
        id: "role",
        header: "Role",
        cell: ({ row }) => {
          if (isInviteRow(row.original)) {
            return <span className="text-foreground">{roleLabel(normalizePlatformRole(row.original.role))}</span>;
          }

          const member = row.original;
          const role = normalizePlatformRole(member.role);
          const isSelf = isCurrentAccountMember(account, member);

          if (!canManage || isSelf) {
            return <span className="text-foreground">{roleLabel(role)}</span>;
          }

          return (
            <RoleSelect
              ariaLabel={`Role for ${displayMemberName(member)}`}
              value={role}
              disabled={pendingRoleMemberId === member.id || updateRoleMutation.isPending}
              className="min-w-[144px]"
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
        id: "actions",
        header: "",
        cell: ({ row }) =>
          isInviteRow(row.original) ? (
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
          ) : (
            <span aria-hidden className="block h-8 w-8" />
          ),
      }),
    ],
    [account, canManage, deleteInviteMutation, pendingRoleMemberId, resendInviteMutation, updateRoleMutation],
  );

  // TanStack Table returns callback-heavy instance methods; this table instance stays local to the page.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: tableRows,
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  if (!activeOrgUuid) {
    return (
      <section className="mx-auto w-full max-w-[1180px]">
        <Card>
          <CardHeader>
            <h1 className="text-xl font-semibold tracking-normal text-foreground">Members</h1>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">No organization is available for this session.</p>
          </CardContent>
        </Card>
      </section>
    );
  }

  return (
    <section className="mx-auto w-full max-w-[1180px]" data-testid="organization-members-page">
      <Toaster position="top-right" duration={2200} closeButton toastOptions={{ closeButtonAriaLabel: "Close" }} />
      <div className="mb-6 flex min-h-9 items-center justify-between gap-4">
        <h1 className="flex min-w-0 items-center gap-2 text-xl font-semibold tracking-normal text-foreground">
          <span>Members</span>
          <Badge variant="secondary" className="min-w-5 rounded-full px-1.5">
            {titleCount}
          </Badge>
        </h1>
        {canManage ? (
          <Button
            size="lg"
            onClick={() => {
              setInviteActionError(null);
              setInviteOpen(true);
            }}
          >
            <Plus className="size-4" aria-hidden />
            Invite
          </Button>
        ) : null}
      </div>

      {updateRoleMutation.isError ? <InlineNotice>{errorMessage(updateRoleMutation.error)}</InlineNotice> : null}
      {inviteActionError ? <InlineNotice>{inviteActionError}</InlineNotice> : null}

      <div className="overflow-hidden border-y border-border">
        <Table className="table-fixed text-left" aria-label="Members">
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
          <TableBody>
            {isInitialLoading ? <MembersSkeletonRows /> : null}
            {!isInitialLoading && hasTableError ? (
              <TableRow>
                <TableCell colSpan={4} className="px-3 py-10">
                  <Alert variant="destructive" className="mx-auto max-w-xl">
                    <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
                    <AlertTitle>Members could not be loaded.</AlertTitle>
                    <AlertDescription>
                      <p>Try again.</p>
                      <Button type="button" variant="outline" size="sm" className="mt-3" onClick={handleRetry}>
                        Try again
                      </Button>
                    </AlertDescription>
                  </Alert>
                </TableCell>
              </TableRow>
            ) : null}
            {!isInitialLoading && !hasTableError && table.getRowModel().rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={4} className="px-3 py-10 text-center text-sm text-muted-foreground">
                  No members found.
                </TableCell>
              </TableRow>
            ) : null}
            {!isInitialLoading && !hasTableError
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
      </div>

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
          toast.success(createdInvites.length === 1 ? "Invite sent." : `${createdInvites.length} invites sent.`);
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
  const [emailsValue, setEmailsValue] = useState("");
  const [role, setRole] = useState<PlatformRole>("user");
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
      setEmailsValue("");
      setRole("user");
      setSubmitValidationError(null);
      onOpenChange(false);
      onInvited(createdInvites);
    },
  });

  const canSubmit = parsedEmails.length > 0 && !inviteMutation.isPending;

  const closeDialog = (nextOpen: boolean) => {
    onOpenChange(nextOpen);
    if (!nextOpen) {
      setEmailsValue("");
      setRole("user");
      setSubmitValidationError(null);
      inviteMutation.reset();
    }
  };

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    if (parsedEmails.length > 50) {
      setSubmitValidationError({ type: "too-many" });
      return;
    }
    const invalidEmails = parsedEmails.filter((email) => !isEmailLike(email));
    if (invalidEmails.length > 0) {
      setSubmitValidationError({ type: "invalid-email", emails: invalidEmails });
      return;
    }
    setSubmitValidationError(null);
    inviteMutation.mutate({ emails: parsedEmails, inviteRole: role });
  };

  return (
    <Dialog open={open} onOpenChange={closeDialog}>
      <DialogContent
        className="max-h-[min(720px,calc(100vh-48px))] overflow-y-auto sm:max-w-[520px]"
        initialFocus={textareaRef}
      >
        <DialogHeader>
          <DialogTitle>Invite members</DialogTitle>
        </DialogHeader>
        <form className="space-y-5" onSubmit={handleSubmit}>
          <div>
            <Label htmlFor="invite-emails" className="mb-2">
              Email addresses
            </Label>
            <Textarea
              ref={textareaRef}
              id="invite-emails"
              value={emailsValue}
              onChange={(event) => {
                setEmailsValue(event.target.value);
                inviteMutation.reset();
              }}
              placeholder={"claude.shannon@example.com\nshannon.claude@example.com"}
              className="min-h-[132px] resize-y"
              disabled={inviteMutation.isPending}
              aria-invalid={submitValidationError ? true : undefined}
              aria-describedby="invite-emails-help"
            />
            <p id="invite-emails-help" className="mt-2 text-xs leading-5 text-muted-foreground">
              Separate addresses with commas, spaces, or new lines. Up to 50 at once.
            </p>
          </div>

          <div>
            <Label className="mb-2">Role</Label>
            <RoleSelect
              ariaLabel="Role"
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

          <div className="flex justify-end">
            <Button type="submit" size="lg" disabled={!canSubmit}>
              {inviteMutation.isPending ? "Inviting..." : "Invite"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RoleSelect({
  ariaLabel,
  value,
  disabled,
  className,
  contentClassName,
  onChange,
}: {
  ariaLabel: string;
  value: PlatformRole;
  disabled?: boolean;
  className?: string;
  contentClassName?: string;
  onChange: (value: PlatformRole) => void;
}) {
  return (
    <Select<PlatformRole>
      value={value}
      items={roleSelectOptions.map((option) => ({ value: option.value, label: option.label }))}
      disabled={disabled}
      onValueChange={(nextValue) => {
        if (nextValue !== null) {
          onChange(nextValue);
        }
      }}
    >
      <SelectTrigger aria-label={ariaLabel} className={className}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false} className={contentClassName}>
        {roleSelectOptions.map((option) => (
          <SelectItem key={option.value} value={option.value} label={option.label} disabled={option.disabled}>
            <span className="block">
              <span className="block leading-5">{option.label}</span>
              {option.description ? (
                <span className="mt-0.5 block text-xs leading-4 text-muted-foreground">{option.description}</span>
              ) : null}
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function MembersSkeletonRows() {
  return (
    <>
      {[0, 1, 2].map((index) => (
        <TableRow key={index} className="border-border last:border-b-0" aria-label="Loading member">
          <TableCell className="px-3 py-3">
            <Skeleton className="h-5 w-44" />
          </TableCell>
          <TableCell className="px-3 py-3">
            <Skeleton className="h-5 w-56" />
          </TableCell>
          <TableCell className="px-3 py-3">
            <Skeleton className="h-5 w-24" />
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
  return (
    <div className="flex justify-end">
      <DropdownMenu>
        <DropdownMenuTrigger
          disabled={disabled}
          render={<Button variant="ghost" size="icon" className="text-muted-foreground" aria-label="More actions" />}
        >
          <MoreVertical className="size-4" aria-hidden />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-44">
          <DropdownMenuItem onClick={() => onResend(invite)}>
            <Send className="size-4" aria-hidden />
            <span>Resend invite</span>
          </DropdownMenuItem>
          <DropdownMenuItem variant="destructive" onClick={() => onRevoke(invite)}>
            <Trash2 className="size-4" aria-hidden />
            <span>Revoke invitation</span>
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
  if (!invite) {
    return null;
  }

  return (
    <AlertDialog
      open={Boolean(invite)}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && !isSubmitting) {
          onCancel();
        }
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Revoke invitation?</AlertDialogTitle>
          <AlertDialogDescription>
            Are you sure you want to revoke the invitation for {invite.email}?
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={isSubmitting} onClick={onCancel}>
            Cancel
          </AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={isSubmitting} onClick={onConfirm}>
            {isSubmitting ? "Revoking..." : "Revoke"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function InviteValidationNotice({ error }: { error: InviteValidationError }) {
  if (error.type === "too-many") {
    return (
      <Alert variant="destructive">
        <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
        <AlertDescription>Invite up to 50 email addresses at once.</AlertDescription>
      </Alert>
    );
  }

  return (
    <Alert variant="destructive">
      <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
      <AlertTitle>{error.emails.length === 1 ? "Invalid email address:" : "Invalid email addresses:"}</AlertTitle>
      <AlertDescription>
        {error.emails.map((email) => (
          <p key={email}>{email}</p>
        ))}
        <p>Please remove or fix before sending invitations.</p>
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
    <span className="grid size-7 shrink-0 place-items-center rounded-full bg-secondary text-[11px] font-medium uppercase text-muted-foreground">
      {label}
    </span>
  );
}

function isInviteRow(row: OrganizationMemberRow): row is OrganizationInvite {
  return row.type === "invite";
}

function headerClassName(columnId: string) {
  switch (columnId) {
    case "name":
      return "w-[34%] px-3 py-2.5 text-muted-foreground";
    case "email":
      return "w-[38%] px-3 py-2.5 text-muted-foreground";
    case "role":
      return "w-[22%] px-3 py-2.5 text-muted-foreground";
    default:
      return "w-10 px-3 py-2.5 text-muted-foreground";
  }
}

function cellClassName(columnId: string) {
  switch (columnId) {
    case "name":
    case "email":
      return "min-w-0 whitespace-normal px-3 py-2.5";
    case "role":
      return "px-3 py-2.5";
    default:
      return "px-3 py-2.5";
  }
}

function roleLabel(role: PlatformRole) {
  return roleOptions.find((option) => option.value === role)?.label ?? titleizeRole(role);
}

function normalizePlatformRole(role: unknown): PlatformRole {
  const value = typeof role === "string" ? role.trim().toLowerCase() : "";
  return roleOptions.some((option) => option.value === value) ? (value as PlatformRole) : "user";
}

function displayMemberName(member: OrganizationMember) {
  return member.name || member.email || "member";
}

function isCurrentAccountMember(account: ReturnType<typeof useAuth>["account"], member: OrganizationMember) {
  if (!account) {
    return false;
  }
  return (
    member.id === account.uuid ||
    member.id === account.tagged_id ||
    (member.email !== "" && member.email.toLowerCase() === account.email_address.toLowerCase())
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
    return "?";
  }
  if (parts.length === 1) {
    return parts[0].slice(0, 2);
  }
  return `${parts[0][0]}${parts[1][0]}`;
}

function titleizeRole(role: string) {
  return role
    .split("_")
    .filter(Boolean)
    .map((part) => `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`)
    .join(" ");
}

function errorMessage(error: unknown) {
  if (error && typeof error === "object" && "message" in error && typeof error.message === "string") {
    return error.message;
  }
  return "Something went wrong.";
}

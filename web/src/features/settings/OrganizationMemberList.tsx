import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  useReactTable,
  type Table as TableInstance,
} from '@tanstack/react-table';
import { useMemo, type ReactNode } from 'react';
import { AlertCircle, Users } from 'lucide-react';
import { useI18n } from '../../shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '../../shared/ui/alert';
import { Badge } from '../../shared/ui/badge';
import { Button } from '../../shared/ui/button';
import { ResourceListState } from '../../shared/ui/resource-list-state';
import { RoleSelect } from '../../shared/permissions/RoleSelect';
import { platformRoleLabel, roleOptions, type PlatformRole } from '../../shared/permissions/roles';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { Skeleton } from '../../shared/ui/skeleton';
import { MemberDirectoryFilters } from './MemberDirectoryFilters';
import type { OrganizationMember, OrganizationInvite } from './membersApi';
export type OrganizationMemberRow = OrganizationMember | OrganizationInvite;
const memberColumnHelper = createColumnHelper<OrganizationMemberRow>();
export function OrganizationMemberList({
  rows,
  canManage,
  canInvite = canManage,
  canEdit,
  onRole,
  pendingRoleMemberId,
  rolePending,
  renderActions,
  loading,
  hasError,
  filtered,
  onRetry,
  search,
  onSearch,
  role,
  onFilterRole,
}: {
  rows: OrganizationMemberRow[];
  canManage: boolean;
  canInvite?: boolean;
  canEdit: (member: OrganizationMember) => boolean;
  onRole: (member: OrganizationMember, role: PlatformRole) => void;
  pendingRoleMemberId?: string | null;
  rolePending: boolean;
  renderActions: (row: OrganizationMemberRow) => ReactNode;
  loading: boolean;
  hasError: boolean;
  filtered: boolean;
  onRetry: () => void;
  search: string;
  onSearch: (value: string) => void;
  role: string;
  onFilterRole: (value: string) => void;
}) {
  const { msg } = useI18n();
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
          const isSelf = !canEdit(member);

          if (!canManage || isSelf) {
            return <span className="text-foreground">{platformRoleLabel(role, msg)}</span>;
          }

          return (
            <RoleSelect
              ariaLabel={msg('members.role.for', 'Role for {name}', { name: displayMemberName(member) })}
              value={role}
              disabled={pendingRoleMemberId === member.id || rolePending}
              className="w-full min-w-0"
              contentClassName="min-w-[300px]"
              onChange={(nextRole) => {
                if (nextRole !== role) {
                  onRole(member, nextRole);
                }
              }}
            />
          );
        },
      }),
      memberColumnHelper.display({
        id: 'actions',
        header: '',
        cell: ({ row }) => renderActions(row.original),
      }),
    ],
    [canEdit, canManage, msg, pendingRoleMemberId, renderActions, rolePending, onRole],
  );
  const table = useReactTable({ data: rows, columns, getCoreRowModel: getCoreRowModel() });
  return (
    <>
      <MemberDirectoryFilters
        search={search}
        onSearch={onSearch}
        role={role}
        onRole={onFilterRole}
        roles={roleOptions.map((role) => ({ value: role.value, label: platformRoleLabel(role.value, msg) }))}
        roleLabelPrefix="members.role"
      />
      <OrganizationMemberDirectory
        table={table}
        loading={loading}
        hasError={hasError}
        filtered={filtered}
        canManage={canInvite}
        onRetry={onRetry}
      />
    </>
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

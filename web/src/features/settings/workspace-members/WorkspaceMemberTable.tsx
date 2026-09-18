import { ArrowUpDown, MoreVertical, Trash2 } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import {
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
  DataTableCell,
  DataTableRow,
} from '../../../shared/ui/data-table-interactions';
import { Table, TableHeader, TableHead, TableRow, TableBody, TableCell } from '../../../shared/ui/table';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { MemberRoleSelect } from './MemberRoleSelect';
import { workspaceMemberRoles, type WorkspaceMember, type WorkspaceMemberRole } from './api';
export type MemberSort = { field: 'name' | 'email' | 'workspace_role'; descending: boolean };
export function WorkspaceMemberTable({
  members,
  loading,
  pending,
  sort,
  setSort,
  onRole,
  onRemove,
}: {
  members: WorkspaceMember[];
  loading: boolean;
  pending: boolean;
  sort: MemberSort;
  setSort: (sort: MemberSort) => void;
  onRole: (member: WorkspaceMember, role: WorkspaceMemberRole) => void;
  onRemove: (member: WorkspaceMember) => void;
}) {
  const { msg } = useI18n();
  const columnLabels = {
    name: msg('members.table.name', 'Name'),
    email: msg('members.email', 'Email'),
    workspace_role: msg('members.role', 'Role'),
  } as const;
  return (
    <Table className={dataTableClassName} aria-label={msg('members.table.workspaceMembers', 'Workspace members')}>
      <colgroup>
        <col className="w-[30%]" />
        <col className="w-[32%]" />
        <col className="w-[26%]" />
        <col className="w-[12%]" />
      </colgroup>
      <TableHeader>
        <TableRow className={dataTableHeaderRowClassName}>
          {(['name', 'email', 'workspace_role'] as const).map((field) => (
            <TableHead key={field} className={dataTableHeaderCellClassName}>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setSort({ field, descending: sort.field === field && !sort.descending })}
              >
                {columnLabels[field]}
                <ArrowUpDown className="size-3" aria-hidden />
              </Button>
            </TableHead>
          ))}
          <TableHead className={dataTableHeaderCellClassName}>
            <span className="sr-only">{msg('members.table.actions', 'Actions')}</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          <TableRow>
            <TableCell colSpan={4}>{msg('members.table.loading', 'Loading members...')}</TableCell>
          </TableRow>
        ) : null}
        {!loading && members.length === 0 ? (
          <TableRow>
            <TableCell colSpan={4}>{msg('members.table.empty', 'No members found.')}</TableCell>
          </TableRow>
        ) : null}
        {members.map((member) => (
          <DataTableRow key={member.user_id}>
            <DataTableCell edge="start" className="max-w-80 truncate">
              {member.name || member.email}
            </DataTableCell>
            <DataTableCell className="max-w-80 truncate">{member.email}</DataTableCell>
            <DataTableCell>
              {member.can_edit ? (
                <MemberRoleSelect
                  value={member.workspace_role}
                  includeBilling
                  disabled={pending}
                  label={msg('members.roleFor', `Role for ${member.name || member.email}`, {
                    name: member.name || member.email,
                  })}
                  onChange={(role) => {
                    if (role !== member.workspace_role) onRole(member, role);
                  }}
                />
              ) : (
                msg(
                  `members.workspaceRole.${member.workspace_role}`,
                  workspaceMemberRoles.find((role) => role.value === member.workspace_role)?.label ??
                    member.workspace_role,
                )
              )}
            </DataTableCell>
            <DataTableCell edge="end">
              {member.can_remove ? (
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={msg('members.moreActionsFor', `More actions for ${member.name || member.email}`, {
                          name: member.name || member.email,
                        })}
                      />
                    }
                  >
                    <MoreVertical className="size-4" aria-hidden />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem
                      variant="destructive"
                      onClick={() => {
                        onRemove(member);
                      }}
                    >
                      <Trash2 className="size-4" aria-hidden />
                      <span className="whitespace-nowrap">{msg('members.removeMember', 'Remove member')}</span>
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              ) : null}
            </DataTableCell>
          </DataTableRow>
        ))}
      </TableBody>
    </Table>
  );
}

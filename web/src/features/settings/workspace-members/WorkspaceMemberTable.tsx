import { ArrowUpDown, Trash2 } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Button } from '../../../shared/ui/button';
import {
  DataTableCell,
  DataTableRow,
  MoreActionsButton,
  dataTableActionsColumnClassName,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
  dataTableMetaColumnClassName,
  dataTablePrimaryColumnClassName,
} from '../../../shared/ui/data-table-interactions';
import { TableSkeletonRows } from '../../../shared/ui/resource-table';
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
  const showRows = !loading && members.length > 0;
  const columnLabels = {
    name: msg('members.table.name', 'Name'),
    email: msg('members.email', 'Email'),
    workspace_role: msg('members.role', 'Role'),
  } as const;
  const columnClasses = {
    name: dataTablePrimaryColumnClassName,
    email: dataTableMetaColumnClassName,
    workspace_role: cn(dataTableMetaColumnClassName, 'w-[200px]'),
  } as const;
  return (
    <Table className={dataTableClassName} aria-label={msg('members.table.workspaceMembers', 'Workspace members')}>
      {showRows || loading ? (
        <TableHeader>
          <TableRow className={dataTableHeaderRowClassName}>
            {(['name', 'email', 'workspace_role'] as const).map((field) => (
              <TableHead
                key={field}
                className={cn(dataTableHeaderCellClassName, columnClasses[field])}
                aria-sort={sort.field === field ? (sort.descending ? 'descending' : 'ascending') : 'none'}
              >
                <Button
                  variant="ghost"
                  size="sm"
                  className="-ml-2 h-7 px-2 text-xs font-medium text-muted-foreground hover:text-foreground"
                  onClick={() => setSort({ field, descending: sort.field === field && !sort.descending })}
                >
                  {columnLabels[field]}
                  <ArrowUpDown className="size-3" aria-hidden />
                </Button>
              </TableHead>
            ))}
            <TableHead className={cn(dataTableHeaderCellClassName, dataTableActionsColumnClassName)}>
              <span className="sr-only">{msg('members.table.actions', 'Actions')}</span>
            </TableHead>
          </TableRow>
        </TableHeader>
      ) : null}
      <TableBody>
        {loading ? <TableSkeletonRows colCount={4} label={msg('members.table.loading', 'Loading members...')} /> : null}
        {!loading && members.length === 0 ? (
          <TableRow>
            <TableCell colSpan={4}>{msg('members.table.empty', 'No members found.')}</TableCell>
          </TableRow>
        ) : null}
        {members.map((member) => (
          <DataTableRow key={member.user_id}>
            <DataTableCell
              edge="start"
              className={cn(dataTablePrimaryColumnClassName, 'max-w-[280px] truncate font-medium')}
            >
              {member.name || member.email}
            </DataTableCell>
            <DataTableCell className={cn(dataTableMetaColumnClassName, 'max-w-[300px] truncate text-muted-foreground')}>
              {member.email}
            </DataTableCell>
            <DataTableCell className={cn(dataTableMetaColumnClassName, 'w-[200px]')}>
              {member.can_edit ? (
                <MemberRoleSelect
                  value={member.workspace_role}
                  className="w-full"
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
            <DataTableCell edge="end" className={dataTableActionsColumnClassName}>
              {member.can_remove ? (
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <MoreActionsButton
                        disabled={pending}
                        label={msg('members.moreActionsFor', `More actions for ${member.name || member.email}`, {
                          name: member.name || member.email,
                        })}
                      />
                    }
                  />
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

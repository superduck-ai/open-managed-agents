import { ArrowUpDown, MoreVertical, Trash2 } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
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
  failed,
  pending,
  sort,
  setSort,
  onRole,
  onRemove,
}: {
  members: WorkspaceMember[];
  loading: boolean;
  failed: boolean;
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
    <Table aria-label={msg('members.table.workspaceMembers', 'Workspace members')}>
      <TableHeader>
        <TableRow>
          {(['name', 'email', 'workspace_role'] as const).map((field) => (
            <TableHead key={field}>
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
          <TableHead className="w-12">
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
        {!loading && !failed && members.length === 0 ? (
          <TableRow>
            <TableCell colSpan={4}>{msg('members.table.empty', 'No members found.')}</TableCell>
          </TableRow>
        ) : null}
        {members.map((member) => (
          <TableRow key={member.user_id}>
            <TableCell className="max-w-80 truncate">{member.name || member.email}</TableCell>
            <TableCell className="max-w-80 truncate">{member.email}</TableCell>
            <TableCell>
              {member.can_edit ? (
                <MemberRoleSelect
                  value={member.workspace_role}
                  billing={member.organization_role === 'billing'}
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
              {member.role_source === 'organization' ? (
                <span className="mt-1 block text-xs text-muted-foreground">
                  {msg('members.table.inheritedFromOrganization', 'Inherited from organization')}
                </span>
              ) : null}
            </TableCell>
            <TableCell>
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
                      {msg('members.removeMember', 'Remove member')}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              ) : null}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

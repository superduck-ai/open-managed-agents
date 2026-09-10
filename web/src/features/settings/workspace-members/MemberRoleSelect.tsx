import { useI18n } from '../../../shared/i18n';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { workspaceMemberRoles, type WorkspaceMemberRole } from './api';

export function MemberRoleSelect({
  value,
  billing = false,
  disabled,
  onChange,
  label,
}: {
  value: WorkspaceMemberRole;
  billing?: boolean;
  disabled?: boolean;
  onChange: (value: WorkspaceMemberRole) => void;
  label: string;
}) {
  const { msg } = useI18n();
  const roles = workspaceMemberRoles.filter((role) =>
    billing
      ? role.value === 'workspace_billing' || role.value === 'workspace_admin'
      : role.value !== 'workspace_billing',
  );
  return (
    <Select
      value={value}
      disabled={disabled}
      onValueChange={(next) => {
        if (next) onChange(next as WorkspaceMemberRole);
      }}
    >
      <SelectTrigger aria-label={label}>
        <SelectValue>
          {msg(
            `members.workspaceRole.${value}`,
            workspaceMemberRoles.find((role) => role.value === value)?.label ?? value,
          )}
        </SelectValue>
      </SelectTrigger>
      <SelectContent>
        {roles.map((role) => (
          <SelectItem key={role.value} value={role.value}>
            {msg(`members.workspaceRole.${role.value}`, role.label)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

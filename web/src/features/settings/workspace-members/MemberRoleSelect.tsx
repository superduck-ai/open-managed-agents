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
        <SelectValue>{workspaceMemberRoles.find((role) => role.value === value)?.label}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        {roles.map((role) => (
          <SelectItem key={role.value} value={role.value}>
            {role.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

import { useI18n } from '../../../shared/i18n';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { workspaceMemberRoles, type WorkspaceMemberRole } from './api';

export function MemberRoleSelect({
  value,
  disabled,
  onChange,
  label,
  id,
  className,
}: {
  value: WorkspaceMemberRole;
  disabled?: boolean;
  onChange: (value: WorkspaceMemberRole) => void;
  label: string;
  id?: string;
  className?: string;
}) {
  const { msg } = useI18n();
  return (
    <Select
      value={value}
      disabled={disabled}
      onValueChange={(next) => {
        if (next) onChange(next as WorkspaceMemberRole);
      }}
    >
      <SelectTrigger id={id} className={className} aria-label={label}>
        <SelectValue>
          {msg(
            `members.workspaceRole.${value}`,
            workspaceMemberRoles.find((role) => role.value === value)?.label ?? value,
          )}
        </SelectValue>
      </SelectTrigger>
      <SelectContent>
        {workspaceMemberRoles.map((role) => (
          <SelectItem key={role.value} value={role.value}>
            {msg(`members.workspaceRole.${role.value}`, role.label)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

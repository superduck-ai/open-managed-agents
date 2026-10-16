import { useI18n } from '../../shared/i18n';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';

export function OrganizationRoleSelect({
  value,
  onChange,
  label,
  disabled,
  includeAdmin = true,
}: {
  value: string;
  onChange: (value: string) => void;
  label: string;
  disabled?: boolean;
  includeAdmin?: boolean;
}) {
  const { msg } = useI18n();

  const organizationRoles = [
    { value: 'user', label: 'User', description: msg('platformAdmin.organizationMember', 'Organization member') },
    {
      value: 'admin',
      label: 'Admin',
      description: msg('platformAdmin.manageOrganizationDetailsAndMembers', 'Manage organization details and members'),
    },
  ];

  const roles = organizationRoles.filter((role) => includeAdmin || role.value !== 'admin');
  return (
    <Select
      value={value}
      items={roles}
      disabled={disabled}
      onValueChange={(next) => {
        if (next) onChange(next);
      }}
    >
      <SelectTrigger className="w-full" aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false} className="w-[min(26rem,calc(100vw-3rem))]">
        {roles.map((role) => (
          <SelectItem key={role.value} value={role.value} label={role.label}>
            <span className="block whitespace-normal">
              <span className="block leading-5">{role.label}</span>
              <span className="mt-0.5 block text-xs leading-4 text-muted-foreground">{role.description}</span>
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

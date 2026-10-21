import { useI18n } from '../i18n';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../ui/select';
import { roleOptions, platformRoleLabel, platformRoleDescription, type PlatformRole } from './roles';

export function RoleSelect({
  ariaLabel,
  value,
  disabled,
  className,
  contentClassName,
  onChange,
  includeAdmin = true,
}: {
  ariaLabel: string;
  value: PlatformRole;
  disabled?: boolean;
  className?: string;
  contentClassName?: string;
  includeAdmin?: boolean;
  onChange: (value: PlatformRole) => void;
}) {
  const { msg } = useI18n();
  const options = roleOptions
    .filter((role) => includeAdmin || role.value !== 'admin')
    .map((role) => ({
      value: role.value,
      label: platformRoleLabel(role.value, msg),
      description: platformRoleDescription(role.value, msg),
    }));
  return (
    <Select<PlatformRole>
      value={value}
      items={options.map((option) => ({ value: option.value, label: option.label }))}
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
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value} label={option.label}>
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

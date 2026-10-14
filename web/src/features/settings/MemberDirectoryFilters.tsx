import { useI18n } from '../../shared/i18n';
import { Input } from '../../shared/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';

export function MemberDirectoryFilters({
  search,
  onSearch,
  role,
  onRole,
  roles,
  roleLabelPrefix = 'members.workspaceRole',
}: {
  search: string;
  onSearch: (value: string) => void;
  role: string;
  onRole: (value: string) => void;
  roles: readonly { value: string; label: string }[];
  roleLabelPrefix?: string;
}) {
  const { msg } = useI18n();
  return (
    <div className="mb-4 flex flex-wrap gap-2">
      <Input
        aria-label={msg('members.search', 'Search members')}
        placeholder={msg('members.searchPlaceholder', 'Search by name or email')}
        className="w-full sm:w-80"
        value={search}
        onChange={(event) => onSearch(event.target.value)}
      />
      <Select
        value={role}
        onValueChange={(value) => {
          if (value) onRole(value);
        }}
      >
        <SelectTrigger aria-label={msg('members.roleFilterLabel', 'Role')} className="w-auto min-w-28">
          <span className="text-muted-foreground">{msg('members.roleFilterLabel', 'Role')}</span>
          <SelectValue className="font-medium text-foreground">
            {role === 'all'
              ? msg('members.roleFilterAll', 'All')
              : msg(`${roleLabelPrefix}.${role}`, roles.find((option) => option.value === role)?.label ?? role)}
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{msg('members.roleFilterAll', 'All')}</SelectItem>
          {roles.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {msg(`${roleLabelPrefix}.${option.value}`, option.label)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

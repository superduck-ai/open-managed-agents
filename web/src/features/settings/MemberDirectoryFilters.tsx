import { useI18n } from '../../shared/i18n';
import { Input } from '../../shared/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';

export function MemberDirectoryFilters({
  search,
  onSearch,
  role,
  onRole,
  roles,
}: {
  search: string;
  onSearch: (value: string) => void;
  role: string;
  onRole: (value: string) => void;
  roles: readonly { value: string; label: string }[];
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
        <SelectTrigger aria-label={msg('members.filterByRole', 'Filter by role')} className="w-auto min-w-28">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{msg('members.allRoles', 'All roles')}</SelectItem>
          {roles.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {msg(`members.workspaceRole.${option.value}`, option.label)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

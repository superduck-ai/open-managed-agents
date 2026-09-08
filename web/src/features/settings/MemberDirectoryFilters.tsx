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
  return (
    <div className="mb-4 flex flex-wrap gap-2">
      <Input
        aria-label="Search members"
        placeholder="Search by name or email"
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
        <SelectTrigger aria-label="Filter by role" className="w-auto min-w-28">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">All roles</SelectItem>
          {roles.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

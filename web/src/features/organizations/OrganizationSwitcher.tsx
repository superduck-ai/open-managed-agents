import { useState } from 'react';
import { Building2, Check } from 'lucide-react';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from '../../shared/ui/command';
import { organizationRoleLabel } from './api';
import {
  filterOrganizations,
  organizationNameParts,
  organizationPickerRemainder,
  type OrganizationPickerItem,
} from './organizationPicker';

export function OrganizationSwitcher({
  open,
  onOpenChange,
  items,
  currentOrgUuid,
  switching,
  onSelect,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  items: OrganizationPickerItem[];
  currentOrgUuid?: string;
  switching: boolean;
  onSelect: (orgUuid: string) => void;
}) {
  const [query, setQuery] = useState('');
  const result = filterOrganizations(items, query, currentOrgUuid);
  const remainder = organizationPickerRemainder(result);
  const changeOpen = (next: boolean) => {
    if (!next) setQuery('');
    onOpenChange(next);
  };
  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogContent className="gap-0 overflow-hidden p-0 sm:max-w-md">
        <DialogHeader className="px-4 pt-4 pb-2">
          <DialogTitle>切换组织</DialogTitle>
          <DialogDescription>按部门名称或完整路径搜索。</DialogDescription>
        </DialogHeader>
        <Command shouldFilter={false}>
          <CommandInput autoFocus aria-label="搜索组织" placeholder="搜索组织" value={query} onValueChange={setQuery} />
          <CommandList className="max-h-80 overflow-y-auto [scrollbar-gutter:stable] [&::-webkit-scrollbar]:w-2 [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-muted-foreground/40">
            {result.searching && result.matches.length === 0 ? <CommandEmpty>没有匹配的组织。</CommandEmpty> : null}
            {result.matches.map((item) => {
              const parts = organizationNameParts(item.name);
              const current = item.uuid === currentOrgUuid;
              return (
                <CommandItem
                  key={item.uuid}
                  value={item.uuid}
                  disabled={switching}
                  onSelect={() => {
                    if (current) {
                      changeOpen(false);
                      return;
                    }
                    onSelect(item.uuid);
                  }}
                >
                  <Building2 />
                  <span className="min-w-0 flex-1">
                    <span className="flex items-baseline gap-2">
                      <span className="truncate">{parts.title}</span>
                      <span className="shrink-0 text-xs text-muted-foreground">{organizationRoleLabel(item.role)}</span>
                    </span>
                    {parts.detail ? (
                      <span className="block truncate text-xs text-muted-foreground">{parts.detail}</span>
                    ) : null}
                  </span>
                  {current ? <Check className="text-foreground" /> : null}
                </CommandItem>
              );
            })}
          </CommandList>
        </Command>
        {remainder ? (
          <p className="border-t border-border px-4 py-2 text-xs text-muted-foreground">{remainder}</p>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

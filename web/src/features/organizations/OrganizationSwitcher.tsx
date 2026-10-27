import { useI18n } from '../../shared/i18n';
import { useEffect, useRef, useState } from 'react';
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
  const { msg } = useI18n();
  const [query, setQuery] = useState('');
  const searchRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (!open) setQuery('');
  }, [open]);
  const result = filterOrganizations(items, query, currentOrgUuid);
  const remainder = organizationPickerRemainder(result, msg);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent initialFocus={searchRef} className="gap-0 overflow-hidden p-0 sm:max-w-md">
        <DialogHeader className="px-4 pt-4 pb-2">
          <DialogTitle>{msg('organizations.switch', 'Switch organization')}</DialogTitle>
          <DialogDescription>
            {msg('organizations.searchDescription', 'Search by department name or full path.')}
          </DialogDescription>
        </DialogHeader>
        <Command
          defaultValue={currentOrgUuid}
          label={msg('organizations.search', 'Search organizations')}
          shouldFilter={false}
        >
          <CommandInput
            ref={searchRef}
            aria-label={msg('organizations.search', 'Search organizations')}
            placeholder={msg('organizations.search', 'Search organizations')}
            value={query}
            onValueChange={setQuery}
          />
          <CommandList className="max-h-80 [scrollbar-gutter:stable]">
            {result.searching && result.matches.length === 0 ? (
              <CommandEmpty>{msg('organizations.noMatches', 'No matching organizations.')}</CommandEmpty>
            ) : null}
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
                      onOpenChange(false);
                      return;
                    }
                    onSelect(item.uuid);
                  }}
                >
                  <Building2 />
                  <span className="min-w-0 flex-1">
                    <span className="flex items-baseline gap-2">
                      <span className="truncate">{parts.title}</span>
                      <span className="shrink-0 text-xs text-muted-foreground">
                        {organizationRoleLabel(item.role, msg)}
                      </span>
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
          <p className="border-t-[0.5px] border-border px-4 py-2 text-xs text-muted-foreground">{remainder}</p>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

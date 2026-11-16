import { useId, useState } from 'react';
import { Check, ChevronsUpDown, Plus } from 'lucide-react';
import type { OAuthProviderRecord } from '../../../shared/api/platform-admin';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from '../../../shared/ui/command';
import { Label } from '../../../shared/ui/label';
import { Popover, PopoverContent, PopoverTrigger } from '../../../shared/ui/popover';
import type { EntityOption } from '../types';
import { WorkspaceOAuthProviderForm } from './workspace-oauth-provider-form';

export function WorkspaceOAuthProviderPicker({
  label,
  value,
  placeholder,
  options,
  orgUuid,
  workspaceId,
  onChange,
  onCreated,
}: {
  label: string;
  value: string;
  placeholder: string;
  options: EntityOption[];
  orgUuid?: string;
  workspaceId?: string;
  onChange: (value: string) => void;
  onCreated: (provider: OAuthProviderRecord) => void;
}) {
  const { msg } = useI18n();
  const id = useId();
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const selected = options.find((option) => option.id === value);
  const customLabel = msg('managedAgents.credentialVaults.credentialDialog.customProvider', 'Custom OAuth provider');
  return (
    <div>
      <Label htmlFor={id}>{label}</Label>
      <Popover
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (!next) setCreating(false);
        }}
      >
        <PopoverTrigger
          render={
            <Button
              id={id}
              type="button"
              variant="outline"
              role="combobox"
              aria-expanded={open}
              className="mt-2 h-10 w-full justify-between font-normal"
            />
          }
        >
          <span className="flex min-w-0 items-center gap-2">
            {selected?.icon}
            <span className="truncate">{selected?.label ?? placeholder}</span>
          </span>
          <ChevronsUpDown className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        </PopoverTrigger>
        <PopoverContent align="start" className="w-(--anchor-width) gap-0 p-0">
          {creating && orgUuid && workspaceId ? (
            <WorkspaceOAuthProviderForm
              orgUuid={orgUuid}
              workspaceId={workspaceId}
              onClose={() => setCreating(false)}
              onCreated={(provider) => {
                onCreated(provider);
                setOpen(false);
                setCreating(false);
              }}
            />
          ) : (
            <Command>
              <CommandInput placeholder={msg('select.search', 'Search options...')} />
              <CommandList>
                <CommandEmpty>{msg('select.empty', 'No matching options.')}</CommandEmpty>
                {[{ id: '', label: placeholder }, ...options].map((option) => (
                  <CommandItem
                    key={option.id}
                    value={option.id || '__empty__'}
                    keywords={[option.label]}
                    onSelect={() => {
                      onChange(option.id);
                      setOpen(false);
                    }}
                  >
                    <span className="flex min-w-0 flex-1 items-center gap-2">
                      {'icon' in option ? option.icon : null}
                      <span className="truncate">{option.label}</span>
                    </span>
                    {option.id === value ? <Check className="size-4 shrink-0" aria-hidden /> : null}
                  </CommandItem>
                ))}
                {orgUuid && workspaceId ? (
                  <CommandItem value="__create_provider__" keywords={[customLabel]} onSelect={() => setCreating(true)}>
                    <Plus className="size-4" aria-hidden />
                    {customLabel}
                  </CommandItem>
                ) : null}
              </CommandList>
            </Command>
          )}
        </PopoverContent>
      </Popover>
    </div>
  );
}

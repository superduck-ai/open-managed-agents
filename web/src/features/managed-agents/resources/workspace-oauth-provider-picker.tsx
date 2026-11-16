import { useId, useState } from 'react';
import { Plus } from 'lucide-react';
import type { OAuthProviderRecord } from '../../../shared/api/platform-admin';
import { useI18n } from '../../../shared/i18n';
import { CommandItem } from '../../../shared/ui/command';
import { Label } from '../../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
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
  const [pending, setPending] = useState(false);
  const selected = options.find((option) => option.id === value);
  const customLabel = msg('managedAgents.credentialVaults.credentialDialog.customProvider', 'Custom OAuth provider');
  return (
    <div>
      <Label htmlFor={id}>{label}</Label>
      <Select
        value={value}
        items={[
          { value: '', label: placeholder },
          ...options.map((option) => ({ value: option.id, label: option.label })),
        ]}
        onValueChange={(next) => {
          if (next !== null) onChange(next);
        }}
        open={open}
        onOpenChange={(next, details) => {
          if (!next && pending) {
            details?.cancel();
            return;
          }
          setOpen(next);
          if (!next) setCreating(false);
        }}
      >
        <SelectTrigger id={id} type="button" className="mt-2 h-10 w-full">
          <SelectValue>
            <span className="flex min-w-0 items-center gap-2">
              {selected?.icon}
              <span className="truncate">{selected?.label ?? placeholder}</span>
            </span>
          </SelectValue>
        </SelectTrigger>
        <SelectContent
          className="overflow-y-auto"
          customContent={
            creating && orgUuid && workspaceId ? (
              <WorkspaceOAuthProviderForm
                orgUuid={orgUuid}
                workspaceId={workspaceId}
                onPendingChange={setPending}
                onClose={() => setCreating(false)}
                onCreated={(provider) => {
                  onCreated(provider);
                  setOpen(false);
                  setCreating(false);
                }}
              />
            ) : undefined
          }
        >
          <SelectItem value="" label={placeholder}>
            {placeholder}
          </SelectItem>
          {options.map((option) => (
            <SelectItem key={option.id} value={option.id} label={option.label}>
              {option.icon}
              <span className="truncate">{option.label}</span>
            </SelectItem>
          ))}
          {orgUuid && workspaceId ? (
            <CommandItem value="__create_provider__" keywords={[customLabel]} onSelect={() => setCreating(true)}>
              <Plus className="size-4" aria-hidden />
              {customLabel}
            </CommandItem>
          ) : null}
        </SelectContent>
      </Select>
    </div>
  );
}

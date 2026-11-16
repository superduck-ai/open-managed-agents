import { useId, useState } from 'react';
import { Pencil, Plus } from 'lucide-react';
import type { OAuthProviderRecord } from '../../../shared/api/platform-admin';
import { useI18n } from '../../../shared/i18n';
import { CommandItem } from '../../../shared/ui/command';
import { Label } from '../../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import type { EntityOption } from '../types';
import { WorkspaceOAuthProviderEditor } from './workspace-oauth-provider-form';
import { Button } from '../../../shared/ui/button';

export function WorkspaceOAuthProviderPicker({
  label,
  value,
  placeholder,
  options,
  orgUuid,
  workspaceId,
  onChange,
  onSaved,
  editableProviderId,
  canCreate = true,
}: {
  label: string;
  value: string;
  placeholder: string;
  options: EntityOption[];
  orgUuid?: string;
  workspaceId?: string;
  onChange: (value: string) => void;
  onSaved: (provider: OAuthProviderRecord) => void;
  editableProviderId?: string;
  canCreate?: boolean;
}) {
  const { msg } = useI18n();
  const id = useId();
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<'create' | 'edit' | null>(null);
  const [pending, setPending] = useState(false);
  const selected = options.find((option) => option.id === value);
  const customLabel = msg('managedAgents.credentialVaults.credentialDialog.customProvider', 'Custom OAuth provider');
  return (
    <div>
      <div className="flex items-center justify-between gap-2">
        <Label htmlFor={id}>{label}</Label>
        {editableProviderId && orgUuid && workspaceId ? (
          <Button
            type="button"
            variant="link"
            size="sm"
            className="h-auto p-0"
            disabled={pending}
            onClick={() => {
              setMode('edit');
              setOpen(true);
            }}
          >
            <Pencil className="size-3" aria-hidden />
            {msg('common.edit', 'Edit')}
          </Button>
        ) : null}
      </div>
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
          if (!next) setMode(null);
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
            mode && orgUuid && workspaceId ? (
              <WorkspaceOAuthProviderEditor
                key={mode === 'edit' ? editableProviderId : 'create'}
                providerId={mode === 'edit' ? editableProviderId : undefined}
                orgUuid={orgUuid}
                workspaceId={workspaceId}
                onPendingChange={setPending}
                onClose={() => setMode(null)}
                onSaved={(provider) => {
                  onSaved(provider);
                  setOpen(false);
                  setMode(null);
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
          {canCreate && orgUuid && workspaceId ? (
            <CommandItem value="__create_provider__" keywords={[customLabel]} onSelect={() => setMode('create')}>
              <Plus className="size-4" aria-hidden />
              {customLabel}
            </CommandItem>
          ) : null}
        </SelectContent>
      </Select>
    </div>
  );
}

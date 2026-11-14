import { useId } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Label } from '../../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { ManagedVaultSelectField } from '../resources/vault-select-field';
import { useChannelResources } from './channelResources';
import { errorMessage } from '../utils';
import { channelVaultIds, type ChannelInput } from './api';

export function ChannelResourceFields<T extends ChannelInput>({
  workspaceId,
  value,
  onChange,
}: {
  workspaceId: string;
  value: T;
  onChange: (value: T) => void;
}) {
  const { msg } = useI18n();
  const id = useId();
  const resources = useChannelResources(workspaceId);
  const none = msg('managedAgents.channels.notMounted', '不挂载');
  const selected = value.memory_store_id ?? 'none';
  const memoryOptions = resources.data?.memoryStores ?? [];
  const items = [
    { value: 'none', label: none },
    ...memoryOptions.map((option) => ({ value: option.id, label: option.label })),
  ];
  if (selected !== 'none' && !memoryOptions.some((option) => option.id === selected))
    items.push({ value: selected, label: selected });
  const vaultIds = channelVaultIds(value);
  const vaultOptions = [...(resources.data?.vaults ?? [])];
  for (const vaultId of vaultIds) {
    if (!vaultOptions.some((option) => option.id === vaultId)) vaultOptions.push({ id: vaultId, label: vaultId });
  }
  return (
    <>
      <div className="space-y-2">
        <Label htmlFor={id}>{msg('managedAgents.channels.defaultMemory', '默认 Memory')}</Label>
        <Select
          value={selected}
          items={items}
          disabled={resources.isPending}
          onValueChange={(next) => onChange({ ...value, memory_store_id: next === 'none' ? null : next })}
        >
          <SelectTrigger id={id} className="h-10 w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {items.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <ManagedVaultSelectField
        label={msg('managedAgents.channels.defaultVault', '默认 Vault')}
        optional
        workspaceId={workspaceId}
        selectedIds={vaultIds}
        options={vaultOptions}
        manageHref={`/workspaces/${encodeURIComponent(workspaceId)}/vaults`}
        manageLabel={msg('managedAgents.credentialVaults.manage', '管理凭据库')}
        onChange={(vaultIds) => onChange({ ...value, vault_ids: vaultIds, vault_id: null })}
      />
      {resources.error ? (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(resources.error)}
        </p>
      ) : null}
    </>
  );
}

import { useId } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Label } from '../../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { useChannelResources } from './channelResources';
import { errorMessage } from '../utils';
import type { ChannelInput } from './api';

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
  const fields = [
    {
      key: 'memory_store_id' as const,
      label: msg('managedAgents.channels.defaultMemory', '默认 Memory'),
      options: resources.data?.memoryStores ?? [],
    },
    {
      key: 'vault_id' as const,
      label: msg('managedAgents.channels.defaultVault', '默认 Vault'),
      options: resources.data?.vaults ?? [],
    },
  ];
  return (
    <>
      {fields.map(({ key, label, options }) => {
        const selected = value[key] ?? 'none';
        const items = [
          { value: 'none', label: none },
          ...options.map((option) => ({ value: option.id, label: option.label })),
        ];
        if (selected !== 'none' && !options.some((option) => option.id === selected))
          items.push({ value: selected, label: selected });
        return (
          <div className="space-y-2" key={key}>
            <Label htmlFor={`${id}-${key}`}>{label}</Label>
            <Select
              value={selected}
              items={items}
              disabled={resources.isPending}
              onValueChange={(next) => onChange({ ...value, [key]: next === 'none' ? null : next })}
            >
              <SelectTrigger id={`${id}-${key}`} className="h-10 w-full">
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
        );
      })}
      {resources.error ? (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(resources.error)}
        </p>
      ) : null}
    </>
  );
}

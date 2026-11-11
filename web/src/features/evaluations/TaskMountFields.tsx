import { useQuery } from '@tanstack/react-query';

import { memoryAttachResources } from '../managed-agents/resources/memory-attach';
import { sessionFileResourcePayload } from '../managed-agents/sessions/file-resource-form';
import { SessionFileResourcesField } from '../managed-agents/sessions/SessionFileResourcesField';
import type { MemoryAttachFormValue, SessionFileResourceFormValue } from '../managed-agents/types';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Label } from '../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { listTaskEntities } from './taskOptions';

export function TaskMountFields({
  workspaceId,
  enabled,
  vaultIds,
  files,
  memories,
  onVaultIds,
  onFiles,
  onMemories,
}: {
  workspaceId: string;
  enabled: boolean;
  vaultIds: string[];
  files: SessionFileResourceFormValue[];
  memories: MemoryAttachFormValue[];
  onVaultIds: (ids: string[]) => void;
  onFiles: (files: SessionFileResourceFormValue[]) => void;
  onMemories: (memories: MemoryAttachFormValue[]) => void;
}) {
  const { msg } = useI18n();
  const vaults = useQuery({
    queryKey: ['evaluation-vaults', workspaceId],
    queryFn: () => listTaskEntities('credential-vaults', workspaceId),
    enabled,
  });
  const stores = useQuery({
    queryKey: ['evaluation-memory-stores', workspaceId],
    queryFn: () => listTaskEntities('memory-stores', workspaceId),
    enabled,
  });
  const vaultOptions = (vaults.data ?? []).map((item) => ({ id: item.id, label: entityLabel(item) }));
  return (
    <div className="grid gap-5">
      <div className="grid gap-2">
        <Label>{msg('evaluations.vaultsOptional', 'Credential vaults, optional')}</Label>
        <Select
          value={null}
          items={vaultOptions.map((item) => ({ value: item.id, label: item.label }))}
          onValueChange={(id) => {
            if (id && !vaultIds.includes(id)) onVaultIds([...vaultIds, id]);
          }}
          emptyLabel={msg('evaluations.emptyVaults', 'No credential vaults yet')}
        >
          <SelectTrigger
            aria-label={msg('evaluations.vaultsOptional', 'Credential vaults, optional')}
            className="w-full"
          >
            <SelectValue placeholder={msg('evaluations.chooseVaults', 'Choose one or more credential vaults')} />
          </SelectTrigger>
          <SelectContent>
            {vaultOptions.map((item) => (
              <SelectItem key={item.id} value={item.id}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {vaultIds.length > 0 ? (
          <ul className="flex flex-wrap gap-2">
            {vaultIds.map((id) => (
              <li key={id}>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => onVaultIds(vaultIds.filter((item) => item !== id))}
                >
                  {vaultOptions.find((item) => item.id === id)?.label ?? id}
                </Button>
              </li>
            ))}
          </ul>
        ) : null}
      </div>
      <section className="grid gap-3">
        <div className="grid gap-1">
          <h3 className="text-sm font-medium">{msg('evaluations.resources', 'Resources')}</h3>
          <p className="text-sm text-muted-foreground">
            {msg('evaluations.resourcesHint', 'Mount files or memory stores on the evaluation session.')}
          </p>
        </div>
        <SessionFileResourcesField
          resources={files}
          showHeading={false}
          variant="plain"
          workspaceId={workspaceId}
          onChange={onFiles}
          memoryAttaches={memories}
          memoryStoreOptions={(stores.data ?? []).map((item) => ({ id: item.id, label: entityLabel(item) }))}
          onMemoryAttachesChange={onMemories}
        />
      </section>
    </div>
  );
}

export function taskMountResources(files: SessionFileResourceFormValue[], memories: MemoryAttachFormValue[]) {
  return [
    ...files.filter((file) => file.fileId.trim()).map(sessionFileResourcePayload),
    ...memoryAttachResources(memories),
  ];
}

function entityLabel(item: { id: string; name?: unknown }) {
  return typeof item.name === 'string' && item.name ? item.name : item.id;
}

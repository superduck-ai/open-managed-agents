import { Database, Trash2 } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Button, ButtonLink } from '../../../shared/ui/button';
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '../../../shared/ui/card';
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from '../../../shared/ui/combobox';
import { Field, FieldDescription, FieldError, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import type { EntityOption, MemoryAttachFormValue } from '../types';
import { MAX_MEMORY_ATTACH_INSTRUCTIONS, memoryInstructionsCodePointCount } from './memory-attach';

export function MemoryStoreResourceCards({
  attaches,
  options,
  workspaceId,
  onChange,
}: {
  attaches: MemoryAttachFormValue[];
  options: EntityOption[];
  workspaceId: string;
  onChange: (attaches: MemoryAttachFormValue[]) => void;
}) {
  const selectedIds = new Set(attaches.map((attach) => attach.memoryStoreId).filter(Boolean));
  return (
    <>
      {attaches.map((attach, index) => (
        <MemoryStoreResourceCard
          key={`${attach.memoryStoreId}-${index}`}
          attach={attach}
          index={index}
          options={options.filter((option) => option.id === attach.memoryStoreId || !selectedIds.has(option.id))}
          workspaceId={workspaceId}
          onChange={(patch) =>
            onChange(attaches.map((item, itemIndex) => (itemIndex === index ? { ...item, ...patch } : item)))
          }
          onRemove={() => onChange(attaches.filter((_, itemIndex) => itemIndex !== index))}
        />
      ))}
    </>
  );
}

function MemoryStoreResourceCard({
  attach,
  index,
  options,
  workspaceId,
  onChange,
  onRemove,
}: {
  attach: MemoryAttachFormValue;
  index: number;
  options: EntityOption[];
  workspaceId: string;
  onChange: (patch: Partial<MemoryAttachFormValue>) => void;
  onRemove: () => void;
}) {
  const { msg } = useI18n();
  const instructionCount = memoryInstructionsCodePointCount(attach.instructions);
  const instructionsOverLimit = instructionCount > MAX_MEMORY_ATTACH_INSTRUCTIONS;
  const readWrite = msg('managedAgents.memoryStores.attach.accessReadWrite', 'Read & write');
  const readOnly = msg('managedAgents.memoryStores.attach.accessReadOnly', 'Read only');
  const storeId = `session-memory-store-${index}`;
  const accessId = `session-memory-access-${index}`;
  const instructionsId = `session-memory-instructions-${index}`;

  return (
    <Card size="sm" className="mx-px gap-3 py-3">
      <CardHeader className="grid-cols-[1fr_auto] items-center px-3">
        <CardTitle className="flex items-center gap-2 text-sm">
          <Database className="size-4 text-muted-foreground" aria-hidden />
          {msg('managedAgents.memoryStores.kindTitle', 'Memory store')}
        </CardTitle>
        <CardAction>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={msg('managedAgents.sessions.resources.removeMemory', 'Remove memory store {index}', {
              index: index + 1,
            })}
            onClick={onRemove}
          >
            <Trash2 aria-hidden />
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-3 px-3">
        <Field>
          <div className="flex items-center justify-between gap-3">
            <FieldLabel htmlFor={storeId}>{msg('managedAgents.memoryStores.kindTitle', 'Memory store')}</FieldLabel>
            <ButtonLink
              href={`/workspaces/${encodeURIComponent(workspaceId)}/memory-stores`}
              target="_blank"
              rel="noreferrer"
              variant="link"
              size="xs"
            >
              {msg('managedAgents.memoryStores.manage', 'Manage memory stores')}
            </ButtonLink>
          </div>
          <MemoryStoreCombobox id={storeId} options={options} value={attach.memoryStoreId} onChange={onChange} />
        </Field>
        <Field>
          <FieldLabel htmlFor={accessId}>{msg('managedAgents.memoryStores.attach.access', 'Access')}</FieldLabel>
          <Select
            value={attach.access}
            onValueChange={(access) => {
              if (access === 'read_write' || access === 'read_only') {
                onChange({ access });
              }
            }}
          >
            <SelectTrigger
              id={accessId}
              aria-label={msg('managedAgents.memoryStores.attach.access', 'Access')}
              className="h-10 w-full px-3 text-sm text-foreground"
            >
              <SelectValue>{attach.access === 'read_only' ? readOnly : readWrite}</SelectValue>
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectItem value="read_write" label={readWrite}>
                {readWrite}
              </SelectItem>
              <SelectItem value="read_only" label={readOnly}>
                {readOnly}
              </SelectItem>
            </SelectContent>
          </Select>
        </Field>
        {attach.mountPath ? (
          <Field className="gap-1">
            <FieldLabel>{msg('managedAgents.memoryStores.attach.mountPath', 'Mount path')}</FieldLabel>
            <p className="font-mono text-sm text-foreground">{attach.mountPath}</p>
            <FieldDescription>
              {msg(
                'managedAgents.memoryStores.attach.mountPathHelp',
                'Assigned by the server. This value is not sent when saving.',
              )}
            </FieldDescription>
          </Field>
        ) : null}
        <Field className="gap-2" data-invalid={instructionsOverLimit || undefined}>
          <FieldLabel htmlFor={instructionsId}>
            {msg('managedAgents.memoryStores.attach.instructions', 'Instructions')}{' '}
            <span className="font-normal text-muted-foreground">
              {msg('managedAgents.common.optionalParen', '(optional)')}
            </span>
          </FieldLabel>
          <Input
            id={instructionsId}
            value={attach.instructions}
            aria-invalid={instructionsOverLimit || undefined}
            placeholder={msg(
              'managedAgents.memoryStores.attach.instructionsPlaceholder',
              'Tell the agent what this store contains and when to use it.',
            )}
            onChange={(event) => onChange({ instructions: event.currentTarget.value })}
          />
          {instructionsOverLimit ? (
            <FieldError>
              {msg(
                'managedAgents.memoryStores.attach.instructionsTooLong',
                'Instructions must be at most {max} characters.',
                { max: MAX_MEMORY_ATTACH_INSTRUCTIONS },
              )}
            </FieldError>
          ) : null}
        </Field>
      </CardContent>
    </Card>
  );
}

function MemoryStoreCombobox({
  id,
  options,
  value,
  onChange,
}: {
  id: string;
  options: EntityOption[];
  value: string;
  onChange: (patch: Partial<MemoryAttachFormValue>) => void;
}) {
  const { msg } = useI18n();
  const selected = options.find((option) => option.id === value) ?? null;
  const emptyText = msg('managedAgents.memoryStores.attach.noStores', 'No memory stores found');
  return (
    <Combobox
      items={options}
      value={selected}
      autoHighlight
      itemToStringLabel={(option) => option.label}
      itemToStringValue={(option) => option.id}
      isItemEqualToValue={(option, current) => option.id === current.id}
      filter={(option, query) =>
        `${option.label}\n${option.id}`.toLocaleLowerCase().includes(query.toLocaleLowerCase())
      }
      onValueChange={(option) => onChange({ memoryStoreId: option?.id ?? '' })}
    >
      <ComboboxInput
        id={id}
        required
        placeholder={msg('managedAgents.memoryStores.attach.selectStore', 'Select a memory store')}
        aria-label={msg('managedAgents.memoryStores.kindTitle', 'Memory store')}
      />
      <ComboboxContent>
        <ComboboxEmpty>{emptyText}</ComboboxEmpty>
        <ComboboxList>
          {(option: EntityOption) => (
            <ComboboxItem key={option.id} value={option}>
              <span className="min-w-0 truncate">{option.label}</span>
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}

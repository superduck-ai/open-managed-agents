import { ArrowUpRight, Plus, X } from 'lucide-react';
import { useState, type ReactNode } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { FieldError } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '../../../shared/ui/popover';
import { EnvironmentSection, type EnvironmentFieldsProps } from './fields';
import { addPackage, packageManagers } from './model';
import { PackageIcon } from './package-icon';

export function EnvironmentPackages({
  values,
  onChange,
  errors,
  readOnly,
  status,
}: EnvironmentFieldsProps & { status?: ReactNode }) {
  const { msg } = useI18n();
  const [expanded, setExpanded] = useState<string[]>([]);
  const active = packageManagers.filter(
    ({ id }) => expanded.includes(id) || values.packages.some((row) => row.manager === id),
  );
  return (
    <EnvironmentSection
      title={msg('managedAgents.environments.packages.title', 'Packages')}
      status={status}
      description={msg('environmentPage.packagesDescription', 'Installed into the container before sessions start.')}
      anchor="#packages"
      readOnly={readOnly}
    >
      <div className="space-y-4">
        {!active.length ? (
          <p className="text-sm text-muted-foreground">
            {msg('environmentPage.noPackages', 'No packages beyond the')}{' '}
            <a
              href="https://platform.claude.com/docs/en/managed-agents/cloud-sandboxes-reference"
              target="_blank"
              rel="noreferrer"
              className="text-foreground/75 underline underline-offset-4"
            >
              {msg('environmentPage.standardImage', 'standard image')}{' '}
              <ArrowUpRight className="inline size-3.5" aria-hidden />
            </a>
            .
          </p>
        ) : null}
        {active.map((manager) => (
          <div
            key={manager.id}
            className="space-y-2"
            role="group"
            aria-label={msg('environmentPage.packageGroup', '{name} packages', { name: manager.name })}
          >
            <div className="flex min-h-7 items-center gap-2 text-sm">
              <PackageIcon manager={manager.id} />
              <span>{msg('environmentPage.packageGroup', '{name} packages', { name: manager.name })}</span>
              <span className="text-xs text-muted-foreground">{manager.registry}</span>
              <span className="text-xs tabular-nums text-muted-foreground">
                {values.packages.filter((row) => row.manager === manager.id).length || ''}
              </span>
              {!readOnly ? (
                <PackageInput
                  autoFocus={expanded.includes(manager.id)}
                  manager={manager}
                  onAdd={(value) => onChange(addPackage(values, manager.id, value))}
                />
              ) : null}
            </div>
            <div className="flex flex-wrap gap-1.5">
              {values.packages.map((row, index) =>
                row.manager === manager.id ? (
                  <div key={`${index}-${row.value}`}>
                    <span className="inline-flex max-w-full items-center gap-1 rounded-md border border-border bg-muted/40 py-1 pl-2 pr-1 font-mono text-xs leading-5">
                      <span className="min-w-0 break-all">{row.value}</span>
                      {!readOnly ? (
                        <Button
                          type="button"
                          size="icon-xs"
                          variant="ghost"
                          className="size-5 shrink-0"
                          aria-label={msg('environmentPage.removePackage', 'Remove {name}', { name: row.value })}
                          onClick={() =>
                            onChange({
                              ...values,
                              packages: values.packages.filter((_, position) => position !== index),
                            })
                          }
                        >
                          <X className="size-3" />
                        </Button>
                      ) : null}
                    </span>
                    {errors.packages[index] ? <FieldError>{errors.packages[index]}</FieldError> : null}
                  </div>
                ) : null,
              )}
            </div>
          </div>
        ))}
        {!readOnly ? (
          <div className="flex flex-wrap gap-1.5">
            {packageManagers
              .filter(({ id }) => !active.some((manager) => manager.id === id))
              .map((manager) => (
                <Button
                  key={manager.id}
                  type="button"
                  variant="outline"
                  size="xs"
                  className="h-8 gap-1.5 px-2 font-normal"
                  aria-label={msg('environmentPage.addManager', 'Add {name} {manager}', {
                    name: manager.name,
                    manager: manager.id,
                  })}
                  onClick={() => setExpanded([...expanded, manager.id])}
                >
                  <Plus className="size-3" strokeWidth={1.5} />
                  <PackageIcon manager={manager.id} />
                  {manager.name} <span className="text-muted-foreground">{manager.id}</span>
                </Button>
              ))}
          </div>
        ) : null}
      </div>
    </EnvironmentSection>
  );
}

function PackageInput({
  manager,
  onAdd,
  autoFocus,
}: {
  manager: (typeof packageManagers)[number];
  autoFocus?: boolean;
  onAdd: (text: string) => void;
}) {
  const { msg } = useI18n();
  const [input, setInput] = useState('');
  const [open, setOpen] = useState(false);
  const value = input.trim();
  const multiple = /\s/.test(value);
  const canAdd = Boolean(value) && !multiple;
  const add = () => {
    if (canAdd) {
      onAdd(value);
      setInput('');
      setOpen(false);
    }
  };
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        autoFocus={autoFocus}
        render={<Button type="button" variant="ghost" size="xs" className="ml-auto font-normal" />}
        aria-label={msg('environmentPage.addPackages', 'Add {name} packages', { name: manager.name })}
      >
        <Plus className="size-3.5" />
        {msg('common.add', 'Add')}
      </PopoverTrigger>
      <PopoverContent className="environment-package-picker w-96 max-w-[calc(100vw-32px)] rounded-xl p-3" align="end">
        <Input
          value={input}
          onChange={(event) => setInput(event.target.value)}
          autoComplete="off"
          autoCapitalize="none"
          spellCheck={false}
          aria-invalid={multiple || undefined}
          aria-label={msg('environmentPage.packagesToAdd', '{name} packages to add', { name: manager.name })}
          placeholder={manager.placeholder}
          className="rounded-lg bg-background"
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.nativeEvent.isComposing) {
              event.preventDefault();
              add();
            }
          }}
        />
        {multiple ? (
          <FieldError>{msg('environmentPage.singlePackage', 'Add one package at a time.')}</FieldError>
        ) : null}
        <div className="flex justify-end">
          <Button type="button" size="sm" disabled={!canAdd} onClick={add}>
            {msg('common.add', 'Add')}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}

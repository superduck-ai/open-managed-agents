import { useState } from 'react';
import { ChevronRight } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Textarea } from '../../../shared/ui/textarea';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import { Field, FieldError, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import type { EnvironmentResourceValues } from '../types';
import { EnvironmentSection, type EnvironmentFieldsProps } from './fields';
import { EnvironmentKeyValueRows } from './key-value-rows';

const resourceFields: Array<{ id: keyof EnvironmentResourceValues; label: string; unit: string }> = [
  { id: 'cpu', label: 'CPU', unit: 'millicores' },
  { id: 'memory', label: 'Memory', unit: 'MiB' },
  { id: 'disk', label: 'Disk', unit: 'GiB' },
];

export function EnvironmentInitScript({ values, onChange, errors, readOnly }: EnvironmentFieldsProps) {
  const { msg } = useI18n();
  return (
    <EnvironmentSection
      title={msg('environmentTemplate.initScript.title', 'Init script')}
      description={msg(
        'environmentTemplate.initScript.description',
        'Runs after packages are installed when the environment is built.',
      )}
    >
      <Field>
        <Textarea
          aria-label={msg('environmentTemplate.initScript.title', 'Init script')}
          className="min-h-32 font-mono text-xs [font-variant-ligatures:none] read-only:bg-muted/50"
          readOnly={readOnly}
          placeholder="curl -fsSL https://example.com/install.sh | bash"
          value={values.initScript}
          aria-invalid={Boolean(errors.initScript)}
          onChange={(event) => onChange({ ...values, initScript: event.target.value })}
        />
        {errors.initScript ? <FieldError>{errors.initScript}</FieldError> : null}
      </Field>
    </EnvironmentSection>
  );
}

export function EnvironmentVariables({ values, onChange, errors, readOnly }: EnvironmentFieldsProps) {
  const { msg } = useI18n();
  return (
    <EnvironmentSection
      title={msg('environmentTemplate.variables.title', 'Environment variables')}
      description={msg('environmentTemplate.variables.description', 'Set in every new sandbox.')}
    >
      <div className="space-y-3">
        <EnvironmentKeyValueRows
          rows={values.environmentRows}
          onRowsChange={(environmentRows) => onChange({ ...values, environmentRows })}
          errors={errors.environmentRows ?? {}}
          readOnly={readOnly}
          limit={64}
          labels={{
            key: (number) => msg('environmentTemplate.variables.key', 'Variable name {number}', { number }),
            value: (number) => msg('environmentTemplate.variables.value', 'Variable value {number}', { number }),
            remove: (key) => msg('environmentPage.removeMetadata', 'Remove {key}', { key }),
            add: msg('environmentTemplate.variables.add', 'Add variable'),
            empty: msg('environmentTemplate.variables.empty', 'No environment variables.'),
          }}
        />
      </div>
    </EnvironmentSection>
  );
}

function resourceSummary(resources: EnvironmentResourceValues, msg: ReturnType<typeof useI18n>['msg']) {
  const parts = resourceFields
    .filter((resource) => resources[resource.id].trim())
    .map(
      (resource) =>
        `${msg(`environmentTemplate.resources.${resource.id}`, resource.label)} ${resources[resource.id].trim()} ${msg(
          `environmentTemplate.resources.${resource.id}Unit`,
          resource.unit,
        )}`,
    );
  return parts.length
    ? parts.map((part) => <span key={part}>{part}</span>)
    : msg('environmentTemplate.resources.default', 'Default size');
}

export function EnvironmentResources({ values, onChange, errors, readOnly }: EnvironmentFieldsProps) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const hasErrors = Object.keys(errors.resources ?? {}).length > 0;
  const expanded = open || hasErrors;
  return (
    <EnvironmentSection
      title={msg('environmentTemplate.resources.title', 'Resources')}
      description={msg('environmentTemplate.resources.description', 'Optional. Leave empty to use the default size.')}
    >
      <Collapsible open={expanded} onOpenChange={setOpen}>
        <CollapsibleTrigger className="flex w-full items-center gap-2 rounded-md py-1 text-left text-sm text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-[0.5px] focus-visible:ring-ring">
          <ChevronRight className={cn('size-4 shrink-0 transition-transform', expanded && 'rotate-90')} aria-hidden />
          <span className="flex min-w-0 flex-wrap gap-x-4 gap-y-1">{resourceSummary(values.resources, msg)}</span>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className="grid gap-4 pt-4 sm:grid-cols-3">
            {resourceFields.map((resource) => (
              <Field key={resource.id}>
                <FieldLabel htmlFor={`environment-resource-${resource.id}`}>
                  {msg(`environmentTemplate.resources.${resource.id}`, resource.label)}
                  <span className="font-normal text-muted-foreground">
                    {msg(`environmentTemplate.resources.${resource.id}Unit`, resource.unit)}
                  </span>
                </FieldLabel>
                <Input
                  id={`environment-resource-${resource.id}`}
                  inputMode="numeric"
                  readOnly={readOnly}
                  className="read-only:bg-muted/50"
                  placeholder={msg('environmentTemplate.resources.placeholder', 'Default')}
                  value={values.resources[resource.id]}
                  aria-invalid={Boolean(errors.resources?.[resource.id])}
                  onChange={(event) =>
                    onChange({ ...values, resources: { ...values.resources, [resource.id]: event.target.value } })
                  }
                />
                {errors.resources?.[resource.id] ? <FieldError>{errors.resources[resource.id]}</FieldError> : null}
              </Field>
            ))}
          </div>
        </CollapsibleContent>
      </Collapsible>
    </EnvironmentSection>
  );
}

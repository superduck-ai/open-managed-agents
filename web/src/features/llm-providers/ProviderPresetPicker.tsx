import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Field, FieldError, FieldLabel } from '../../shared/ui/field';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import type { LLMProviderPreset, ProviderPresetCatalog } from './api';

export function ProviderPresetPicker({
  catalog,
  value,
  error,
  onReload,
  onChange,
}: {
  catalog?: ProviderPresetCatalog;
  value: string;
  error?: string;
  onReload?: () => void;
  onChange: (preset: LLMProviderPreset) => void;
}) {
  const { msg } = useI18n();
  return (
    <Field>
      <FieldLabel htmlFor="llm-supplier">{msg('llmModels.supplier', 'Model supplier')}</FieldLabel>
      <Select
        value={value || null}
        items={catalog?.providers.map((preset) => ({ value: preset.id, label: preset.name })) ?? []}
        onValueChange={(id) => {
          const preset = catalog?.providers.find((item) => item.id === id);
          if (preset) onChange(preset);
        }}
      >
        <SelectTrigger
          aria-invalid={Boolean(error)}
          aria-describedby={error ? 'llm-supplier-error' : undefined}
          id="llm-supplier"
          className="w-full"
        >
          <SelectValue placeholder={msg('llmModels.selectSupplier', 'Select a supplier')} />
        </SelectTrigger>
        <SelectContent>
          {catalog?.providers.map((preset) => (
            <SelectItem key={preset.id} value={preset.id}>
              {preset.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {error ? <FieldError id="llm-supplier-error">{error}</FieldError> : null}
      {error && onReload ? (
        <Button type="button" variant="ghost" size="sm" className="w-fit" onClick={onReload}>
          {msg('common.refresh', 'Refresh')}
        </Button>
      ) : null}
      {catalog?.providers.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {msg('llmModels.noSuppliers', 'No suppliers configured. Use Custom model API.')}
        </p>
      ) : null}
    </Field>
  );
}

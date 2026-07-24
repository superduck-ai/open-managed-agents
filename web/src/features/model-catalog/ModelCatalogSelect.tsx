import { Label } from '@/shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import { cn } from '@/shared/lib/utils';
import { AlertCircle, Loader2 } from 'lucide-react';
import { type ModelCatalogModel, modelCatalogDisplayName } from './model';

export function ModelCatalogSelect({
  models,
  value,
  onValueChange,
  loading = false,
  error = false,
  stale = false,
  disabled = false,
  label = 'Model',
  className,
}: {
  models: ModelCatalogModel[];
  value: string;
  onValueChange: (modelID: string) => void;
  loading?: boolean;
  error?: boolean;
  stale?: boolean;
  disabled?: boolean;
  label?: string;
  className?: string;
}) {
  const selectedModel = models.find((model) => model.model_name === value);
  const placeholder = loading ? 'Loading models...' : error ? 'Models unavailable' : 'Select model';

  return (
    <div className={cn('grid min-w-0 gap-1.5', className)}>
      <div className="flex items-center justify-between gap-3">
        <Label className="text-xs text-muted-foreground">{label}</Label>
        {stale ? (
          <span className="inline-flex items-center gap-1 text-xs text-amber-700 dark:text-amber-400">
            <AlertCircle className="size-3" aria-hidden />
            Stale catalog
          </span>
        ) : null}
      </div>
      <Select<string>
        value={value}
        items={models.map((model) => ({ value: model.model_name, label: modelCatalogDisplayName(model) }))}
        disabled={disabled || loading || error || models.length === 0}
        onValueChange={(nextValue) => nextValue && onValueChange(nextValue)}
      >
        <SelectTrigger aria-label={label} className="w-full min-w-0">
          {loading ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
          <SelectValue className={selectedModel ? 'text-foreground' : 'text-muted-foreground'}>
            {selectedModel ? modelCatalogDisplayName(selectedModel) : placeholder}
          </SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          {models.map((model) => (
            <SelectItem key={model.model_name} value={model.model_name} label={modelCatalogDisplayName(model)}>
              <span className="grid min-w-0">
                <span className="truncate">{modelCatalogDisplayName(model)}</span>
                {model.display_name && model.display_name !== model.model_name ? (
                  <span className="truncate text-xs text-muted-foreground">{model.model_name}</span>
                ) : null}
              </span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

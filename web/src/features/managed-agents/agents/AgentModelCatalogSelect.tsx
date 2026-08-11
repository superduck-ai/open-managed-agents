import { useI18n } from '../../../shared/i18n';
import { ModelCatalogSelect } from '../../model-catalog/ModelCatalogSelect';
import { type ModelCatalogModel } from '../../model-catalog/model';

export function AgentModelCatalogSelect({
  models,
  value,
  onValueChange,
  loading,
  error,
  stale,
  disabled,
  className,
}: {
  models: ModelCatalogModel[];
  value: string;
  onValueChange: (modelID: string) => void;
  loading: boolean;
  error: boolean;
  stale: boolean;
  disabled: boolean;
  className: string;
}) {
  const { msg } = useI18n();
  return (
    <ModelCatalogSelect
      models={models}
      value={value}
      onValueChange={onValueChange}
      loading={loading}
      error={error}
      stale={stale}
      disabled={disabled}
      label={msg('analytics.table.model', 'Model')}
      className={className}
    />
  );
}

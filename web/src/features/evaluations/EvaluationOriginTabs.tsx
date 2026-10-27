import { useI18n } from '../../shared/i18n';
import { Tabs, TabsList, TabsTrigger } from '../../shared/ui/tabs';

export type EvaluationOrigin = 'custom' | 'presets';

export function EvaluationOriginTabs({
  section,
  onSection,
  kind = 'dataset',
}: {
  section: EvaluationOrigin;
  onSection: (section: EvaluationOrigin) => void;
  kind?: 'dataset' | 'evaluator';
}) {
  const { msg } = useI18n();
  const custom =
    kind === 'evaluator' ? msg('evaluations.customEvaluators', 'Custom') : msg('evaluations.custom', 'Custom');
  const presets =
    kind === 'evaluator' ? msg('evaluations.presetEvaluators', 'Presets') : msg('evaluations.presets', 'Presets');
  return (
    <Tabs
      value={section}
      onValueChange={(value) => {
        if (value === 'custom' || value === 'presets') {
          onSection(value);
        }
      }}
      className="mb-3"
    >
      <TabsList className="h-auto gap-1 bg-transparent p-0">
        <TabsTrigger
          value="custom"
          className="h-8 w-auto flex-none rounded-md px-3 text-muted-foreground data-active:bg-[var(--success-bg)] data-active:text-[var(--success)] data-active:shadow-none"
        >
          {custom}
        </TabsTrigger>
        <TabsTrigger
          value="presets"
          className="h-8 w-auto flex-none rounded-md px-3 text-muted-foreground data-active:bg-[var(--success-bg)] data-active:text-[var(--success)] data-active:shadow-none"
        >
          {presets}
        </TabsTrigger>
      </TabsList>
    </Tabs>
  );
}

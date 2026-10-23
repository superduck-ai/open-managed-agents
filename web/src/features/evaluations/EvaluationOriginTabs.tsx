import { useI18n } from '../../shared/i18n';
import { Tabs, TabsList, TabsTrigger } from '../../shared/ui/tabs';

export type EvaluationOrigin = 'custom' | 'presets';

export function EvaluationOriginTabs({
  section,
  onSection,
}: {
  section: EvaluationOrigin;
  onSection: (section: EvaluationOrigin) => void;
}) {
  const { msg } = useI18n();
  return (
    <Tabs
      value={section}
      onValueChange={(value) => {
        if (value === 'custom' || value === 'presets') {
          onSection(value);
        }
      }}
      className="mb-4"
    >
      <TabsList>
        <TabsTrigger value="custom">{msg('evaluations.custom', 'Custom')}</TabsTrigger>
        <TabsTrigger value="presets">{msg('evaluations.presets', 'Presets')}</TabsTrigger>
      </TabsList>
    </Tabs>
  );
}

import { useEffect, useRef, useState } from 'react';
import { Plus, Trash2 } from 'lucide-react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Input } from '../../shared/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { Textarea } from '../../shared/ui/textarea';
import type { EvaluationCasePolarity, EvaluationCaseScenario } from './api';
import { MAX_PLAN_SCENARIOS, blankScenario, planTotal } from './caseGeneration';

export function CasePlanEditor({
  scenarios,
  onChange,
  disabled,
}: {
  scenarios: EvaluationCaseScenario[];
  onChange: (scenarios: EvaluationCaseScenario[]) => void;
  disabled: boolean;
}) {
  const { msg } = useI18n();

  function update(index: number, patch: Partial<EvaluationCaseScenario>) {
    onChange(scenarios.map((scenario, position) => (position === index ? { ...scenario, ...patch } : scenario)));
  }

  return (
    <div className="grid gap-4">
      <div className="grid gap-1">
        <p className="text-sm font-medium">{msg('evaluations.planTitle', 'Coverage plan')}</p>
        <p className="text-sm text-muted-foreground">
          {msg('evaluations.planSummary', '{scenarios} scenarios, {count} cases in total.', {
            scenarios: scenarios.length,
            count: planTotal(scenarios),
          })}
        </p>
      </div>
      {scenarios.map((scenario, index) => (
        <ScenarioCard
          key={index}
          scenario={scenario}
          disabled={disabled}
          onChange={(patch) => update(index, patch)}
          onRemove={() => onChange(scenarios.filter((_, position) => position !== index))}
        />
      ))}
      <Button
        type="button"
        variant="outline"
        className="w-fit"
        disabled={disabled || scenarios.length >= MAX_PLAN_SCENARIOS}
        onClick={() => onChange([...scenarios, blankScenario()])}
      >
        <Plus />
        {msg('evaluations.addScenario', 'Add scenario')}
      </Button>
    </div>
  );
}

function ScenarioCard({
  scenario,
  disabled,
  onChange,
  onRemove,
}: {
  scenario: EvaluationCaseScenario;
  disabled: boolean;
  onChange: (patch: Partial<EvaluationCaseScenario>) => void;
  onRemove: () => void;
}) {
  const { msg } = useI18n();
  const [countDraft, setCountDraft] = useState<string | null>(null);
  const committedCount = useRef(scenario.count);
  useEffect(() => {
    if (scenario.count !== committedCount.current) {
      committedCount.current = scenario.count;
      setCountDraft(null);
    }
  }, [scenario.count]);
  const polarityItems = [
    { value: 'positive', label: msg('evaluations.positive', 'Positive') },
    { value: 'negative', label: msg('evaluations.negative', 'Negative') },
  ];
  return (
    <div className="grid gap-3 rounded-xl border border-border p-3">
      <div className="flex items-center gap-2">
        <Input
          aria-label={msg('evaluations.scenarioName', 'Scenario name')}
          value={scenario.name}
          disabled={disabled}
          placeholder={msg('evaluations.scenarioName', 'Scenario name')}
          onChange={(event) => onChange({ name: event.target.value })}
        />
        <Select
          value={scenario.polarity}
          items={polarityItems}
          disabled={disabled}
          onValueChange={(value) => onChange({ polarity: scenarioPolarity(value) })}
        >
          <SelectTrigger className="w-28 shrink-0" aria-label={msg('evaluations.polarity', 'Label')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {polarityItems.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          aria-label={msg('evaluations.scenarioCount', 'Cases')}
          className="w-20 shrink-0"
          type="number"
          min={1}
          value={countDraft ?? String(scenario.count)}
          disabled={disabled}
          onChange={(event) => {
            const raw = event.target.value;
            setCountDraft(raw);
            const next = raw.trim() === '' ? 0 : Math.max(0, Math.floor(Number(raw) || 0));
            committedCount.current = next;
            onChange({ count: next });
          }}
          onBlur={() => {
            if (countDraft !== null && countDraft.trim() !== '') {
              setCountDraft(null);
            }
          }}
        />
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={msg('evaluations.removeScenario', 'Remove scenario')}
          disabled={disabled}
          onClick={onRemove}
        >
          <Trash2 />
        </Button>
      </div>
      <Textarea
        aria-label={msg('evaluations.scenarioDescription', 'What this scenario tests')}
        value={scenario.description}
        rows={2}
        disabled={disabled}
        placeholder={msg('evaluations.scenarioDescription', 'What this scenario tests')}
        onChange={(event) => onChange({ description: event.target.value })}
      />
      <Input
        aria-label={msg('evaluations.scenarioPersona', 'Who is asking, optional')}
        value={scenario.persona}
        disabled={disabled}
        placeholder={msg('evaluations.scenarioPersona', 'Who is asking, optional')}
        onChange={(event) => onChange({ persona: event.target.value })}
      />
    </div>
  );
}

function scenarioPolarity(value: unknown): EvaluationCasePolarity {
  return value === 'negative' ? 'negative' : 'positive';
}

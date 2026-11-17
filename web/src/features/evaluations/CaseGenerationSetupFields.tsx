import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { Textarea } from '../../shared/ui/textarea';
import { MAX_GENERATED_CASES } from './caseGeneration';
import type { CaseGeneration } from './useCaseGeneration';

function clampCount(count: number) {
  if (!Number.isInteger(count) || count < 1) return 1;
  return Math.min(count, MAX_GENERATED_CASES);
}

function stepCount(count: number, delta: number) {
  return clampCount((Number.isInteger(count) ? count : 0) + delta);
}

export function CaseGenerationSetupFields({
  generation,
  requireAgent,
  disabled = false,
}: {
  generation: CaseGeneration;
  requireAgent: boolean;
  disabled?: boolean;
}) {
  const { msg } = useI18n();
  const { setup, updateSetup } = generation;
  const agentLabel = requireAgent
    ? msg('evaluations.agentRequired', 'Agent')
    : msg('evaluations.agentOptionalSelect', 'Agent, optional');
  const count = Number(setup.count);
  return (
    <>
      <Label>{agentLabel}</Label>
      <Select
        value={setup.agentId || null}
        items={generation.agents.map((agent) => ({ value: agent.id, label: agent.name }))}
        onValueChange={(value) => updateSetup({ agentId: value ?? '', version: '' })}
        disabled={disabled}
      >
        <SelectTrigger className="w-full" aria-label={agentLabel}>
          <SelectValue placeholder={msg('evaluations.chooseAgent', 'Choose an agent')} />
        </SelectTrigger>
        <SelectContent>
          {generation.agents.map((agent) => (
            <SelectItem key={agent.id} value={agent.id}>
              {agent.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Label>{msg('evaluations.versionRequired', 'Version')}</Label>
      <Select
        value={setup.version || null}
        items={generation.versions.map((item) => ({ value: String(item.version), label: `v${item.version}` }))}
        onValueChange={(value) => updateSetup({ version: value ?? '' })}
        disabled={disabled}
      >
        <SelectTrigger className="w-full" aria-label={msg('evaluations.versionRequired', 'Version')}>
          <SelectValue placeholder={msg('evaluations.chooseAgentFirst', 'Choose an agent first')} />
        </SelectTrigger>
        <SelectContent>
          {generation.versions.map((item) => (
            <SelectItem key={item.version} value={String(item.version)}>
              v{item.version}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Label htmlFor="evaluation-generate-count">{msg('evaluations.generateCount', 'How many')}</Label>
      <div className="flex items-center gap-2">
        <Button
          type="button"
          variant="outline"
          disabled={disabled}
          onClick={() => updateSetup({ count: String(stepCount(count, -1)) })}
        >
          −
        </Button>
        <Input
          id="evaluation-generate-count"
          className="w-20 bg-background text-center tabular-nums"
          inputMode="numeric"
          disabled={disabled}
          value={setup.count}
          onChange={(event) => updateSetup({ count: event.target.value.replace(/\D/g, '') })}
          onBlur={(event) => updateSetup({ count: String(clampCount(Number(event.currentTarget.value))) })}
        />
        <Button
          type="button"
          variant="outline"
          disabled={disabled}
          onClick={() => updateSetup({ count: String(stepCount(count, 1)) })}
        >
          +
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        {msg(
          'evaluations.generateCountHint',
          '1–50. Cover each skill or tool at least once. About half should be normal paths, the rest edge cases.',
        )}
      </p>
      <Label htmlFor="evaluation-generate-note">{msg('evaluations.generateNote', 'What to cover')}</Label>
      <Textarea
        id="evaluation-generate-note"
        value={setup.note}
        disabled={disabled}
        onChange={(event) => updateSetup({ note: event.target.value })}
        rows={4}
        placeholder={msg(
          'evaluations.generateNotePlaceholder',
          'For example: the user is an overseas customer asking in English. Cover the refund flow.',
        )}
      />
      <p className="text-xs text-muted-foreground">
        {msg(
          'evaluations.generateFlowHint',
          'Scenarios are planned automatically before cases are generated. To adjust them first, choose Review the plan first. Generated cases are saved only after you review them.',
        )}
      </p>
    </>
  );
}

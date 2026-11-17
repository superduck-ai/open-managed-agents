import type {
  EvaluationCaseDraft,
  EvaluationCaseInput,
  EvaluationCaseScenario,
  EvaluationGenerationSource,
} from './api';

export const MAX_GENERATED_CASES = 50;
export const MAX_PLAN_SCENARIOS = 12;

export function generationErrorCopy(message: string) {
  if (message === 'Could not generate evaluation cases') {
    return { id: 'evaluations.generateFailed', fallback: 'Could not generate evaluation cases.' };
  }
  return null;
}

export type CaseGenerationStep = 'setup' | 'plan' | 'review';

export type CaseGenerationSetup = {
  agentId: string;
  version: string;
  count: string;
  note: string;
};

export type ReviewDraft = EvaluationCaseDraft & { key: string; selected: boolean };

export type GenerationStats = { requested: number; duplicates: number; failed: number };

type MessageRef = { id: string; fallback: string };

export const initialSetup: CaseGenerationSetup = { agentId: '', version: '', count: '20', note: '' };

export function setupError(
  setup: CaseGenerationSetup,
  options: { requireAgent: boolean; hasDataset: boolean; versionCount: number },
): MessageRef | null {
  const amount = Number(setup.count);
  if (!Number.isInteger(amount) || amount < 1 || amount > MAX_GENERATED_CASES) {
    return { id: 'evaluations.generateCountInvalid', fallback: 'Generate between 1 and 50 cases.' };
  }
  if (options.requireAgent && !setup.agentId) return { id: 'evaluations.chooseAgent', fallback: 'Choose an agent' };
  if (setup.agentId && options.versionCount > 0 && !setup.version) {
    return { id: 'evaluations.chooseVersion', fallback: 'Choose a version' };
  }
  if (!setup.agentId && !options.hasDataset && !setup.note.trim()) {
    return { id: 'evaluations.generateSourceRequired', fallback: 'Choose an agent or describe what to cover.' };
  }
  return null;
}

export function generationSource(setup: CaseGenerationSetup, datasetId?: string): EvaluationGenerationSource {
  return {
    note: setup.note,
    agent_id: setup.agentId || undefined,
    agent_version: Number(setup.version) || undefined,
    dataset_id: datasetId || undefined,
  };
}

export function planTotal(scenarios: EvaluationCaseScenario[]) {
  return scenarios.reduce((sum, scenario) => sum + scenario.count, 0);
}

export function samePlan(left: EvaluationCaseScenario[], right: EvaluationCaseScenario[]) {
  return (
    left.length === right.length &&
    left.every((scenario, index) => scenarioSignature(scenario) === scenarioSignature(right[index]))
  );
}

function scenarioSignature(scenario: EvaluationCaseScenario) {
  return [scenario.name, scenario.description, scenario.persona, scenario.polarity, String(scenario.count)].join('\0');
}

export function planError(scenarios: EvaluationCaseScenario[]): MessageRef | null {
  if (scenarios.length === 0) return { id: 'evaluations.planEmpty', fallback: 'Add at least one scenario.' };
  if (scenarios.some((scenario) => !scenario.name.trim() || !Number.isInteger(scenario.count) || scenario.count < 1)) {
    return { id: 'evaluations.planInvalid', fallback: 'Each scenario needs a name and at least one case.' };
  }
  if (scenarios.length > MAX_PLAN_SCENARIOS || planTotal(scenarios) > MAX_GENERATED_CASES) {
    return { id: 'evaluations.planTooLarge', fallback: 'A plan allows at most 12 scenarios and 50 cases.' };
  }
  return null;
}

export function blankScenario(): EvaluationCaseScenario {
  return { name: '', description: '', persona: '', polarity: 'positive', count: 1 };
}

export function reviewDrafts(drafts: EvaluationCaseDraft[]): ReviewDraft[] {
  return drafts.map((draft, index) => ({ ...draft, key: `draft-${index}`, selected: true }));
}

export function selectedCaseInputs(drafts: ReviewDraft[]): EvaluationCaseInput[] {
  return drafts
    .filter((draft) => draft.selected && draft.query.trim())
    .map((draft) => ({
      query: draft.query,
      expected_answer: draft.expected_answer,
      expected_tool_trace: draft.expected_tool_trace ?? undefined,
      polarity: draft.polarity,
      acceptance: draft.polarity === 'negative' ? draft.acceptance : '',
      origin: 'generated',
    }));
}

export function toolTraceText(trace: unknown) {
  if (!Array.isArray(trace)) return '';
  return trace
    .map((step) => (typeof step === 'string' ? step : ''))
    .filter(Boolean)
    .join(' → ');
}

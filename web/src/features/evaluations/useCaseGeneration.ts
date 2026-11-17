import { useMutation, useQuery } from '@tanstack/react-query';
import { useRef, useState } from 'react';

import { listAgentVersions } from '../managed-agents/api';
import { useI18n } from '../../shared/i18n';
import { draftEvaluationCases, evaluationError, planEvaluationCases, type EvaluationCaseScenario } from './api';
import { listTaskAgents } from './taskOptions';
import {
  generationErrorCopy,
  generationSource,
  initialSetup,
  planError,
  reviewDrafts,
  samePlan,
  selectedCaseInputs,
  setupError,
  type CaseGenerationSetup,
  type CaseGenerationStep,
  type GenerationStats,
  type ReviewDraft,
} from './caseGeneration';
import { useEvaluationScope } from './useEvaluationScope';

const emptyStats: GenerationStats = { requested: 0, duplicates: 0, failed: 0 };

type PlanTarget = 'drafts' | 'edit';

export type CaseGeneration = ReturnType<typeof useCaseGeneration>;

export function useCaseGeneration({
  open,
  datasetId,
  requireAgent,
}: {
  open: boolean;
  datasetId?: string;
  requireAgent: boolean;
}) {
  const { msg } = useI18n();
  const scope = useEvaluationScope();
  const [step, setStep] = useState<CaseGenerationStep>('setup');
  const [setup, setSetup] = useState<CaseGenerationSetup>(initialSetup);
  const [scenarios, setScenarios] = useState<EvaluationCaseScenario[]>([]);
  const [drafts, setDrafts] = useState<ReviewDraft[]>([]);
  const [generatedScenarios, setGeneratedScenarios] = useState<EvaluationCaseScenario[]>([]);
  const [stats, setStats] = useState<GenerationStats>(emptyStats);
  const [error, setError] = useState('');
  const generation = useRef(0);
  const agents = useQuery({
    queryKey: ['evaluation-agents', scope.workspaceId],
    queryFn: () => listTaskAgents(scope.workspaceId),
    enabled: open && scope.ready,
  });
  const versions = useQuery({
    queryKey: ['evaluation-dataset-agent-versions', scope.workspaceId, setup.agentId],
    queryFn: () => listAgentVersions(setup.agentId, scope.workspaceId),
    enabled: open && scope.ready && Boolean(setup.agentId),
  });
  const versionRows = versions.data?.data ?? [];
  const latestVersion = setup.agentId && versionRows[0] ? String(versionRows[0].version) : '';
  const effectiveSetup = { ...setup, version: setup.version || latestVersion };
  const draft = useMutation({
    mutationFn: (planned: EvaluationCaseScenario[]) => {
      const token = generation.current;
      return draftEvaluationCases(scope.orgUuid, scope.workspaceId, {
        ...generationSource(effectiveSetup, datasetId),
        scenarios: planned,
      }).then(
        (result) => ({ ok: true as const, result, planned, token }),
        (cause: unknown) => ({ ok: false as const, cause, token }),
      );
    },
    onSuccess: (outcome) => {
      if (outcome.token !== generation.current) return;
      if (!outcome.ok) {
        setStep('plan');
        setError(shownGenerationError(outcome.cause, msg));
        return;
      }
      setDrafts(reviewDrafts(outcome.result.data));
      setGeneratedScenarios(outcome.planned.map((scenario) => ({ ...scenario })));
      setStats({
        requested: outcome.result.requested,
        duplicates: outcome.result.duplicates,
        failed: outcome.result.failed,
      });
      setStep('review');
      setError('');
    },
  });
  const plan = useMutation({
    mutationFn: (target: PlanTarget) => {
      const token = generation.current;
      return planEvaluationCases(scope.orgUuid, scope.workspaceId, {
        ...generationSource(effectiveSetup, datasetId),
        count: Number(setup.count),
      }).then(
        (result) => ({ ok: true as const, scenarios: result.scenarios, target, token }),
        (cause: unknown) => ({ ok: false as const, cause, token }),
      );
    },
    onSuccess: (outcome) => {
      if (outcome.token !== generation.current) return;
      if (!outcome.ok) {
        setError(shownGenerationError(outcome.cause, msg));
        return;
      }
      setScenarios(outcome.scenarios);
      setError('');
      if (outcome.target === 'edit') setStep('plan');
      else draft.mutate(outcome.scenarios);
    },
  });

  function fail(message: { id: string; fallback: string } | null) {
    if (!message) return false;
    setError(msg(message.id, message.fallback));
    return true;
  }

  function startPlan(target: PlanTarget) {
    const options = { requireAgent, hasDataset: Boolean(datasetId), versionCount: versionRows.length };
    if (!fail(setupError(effectiveSetup, options))) plan.mutate(target);
  }

  const planChanged = drafts.length > 0 && !samePlan(scenarios, generatedScenarios);

  function regenerate() {
    if (!fail(planError(scenarios))) draft.mutate(scenarios);
  }

  function returnToDrafts() {
    setError('');
    setStep('review');
  }

  function advance() {
    if (step === 'setup') startPlan('drafts');
    else if (step === 'plan' && drafts.length > 0 && !planChanged) returnToDrafts();
    else if (step === 'plan') regenerate();
  }

  function back() {
    setError('');
    setStep(step === 'review' ? 'plan' : 'setup');
  }

  function reset() {
    generation.current += 1;
    plan.reset();
    draft.reset();
    setStep('setup');
    setSetup(initialSetup);
    setScenarios([]);
    setDrafts([]);
    setGeneratedScenarios([]);
    setStats(emptyStats);
    setError('');
  }

  return {
    step,
    setup: effectiveSetup,
    updateSetup: (patch: Partial<CaseGenerationSetup>) => setSetup((value) => ({ ...value, ...patch })),
    agents: agents.data ?? [],
    versions: versionRows,
    scenarios,
    setScenarios,
    drafts,
    setDrafts,
    stats,
    error,
    busy: plan.isPending || draft.isPending,
    planning: plan.isPending,
    selected: selectedCaseInputs(drafts),
    planChanged,
    advance,
    regenerate,
    returnToDrafts,
    previewPlan: () => startPlan('edit'),
    back,
    reset,
  };
}

function shownGenerationError(cause: unknown, msg: (id: string, fallback: string) => string) {
  const message = evaluationError(cause);
  const copy = generationErrorCopy(message);
  return copy ? msg(copy.id, copy.fallback) : message;
}

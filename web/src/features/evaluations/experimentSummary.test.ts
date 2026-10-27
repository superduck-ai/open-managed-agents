import { expect, test } from 'bun:test';
import type { EvaluationExperiment } from './api';
import {
  callStep,
  detailFacts,
  reportIssues,
  reportStep,
  scorePercentLabel,
  scoreStep,
  stepCountLabel,
} from './experimentSummary';

test('report issues keep full marks out and turn model rounds into facts', () => {
  const experiment = runningExperiment();
  experiment.scores = [
    {
      id: 'evalscore_full',
      item_id: 'evalitem_0',
      evaluator_id: 'evalev_test',
      version_id: 'eev_test',
      status: 'completed',
      score: 1,
      explanation: 'one round',
      details: {
        rounds: [
          {
            index: 1,
            model: 'default-2/deepseek-v4.1-flash',
            duration_seconds: 4,
            input_tokens: 258,
            output_tokens: 311,
          },
        ],
        slowest_seconds: 4,
      },
    },
    {
      id: 'evalscore_low',
      item_id: 'evalitem_1',
      evaluator_id: 'evalev_test',
      version_id: 'eev_test',
      status: 'completed',
      score: 0.6,
      explanation: 'slow',
    },
    {
      id: 'evalscore_bad',
      item_id: 'evalitem_2',
      evaluator_id: 'evalev_test',
      version_id: 'eev_test',
      status: 'failed',
      error: 'Evaluation score file is missing',
    },
  ];
  const issues = reportIssues(experiment);
  expect(issues).toHaveLength(1);
  expect(issues[0]?.affected).toBe(2);
  expect(issues[0]?.failed).toBe(1);
  expect(issues[0]?.partial).toBe(1);
  expect(issues[0]?.notes.map((note) => note.text)).toEqual(['slow', 'Evaluation score file is missing']);
  expect(detailFacts(experiment.scores[0]?.details)).toEqual([
    {
      kind: 'round',
      index: 1,
      model: 'default-2/deepseek-v4.1-flash',
      seconds: 4,
      inputTokens: 258,
      outputTokens: 311,
    },
  ]);
  expect(scorePercentLabel(1)).toBe('100%');
  expect(scorePercentLabel(0.979)).toBe('97.9%');
  expect(scorePercentLabel(0.6)).toBe('60%');
});

test('step progress matches finished and successful counts while running', () => {
  const experiment = runningExperiment();
  expect(stepCountLabel(callStep(experiment))).toBe('57 / 100 (50 ok)');
  expect(stepCountLabel(scoreStep(experiment))).toBe('40 / 200 (30 ok)');
  expect(stepCountLabel(reportStep(scoreStep(experiment), experiment))).toBe('40 / 200 (30 ok)');
});

function runningExperiment(): EvaluationExperiment {
  return {
    id: 'evalexp_test',
    type: 'evaluation_experiment',
    name: 'nightly',
    desc: '',
    status: 'running',
    dataset_id: 'evalds_test',
    version_id: 'edv_test',
    agent_id: 'agent_test',
    agent_version: 1,
    environment_id: 'env_test',
    timeout_seconds: 60,
    item_count: 100,
    session_count: 57,
    evaluator_count: 2,
    items: [
      ...Array.from({ length: 50 }, (_, index) => item(index, 'completed')),
      ...Array.from({ length: 7 }, (_, index) => item(50 + index, 'failed')),
      ...Array.from({ length: 43 }, (_, index) => item(57 + index, 'running')),
    ],
    scores: [
      ...Array.from({ length: 30 }, (_, index) => score(index, 'completed')),
      ...Array.from({ length: 10 }, (_, index) => score(30 + index, 'failed')),
    ],
    created_at: '2026-10-22T00:00:00Z',
    updated_at: '2026-10-22T00:00:00Z',
  };
}

function item(index: number, status: 'completed' | 'failed' | 'running') {
  return {
    id: `evalitem_${index}`,
    case_id: `evalcase_${index}`,
    position: index + 1,
    status,
  };
}

function score(index: number, status: 'completed' | 'failed') {
  return {
    id: `evalscore_${index}`,
    item_id: `evalitem_${index}`,
    evaluator_id: 'evalev_test',
    version_id: 'eev_test',
    status,
  };
}

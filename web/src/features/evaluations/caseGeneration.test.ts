import { expect, test } from 'bun:test';

import {
  generationErrorCopy,
  initialSetup,
  planError,
  reviewDrafts,
  samePlan,
  selectedCaseInputs,
  setupError,
  toolTraceText,
} from './caseGeneration';

test('a plan matches only when every scenario field is unchanged', () => {
  const scenario = { name: '查询订单', description: '常规', persona: '客户', polarity: 'positive' as const, count: 2 };
  expect(samePlan([scenario], [{ ...scenario }])).toBe(true);
  expect(samePlan([scenario], [{ ...scenario, name: '物流查询' }])).toBe(false);
  expect(samePlan([scenario], [{ ...scenario, count: 3 }])).toBe(false);
});

test('generation failures keep a stable message for the dialog', () => {
  expect(generationErrorCopy('Could not generate evaluation cases')?.id).toBe('evaluations.generateFailed');
  expect(generationErrorCopy('Agent not found')).toBeNull();
});

test('setup needs a valid count and some source to generate from', () => {
  const options = { requireAgent: false, hasDataset: false, versionCount: 0 };
  expect(setupError({ ...initialSetup, count: '0', note: '退款' }, options)?.id).toBe(
    'evaluations.generateCountInvalid',
  );
  expect(setupError({ ...initialSetup, count: '51', note: '退款' }, options)?.id).toBe(
    'evaluations.generateCountInvalid',
  );
  expect(setupError({ ...initialSetup, note: '  ' }, options)?.id).toBe('evaluations.generateSourceRequired');
  expect(setupError(initialSetup, { ...options, requireAgent: true })?.id).toBe('evaluations.chooseAgent');
  expect(setupError({ ...initialSetup, agentId: 'agent_1' }, { ...options, versionCount: 2 })?.id).toBe(
    'evaluations.chooseVersion',
  );
  expect(setupError(initialSetup, { ...options, hasDataset: true })).toBeNull();
  expect(setupError({ ...initialSetup, note: '覆盖退款' }, options)).toBeNull();
});

test('plan rejects empty, unnamed, or oversized scenario lists', () => {
  const scenario = { name: '查询订单', description: '', persona: '', polarity: 'positive' as const, count: 5 };
  expect(planError([])?.id).toBe('evaluations.planEmpty');
  expect(planError([{ ...scenario, name: ' ' }])?.id).toBe('evaluations.planInvalid');
  expect(planError([{ ...scenario, count: 0 }])?.id).toBe('evaluations.planInvalid');
  expect(
    planError([
      { ...scenario, count: 30 },
      { ...scenario, count: 30 },
    ])?.id,
  ).toBe('evaluations.planTooLarge');
  expect(planError(Array.from({ length: 13 }, () => ({ ...scenario, count: 1 })))?.id).toBe('evaluations.planTooLarge');
  expect(planError([scenario])).toBeNull();
});

test('only selected drafts with a question become cases, and only negatives keep acceptance', () => {
  const drafts = reviewDrafts([
    {
      query: '查询订单',
      expected_answer: '物流节点',
      expected_tool_trace: ['lookup_order'],
      polarity: 'positive',
      acceptance: '不应保留',
      scenario: '查询',
    },
    {
      query: '直接退款',
      expected_answer: '',
      expected_tool_trace: null,
      polarity: 'negative',
      acceptance: '拒绝绕过审核',
      scenario: '越权',
    },
    {
      query: ' ',
      expected_answer: '',
      expected_tool_trace: null,
      polarity: 'positive',
      acceptance: '',
      scenario: '空',
    },
    {
      query: '未选',
      expected_answer: '',
      expected_tool_trace: null,
      polarity: 'positive',
      acceptance: '',
      scenario: '',
    },
  ]);
  drafts[3] = { ...drafts[3], selected: false };

  expect(selectedCaseInputs(drafts)).toEqual([
    {
      query: '查询订单',
      expected_answer: '物流节点',
      expected_tool_trace: ['lookup_order'],
      polarity: 'positive',
      acceptance: '',
      origin: 'generated',
    },
    {
      query: '直接退款',
      expected_answer: '',
      expected_tool_trace: undefined,
      polarity: 'negative',
      acceptance: '拒绝绕过审核',
      origin: 'generated',
    },
  ]);
});

test('tool trace text joins tool names and ignores other shapes', () => {
  expect(toolTraceText(['lookup_order', 'refund'])).toBe('lookup_order → refund');
  expect(toolTraceText(null)).toBe('');
  expect(toolTraceText({ name: 'x' })).toBe('');
});

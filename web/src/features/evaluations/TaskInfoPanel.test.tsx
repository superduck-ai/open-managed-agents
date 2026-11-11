import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import type { EvaluationTask } from './api';
import { TaskInfoPanel } from './TaskInfoPanel';

const { cleanup, render, screen } = await import('@testing-library/react');

afterEach(cleanup);

const task: EvaluationTask = {
  id: 'evaltask_demo',
  name: '回归',
  mode: 'evaluate',
  status: 'completed',
  agent_id: 'agent_demo',
  agent_version: 2,
  environment_id: 'env_demo',
  dataset_id: 'evalds_demo',
  dataset_version_id: 'edv_demo',
  rubric_id: 'evalrb_demo',
  rubric_version_id: 'erv_demo',
  pass_score: 60,
  error_message: '',
  created_at: '2026-11-11T02:00:00Z',
  resource_count: 0,
  vault_count: 0,
  usage: {
    input_tokens: 1200,
    output_tokens: 300,
    cache_read_input_tokens: 0,
    judge_input_tokens: 4567,
    judge_output_tokens: 89,
  },
  summary: { round: 1, score: 80, pass_rate: 1, scored: 1, passed: 1, exceptions: 0, total: 1 },
};

test('task info shows judge token usage beside agent usage', () => {
  render(
    <I18nProvider initialLocale="zh-CN">
      <TaskInfoPanel task={task} />
    </I18nProvider>,
  );

  expect(screen.getByText('评审输入 Tokens').nextElementSibling?.textContent).toBe((4567).toLocaleString());
  expect(screen.getByText('评审输出 Tokens').nextElementSibling?.textContent).toBe('89');
  expect(screen.getByText('输入 Tokens').nextElementSibling?.textContent).toBe((1200).toLocaleString());
});

import { expect, test } from 'bun:test';
import { quickstartChatReplyToolResult, quickstartTurnMessages, toolResultBlock } from './chatModel';
import type { QuickstartMessage } from './platformQuickstartRequest';

test('legacy tool errors preserve the tool ID and error flag instead of reporting success', () => {
  expect(toolResultBlock('create-env', { content: 'Network policy rejected.', isError: true })).toEqual({
    type: 'tool_result',
    tool_use_id: 'create-env',
    content: 'Network policy rejected.',
    is_error: true,
  });
  expect(toolResultBlock('selected-env', { content: 'Selected.' })).not.toHaveProperty('is_error');
});
test('legacy turn context reflects the current step and config without changing saved visible history', () => {
  const messages: QuickstartMessage[] = [{ role: 'user', content: [toolResultBlock('next', { content: 'Continue' })] }];
  const snapshot = JSON.stringify(messages);
  const result = quickstartTurnMessages(messages, {
    step: 'environment',
    wantsSchedule: true,
    config: { name: 'Latest config' },
    modelID: 'workspace-model',
    locale: 'en',
  });
  expect(JSON.stringify(result)).toContain('[Current quickstart step: \\"environment\\".');
  expect(JSON.stringify(result)).toContain('Latest config');
  expect(JSON.stringify(result)).toContain('[Deployment schedule planned: yes.]');
  expect(JSON.stringify(messages)).toBe(snapshot);
  expect(
    quickstartTurnMessages([], {
      step: 'agent',
      wantsSchedule: false,
      config: {},
      modelID: 'workspace-model',
      locale: 'en',
    }),
  ).toEqual([]);
});
test('legacy replies distinguish proposal refinements and questions during an open test', () => {
  expect(quickstartChatReplyToolResult({ name: 'build_agent_config' }, 'Read only')).toBe(
    'User sent a message instead: "Read only"',
  );
  expect(quickstartChatReplyToolResult({ name: 'await_test_run' }, 'Why did that tool fail?')).toBe(
    'User sent a message instead: "Why did that tool fail?"',
  );
  expect(quickstartChatReplyToolResult({ name: 'offer_next_step' }, '先等等', 'zh-CN')).toBe(
    '用户在聊天中回复：先等等',
  );
});

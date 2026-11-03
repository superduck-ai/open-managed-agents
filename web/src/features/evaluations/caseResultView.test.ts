import { expect, test } from 'bun:test';

import { caseResultTrace, checkpointLines, detailsSnippet, formatElapsed, scoreHundredLabel } from './caseResultView';

test('case result trace keeps the session output and score scale', () => {
  const trace = caseResultTrace(
    {
      type: 'session',
      created_at: '2026-10-27T11:29:22.000Z',
      updated_at: '2026-10-27T11:29:32.150Z',
      usage: { input_tokens: 12, output_tokens: 40 },
    },
    [
      { type: 'user.message', content: '写一封拒绝合作的邮件。' },
      { type: 'agent.tool_use', name: 'search' },
      { type: 'tool_result', content: 'none' },
      { type: 'agent.message', content: [{ type: 'text', text: '您好，无法继续合作。' }] },
    ],
  );
  expect(trace.messages.map((message) => message.role)).toEqual(['user', 'agent']);
  expect(trace.output).toBe('您好，无法继续合作。');
  expect(trace.rounds).toBe(1);
  expect(trace.toolCalls).toBe(1);
  expect(trace.inputTokens).toBe(12);
  expect(trace.outputTokens).toBe(40);
  expect(formatElapsed(trace.elapsedSeconds ?? 0)).toBe('10.15s');
  expect(scoreHundredLabel(1)).toBe('100.0');
  expect(scoreHundredLabel(0.91)).toBe('91.0');
  expect(checkpointLines(['明确拒绝', '语气礼貌'])).toEqual(['明确拒绝', '语气礼貌']);
  expect(detailsSnippet({ findings: 'slow' }).startsWith('{"findings":')).toBe(true);
});

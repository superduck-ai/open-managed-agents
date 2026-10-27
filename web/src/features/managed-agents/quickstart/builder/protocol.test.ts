import { afterEach, expect, test } from 'bun:test';
import { streamBuilderTurn, chatMessages } from './protocol';
import { setConsoleRequestContext } from '../../../../shared/api/client';

const originalFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});
function stream(events: unknown[]) {
  return new Response(events.map((data) => `data: ${JSON.stringify(data)}\n\n`).join(''), {
    headers: { 'Content-Type': 'text/event-stream' },
  });
}
function input(backend: 'messages' | 'chat_completions' = 'messages') {
  return {
    orgUuid: 'org_test',
    workspaceId: 'workspace_test',
    model: 'provider/model',
    backend,
    system: [],
    tools: [],
    messages: [{ role: 'user' as const, content: 'Build an extractor' }],
    signal: new AbortController().signal,
    onText: () => {},
  };
}
function toolEvents(json: string, reason = 'tool_use') {
  return [
    {
      type: 'content_block_start',
      index: 0,
      content_block: { type: 'tool_use', id: 'tool1', name: 'build_agent_config', input: {} },
    },
    { type: 'content_block_delta', index: 0, delta: { type: 'input_json_delta', partial_json: json } },
    { type: 'message_delta', delta: { stop_reason: reason } },
    { type: 'message_stop' },
  ];
}
test('truncated or malformed tool calls never become executable tools', async () => {
  for (const events of [
    toolEvents('{"name":'),
    toolEvents('{"name":"Draft"}', 'max_tokens'),
    toolEvents('{"name":"Draft"}').slice(0, -1),
  ]) {
    globalThis.fetch = (async () => stream(events)) as typeof fetch;
    await expect(streamBuilderTurn(input())).rejects.toThrow();
  }
});
test('Messages collects fragmented tool JSON and sends scoped CSRF without provider credentials', async () => {
  setConsoleRequestContext({ csrfToken: 'csrf_test' });
  let captured: RequestInit | undefined;
  globalThis.fetch = (async (_url, init) => {
    captured = init;
    return stream([
      ...toolEvents('{"name":').slice(0, 2),
      { type: 'content_block_delta', index: 0, delta: { type: 'input_json_delta', partial_json: '"Draft"}' } },
      ...toolEvents('').slice(2),
    ]);
  }) as typeof fetch;
  const turn = await streamBuilderTurn(input());
  expect(turn.tools).toEqual([{ id: 'tool1', name: 'build_agent_config', input: { name: 'Draft' } }]);
  const headers = new Headers(captured?.headers);
  expect(headers.get('X-CSRF-Token')).toBe('csrf_test');
  expect(headers.get('X-Workspace-ID')).toBe('workspace_test');
  expect(headers.has('Authorization')).toBe(false);
});
test('Chat collects fragmented calls and retains reasoning for the following tool result', async () => {
  globalThis.fetch = (async () =>
    stream([
      {
        choices: [
          {
            delta: {
              reasoning_content: 'reasoning',
              tool_calls: [{ index: 0, id: 'call1', function: { name: 'create_environment', arguments: '{"name":' } }],
            },
          },
        ],
      },
      {
        choices: [
          { delta: { tool_calls: [{ index: 0, function: { arguments: '"Sandbox"}' } }] }, finish_reason: 'tool_calls' },
        ],
      },
    ])) as typeof fetch;
  const turn = await streamBuilderTurn(input('chat_completions'));
  expect(turn.tools[0].input).toEqual({ name: 'Sandbox' });
  expect(
    chatMessages([
      turn.message,
      { role: 'user', content: [{ type: 'tool_result', tool_use_id: 'call1', content: 'confirmed' }] },
    ]),
  ).toEqual([
    {
      role: 'assistant',
      content: null,
      tool_calls: [
        { id: 'call1', type: 'function', function: { name: 'create_environment', arguments: '{"name":"Sandbox"}' } },
      ],
      reasoning_content: 'reasoning',
    },
    { role: 'tool', tool_call_id: 'call1', content: 'confirmed' },
  ]);
});
test('Chat length termination rejects even syntactically valid tool arguments', async () => {
  globalThis.fetch = (async () =>
    stream([
      {
        choices: [
          {
            delta: {
              tool_calls: [
                { index: 0, id: 'call', function: { name: 'create_vault', arguments: '{"name":"Unsafe"}' } },
              ],
            },
            finish_reason: 'length',
          },
        ],
      },
    ])) as typeof fetch;
  await expect(streamBuilderTurn(input('chat_completions'))).rejects.toThrow('incomplete');
});

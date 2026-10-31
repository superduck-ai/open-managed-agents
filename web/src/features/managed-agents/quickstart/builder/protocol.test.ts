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
  expect(JSON.parse(String(captured?.body))).not.toHaveProperty('stream_options');
});
test('Chat collects fragmented calls and retains reasoning for the following tool result', async () => {
  let captured: RequestInit | undefined;
  globalThis.fetch = (async (_url, init) => {
    captured = init;
    return stream([
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
    ]);
  }) as typeof fetch;
  const turn = await streamBuilderTurn(input('chat_completions'));
  expect(JSON.parse(String(captured?.body)).stream_options).toEqual({ include_usage: true });
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

test('synthetic template tool history carries an empty reasoning field for thinking-mode continuations', () => {
  const messages = chatMessages([
    { role: 'user', content: 'Use the Deep research template.' },
    {
      role: 'assistant',
      content: [
        { type: 'tool_use', id: 'template-call', name: 'build_agent_config', input: { name: 'Deep research' } },
      ],
    },
    {
      role: 'user',
      content: [{ type: 'tool_result', tool_use_id: 'template-call', content: JSON.stringify({ saved: true }) }],
    },
  ]);
  expect(messages[1]).toMatchObject({
    role: 'assistant',
    content: null,
    reasoning_content: '',
    tool_calls: [{ id: 'template-call' }],
  });
  expect(messages[2]).toMatchObject({ role: 'tool', tool_call_id: 'template-call' });
});

test('legacy turn context follows every tool result in Chat Completions history', () => {
  const messages = chatMessages([
    {
      role: 'assistant',
      content: [
        { type: 'tool_use', id: 'first', name: 'list_environments', input: {} },
        { type: 'tool_use', id: 'second', name: 'ask_user_questions', input: {} },
      ],
    },
    { role: 'user', content: [{ type: 'tool_result', tool_use_id: 'first', content: '[]' }] },
    {
      role: 'user',
      content: [
        { type: 'tool_result', tool_use_id: 'second', content: 'User chose limited networking.' },
        { type: 'text', text: '[Current quickstart step: "environment".]' },
      ],
    },
  ]);
  expect(messages.map((message) => message.role)).toEqual(['assistant', 'tool', 'tool', 'user']);
  expect(messages[2]).toMatchObject({ tool_call_id: 'second' });
  expect(messages[3]).toMatchObject({ content: '[Current quickstart step: "environment".]' });
});

test('Chat accepts absent, empty and whitespace arguments for parameterless tools but rejects malformed JSON', async () => {
  for (const argumentsValue of [undefined, '', '  ', '{']) {
    globalThis.fetch = (async () =>
      stream([
        {
          choices: [
            {
              delta: {
                tool_calls: [
                  { index: 0, id: 'empty', function: { name: 'list_environments', arguments: argumentsValue } },
                ],
              },
              finish_reason: 'tool_calls',
            },
          ],
        },
      ])) as typeof fetch;
    if (argumentsValue === '{') await expect(streamBuilderTurn(input('chat_completions'))).rejects.toThrow();
    else
      expect((await streamBuilderTurn(input('chat_completions'))).tools).toEqual([
        { id: 'empty', name: 'list_environments', input: {} },
      ]);
  }
});

test('text-only optimization requests omit tool configuration for both backends', async () => {
  for (const backend of ['messages', 'chat_completions'] as const) {
    let body: Record<string, unknown> = {};
    globalThis.fetch = (async (_url, init) => {
      body = JSON.parse(String(init?.body));
      return stream(
        backend === 'messages'
          ? [
              { type: 'content_block_start', index: 0, content_block: { type: 'text', text: 'Improved draft' } },
              { type: 'message_delta', delta: { stop_reason: 'end_turn' } },
              { type: 'message_stop' },
            ]
          : [{ choices: [{ delta: { content: 'Improved draft' }, finish_reason: 'stop' }] }],
      );
    }) as typeof fetch;
    const result = await streamBuilderTurn(input(backend));
    expect(body).not.toHaveProperty('tools');
    expect(body).not.toHaveProperty('tool_choice');
    expect(result.tools).toEqual([]);
  }
});

import { postJsonSseStream } from '../../../../shared/api/streaming';
import { postQuickstartProxyStream, quickstartStreamError } from '../../api';
import { toRecord } from '../../utils';
import { builderToolSchema, type BuilderMessage, type BuilderTool } from './model';

export type BuilderTurn = { message: BuilderMessage; tools: BuilderTool[] };
type TurnInput = {
  orgUuid: string;
  workspaceId: string;
  model: string;
  backend: 'messages' | 'chat_completions';
  system: Array<{ type: string; text: string }>;
  tools: Array<Record<string, unknown>>;
  messages: BuilderMessage[];
  signal: AbortSignal;
  onText: (text: string) => void;
};

export async function streamBuilderTurn(input: TurnInput): Promise<BuilderTurn> {
  return input.backend === 'chat_completions' ? streamChatTurn(input) : streamMessagesTurn(input);
}
async function streamMessagesTurn(input: TurnInput): Promise<BuilderTurn> {
  const blocks = new Map<number, { value: Record<string, unknown>; json: string }>();
  let ended = false;
  let reason = '';
  await postQuickstartProxyStream({
    ...input,
    body: {
      model: input.model,
      max_tokens: 8192,
      system: input.system,
      messages: input.messages.map(({ role, content }) => ({ role, content })),
      tools: input.tools,
      tool_choice: { type: 'auto', disable_parallel_tool_use: true },
      stream: true,
    },
    onEvent: (event) => {
      const data = event.data;
      const type = String(data.type ?? event.event ?? '');
      if (type === 'error') throw new Error(String(toRecord(data.error)?.message ?? 'Model stream failed.'));
      const index = Number(data.index ?? 0);
      if (type === 'content_block_start') blocks.set(index, { value: { ...toRecord(data.content_block) }, json: '' });
      if (type === 'content_block_delta') applyMessagesDelta(data, blocks, input);
      if (type === 'message_delta') reason = String(toRecord(data.delta)?.stop_reason ?? '');
      if (type === 'message_stop') ended = true;
    },
  });
  if (!ended || !['end_turn', 'tool_use'].includes(reason))
    throw new Error('Model response was incomplete. Retry before applying any changes.');
  const content: Record<string, unknown>[] = [...blocks.entries()]
    .sort(([a], [b]) => a - b)
    .map(([, block]) =>
      block.value.type === 'tool_use'
        ? { ...block.value, input: block.json ? JSON.parse(block.json) : block.value.input }
        : block.value,
    );
  const tools = content.filter((block) => block.type === 'tool_use').map((block) => builderToolSchema.parse(block));
  if (!content.length) throw new Error('The model returned no content.');
  validateToolIDs(tools);
  return { message: { role: 'assistant', content }, tools };
}
export function chatMessages(messages: BuilderMessage[]) {
  return messages.flatMap((message) => {
    if (typeof message.content === 'string') return [{ role: message.role, content: message.content }];
    const text = message.content
      .filter((block) => block.type === 'text')
      .map((block) => String(block.text ?? ''))
      .join('');
    if (message.role === 'assistant') {
      const tools = message.content
        .filter((block) => block.type === 'tool_use')
        .map((block) => ({
          id: block.id,
          type: 'function',
          function: { name: block.name, arguments: JSON.stringify(block.input) },
        }));
      return [
        {
          role: 'assistant',
          content: text || null,
          ...(tools.length ? { tool_calls: tools } : {}),
          ...(message.reasoning ? { reasoning_content: message.reasoning } : {}),
        },
      ];
    }
    return [
      ...(text ? [{ role: 'user', content: text }] : []),
      ...message.content
        .filter((block) => block.type === 'tool_result')
        .map((block) => ({ role: 'tool', tool_call_id: block.tool_use_id, content: String(block.content ?? '') })),
    ];
  });
}
async function streamChatTurn(input: TurnInput): Promise<BuilderTurn> {
  let text = '';
  let reasoning = '';
  let reason = '';
  const calls = new Map<number, { id: string; name: string; arguments: string }>();
  const tools = input.tools.map((tool) => ({
    type: 'function',
    function: { name: tool.name, description: tool.description, parameters: tool.input_schema },
  }));
  await postJsonSseStream<Record<string, unknown>>({
    url: `/api/organizations/${encodeURIComponent(input.orgUuid)}/proxy/v1/chat/completions`,
    headers: { 'X-Organization-UUID': input.orgUuid, 'X-Workspace-ID': input.workspaceId },
    body: {
      model: input.model,
      max_tokens: 8192,
      messages: [
        { role: 'system', content: input.system.map((block) => block.text).join('\n\n') },
        ...chatMessages(input.messages),
      ],
      tools,
      tool_choice: 'auto',
      stream: true,
    },
    signal: input.signal,
    errorFromResponse: quickstartStreamError,
    onEvent: (event) => {
      if (event.data.error) throw new Error(String(toRecord(event.data.error)?.message ?? 'Model stream failed.'));
      const choice = toRecord(Array.isArray(event.data.choices) ? event.data.choices[0] : null);
      if (!choice) return;
      if (choice.finish_reason) reason = String(choice.finish_reason);
      const delta = toRecord(choice.delta);
      if (!delta) return;
      text += String(delta.content ?? '');
      reasoning += String(delta.reasoning_content ?? '');
      input.onText(text);
      for (const item of Array.isArray(delta.tool_calls) ? delta.tool_calls : []) {
        const call = toRecord(item);
        if (!call) continue;
        const index = Number(call.index ?? 0);
        const current = calls.get(index) ?? { id: '', name: '', arguments: '' };
        const fn = toRecord(call.function);
        if (call.id) current.id = String(call.id);
        current.name += String(fn?.name ?? '');
        current.arguments += String(fn?.arguments ?? '');
        calls.set(index, current);
      }
    },
  });
  if (!['stop', 'tool_calls'].includes(reason))
    throw new Error('Model response was incomplete. Retry before applying changes.');
  const parsed = [...calls.entries()]
    .sort(([a], [b]) => a - b)
    .map(([, call]) => builderToolSchema.parse({ id: call.id, name: call.name, input: JSON.parse(call.arguments) }));
  validateToolIDs(parsed);
  if (!text && !parsed.length) throw new Error('The model returned no content.');
  return {
    message: {
      role: 'assistant',
      content: [...(text ? [{ type: 'text', text }] : []), ...parsed.map((tool) => ({ type: 'tool_use', ...tool }))],
      ...(reasoning ? { reasoning } : {}),
    },
    tools: parsed,
  };
}

function applyMessagesDelta(
  data: Record<string, unknown>,
  blocks: Map<number, { value: Record<string, unknown>; json: string }>,
  input: TurnInput,
) {
  const index = Number(data.index ?? 0);
  const block = blocks.get(index);
  const delta = toRecord(data.delta);
  if (!block || !delta) return;
  if (delta.type === 'input_json_delta') block.json += String(delta.partial_json ?? '');
  if (delta.type === 'text_delta') {
    block.value.text = String(block.value.text ?? '') + String(delta.text ?? '');
    input.onText(
      [...blocks.values()].map((item) => (item.value.type === 'text' ? String(item.value.text ?? '') : '')).join(''),
    );
  }
  if (delta.type === 'thinking_delta')
    block.value.thinking = String(block.value.thinking ?? '') + String(delta.thinking ?? '');
  if (delta.type === 'signature_delta')
    block.value.signature = String(block.value.signature ?? '') + String(delta.signature ?? '');
}

function validateToolIDs(tools: BuilderTool[]) {
  if (new Set(tools.map((tool) => tool.id)).size !== tools.length)
    throw new Error('The model returned duplicate tool call IDs. Retry before applying changes.');
}

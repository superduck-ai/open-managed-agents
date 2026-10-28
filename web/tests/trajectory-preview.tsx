import '../src/styles.css';
import { createRoot } from 'react-dom/client';
import { NativeTrajectory } from '../src/features/observability/trajectory/NativeTrajectory';

const time = Date.now() - 5000;
const events = [
  { type: 'turn/start', data: { turn: 1 } },
  { type: 'step/start', data: { turn: 1, step: 1 } },
  {
    type: 'system/message',
    surfaceOp: 'append',
    data: {
      message: {
        id: 'system-1',
        role: 'system',
        content: [{ type: 'text', text: 'You are a helpful assistant.' }],
        source: { kind: 'system-prompt' },
      },
    },
  },
  {
    type: 'user/message',
    surfaceOp: 'append',
    data: {
      id: 'user-1',
      source: { kind: 'user' },
      content: [{ type: 'text', text: 'Inspect the current directory' }],
    },
  },
  {
    type: 'assistant/message',
    surfaceOp: 'append',
    data: {
      turn: 1,
      step: 1,
      message: {
        id: 'message-1',
        role: 'assistant',
        content: [
          { type: 'reasoning', text: 'I will inspect the files.' },
          { type: 'text', text: 'Checking the directory.' },
          { type: 'tool-call', id: 'call-1', name: 'Bash', arguments: '{"command":"pwd"}' },
        ],
        source: { kind: 'model', provider: 'oma', model: 'deepseek' },
      },
      stream: [
        { type: 'text-chunks', time0: time + 200, index: 1, dt: [0, 100], texts: ['Checking ', 'the directory.'] },
      ],
      usage: { inputTokens: 100, outputTokens: 20, cacheReadTokens: 80 },
    },
  },
  { type: 'tool/call', data: { turn: 1, step: 1, callId: 'call-1', name: 'Bash', arguments: '{"command":"pwd"}' } },
  {
    type: 'tool/result',
    surfaceOp: 'append',
    data: {
      turn: 1,
      step: 1,
      message: {
        id: 'result-1',
        role: 'tool',
        source: { kind: 'tool', callId: 'call-1' },
        toolCallId: 'call-1',
        content: [{ type: 'text', text: '/workspace' }],
      },
    },
  },
  { type: 'step/end', data: { turn: 1, step: 1 } },
  { type: 'turn/end', data: { turn: 1, reason: { kind: 'completed' } } },
].map((event, seq) => ({ ...event, seq, time: time + seq * 100 }));
const request = window.fetch.bind(window);
window.fetch = (input, init) => {
  if (String(input).includes('/observability/trajectories/fixture')) {
    const after = Number(new URL(String(input), window.location.origin).searchParams.get('after_seq'));
    return Promise.resolve(
      Response.json({
        header: { version: 4, id: 'fixture', createdAt: time },
        events: events.filter((event) => event.seq > after),
        next_seq: 8,
        has_more: false,
      }),
    );
  }
  return request(input, init);
};
const scope = { orgUuid: 'fixture', workspaceId: 'fixture' };
createRoot(document.getElementById('root')!).render(<NativeTrajectory scope={scope} id="fixture" />);

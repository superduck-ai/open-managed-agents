import { afterEach, describe, expect, mock, test } from 'bun:test';
import { I18nProvider } from '../../../shared/i18n';
import {
  MessageScroller,
  MessageScrollerContent,
  MessageScrollerProvider,
  MessageScrollerViewport,
} from '../../../shared/ui/message-scroller';
import { resetTestDom } from '../../../test/setup';
import {
  type DisplayEventEntry,
  type IdleGapEntry,
  type SessionEventListEntry,
  type SessionEventUsage,
  type ToolCallEntry,
} from '../types';
import { buildSessionEventEntries } from './sessionTraceModel';
import { SessionDetailDeltaFramesContext } from './sessionDetailData';
import { SessionTranscriptView } from './SessionTranscriptView';
import { type ReactNode } from 'react';

const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');

const EMPTY_USAGE: SessionEventUsage = {
  input: 0,
  output: 0,
  input_tokens: 0,
  output_tokens: 0,
  cache_read_input_tokens: 0,
  cache_creation_input_tokens: 0,
};

afterEach(() => cleanup());

describe('SessionTranscriptView', () => {
  test.each([0, 40])('prefers reported thinking and model durations %s over bracket timing', (durationMs) => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const entries = buildSessionEventEntries(
      [
        { id: 'request', type: 'span.model_request_start', processed_at: '2026-10-01T05:26:53.000Z' },
        {
          id: 'thinking',
          type: 'agent.thinking',
          processed_at: '2026-10-01T05:26:54.000Z',
          model_request_start_id: 'request',
          duration_ms: durationMs,
          content: [{ type: 'thinking', thinking: 'Checking.' }],
        },
        {
          id: 'answer',
          type: 'agent.message',
          processed_at: '2026-10-01T05:26:55.000Z',
          model_request_start_id: 'request',
          inference_ms: durationMs,
          duration_ms: 300,
          content: [{ type: 'text', text: 'Done.' }],
        },
        {
          id: 'request-end',
          type: 'span.model_request_end',
          processed_at: '2026-10-01T05:26:55.000Z',
          model_request_start_id: 'request',
        },
      ],
      'transcript',
    );

    const { container } = render(transcriptTree(entries));

    expect(container.querySelector('[data-transcript-thinking-row]')?.textContent).toContain('Thought for 0.0s');
    expect(
      container.querySelector('[data-transcript-iteration] [data-testid="session-meta-strip"]')?.textContent,
    ).toContain(`${durationMs}ms`);
    expect(screen.queryByText('2.0s')).toBeNull();
  });

  test.each(['inference_ms', 'model_duration_ms', 'model_request_duration_ms', 'duration_ms'])(
    'shows explicitly reported zero model duration from %s without a bracket',
    (key) => {
      resetTestDom('https://oma.duck.ai/sessions/test');
      const entries = buildSessionEventEntries(
        [
          {
            id: 'answer',
            type: 'agent.message',
            processed_at: '2026-10-01T05:26:55.000Z',
            content: [{ type: 'text', text: 'Done.' }],
            [key]: 0,
          },
        ],
        'transcript',
      );

      render(transcriptTree(entries));

      expect(screen.getByText('0ms')).toBeTruthy();
    },
  );

  test('uses a reported agent duration while its model request is still open', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const entries = buildSessionEventEntries(
      [
        { id: 'request', type: 'span.model_request_start', processed_at: '2026-10-01T05:26:53.000Z' },
        {
          id: 'answer',
          type: 'agent.message',
          processed_at: '2026-10-01T05:26:54.000Z',
          model_request_start_id: 'request',
          duration_ms: 40,
          content: [{ type: 'text', text: 'Done.' }],
        },
      ],
      'transcript',
    );

    const { container } = render(transcriptTree(entries));

    expect(
      container.querySelector('[data-transcript-iteration] [data-testid="session-meta-strip"]')?.textContent,
    ).toContain('40ms');
  });

  test('uses tool call and result timestamps when execution duration was not reported', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const entries = buildSessionEventEntries(
      [
        { id: 'request', type: 'span.model_request_start', processed_at: '2026-10-01T05:26:51.000Z' },
        { id: 'write', type: 'agent.tool_use', processed_at: '2026-10-01T05:26:52.000Z', name: 'Write', input: {} },
        {
          id: 'request-end',
          type: 'span.model_request_end',
          processed_at: '2026-10-01T05:26:52.000Z',
          model_request_start_id: 'request',
        },
        {
          id: 'written',
          type: 'agent.tool_result',
          processed_at: '2026-10-01T05:26:52.750Z',
          tool_use_id: 'write',
          content: 'Done',
        },
      ],
      'transcript',
    );

    const { container } = render(transcriptTree(entries));

    expect(container.querySelector('[data-transcript-tool-row]')?.textContent).toContain('750ms');
    expect(
      container
        .querySelector('[data-transcript-tool-row]')
        ?.closest('[data-transcript-iteration]')
        ?.querySelector('[data-testid="session-meta-strip"]')?.textContent,
    ).toContain('750ms');
    expect(screen.queryByText('0ms')).toBeNull();
  });

  test('shows thinking and model request durations from persisted events without reported execution timing', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const entries = buildSessionEventEntries(
      [
        { id: 'request', type: 'span.model_request_start', processed_at: '2026-10-01T05:26:53.237442Z' },
        {
          id: 'thinking',
          type: 'agent.thinking',
          processed_at: '2026-10-01T05:26:54.118Z',
          model_request_start_id: 'request',
          content: [{ type: 'thinking', thinking: 'File written.' }],
        },
        {
          id: 'answer',
          type: 'agent.message',
          processed_at: '2026-10-01T05:26:54.898337Z',
          model_request_start_id: 'request',
          content: [{ type: 'text', text: 'Done.' }],
        },
        {
          id: 'request-end',
          type: 'span.model_request_end',
          processed_at: '2026-10-01T05:26:54.898337Z',
          model_request_start_id: 'request',
        },
      ],
      'transcript',
    );

    const { container } = render(transcriptTree(entries));

    expect(screen.getByText('Thought for 0.9s')).toBeTruthy();
    const thinkingPanel = container.querySelector('[data-thinking-only]');
    expect(thinkingPanel?.querySelector('[data-testid="session-meta-strip"]')?.textContent).toContain('881ms');
    expect(screen.getByText('1.7s')).toBeTruthy();
    expect(screen.queryByText('0ms')).toBeNull();
  });

  test('hides unknown durations on thinking, messages, and tools', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const entries = buildSessionEventEntries(
      [
        {
          id: 'thinking',
          type: 'agent.thinking',
          processed_at: '2026-10-01T05:26:54.118Z',
          content: [{ type: 'thinking', thinking: 'Checking.' }],
        },
        {
          id: 'answer',
          type: 'agent.message',
          processed_at: '2026-10-01T05:26:54.898337Z',
          bracket_id: 'answer',
          content: [{ type: 'text', text: 'Done.' }],
        },
        { id: 'write', type: 'agent.tool_use', bracket_id: 'write', name: 'Write', input: {} },
        { id: 'written', type: 'agent.tool_result', tool_use_id: 'write', content: 'Done' },
      ],
      'transcript',
    );

    const { container } = render(transcriptTree(entries));

    expect(screen.getByText('Thought')).toBeTruthy();
    expect(container.querySelector('[data-transcript-tool-row]')?.textContent).toContain('Completed');
    expect(screen.queryByText('0ms')).toBeNull();
    expect(container.querySelectorAll('[data-testid="session-meta-strip"]')).toHaveLength(3);
  });

  test.each([
    [-1, 300, '300ms'],
    [-1, -2, '750ms'],
    [undefined, undefined, '750ms'],
    [0, 300, '0ms'],
    [40, 300, '40ms'],
  ])('uses valid result duration %s then call duration %s before timestamps', (reportedMs, callMs, expected) => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const entries = buildSessionEventEntries(
      [
        {
          id: 'write',
          type: 'agent.tool_use',
          processed_at: '2026-10-01T05:26:50.000Z',
          bracket_id: 'write',
          name: 'Write',
          input: {},
          permission_behavior: 'ask',
          duration_ms: callMs,
        },
        {
          id: 'approved',
          type: 'user.tool_confirmation',
          processed_at: '2026-10-01T05:26:52.000Z',
          tool_use_id: 'write',
          decision: 'allow',
        },
        {
          id: 'written',
          type: 'agent.tool_result',
          processed_at: '2026-10-01T05:26:52.750Z',
          tool_use_id: 'write',
          content: 'Done',
          duration_ms: reportedMs,
        },
      ],
      'transcript',
    );

    const { container } = render(transcriptTree(entries));

    expect(container.querySelector('[data-transcript-tool-row]')?.textContent).toContain(expected);
  });

  test('hides invalid tool durations when timestamps are unavailable', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const entries = buildSessionEventEntries(
      [
        { id: 'write', type: 'agent.tool_use', name: 'Write', input: {}, duration_ms: -2 },
        { id: 'written', type: 'agent.tool_result', tool_use_id: 'write', content: 'Done', duration_ms: -1 },
      ],
      'transcript',
    );

    const { container } = render(transcriptTree(entries));

    expect(container.querySelector('[data-transcript-tool-row]')?.textContent).toContain('Completed');
    expect(screen.queryByText('0ms')).toBeNull();
  });

  test('renders Markdown as an agent message grows and after the stream ends', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const answer = displayEntry('live-answer', 'agent', 'A **stale reply**', 'bracket-live');
    answer.displayEvent.isStreaming = true;
    answer.inProgress = true;
    const liveTranscript = (content: string) => (
      <SessionDetailDeltaFramesContext.Provider
        value={{
          'display-live-answer': {
            message: { type: 'agent.message', content: [{ type: 'text', text: content }] },
            frames: [],
          },
        }}
      >
        {transcriptTree([answer])}
      </SessionDetailDeltaFramesContext.Provider>
    );

    const view = render(liveTranscript('A **bold reply**'));
    expect(view.container.querySelector('[data-event-id="trace-live-answer"] strong')?.textContent).toBe('bold reply');

    view.rerender(liveTranscript('A **bold reply**\n\n- first item'));
    expect(view.container.querySelector('[data-event-id="trace-live-answer"] li')?.textContent).toBe('first item');

    const complete = displayEntry('live-answer', 'agent', 'A **bold reply**\n\n- first item', 'bracket-live');
    view.rerender(transcriptTree([complete]));
    expect(view.container.querySelector('[data-event-id="trace-live-answer"] li')?.textContent).toBe('first item');
  });

  test('renders streamed thinking Markdown and replaces it with final content', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const thinking = displayEntry('live-thinking', 'thinking', '', 'bracket-live');
    thinking.displayEvent.isStreaming = true;
    thinking.inProgress = true;
    const tree = (text: string, entry = thinking) => (
      <SessionDetailDeltaFramesContext.Provider
        value={{
          'display-live-thinking': {
            message: { type: 'agent.thinking', content: [{ type: 'thinking', thinking: text }] },
            frames: [],
          },
        }}
      >
        {transcriptTree([entry])}
      </SessionDetailDeltaFramesContext.Provider>
    );
    const view = render(tree('公开 **思考**'));
    expect(view.container.querySelector('[data-transcript-thinking-content] strong')?.textContent).toBe('思考');
    view.rerender(tree('公开 **思考**\n\n- 下一步'));
    expect(view.container.querySelector('[data-transcript-thinking-content] li')?.textContent).toBe('下一步');
    view.rerender(tree('', displayEntry('live-thinking', 'thinking', '最终思考正文', 'bracket-live')));
    const summary = view.container.querySelector('[data-transcript-thinking-row]')!;
    expect(summary.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByText('最终思考正文')).toBeNull();
    fireEvent.click(summary);
    expect(summary.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByText('最终思考正文')).toBeTruthy();
    fireEvent.click(summary);
    expect(summary.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByText('下一步')).toBeNull();
  });

  test('keeps markdown links outside buttons and ignores them when selecting a message', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const onSelectEntry = mock(() => {});
    const answer = displayEntry('answer', 'agent', 'See [the docs](https://example.com).', 'bracket-1');

    renderTranscript([answer], onSelectEntry);

    const link = screen.getByRole('link', { name: 'the docs' });
    expect(link.closest('button')).toBeNull();
    fireEvent.click(link);
    expect(onSelectEntry).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText(/See/));
    expect(onSelectEntry).toHaveBeenCalledWith('display-answer');
  });

  test('lays out user messages on the right and agent messages on the left with shadcn message primitives', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const user = displayEntry('question', 'user', 'Can you inspect this?');
    const answer = displayEntry('answer', 'agent', 'I am inspecting it.', 'bracket-1');

    const { container } = renderTranscript([user, answer], () => {});

    const userMessage = container.querySelector('[data-event-id="trace-question"]')?.closest('[data-slot="message"]');
    const agentMessage = container.querySelector('[data-event-id="trace-answer"]')?.closest('[data-slot="message"]');
    expect(userMessage?.getAttribute('data-align')).toBe('end');
    expect(agentMessage?.getAttribute('data-align')).toBe('start');
    expect(userMessage?.querySelector('[data-slot="message-content"]')?.className).toContain('max-w-[92%]');
    const userBubble = userMessage?.querySelector('[data-slot="bubble"]');
    expect(userBubble?.getAttribute('data-align')).toBe('end');
    expect(userBubble?.getAttribute('data-variant')).toBe('ghost');
    const userMessageBody = userMessage?.querySelector('[data-transcript-message-body]');
    expect(userMessageBody?.className).toContain('!rounded-[10px]');
    expect(userMessageBody?.className).toContain('!px-[11px]');
    expect(userMessageBody?.className).toContain('!py-[6px]');
    expect(userMessageBody?.className).toContain('!bg-session-speaker-user/10');
    expect(userMessageBody?.className).toContain('!border-[0.5px]');
    expect(userMessageBody?.className).not.toContain('min-h-10');
    const agentIteration = agentMessage?.querySelector('[data-transcript-iteration]');
    expect(agentIteration).toBeTruthy();
    expect(agentIteration?.className).toContain('border-[0.5px]');
    expect(agentIteration?.className).toContain('border-session-border');
    expect(agentIteration?.className).toContain('bg-session-surface');
    const userItem = userMessage?.closest('[data-slot="message-scroller-item"]');
    expect(userItem?.getAttribute('data-scroll-anchor')).toBe('false');
    expect(userItem?.parentElement?.getAttribute('data-slot')).toBe('message-scroller-content');
  });

  test('keeps unbracketed agent prose left-aligned and system boundaries full width', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const agent = displayEntry('answer', 'agent', 'Standalone response');
    const idle = idleEntry();

    const { container } = renderTranscript([agent, idle], () => {});

    const agentMessage = container.querySelector('[data-event-id="trace-answer"]')?.closest('[data-slot="message"]');
    const idleRow = container.querySelector('[data-entry-kind="idle_gap"]');
    expect(agentMessage?.getAttribute('data-align')).toBe('start');
    expect(agentMessage?.querySelector('[data-slot="bubble"]')?.getAttribute('data-align')).toBe('start');
    expect(idleRow?.closest('[data-slot="message"]')).toBeNull();
    expect(idleRow?.querySelector('[data-idle-gap-stripes]')).toBeNull();
    expect(idleRow?.querySelector('time')).toBeTruthy();
    expect(idleRow?.querySelectorAll('.border-t-\\[0\\.5px\\]')).toHaveLength(2);
  });

  test('renders one agent header, a thinking summary, and compact rows per iteration', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const thinking = displayEntry('thinking', 'thinking', 'private chain of thought', undefined, 2_000);
    const answer = displayEntry('answer', 'agent', 'Visible answer', 'bracket-1');
    const tool = toolEntry('tool', 'bracket-1');

    const { container } = renderTranscript([thinking, answer, tool], () => {});

    expect(screen.getAllByText('Researcher')).toHaveLength(1);
    expect(screen.getByText('Thought for 2.0s')).toBeTruthy();
    expect(screen.queryByText('private chain of thought')).toBeNull();
    expect(screen.getByText('Thought for 2.0s').closest('[data-slot="bubble"]')?.getAttribute('data-variant')).toBe(
      'ghost',
    );
    expect(screen.getByText('Visible answer').closest('[data-slot="bubble"]')?.getAttribute('data-variant')).toBe(
      'ghost',
    );
    expect(container.querySelectorAll('[data-transcript-iteration]')).toHaveLength(1);
    expect(container.querySelector('[data-transcript-tool-row]')?.className).toContain('h-6');
  });

  test('keeps approval lifecycle status visible without expanding compact tool rows', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const answer = displayEntry('answer', 'agent', 'I need permission.', 'bracket-1');
    const tool = toolEntry('tool', 'bracket-1', 'awaiting_approval');

    const { container } = renderTranscript([answer, tool], () => {});

    expect(screen.getAllByText('awaiting approval').length).toBeGreaterThan(0);
    expect(container.querySelector('[data-slot="collapsible-content"]')).toBeNull();
    expect(container.querySelector('[data-transcript-tool-row]')?.getAttribute('aria-expanded')).toBeNull();
  });

  test('shows a textual lifecycle for every compact tool row', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const entries = [
      toolEntry('awaiting', 'bracket-awaiting', 'awaiting_approval'),
      toolEntry('denied', 'bracket-denied', 'denied'),
      toolEntry('failed', 'bracket-failed', 'failed'),
      toolEntry('completed', 'bracket-completed', 'completed'),
      toolEntry('running', 'bracket-running', 'running'),
    ];

    const { container } = renderTranscript(entries, () => {});

    expect(toolLifecycleText(container, 'awaiting', 'awaiting_approval')).toBe('awaiting approval');
    expect(toolLifecycleText(container, 'denied', 'denied')).toBe('denied');
    expect(toolLifecycleText(container, 'failed', 'failed')).toBe('Failed');
    expect(toolLifecycleText(container, 'completed', 'completed')).toBe('Completed');
    expect(toolLifecycleText(container, 'running', 'running')).toBe('Executing');
  });

  test('opens completed tool details in the inspector without expanding the transcript', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const onSelectEntry = mock(() => {});
    const tool = toolEntry('tool', 'bracket-1');
    tool.event = {
      ...tool.event,
      type: 'agent.tool_use',
      name: 'Read',
      input: { path: '/tmp/file.ts' },
    };

    const { container } = renderTranscript([tool], onSelectEntry);
    const trigger = container.querySelector('[data-transcript-tool-row]');
    expect(trigger?.getAttribute('aria-expanded')).toBeNull();

    fireEvent.click(trigger!);

    expect(onSelectEntry).toHaveBeenCalledWith('display-tool');
    expect(screen.queryByText('Tool use')).toBeNull();
    expect(screen.getAllByText(/\/tmp\/file\.ts/)).toHaveLength(1);
  });

  test('uses the synchronized shimmer for streaming thinking', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const thinking = displayEntry('thinking-stream', 'thinking', 'private chain of thought', 'bracket-thinking');
    thinking.displayEvent.isStreaming = true;
    thinking.inProgress = true;

    const { container } = renderTranscript([thinking], () => {});

    expect(screen.getByText('Thinking…')).toBeTruthy();
    expect(container.querySelector('[data-transcript-thinking-row] [data-cds="ShimmerText"]')).toBeTruthy();
    expect(container.querySelector('[data-transcript-block="agent"]')?.hasAttribute('data-open')).toBe(true);
  });

  test('marks only open agent turns for the Claude entrance animation', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const completed = displayEntry('completed', 'agent', 'Done.', 'bracket-completed');
    const runningTool = toolEntry('running-tool', 'bracket-running', 'running');

    const { container } = renderTranscript(
      [completed, displayEntry('boundary', 'user', 'Continue.'), runningTool],
      () => {},
    );

    const turns = container.querySelectorAll('[data-transcript-block="agent"]');
    expect(turns).toHaveLength(2);
    expect(turns[0]?.hasAttribute('data-open')).toBe(false);
    expect(turns[1]?.hasAttribute('data-open')).toBe(true);
    expect(container.querySelector('[data-transcript-tool-row] [role="status"]')).toBeTruthy();
  });

  test('animates every open turn when it mounts, including after a remount', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const initial = streamingThinkingEntry('initial-live');
    const next = streamingThinkingEntry('next-live');
    const boundary = displayEntry('boundary', 'user', 'Continue.');
    const view = render(transcriptTree([initial]));

    expect(view.container.querySelector('[data-transcript-block="agent"]')?.hasAttribute('data-entering')).toBe(true);

    view.rerender(transcriptTree([initial, boundary, next]));
    expect(
      view.container
        .querySelector('[data-event-id="trace-next-live"]')
        ?.closest('[data-transcript-block="agent"]')
        ?.hasAttribute('data-entering'),
    ).toBe(true);

    view.unmount();
    const remounted = render(transcriptTree([initial, boundary, next]));
    expect(remounted.container.querySelectorAll('[data-transcript-block][data-entering]')).toHaveLength(2);
  });

  test('shows Claude generating feedback for an open model request without transcript blocks', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const openModelRequest = {
      id: 'span-open',
      type: 'span.model_request_start',
      processed_at: '2026-01-01T08:00:01.000Z',
      agent_name: 'Researcher',
    };

    const view = render(
      transcriptScrollerTree(
        <SessionTranscriptView
          entries={[]}
          openModelRequest={openModelRequest}
          traceStartMs={Date.parse('2026-01-01T08:00:00.000Z')}
          selectedEntryId={null}
          onSelectEntry={() => {}}
          threadNameById={new Map()}
          onThreadClick={() => {}}
        />,
      ),
    );

    expect(screen.getAllByText('Generating…')).toHaveLength(2);
    expect(view.container.querySelector('[data-transcript-block="agent"][data-open]')).toBeTruthy();
    expect(view.container.querySelector('[data-transcript-iteration="span-open"] [role="status"]')).toBeTruthy();
  });

  test('renders a completed thinking-only iteration in the same shadcn panel as other iterations', () => {
    resetTestDom('https://oma.duck.ai/sessions/test');
    const thinking = displayEntry('thinking', 'thinking', 'private chain of thought', 'bracket-thinking', 2_000);

    const { container } = renderTranscript([thinking], () => {});

    const iteration = container.querySelector('[data-transcript-iteration]');
    expect(iteration?.getAttribute('data-thinking-only')).toBe('true');
    expect(iteration?.className).toContain('border-[0.5px]');
    expect(iteration?.className).toContain('border-session-border');
    expect(screen.getByText('Thought for 2.0s').closest('[data-slot="bubble"]')?.getAttribute('data-variant')).toBe(
      'ghost',
    );
    expect(screen.getByText('Thought for 2.0s').closest('button')?.className).toContain('hover:bg-session-hover');

    const unknownDuration = displayEntry('thinking-unknown', 'thinking', 'private chain of thought');
    renderTranscript([unknownDuration], () => {});
    expect(screen.getByText('Thought')).toBeTruthy();
  });
});

function renderTranscript(
  entries: Array<DisplayEventEntry | ToolCallEntry>,
  onSelectEntry: (id: string | null) => void,
) {
  return render(transcriptTree(entries, onSelectEntry));
}

function transcriptTree(entries: SessionEventListEntry[], onSelectEntry: (id: string | null) => void = () => {}) {
  return transcriptScrollerTree(
    <SessionTranscriptView
      entries={entries}
      visibleEntries={entries}
      selectedEntryId={null}
      onSelectEntry={onSelectEntry}
      threadNameById={new Map()}
      onThreadClick={() => {}}
    />,
  );
}

function transcriptScrollerTree(children: ReactNode) {
  return (
    <I18nProvider initialLocale="en">
      <MessageScrollerProvider>
        <MessageScroller>
          <MessageScrollerViewport>
            <MessageScrollerContent>{children}</MessageScrollerContent>
          </MessageScrollerViewport>
        </MessageScroller>
      </MessageScrollerProvider>
    </I18nProvider>
  );
}

function streamingThinkingEntry(id: string) {
  const entry = displayEntry(id, 'thinking', 'private chain of thought', `bracket-${id}`);
  entry.displayEvent.isStreaming = true;
  entry.inProgress = true;
  return entry;
}

function displayEntry(
  id: string,
  type: 'agent' | 'thinking' | 'user',
  content: string,
  bracketId?: string,
  thinkingDurationMs?: number,
): DisplayEventEntry {
  const event = { id: `event-${id}`, session_thread_id: 'main', agent_name: 'Researcher' };
  const processedAtMs = Date.UTC(2026, 0, 1, 8);
  return {
    id,
    kind: 'message',
    displayEvent: {
      id: `display-${id}`,
      type,
      rawType: `agent.${type}`,
      label: 'Researcher',
      content,
      event,
      isStreaming: false,
      isError: false,
      createdAtMs: Date.UTC(2026, 0, 1, 8),
      processedAtMs,
      relativeTime: '0:01',
    },
    traceEntry: {
      id: `trace-${id}`,
      type: `agent.${type}`,
      family: 'agent',
      label: 'Researcher',
      preview: content,
      displayText: content,
      displayKind: type === 'thinking' ? 'thinking' : 'prose',
      event,
      createdAtMs: Date.UTC(2026, 0, 1, 8),
      relativeTime: '0:01',
      rawEventId: `event-${id}`,
      searchText: content.toLowerCase(),
      isError: false,
    },
    event,
    type: `agent.${type}`,
    rawEventId: `event-${id}`,
    createdAtMs: Date.UTC(2026, 0, 1, 8),
    processedAtMs,
    relativeTime: '0:01',
    searchText: content.toLowerCase(),
    isError: false,
    usage: EMPTY_USAGE,
    inferenceMs: 100,
    bracketId,
    bracketStartMs: thinkingDurationMs === undefined ? undefined : processedAtMs - thinkingDurationMs,
  };
}

function idleEntry(): IdleGapEntry {
  return {
    id: 'idle',
    kind: 'idle_gap',
    durationMs: 1_000,
    createdAtMs: Date.UTC(2026, 0, 1, 8),
    processedAtMs: Date.UTC(2026, 0, 1, 8, 0, 1),
    relativeTime: '0:01',
    searchText: 'idle',
    isError: false,
  };
}

function toolEntry(id: string, bracketId: string, lifecycle: ToolCallEntry['lifecycle'] = 'completed'): ToolCallEntry {
  return {
    ...displayEntry(id, 'agent', 'Read file', bracketId),
    kind: 'tool_call',
    displayEvent: {
      ...displayEntry(id, 'agent', 'Read file', bracketId).displayEvent,
      type: 'tool_use',
      label: 'Read',
    },
    name: 'Read',
    inputPreview: '/tmp/file.ts',
    lifecycle,
    bracketId,
    executionMs: 40,
  };
}

function toolLifecycleText(container: HTMLElement, id: string, lifecycle: ToolCallEntry['lifecycle']) {
  return container.querySelector(`[data-event-id="trace-${id}"] [data-tool-lifecycle="${lifecycle}"]`)?.textContent;
}

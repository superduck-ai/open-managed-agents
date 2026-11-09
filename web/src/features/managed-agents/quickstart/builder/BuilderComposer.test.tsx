import { afterEach, expect, mock, test } from 'bun:test';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { BuilderComposer } from './BuilderComposer';
import type { Builder } from './useBuilder';

afterEach(cleanup);
function setup(optimizePrompt: Builder['optimizePrompt']) {
  const onSend = mock(() => {});
  const builder = { state: {}, busy: false, draftEditing: false, optimizePrompt } as Builder;
  const view = render(<BuilderComposer builder={builder} entry onSend={onSend} />);
  const input = screen.getByRole('textbox') as HTMLTextAreaElement;
  fireEvent.change(input, { target: { value: 'original' } });
  fireEvent.click(screen.getByRole('button', { name: 'Improve description' }));
  return { input, onSend, view };
}

test('optimization errors preserve the editable original', async () => {
  const { input, onSend } = setup(async () => {
    throw new Error('offline');
  });
  await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('offline'));
  expect(input.value).toBe('original');
  expect(onSend).not.toHaveBeenCalled();
});

test('editing or canceling prevents stale output overwriting the draft and pending Enter cannot submit', async () => {
  for (const edit of [true, false]) {
    let finish!: (text: string) => void;
    let signal!: AbortSignal;
    const { input, onSend, view } = setup((_text, abort) => {
      signal = abort;
      return new Promise((resolve) => {
        finish = resolve;
      });
    });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onSend).not.toHaveBeenCalled();
    if (edit) fireEvent.change(input, { target: { value: 'edited' } });
    else fireEvent.click(screen.getByRole('button', { name: 'Cancel optimization' }));
    expect(signal.aborted).toBe(true);
    finish('stale');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Improve description' })).toBeTruthy());
    expect(input.value).toBe(edit ? 'edited' : 'original');
    view.unmount();
  }
});

test('successful optimization updates only draft and can be undone', async () => {
  const { input, onSend } = setup(async () => 'improved');
  await waitFor(() => expect(input.value).toBe('improved'));
  expect(onSend).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Undo improvement' }));
  expect(input.value).toBe('original');
});

test('leaving entry aborts the request', () => {
  let signal!: AbortSignal;
  const { view } = setup((_text, abort) => {
    signal = abort;
    return new Promise(() => {});
  });
  view.unmount();
  expect(signal.aborted).toBe(true);
});

test('empty entry accepts the visible example with Tab without sending and then allows normal Tab navigation', () => {
  const onSend = mock(() => {});
  render(<BuilderComposer builder={{ state: {}, busy: false } as Builder} entry onSend={onSend} />);
  const input = screen.getByRole('textbox') as HTMLTextAreaElement;
  expect(fireEvent.keyDown(input, { key: 'Tab' })).toBe(false);
  expect(input.value).toBe(input.placeholder);
  expect(onSend).not.toHaveBeenCalled();
  expect(fireEvent.keyDown(input, { key: 'Tab' })).toBe(true);
});

test('entry Enter sends the example while typed text takes priority', () => {
  const onSend = mock(() => {});
  render(<BuilderComposer builder={{ state: {}, busy: false } as Builder} entry onSend={onSend} />);
  const input = screen.getByRole('textbox') as HTMLTextAreaElement;
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(onSend).toHaveBeenLastCalledWith(input.placeholder);
  fireEvent.change(input, { target: { value: 'My request' } });
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(onSend).toHaveBeenLastCalledWith('My request');
});

test('Shift Tab, Shift Enter and IME composition do not accept or send the example', () => {
  const onSend = mock(() => {});
  render(<BuilderComposer builder={{ state: {}, busy: false } as Builder} entry onSend={onSend} />);
  const input = screen.getByRole('textbox') as HTMLTextAreaElement;
  expect(fireEvent.keyDown(input, { key: 'Tab', shiftKey: true })).toBe(true);
  expect(fireEvent.keyDown(input, { key: 'Enter', shiftKey: true })).toBe(true);
  fireEvent.keyDown(input, { key: 'Enter', isComposing: true });
  expect(input.value).toBe('');
  expect(onSend).not.toHaveBeenCalled();
});

test('conversation composer never sends or accepts an entry example', () => {
  const reply = mock(() => {});
  render(<BuilderComposer builder={{ state: {}, busy: false, reply } as unknown as Builder} />);
  const input = screen.getByRole('textbox') as HTMLTextAreaElement;
  expect(fireEvent.keyDown(input, { key: 'Tab' })).toBe(true);
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(input.value).toBe('');
  expect(reply).not.toHaveBeenCalled();
});

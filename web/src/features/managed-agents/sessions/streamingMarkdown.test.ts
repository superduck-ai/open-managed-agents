import { describe, expect, test } from 'bun:test';
import { streamingMarkdownSnapshot } from './streamingMarkdown';

describe('streamingMarkdownSnapshot', () => {
  test('holds an unfinished table so a missing delimiter cannot reflow earlier markdown', () => {
    const snapshot = streamingMarkdownSnapshot('## Title\n\n| Name | Value |');

    expect(snapshot).toEqual({ markdown: '## Title\n', pending: '| Name | Value |' });
  });

  test('holds a partial table row after the delimiter is already closed', () => {
    const snapshot = streamingMarkdownSnapshot('| Name | Value |\n| --- | --- |\n| one');

    expect(snapshot).toEqual({
      markdown: '| Name | Value |\n| --- | --- |',
      pending: '| one',
    });
  });

  test('holds a bare list marker and a partial fence until the line can be parsed', () => {
    expect(streamingMarkdownSnapshot('## Title\n\n-')).toEqual({ markdown: '## Title\n', pending: '-' });
    expect(streamingMarkdownSnapshot('See\n``')).toEqual({ markdown: 'See', pending: '``' });
  });

  test('renders arrived headings, lists, and an open code fence without waiting for the closer', () => {
    const snapshot = streamingMarkdownSnapshot('## Title\n\n- alpha\n- beta\n\n```ts\nconst answer = 1');

    expect(snapshot.pending).toBe('');
    expect(snapshot.markdown).toBe('## Title\n\n- alpha\n- beta\n\n```ts\nconst answer = 1\n```');
  });

  test('leaves a closed document unchanged', () => {
    const value = ['## Title', '', '- alpha', '', '```ts', 'const answer = 1', '```', ''].join('\n');

    expect(streamingMarkdownSnapshot(value)).toEqual({ markdown: value, pending: '' });
  });

  test('keeps a finished table in the markdown prefix', () => {
    const value = '| Name | Value |\n| --- | --- |\n| one | two |\n';

    expect(streamingMarkdownSnapshot(value)).toEqual({ markdown: value, pending: '' });
  });
});

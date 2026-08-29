import { afterEach, describe, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { MarkdownTranscriptContent, TranscriptContent } from './SessionTranscriptContent';

const { cleanup, render, screen } = await import('@testing-library/react');

afterEach(() => cleanup());

describe('SessionTranscriptContent', () => {
  test('renders CommonMark and GFM transcript content', () => {
    resetTestDom();
    const { container } = render(
      <MarkdownTranscriptContent
        value={[
          'A **strong reply** with ~~old text~~.',
          '',
          '> quoted answer',
          '',
          '1. first',
          '   - nested',
          '2. second',
          '',
          '- [x] shipped',
          '',
          '| Name | Value |',
          '| --- | --- |',
          '| one | two |',
          '',
          '---',
        ].join('\n')}
      />,
    );

    expect(screen.getByText('strong reply').tagName).toBe('STRONG');
    expect(screen.getByText('old text').tagName).toBe('DEL');
    expect(screen.getByText('quoted answer').closest('blockquote')).toBeTruthy();
    expect(screen.getByText('first').closest('ol')).toBeTruthy();
    expect(screen.getByText('nested').closest('ul')).toBeTruthy();
    expect((screen.getByRole('checkbox') as HTMLInputElement).disabled).toBe(true);
    expect(screen.getByRole('table')).toBeTruthy();
    expect(container.querySelector('hr')).toBeTruthy();
    expect(container.textContent).not.toContain('**');
    expect(container.textContent).not.toContain('---');
  });

  test('keeps links and code safe while ignoring raw HTML', () => {
    resetTestDom();
    const { container } = render(
      <MarkdownTranscriptContent
        value={[
          '[safe](https://example.com) [unsafe](javascript:alert)',
          '',
          '![diagram](https://example.com/diagram.png)',
          '',
          '```json',
          '{"ok":true}',
          '```',
          '',
          '<span>raw html</span>',
        ].join('\n')}
      />,
    );

    expect(screen.getByRole('link', { name: 'safe' }).getAttribute('href')).toBe('https://example.com');
    expect(screen.getByText('unsafe').closest('a')?.hasAttribute('href')).toBe(false);
    expect(screen.getByRole('img', { name: 'diagram' }).getAttribute('loading')).toBe('lazy');
    expect(
      screen.getByTestId('session-trace-code-block').querySelector('code')?.classList.contains('language-json'),
    ).toBe(true);
    expect(screen.getByText('raw html').tagName).not.toBe('SPAN');
    expect(container.querySelector('script')).toBeNull();
  });

  test('limits long transcript text and clearly reports how much is shown', () => {
    resetTestDom();
    const { container } = render(<MarkdownTranscriptContent value={`${'a'.repeat(50_000)}THE_END`} />);

    expect(container.textContent).not.toContain('THE_END');
    expect(screen.getByText('Truncated — showing the first 50,000 of 50,007 characters.')).toBeTruthy();
  });

  test('applies the same limit before rendering a fenced code transcript', () => {
    resetTestDom();
    const { container } = render(<TranscriptContent value={`\`\`\`text\n${'a'.repeat(50_000)}THE_END\n\`\`\``} />);

    expect(container.textContent).not.toContain('THE_END');
    expect(screen.getByTestId('session-trace-code-block')).toBeTruthy();
    expect(screen.getByText('Truncated — showing the first 50,000 of 50,019 characters.')).toBeTruthy();
  });
});

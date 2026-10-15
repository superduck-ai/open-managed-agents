import { SessionFileContext } from './SessionFileContext';
import { I18nProvider } from '../../../shared/i18n';
import { afterEach, describe, expect, mock, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { MarkdownTranscriptContent, TranscriptContent } from './SessionTranscriptContent';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');

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

  test('keeps loose Markdown lists compact', () => {
    resetTestDom();
    render(
      <MarkdownTranscriptContent
        value={[
          '- **Three traits:**',
          '',
          '  - Encapsulation',
          '',
          '  - Inheritance',
          '',
          '  - Polymorphism',
          '',
          '- **Access modifiers:** public, private, protected',
        ].join('\n')}
      />,
    );

    const markdown = screen.getByTestId('session-trace-markdown');
    expect(markdown.classList.contains('gap-1.5')).toBe(true);
    expect(markdown.classList.contains('whitespace-normal')).toBe(true);
    expect([...markdown.querySelectorAll('li')].every((item) => !item.classList.contains('my-1'))).toBe(true);
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

test('downloads path links through Files API with the current workspace and preserves the filename', async () => {
  resetTestDom('https://oma.duck.ai/');
  const originalFetch = globalThis.fetch;
  const originalCreate = URL.createObjectURL;
  const originalClick = window.HTMLAnchorElement.prototype.click;
  const requests: Array<{ url: string; workspace: string | null }> = [];
  const clicks: string[] = [];
  try {
    globalThis.fetch = async (input, init) => {
      requests.push({ url: String(input), workspace: new Headers(init?.headers).get('x-workspace-id') });
      return new Response('one joke');
    };
    URL.createObjectURL = mock(() => 'blob:file');
    window.HTMLAnchorElement.prototype.click = function () {
      clicks.push(this.download);
    };
    render(
      <I18nProvider initialLocale="en">
        <SessionFileContext.Provider
          value={{
            workspaceId: 'workspace-links',
            running: false,
            files: [
              {
                id: 'joke-file',
                filename: '笑话.txt',
                path: '/outputs/one/笑话.txt',
                type: 'file',
                mime_type: 'text/plain',
                size_bytes: 8,
                downloadable: true,
                created_at: '2026-10-15T00:00:00Z',
              },
            ],
          }}
        >
          <MarkdownTranscriptContent value="[笑话.txt](sandbox:/mnt/user-data/outputs/one/%E7%AC%91%E8%AF%9D.txt)" />
        </SessionFileContext.Provider>
      </I18nProvider>,
    );
    fireEvent.click(screen.getByRole('link', { name: '笑话.txt' }));
    await waitFor(() => expect(clicks).toEqual(['笑话.txt']));
    expect(requests[0].url).toContain('/v1/files/joke-file/content?beta=true');
    expect(requests[0].workspace).toBe('workspace-links');
  } finally {
    globalThis.fetch = originalFetch;
    URL.createObjectURL = originalCreate;
    window.HTMLAnchorElement.prototype.click = originalClick;
  }
});

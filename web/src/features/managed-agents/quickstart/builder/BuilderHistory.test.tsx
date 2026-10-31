import { afterEach, expect, test } from 'bun:test';
import { I18nProvider } from '../../../../shared/i18n';
import { render, screen, resetTestDom, resetManagedAgentsTestState } from '../../ManagedAgentsPage.test-utils';
import { BuilderHistory } from './BuilderHistory';

afterEach(resetManagedAgentsTestState);

test('resource results preserve historical names and versions while answers remain user messages', () => {
  resetTestDom();
  render(
    <I18nProvider initialLocale="en">
      <BuilderHistory
        messages={[
          { role: 'user', content: 'Create a research agent.' },
          {
            role: 'user',
            content: [
              {
                type: 'tool_result',
                content: JSON.stringify({ saved: true, agent: { id: 'agent_one', name: 'Research', version: 1 } }),
              },
            ],
          },
          { role: 'assistant', content: [{ type: 'text', text: '**Which sources?**' }] },
          {
            role: 'user',
            content: [{ type: 'tool_result', content: JSON.stringify([{ answers: ['Official documentation'] }]) }],
          },
          {
            role: 'user',
            content: [
              {
                type: 'tool_result',
                content: JSON.stringify({ saved: true, agent: { id: 'agent_one', name: 'Sources', version: 2 } }),
              },
            ],
          },
        ]}
      />
    </I18nProvider>,
  );
  const saved = screen.getAllByRole('group', { name: 'Saved result' });
  expect(saved[0].textContent).toContain('Research');
  expect(saved[0].textContent).toContain('v1');
  expect(saved[1].textContent).toContain('Sources');
  expect(saved[1].textContent).toContain('v2');
  expect(screen.getAllByRole('article', { name: 'User message' })).toHaveLength(2);
  expect(screen.getByRole('article', { name: 'Assistant message' }).querySelector('strong')?.textContent).toBe(
    'Which sources?',
  );
  expect(screen.getByText('Official documentation').closest('article')?.getAttribute('aria-label')).toBe(
    'User message',
  );
});

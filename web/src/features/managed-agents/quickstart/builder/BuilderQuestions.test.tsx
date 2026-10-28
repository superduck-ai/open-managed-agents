import { afterEach, expect, test } from 'bun:test';
import { I18nProvider } from '../../../../shared/i18n';
import {
  fireEvent,
  render,
  screen,
  resetTestDom,
  resetManagedAgentsTestState,
} from '../../ManagedAgentsPage.test-utils';
import { BuilderQuestions } from './BuilderQuestions';
import { builderSystem } from './prompt';
import { newBuilderState } from './model';
import { blankAgentTemplate, createDialogAgentConfig } from '../../agentConfig';
import { quickstartBuilderBehavior } from '../quickstartPromptText';

afterEach(resetManagedAgentsTestState);
const tool = {
  id: 'questions',
  name: 'ask_user_questions',
  input: {
    questions: [
      {
        header: 'Network',
        question: 'Which network?',
        options: [{ label: 'Limited', description: 'Only required hosts' }, { label: 'Unrestricted' }],
      },
      {
        header: 'Hosts',
        question: 'Which hosts?',
        multi_select: true,
        options: [{ label: 'Docs' }, { label: 'Code' }],
      },
    ],
  },
};
function show(disabled = false) {
  resetTestDom();
  const answers: unknown[] = [];
  render(
    <I18nProvider initialLocale="en">
      <BuilderQuestions tool={tool} disabled={disabled} onAnswer={(answer) => answers.push(answer)} />
    </I18nProvider>,
  );
  return answers;
}
test('legacy questionnaire prevents advancing empty questions and respects disabled state', () => {
  show(true);
  expect(screen.getByRole('button', { name: 'Next', exact: true }).hasAttribute('disabled')).toBe(true);
  expect(screen.getByRole('button', { name: 'Skip', exact: true }).closest('fieldset')?.disabled).toBe(true);
});
test('legacy questionnaire presents questions sequentially and preserves answers across navigation', () => {
  const answers = show();
  expect(screen.getByText('Only required hosts')).toBeTruthy();
  fireEvent.click(screen.getByRole('radio', { name: /Limited/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Next', exact: true }));
  fireEvent.click(screen.getByRole('checkbox', { name: /Docs/ }));
  fireEvent.click(screen.getByRole('checkbox', { name: /Code/ }));
  fireEvent.change(screen.getByRole('textbox', { name: 'Something else' }), { target: { value: 'Custom host' } });
  fireEvent.click(screen.getByRole('button', { name: 'Previous', exact: true }));
  expect((screen.getByRole('radio', { name: /Limited/ }) as HTMLInputElement).checked).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Next', exact: true }));
  fireEvent.click(screen.getByRole('button', { name: 'Confirm', exact: true }));
  expect(answers).toEqual([
    [
      { header: 'Network', question: 'Which network?', answers: ['Limited'] },
      { header: 'Hosts', question: 'Which hosts?', answers: ['Docs', 'Code', 'Custom host'] },
    ],
  ]);
});
test('legacy questionnaire skip resolves the waiting tool with an empty answer', () => {
  const answers = show();
  fireEvent.click(screen.getByRole('button', { name: 'Skip', exact: true }));
  expect(answers).toEqual([[]]);
});
test('builder uses the existing localized flow with actual catalog and current runtime contract', () => {
  const state = newBuilderState(createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'workspace-model'));
  for (const locale of ['en', 'zh-CN'] as const) {
    const system = builderSystem(state, locale, ['workspace-model'], [], []);
    expect(system[0].text).toBe(quickstartBuilderBehavior(locale));
    expect(system[0].text).not.toContain('https://mcp.notion.com/mcp');
    expect(system[0].text).not.toContain('vault_sharing_notice');
    expect(system[0].text).not.toContain('claude-sonnet-4-6');
    expect(system[1].text).toContain('take precedence');
    expect(system[2].text).toContain('workspace-model');
  }
});

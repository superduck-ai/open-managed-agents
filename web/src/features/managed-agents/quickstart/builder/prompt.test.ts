import { expect, test } from 'bun:test';
import { blankAgentTemplate, createDialogAgentConfig } from '../../agentConfig';
import { newBuilderState } from './model';
import { builderSystem } from './prompt';
import { builderTools } from './tools';

for (const locale of ['en', 'zh-CN'] as const) {
  test(`builder removes saved-editor assumptions for an unsaved draft (${locale})`, () => {
    const state = newBuilderState(createDialogAgentConfig(blankAgentTemplate, locale, undefined, 'model'));
    const system = builderSystem(state, locale, ['model'], [], []);
    const text = system.map((block) => block.text).join('\n');
    expect(text).not.toContain('The user has already CREATED');
    expect(text).not.toContain('already live in the editor');
    expect(text).not.toContain('用户已经创建了 agent');
    expect(text).not.toContain('上下文中显示的配置已经在编辑器里生效');
    expect(text).toContain('Builder conversation history is not available to the saved agent');
    expect(text).toContain('Do not substitute source material into suggested_first_message');
  });
}
test('configuration tool describes a proposal rather than a completed save', () => {
  const tool = builderTools(['model']).find((tool) => tool.name === 'build_agent_config');
  expect(tool?.description).toContain('user confirmation');
  expect(tool?.description).toContain('source material');
});

test('builder preserves explicit scope restrictions without inventing optional exceptions', () => {
  const state = newBuilderState(createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'model'));
  const text = builderSystem(state, 'en', ['model'], [], [])
    .map((block) => block.text)
    .join('\n');
  expect(text).toContain("user's own wording in a separate User constraints section");
  expect(text).toContain('Do not weaken exclusions');
  expect(text).toContain('optional, secondary or pending-confirmation exceptions');
});

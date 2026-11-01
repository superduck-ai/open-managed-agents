import { expect, test } from 'bun:test';
import { quickstartAgentBody, quickstartDraft } from './model';
import { quickstartTemplates } from './templates';

for (const locale of ['en', 'zh-CN'] as const) {
  test(`review template limits findings to evidenced failures within the supplied contract (${locale})`, () => {
    const config = quickstartAgentBody(quickstartDraft('review', locale, 'workspace-model'));
    const template = quickstartTemplates(locale, 'workspace-model').find((item) => item.id === 'review');
    expect(config.system).toMatch(locale === 'en' ? /current code/ : /当前代码/);
    expect(config.system).toMatch(locale === 'en' ? /stated contract/ : /已给定的契约/);
    expect(config.system).toMatch(locale === 'en' ? /hypothetical future/ : /假设的未来/);
    expect(config.system).toMatch(locale === 'en' ? /entire response/ : /不进入回答/);
    expect(template?.config.system).toBe(config.system);
    expect(config.tools).toEqual([{ type: 'agent_toolset_20260401' }]);
  });
}

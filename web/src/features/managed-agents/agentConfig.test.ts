import { describe, expect, test } from 'bun:test';
import {
  agentTemplates,
  blankAgentTemplate,
  createDialogAgentConfig,
  createDialogTemplateConfigs,
  createDialogTemplateConfigsZh,
  jsonForTemplate,
  quickstartBuildAgentConfigInput,
  templateSystem,
  yamlForTemplate,
} from './agentConfig';

describe('localized create-agent template configs', () => {
  test('keeps the English and Chinese template catalogs structurally aligned', () => {
    expect(Object.keys(createDialogTemplateConfigsZh).sort()).toEqual(Object.keys(createDialogTemplateConfigs).sort());

    for (const [id, englishConfig] of Object.entries(createDialogTemplateConfigs)) {
      const chineseConfig = createDialogTemplateConfigsZh[id];

      expect(chineseConfig).toBeTruthy();
      expect(chineseConfig.name).not.toBe(englishConfig.name);
      expect(chineseConfig.description).not.toBe(englishConfig.description);
      expect(chineseConfig.system).not.toBe(englishConfig.system);
      expect({
        model: chineseConfig.model,
        mcp_servers: chineseConfig.mcp_servers,
        tools: chineseConfig.tools,
        skills: chineseConfig.skills,
        metadata: chineseConfig.metadata,
      }).toEqual({
        model: englishConfig.model,
        mcp_servers: englishConfig.mcp_servers,
        tools: englishConfig.tools,
        skills: englishConfig.skills,
        metadata: englishConfig.metadata,
      });
    }
  });

  test('uses locale as the second argument and defaults to English', () => {
    expect(createDialogAgentConfig(blankAgentTemplate)).toEqual(createDialogTemplateConfigs.blank);
    expect(createDialogAgentConfig(blankAgentTemplate, 'zh-CN')).toEqual(createDialogTemplateConfigsZh.blank);
  });

  test('uses the configured effective model id in template configs', () => {
    const config = createDialogAgentConfig(blankAgentTemplate, 'en', undefined, {
      'claude-sonnet-4-6': 'glm-5-turbo',
    });

    expect(config.model).toBe('glm-5-turbo');
  });

  test('uses the configured effective model id in template previews', () => {
    const mappings = {
      'claude-sonnet-4-6': 'glm-5-turbo',
    };

    expect(yamlForTemplate(blankAgentTemplate, 'en', mappings)).toContain('model: glm-5-turbo');
    expect(jsonForTemplate(blankAgentTemplate, 'en', mappings)).toContain('"model": "glm-5-turbo"');
  });

  test('uses the configured effective model id in generated agent configs', () => {
    const fallback = createDialogAgentConfig(blankAgentTemplate);
    const mappings = { 'claude-sonnet-4-6': 'glm-5-turbo' };

    expect(quickstartBuildAgentConfigInput({ model: 'claude-sonnet-4-6' }, fallback, mappings).model).toBe(
      'glm-5-turbo',
    );
    expect(
      quickstartBuildAgentConfigInput({ model: { id: 'claude-sonnet-4-6', speed: 'fast' } }, fallback, mappings).model,
    ).toEqual({ id: 'glm-5-turbo', speed: 'fast' });
  });

  test('uses the localized config table as the system prompt source for every built-in template', () => {
    for (const template of agentTemplates) {
      const englishSystem = createDialogTemplateConfigs[template.id]?.system;
      const chineseSystem = createDialogTemplateConfigsZh[template.id]?.system;

      expect(typeof englishSystem).toBe('string');
      expect(typeof chineseSystem).toBe('string');
      expect(templateSystem(template)).toBe(englishSystem);
      expect(templateSystem(template, 'zh-CN')).toBe(chineseSystem);
    }
  });

  test('keeps the generic system prompt fallback for templates outside the built-in config tables', () => {
    const customTemplate = {
      id: 'custom-template',
      slug: 'custom-template',
      title: 'Custom template',
      body: 'A custom template.',
      prompt: 'Handle this custom workflow.',
    };

    expect(templateSystem(customTemplate)).toBe(
      'Handle this custom workflow. Keep outputs concise, cite tool results when relevant, and ask for clarification before taking irreversible action.',
    );
    expect(templateSystem(customTemplate, 'zh-CN')).toBe(
      'Handle this custom workflow. 输出保持简洁；相关时引用工具结果；不可逆操作前先确认。',
    );

    expect(createDialogAgentConfig(customTemplate).system).toBe(templateSystem(customTemplate));
    expect(createDialogAgentConfig(customTemplate, 'zh-CN').system).toBe(templateSystem(customTemplate, 'zh-CN'));
  });

  test('applies a description override after selecting the localized template config', () => {
    const config = createDialogAgentConfig(blankAgentTemplate, 'zh-CN', '  自定义描述  ');

    expect(config.name).toBe(createDialogTemplateConfigsZh.blank.name);
    expect(config.description).toBe('自定义描述');
    expect(config.system).toBe(createDialogTemplateConfigsZh.blank.system);
    expect(config.metadata).toEqual({ source: 'description' });
    expect(createDialogTemplateConfigsZh.blank.metadata).toBeUndefined();
  });
});

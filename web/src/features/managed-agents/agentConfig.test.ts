import { describe, expect, test } from 'bun:test';
import {
  agentTemplates,
  blankAgentTemplate,
  createDialogAgentConfig,
  createDialogTemplateConfigs,
  createDialogTemplateConfigsZh,
  jsonForTemplate,
  quickstartBuildAgentConfigInput,
  resolveAgentModelInput,
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
    expect(createDialogAgentConfig(blankAgentTemplate)).toEqual({ ...createDialogTemplateConfigs.blank, model: '' });
    expect(createDialogAgentConfig(blankAgentTemplate, 'zh-CN')).toEqual({
      ...createDialogTemplateConfigsZh.blank,
      model: '',
    });
    expect(createDialogAgentConfig(blankAgentTemplate, 'en', null, 'gateway/agent-model').model).toBe(
      'gateway/agent-model',
    );
  });

  test('uses the configured effective model id in generated agent configs', () => {
    const fallback = createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'glm-5-turbo');
    const mappings = { 'claude-sonnet-4-6': 'glm-5-turbo' };

    expect(
      quickstartBuildAgentConfigInput({ model: 'claude-sonnet-4-6' }, fallback, ['glm-5-turbo'], mappings).model,
    ).toBe('glm-5-turbo');
    expect(
      quickstartBuildAgentConfigInput(
        { model: { id: 'claude-sonnet-4-6', speed: 'fast', effort: { type: 'high' } } },
        fallback,
        ['glm-5-turbo'],
        mappings,
      ).model,
    ).toEqual({ id: 'glm-5-turbo', speed: 'fast', effort: { type: 'high' } });
  });

  test('keeps the selected fallback when the model catalog is empty', () => {
    const fallback = createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'provider/fallback');

    expect(quickstartBuildAgentConfigInput({ model: 'provider/generated' }, fallback, []).model).toBe(
      'provider/fallback',
    );
  });

  test('distinguishes an omitted generated multiagent from an explicit null', () => {
    const fallback = {
      ...createDialogAgentConfig(blankAgentTemplate),
      multiagent: {
        type: 'coordinator' as const,
        agents: [{ type: 'agent' as const, id: 'agent_helper', version: 3 }],
      },
    };

    expect(quickstartBuildAgentConfigInput({}, fallback).multiagent).toEqual(fallback.multiagent);
    expect(quickstartBuildAgentConfigInput({ multiagent: null }, fallback).multiagent).toBeUndefined();
  });

  test('trims mapped and unmapped model ids at the configuration boundary', () => {
    const mappings = { 'claude-sonnet-4-6': ' glm-5-turbo ' };

    expect(resolveAgentModelInput(' claude-sonnet-4-6 ', mappings)).toBe('glm-5-turbo');
    expect(resolveAgentModelInput(' glm-5 ', mappings)).toBe('glm-5');
    expect(resolveAgentModelInput({ id: ' claude-sonnet-4-6 ', speed: 'fast', effort: 'high' }, mappings)).toEqual({
      id: 'glm-5-turbo',
      speed: 'fast',
      effort: 'high',
    });
    expect(resolveAgentModelInput({ id: ' glm-5 ', speed: 'standard' }, mappings)).toEqual({
      id: 'glm-5',
      speed: 'standard',
    });
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

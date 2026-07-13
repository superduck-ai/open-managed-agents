import { describe, expect, test } from 'bun:test';
import {
  blankAgentTemplate,
  createDialogAgentConfig,
  createDialogTemplateConfigs,
  createDialogTemplateConfigsZh,
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

  test('applies a description override after selecting the localized template config', () => {
    const config = createDialogAgentConfig(blankAgentTemplate, 'zh-CN', '  自定义描述  ');

    expect(config.name).toBe(createDialogTemplateConfigsZh.blank.name);
    expect(config.description).toBe('自定义描述');
    expect(config.system).toBe(createDialogTemplateConfigsZh.blank.system);
    expect(config.metadata).toEqual({ source: 'description' });
    expect(createDialogTemplateConfigsZh.blank.metadata).toBeUndefined();
  });
});

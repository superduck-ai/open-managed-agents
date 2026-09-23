import { describe, expect, test } from 'bun:test';
import { type AgentEditConfig } from '../types';
import { renderedAgentEditDraft } from './use-agent-edit-draft';

const editConfig: AgentEditConfig = {
  name: 'Coordinator',
  description: null,
  model: { id: 'claude-sonnet-4-6', speed: 'fast' },
  system: null,
  mcp_servers: [],
  tools: [{ type: 'agent_toolset_20260401' }],
  skills: [{ type: 'custom', skill_id: 'skill_release', version: '3' }],
  metadata: { team: 'release' },
  multiagent: {
    type: 'coordinator',
    agents: [{ type: 'self' }, { type: 'agent', id: 'agent_worker', version: 7 }],
  },
};

describe('agent edit rendered compatibility', () => {
  test('keeps duplicate toolsets out of Rendered mode', () => {
    const result = renderedAgentEditDraft({
      ...editConfig,
      tools: [{ type: 'agent_toolset_20260401' }, { type: 'agent_toolset_20260401' }],
    });

    expect(result.ok).toBe(false);
    expect(result.error).toContain('Continue in Raw');
  });

  test('keeps valid but unsupported legacy tool shapes out of Rendered mode', () => {
    const result = renderedAgentEditDraft({
      ...editConfig,
      tools: [{ type: 'future_toolset_20269999', config: { preserve: true } }],
    });

    expect(result.ok).toBe(false);
    expect(result.error).toContain('Continue in Raw');
  });

  test('preserves supported pinned references and model modifiers', () => {
    const result = renderedAgentEditDraft(editConfig);

    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.draft.model).toEqual({ id: 'claude-sonnet-4-6', speed: 'fast' });
      expect(result.draft.multiagent).toEqual(editConfig.multiagent);
      expect(result.draft.skills).toEqual(editConfig.skills);
    }
  });
});

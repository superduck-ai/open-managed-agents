import { expect, test } from 'bun:test';
import { createAgentConfigText, parseCreateAgentConfigText } from '../agentConfig';
import type { AgentDelegationInput, CreateAgentInput } from '../types';
import { multiagentSchema, selectedSubagentReferences, subagentSettings, updateSubagents } from './subagents-model';

const draft: CreateAgentInput = { name: 'Agent', model: 'test-model', tools: [], skills: [], mcp_servers: [] };

test('rejects predefined-only mode without agents and duplicate references', () => {
  const subagents = { type: 'enabled', inline_agents: { type: 'disabled' }, predefined_agents: [] };
  expect(multiagentSchema.safeParse({ type: 'multiagent_20261001', subagents }).success).toBe(false);
  const reference = { type: 'agent', id: 'agent_child', version: 2 };
  expect(
    multiagentSchema.safeParse({
      type: 'multiagent_20261001',
      subagents: { ...subagents, predefined_agents: [reference, reference] },
    }).success,
  ).toBe(false);
});

test('round trips enabled and disabled subagents through both Raw formats', () => {
  const variants: AgentDelegationInput[] = [
    { type: 'disabled' },
    { type: 'enabled', inline_agents: { type: 'enabled' }, predefined_agents: [] },
  ];
  for (const settings of variants) {
    const input = updateSubagents(draft, settings);
    for (const format of ['JSON', 'YAML'] as const) {
      const result = parseCreateAgentConfigText(createAgentConfigText(input, format), format);
      expect(result.ok).toBe(true);
      if (result.ok) expect(result.input.multiagent).toEqual(input.multiagent);
    }
  }
});

test('preserves legacy references and hidden settings when editing subagents', () => {
  const reference = { type: 'agent' as const, id: 'agent_child', version: 2 };
  const legacy: CreateAgentInput = { ...draft, multiagent: { type: 'coordinator', agents: [reference] } };
  expect(subagentSettings(legacy)).toEqual({
    type: 'enabled',
    inline_agents: { type: 'disabled' },
    predefined_agents: [reference],
  });
  const migrated = updateSubagents(legacy, subagentSettings(legacy));
  expect(selectedSubagentReferences(migrated)).toEqual([reference]);
  const stored: CreateAgentInput = {
    ...draft,
    multiagent: {
      type: 'multiagent_20261001',
      workflows: { type: 'enabled' },
      advisor: { type: 'enabled', model: 'advisor-model' },
    },
  };
  expect(updateSubagents(stored, { type: 'disabled' }).multiagent).toMatchObject({
    workflows: { type: 'enabled' },
    advisor: { type: 'enabled', model: 'advisor-model' },
    subagents: { type: 'disabled' },
  });
  expect(selectedSubagentReferences(updateSubagents(migrated, { type: 'disabled' }))).toEqual([]);
});

import { expect, test } from 'bun:test';
import { createAgentConfigText, parseCreateAgentConfigText } from '../agentConfig';
import type { AgentDelegationInput, CreateAgentInput } from '../types';
import { normalizeCreateAgentDraft } from './create-dialog-model';
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

test('clears multiagent after disabling delegation for new, legacy and previously disabled agents', () => {
  const enabled = updateSubagents(draft, { type: 'enabled', predefined_agents: [] });
  const legacy: CreateAgentInput = {
    ...draft,
    multiagent: { type: 'coordinator', agents: [{ type: 'agent', id: 'agent_child', version: 2 }] },
  };
  const disabled: CreateAgentInput = {
    ...draft,
    multiagent: {
      type: 'multiagent_20261001',
      subagents: { type: 'disabled' },
      workflows: { type: 'disabled' },
      advisor: { type: 'disabled' },
    },
  };
  for (const input of [draft, enabled, legacy, disabled]) {
    const updated = updateSubagents(input, { type: 'disabled' });
    expect(updated.multiagent).toBeNull();
    expect(normalizeCreateAgentDraft(updated).multiagent).toBeNull();
    expect(subagentSettings(updated)).toEqual({ type: 'disabled' });
    expect(selectedSubagentReferences(updated)).toEqual([]);
  }
});

test('keeps hidden capabilities and their defaults when disabling subagents', () => {
  for (const config of [
    { workflows: { type: 'enabled' as const } },
    { workflows: { type: 'disabled' as const }, advisor: { type: 'enabled' as const, model: 'advisor' } },
    {},
  ]) {
    const multiagent = { type: 'multiagent_20261001' as const, ...config };
    expect(updateSubagents({ ...draft, multiagent }, { type: 'disabled' }).multiagent).toEqual({
      ...multiagent,
      subagents: { type: 'disabled' },
    });
  }
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

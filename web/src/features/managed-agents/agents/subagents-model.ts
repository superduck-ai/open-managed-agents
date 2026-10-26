import { z } from 'zod';
import type { AgentApiResponse, AgentDelegationInput, CreateAgentInput, SubagentReference } from '../types';

const referenceSchema = z.union([
  z.object({ type: z.literal('agent'), id: z.string().trim().min(1), version: z.number().int().min(1) }).strict(),
  z.object({ type: z.literal('self') }).strict(),
]);

const referencesSchema = z
  .array(referenceSchema)
  .max(20)
  .superRefine((references, context) => {
    const ids = references.map((reference) => (reference.type === 'self' ? 'self' : reference.id));
    if (new Set(ids).size !== ids.length) {
      context.addIssue({ code: 'custom', message: 'Subagents must be unique.' });
    }
  });

const delegationSchema = z.discriminatedUnion('type', [
  z.object({ type: z.literal('disabled') }).strict(),
  z
    .object({
      type: z.literal('enabled'),
      inline_agents: z
        .object({ type: z.enum(['enabled', 'disabled']) })
        .strict()
        .optional(),
      predefined_agents: referencesSchema.optional(),
    })
    .strict()
    .superRefine((value, context) => {
      if (value.inline_agents?.type === 'disabled' && !value.predefined_agents?.length) {
        context.addIssue({ code: 'custom', message: 'Add a saved agent, or let it define its own.' });
      }
    }),
]);

export const multiagentSchema = z.union([
  z.object({ type: z.literal('coordinator'), agents: referencesSchema.refine((value) => value.length > 0) }).strict(),
  z
    .object({
      type: z.literal('multiagent_20261001'),
      subagents: delegationSchema.optional(),
      workflows: delegationSchema.optional(),
      advisor: z
        .discriminatedUnion('type', [
          z.object({ type: z.literal('disabled') }).strict(),
          z.object({ type: z.literal('enabled'), model: z.string().min(1) }).strict(),
        ])
        .optional(),
    })
    .strict(),
]);

export function subagentSettings(draft: CreateAgentInput): AgentDelegationInput {
  const config = draft.multiagent;
  if (!config) return { type: 'disabled' };
  if (config.type === 'coordinator') {
    return { type: 'enabled', inline_agents: { type: 'disabled' }, predefined_agents: config.agents };
  }
  return config.subagents ?? { type: 'enabled' };
}

export function selectedSubagentReferences(draft: CreateAgentInput): SubagentReference[] {
  const settings = subagentSettings(draft);
  return settings.type === 'enabled' ? (settings.predefined_agents ?? []) : [];
}

export function updateSubagents(draft: CreateAgentInput, settings: AgentDelegationInput): CreateAgentInput {
  const config =
    draft.multiagent?.type === 'multiagent_20261001'
      ? draft.multiagent
      : {
          type: 'multiagent_20261001' as const,
          workflows: { type: 'disabled' as const },
          advisor: { type: 'disabled' as const },
        };
  const multiagent = { ...config, subagents: settings };
  const disabled =
    settings.type === 'disabled' && config.workflows?.type === 'disabled' && config.advisor?.type !== 'enabled';
  return { ...draft, multiagent: disabled ? null : multiagent };
}

export function toggleSubagent(draft: CreateAgentInput, agent: AgentApiResponse): CreateAgentInput {
  const current = selectedSubagentReferences(draft);
  const exists = current.some((reference) => reference.type === 'agent' && reference.id === agent.id);
  if (!exists && current.length >= 20) return draft;
  const agents = exists
    ? current.filter((reference) => reference.type !== 'agent' || reference.id !== agent.id)
    : [...current, { type: 'agent' as const, id: agent.id, version: agent.version }];
  const settings = subagentSettings(draft);
  return updateSubagents(draft, {
    ...(settings.type === 'enabled' ? settings : { type: 'enabled', inline_agents: { type: 'enabled' } }),
    predefined_agents: agents,
  });
}

import { z } from 'zod';
import {
  type AgentApiResponse,
  type AgentModelInput,
  type AgentMultiagentInput,
  type AgentSkillInput,
  type CreateAgentInput,
} from '../types';
import { cloneJsonValue, toRecord } from '../utils';
import {
  BUILT_IN_AGENT_TOOLSETS,
  aggregateToolPermissions,
  effectiveToolPermission,
  type ToolPermissionState,
} from './tools/model';

export type CreateAgentView = 'rendered' | 'raw';
export type EditablePermission = Exclude<ToolPermissionState, 'custom'>;

export type AgentModelOption = {
  id: string;
  displayName: string;
};

export type AgentSkillOption = {
  id: string;
  displayTitle: string;
  latestVersion: string;
  source: 'anthropic' | 'custom';
};

export type McpServerInput = {
  name: string;
  url: string;
};

export type McpServerInputErrors = {
  name?: 'required' | 'too_long' | 'duplicate';
  url?: 'required' | 'too_long' | 'invalid';
  form?: 'limit';
};

export type AddMcpServerResult = { ok: true; draft: CreateAgentInput } | { ok: false; errors: McpServerInputErrors };

const modelSchema = z.union([
  z.string().trim().min(1, 'Model is required.'),
  z
    .object({
      id: z.string().trim().min(1, 'Model id is required.'),
      speed: z.enum(['standard', 'fast']).optional(),
    })
    .strict(),
]);

const agentReferenceSchema = z.union([
  z
    .object({
      type: z.literal('agent'),
      id: z.string().trim().min(1),
      version: z.number().int().min(1),
    })
    .strict(),
  z.object({ type: z.literal('self') }).strict(),
]);

const multiagentSchema = z
  .object({
    type: z.literal('coordinator'),
    agents: z.array(agentReferenceSchema).min(1).max(20),
  })
  .strict()
  .superRefine((value, context) => {
    const ids = value.agents.filter((agent) => agent.type === 'agent').map((agent) => agent.id);
    if (new Set(ids).size !== ids.length) {
      context.addIssue({ code: 'custom', message: 'Subagents must be unique.', path: ['agents'] });
    }
    if (value.agents.filter((agent) => agent.type === 'self').length > 1) {
      context.addIssue({ code: 'custom', message: 'Self may only be referenced once.', path: ['agents'] });
    }
  });

const skillSchema = z
  .object({
    type: z.enum(['anthropic', 'custom']),
    skill_id: z.string().trim().min(1),
    version: z.string().trim().min(1).optional(),
  })
  .strict();

const mcpServerSchema = z
  .object({
    name: z.string().trim().min(1).max(255),
    type: z.literal('url'),
    url: z.string().trim().max(2048).refine(isHTTPURL, 'MCP server URL must be a safe HTTP/HTTPS URL.'),
  })
  .strict();

const permissionPolicySchema = z.object({ type: z.enum(['always_allow', 'always_ask']) }).strict();

const permissionConfigSchema = z
  .object({
    enabled: z.boolean().optional(),
    permission_policy: permissionPolicySchema.optional(),
  })
  .strict();

const builtInToolNameSchema = z.enum([
  'task',
  'ask_user_question',
  'bash',
  'cron_create',
  'cron_delete',
  'cron_list',
  'edit',
  'enter_plan_mode',
  'enter_worktree',
  'exit_plan_mode',
  'exit_worktree',
  'glob',
  'grep',
  'notebook_edit',
  'read',
  'schedule_wakeup',
  'skill',
  'task_output',
  'task_stop',
  'todo_write',
  'web_fetch',
  'write',
]);

const builtInToolConfigSchema = permissionConfigSchema.extend({ name: builtInToolNameSchema }).strict();

const mcpToolConfigSchema = permissionConfigSchema.extend({ name: z.string().trim().min(1).max(128) }).strict();

const builtInToolsetSchema = z
  .object({
    type: z.literal('agent_toolset_20260401'),
    default_config: permissionConfigSchema.optional(),
    configs: z.array(builtInToolConfigSchema).optional(),
  })
  .strict();

const mcpToolsetSchema = z
  .object({
    type: z.literal('mcp_toolset'),
    mcp_server_name: z.string().trim().min(1).max(255),
    default_config: permissionConfigSchema.optional(),
    configs: z.array(mcpToolConfigSchema).optional(),
  })
  .strict();

const customToolSchema = z
  .object({
    type: z.literal('custom'),
    name: z.string().regex(/^[A-Za-z0-9_-]{1,128}$/),
    description: z.string().min(1).max(1024),
    input_schema: z.object({ type: z.literal('object') }).passthrough(),
  })
  .strict();

const toolSchema = z.discriminatedUnion('type', [builtInToolsetSchema, mcpToolsetSchema, customToolSchema]);

export const createAgentDraftSchema = z
  .object({
    name: z.string().trim().min(1, 'Name is required.'),
    description: z.string().nullable().optional(),
    model: modelSchema,
    system: z.string().nullable().optional(),
    mcp_servers: z.array(mcpServerSchema).max(20),
    tools: z.array(toolSchema),
    skills: z.array(skillSchema).max(20),
    metadata: z
      .record(z.string().min(1).max(64), z.string().max(512))
      .refine((metadata) => Object.keys(metadata).length <= 16, 'Metadata must contain at most 16 keys.')
      .optional(),
    multiagent: multiagentSchema.nullable().optional(),
  })
  .strict()
  .superRefine((draft, context) => {
    const serverNames = draft.mcp_servers.map((server) => server.name);
    if (new Set(serverNames).size !== serverNames.length) {
      context.addIssue({ code: 'custom', message: 'MCP server names must be unique.', path: ['mcp_servers'] });
    }
    const toolsetKeys = draft.tools.flatMap((tool) => {
      if (tool.type === 'agent_toolset_20260401') {
        return [tool.type];
      }
      return tool.type === 'mcp_toolset' ? [`${tool.type}:${tool.mcp_server_name}`] : [];
    });
    if (new Set(toolsetKeys).size !== toolsetKeys.length) {
      context.addIssue({ code: 'custom', message: 'Toolsets must be unique.', path: ['tools'] });
    }
    const toolsets = draft.tools.filter((tool) => tool.type === 'mcp_toolset');
    for (const toolset of toolsets) {
      if (!serverNames.includes(String(toolset.mcp_server_name))) {
        context.addIssue({
          code: 'custom',
          message: 'MCP toolset must reference a configured server.',
          path: ['tools'],
        });
      }
    }
    if (serverNames.some((name) => !toolsets.some((toolset) => toolset.mcp_server_name === name))) {
      context.addIssue({
        code: 'custom',
        message: 'Every MCP server must have a matching toolset.',
        path: ['tools'],
      });
    }
    const customNames = draft.tools.filter((tool) => tool.type === 'custom').map((tool) => String(tool.name));
    if (new Set(customNames).size !== customNames.length) {
      context.addIssue({ code: 'custom', message: 'Custom tool names must be unique.', path: ['tools'] });
    }
    const totalTools = draft.tools.reduce(
      (total, tool) => total + 1 + ('configs' in tool && Array.isArray(tool.configs) ? tool.configs.length : 0),
      0,
    );
    if (totalTools > 128) {
      context.addIssue({ code: 'custom', message: 'Tools must contain at most 128 total tools.', path: ['tools'] });
    }
  });

export function normalizeCreateAgentDraft(input: CreateAgentInput): CreateAgentInput {
  const parsed = createAgentDraftSchema.parse(input);
  return {
    ...parsed,
    name: parsed.name.trim(),
    description: nullableString(parsed.description),
    model: normalizeDraftModel(parsed.model),
    system: nullableString(parsed.system),
    mcp_servers: cloneJsonValue(parsed.mcp_servers),
    tools: cloneJsonValue(parsed.tools),
    skills: cloneJsonValue(parsed.skills),
    ...(parsed.metadata ? { metadata: { ...parsed.metadata } } : {}),
    ...(parsed.multiagent === undefined ? {} : { multiagent: cloneJsonValue(parsed.multiagent) }),
  };
}

export function updateDraftModelID(model: AgentModelInput, id: string): AgentModelInput {
  const nextID = id.trim();
  return typeof model === 'string' ? nextID : { ...model, id: nextID };
}

export function selectedSubagentReferences(draft: CreateAgentInput): AgentMultiagentInput['agents'] {
  return draft.multiagent?.agents ?? [];
}

export function toggleSubagent(draft: CreateAgentInput, agent: AgentApiResponse): CreateAgentInput {
  const current = selectedSubagentReferences(draft);
  const exists = current.some((reference) => reference.type === 'agent' && reference.id === agent.id);
  if (!exists && current.length >= 20) {
    return draft;
  }
  const agents = exists
    ? current.filter((reference) => reference.type !== 'agent' || reference.id !== agent.id)
    : [...current, { type: 'agent' as const, id: agent.id, version: agent.version }];
  return { ...draft, multiagent: agents.length ? { type: 'coordinator', agents } : null };
}

export function selectedSkillReferences(draft: CreateAgentInput): AgentSkillInput[] {
  return draft.skills.flatMap((skill) => {
    const record = toRecord(skill);
    const type = record?.type;
    const skillID = record?.skill_id;
    if ((type !== 'anthropic' && type !== 'custom') || typeof skillID !== 'string') {
      return [];
    }
    return [{ type, skill_id: skillID, version: typeof record?.version === 'string' ? record.version : 'latest' }];
  });
}

export function toggleSkill(draft: CreateAgentInput, skill: AgentSkillOption): CreateAgentInput {
  const current = selectedSkillReferences(draft);
  const exists = current.some((reference) => reference.skill_id === skill.id);
  const skills = exists
    ? draft.skills.filter((reference) => toRecord(reference)?.skill_id !== skill.id)
    : [...draft.skills, { type: skill.source, skill_id: skill.id, version: 'latest' }];
  return { ...draft, skills };
}

export function addMcpServer(draft: CreateAgentInput, input: McpServerInput): AddMcpServerResult {
  const name = input.name.trim();
  const url = input.url.trim();
  const errors = validateMcpServerInput(draft, name, url);
  if (Object.keys(errors).length) {
    return { ok: false, errors };
  }
  const nextDraft = {
    ...draft,
    mcp_servers: [...draft.mcp_servers, { name, type: 'url', url }],
    tools: [
      ...draft.tools,
      {
        type: 'mcp_toolset',
        mcp_server_name: name,
        default_config: permissionConfig('always_ask'),
        configs: [],
      },
    ],
  };
  return { ok: true, draft: nextDraft };
}

export function addBuiltInToolset(draft: CreateAgentInput): CreateAgentInput {
  if (draft.tools.some((tool) => tool.type === 'agent_toolset_20260401')) {
    return draft;
  }
  return { ...draft, tools: [{ type: 'agent_toolset_20260401' }, ...draft.tools] };
}

export function removeToolset(draft: CreateAgentInput, key: string): CreateAgentInput {
  if (key === 'agent_toolset_20260401') {
    return { ...draft, tools: draft.tools.filter((tool) => tool.type !== key) };
  }
  return {
    ...draft,
    mcp_servers: draft.mcp_servers.filter((server) => toRecord(server)?.name !== key),
    tools: draft.tools.filter((tool) => !(tool.type === 'mcp_toolset' && tool.mcp_server_name === key)),
  };
}

export function updateCustomTool(
  draft: CreateAgentInput,
  index: number,
  update: Record<string, unknown>,
): CreateAgentInput {
  return {
    ...draft,
    tools: draft.tools.map((tool, toolIndex) => (toolIndex === index ? { ...tool, ...update } : tool)),
  };
}

export function removeCustomTool(draft: CreateAgentInput, index: number): CreateAgentInput {
  return { ...draft, tools: draft.tools.filter((_, toolIndex) => toolIndex !== index) };
}

export function toolsetPermission(
  toolset: Record<string, unknown>,
  names: string[],
  fallback: EditablePermission,
): ToolPermissionState {
  const defaultPermission = effectiveToolPermission(toRecord(toolset.default_config) ?? undefined, fallback);
  const configs = Array.isArray(toolset.configs) ? toolset.configs : [];
  const permissions = names.map((name) => {
    const override = configs.map(toRecord).find((config) => config?.name === name);
    return effectiveToolPermission(override ?? undefined, defaultPermission);
  });
  return aggregateToolPermissions(permissions, defaultPermission);
}

export function setToolsetPermission(
  draft: CreateAgentInput,
  predicate: (tool: Record<string, unknown>) => boolean,
  permission: EditablePermission,
): CreateAgentInput {
  return {
    ...draft,
    tools: draft.tools.map((tool) =>
      predicate(tool) ? { ...tool, default_config: permissionConfig(permission), configs: [] } : tool,
    ),
  };
}

export function setToolPermission(
  draft: CreateAgentInput,
  predicate: (tool: Record<string, unknown>) => boolean,
  name: string,
  permission: EditablePermission,
  fallback: EditablePermission,
): CreateAgentInput {
  return {
    ...draft,
    tools: draft.tools.map((tool) => {
      if (!predicate(tool)) {
        return tool;
      }
      const defaultPermission = effectiveToolPermission(toRecord(tool.default_config) ?? undefined, fallback);
      const existing = Array.isArray(tool.configs)
        ? tool.configs.map(toRecord).filter((config): config is Record<string, unknown> => Boolean(config))
        : [];
      const others = existing.filter((config) => config.name !== name);
      return {
        ...tool,
        default_config: toRecord(tool.default_config) ?? permissionConfig(defaultPermission),
        configs: permission === defaultPermission ? others : [...others, { name, ...permissionConfig(permission) }],
      };
    }),
  };
}

export function builtInToolNames() {
  return BUILT_IN_AGENT_TOOLSETS.agent_toolset_20260401.map((tool) => tool.name);
}

export function permissionConfig(permission: EditablePermission) {
  return permission === 'always_deny'
    ? { enabled: false, permission_policy: { type: 'always_allow' } }
    : { enabled: true, permission_policy: { type: permission } };
}

function validateMcpServerInput(draft: CreateAgentInput, name: string, url: string): McpServerInputErrors {
  const errors: McpServerInputErrors = {};
  if (!name) {
    errors.name = 'required';
  } else if (name.length > 255) {
    errors.name = 'too_long';
  } else if (
    draft.mcp_servers.some((server) => toRecord(server)?.name === name) ||
    draft.tools.some((tool) => tool.type === 'mcp_toolset' && tool.mcp_server_name === name)
  ) {
    errors.name = 'duplicate';
  }

  if (!url) {
    errors.url = 'required';
  } else if (url.length > 2048) {
    errors.url = 'too_long';
  } else if (!isHTTPURL(url)) {
    errors.url = 'invalid';
  }

  if (draft.mcp_servers.length >= 20) {
    errors.form = 'limit';
  }
  return errors;
}

function isHTTPURL(value: string) {
  try {
    const parsed = new URL(value);
    return (
      (parsed.protocol === 'http:' || parsed.protocol === 'https:') &&
      Boolean(parsed.hostname) &&
      !parsed.username &&
      !parsed.password &&
      !parsed.hash
    );
  } catch {
    return false;
  }
}

function normalizeDraftModel(model: AgentModelInput): AgentModelInput {
  return typeof model === 'string'
    ? model.trim()
    : { id: model.id.trim(), ...(model.speed ? { speed: model.speed } : {}) };
}

function nullableString(value: string | null | undefined) {
  return typeof value === 'string' && value.trim() ? value : null;
}

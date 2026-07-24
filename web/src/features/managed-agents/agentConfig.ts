import { type ApiError } from '../../shared/api/client';
import { type Locale } from '../../shared/i18n';
import { zhTemplateText } from './agentConfigTemplateText';
import { buildPlatformQuickstartRequest } from './quickstart/platformQuickstartRequest';
import YAML from 'yaml';
import { z } from 'zod';
import { agentModelName, BUILT_IN_AGENT_TOOLSETS } from './agents/AgentsResourcePage';
import { postQuickstartProxyStream } from './api';
import {
  type AgentApiResponse,
  type AgentEditConfig,
  type AgentModelInput,
  type AgentTemplate,
  type AgentUpdateInput,
  type CodeFormat,
  type CreateAgentInput,
} from './types';
import { cloneJsonValue, objectRecord, parseToolInput, toRecord } from './utils';

export {
  agentTemplates,
  blankAgentTemplate,
  createAgentTemplates,
  createTemplateAppTags,
  templateTags,
} from './agentTemplateCatalog';

export const agentModelInputSchema = z.union([
  z.string().trim().min(1, 'Model is required.'),
  z
    .object({
      id: z.string().trim().min(1, 'Model id is required.'),
      speed: z.string().trim().optional(),
    })
    .strict(),
]);

export const agentEditObjectSchema = z.record(z.string(), z.unknown());

export const agentEditConfigSchema = z
  .object({
    name: z.string().trim().min(1, 'Name is required.'),
    description: z.string().nullable().optional(),
    model: agentModelInputSchema,
    system: z.string().nullable().optional(),
    mcp_servers: z.array(z.unknown()).optional(),
    tools: z.array(agentEditObjectSchema).optional(),
    skills: z.array(z.unknown()).optional(),
    metadata: agentEditObjectSchema.optional(),
    multiagent: agentEditObjectSchema.nullable().optional(),
  })
  .strict();

export function yamlForTemplate(
  template: AgentTemplate,
  locale: Locale = 'en',
  modelMappings: Record<string, string> = {},
) {
  return yamlStringify(displayAgentConfig(createDialogAgentConfig(template, locale, undefined, modelMappings)));
}

export function jsonForTemplate(
  template: AgentTemplate,
  locale: Locale = 'en',
  modelMappings: Record<string, string> = {},
) {
  return JSON.stringify(
    displayAgentConfig(createDialogAgentConfig(template, locale, undefined, modelMappings)),
    null,
    2,
  );
}

export function codeForTemplate(
  template: AgentTemplate,
  format: CodeFormat,
  locale: Locale = 'en',
  modelMappings: Record<string, string> = {},
) {
  return format === 'YAML'
    ? yamlForTemplate(template, locale, modelMappings)
    : jsonForTemplate(template, locale, modelMappings);
}

export function createAgentToolset() {
  return {
    type: 'agent_toolset_20260401',
  };
}

export function createMcpServer(name: string, url: string) {
  return {
    name,
    type: 'url',
    url,
  };
}

export function createMcpToolset(name: string) {
  return {
    type: 'mcp_toolset',
    mcp_server_name: name,
    default_config: {
      permission_policy: {
        type: 'always_allow',
      },
    },
  };
}

export const createDialogTemplateConfigs: Record<string, CreateAgentInput> = {
  blank: {
    name: 'Untitled agent',
    description: 'A blank starting point with the core toolset.',
    model: 'claude-sonnet-4-6',
    system:
      "You are a general-purpose agent that can research, write code, run commands, and use connected tools to complete the user's task end to end.",
    mcp_servers: [],
    tools: [{ type: 'agent_toolset_20260401' }],
    skills: [],
  },
  'deep-researcher': {
    name: 'Deep researcher',
    description: 'Conducts multi-step web research with source synthesis and citations.',
    model: 'claude-sonnet-4-6',
    system: `You are a research agent. Given a question or topic:

1. Decompose it into 3-5 concrete sub-questions that, answered together, cover the topic.
2. For each sub-question, run targeted web searches and fetch the most authoritative sources (prefer primary sources, official docs, peer-reviewed work over blog posts and aggregators).
3. Read the sources in full — don't skim. Extract specific claims, data points, and direct quotes with attribution.
4. Synthesize a report that answers the original question. Structure it by sub-question, cite every non-obvious claim inline, and close with a "confidence & gaps" section noting where sources disagreed or where you couldn't find good coverage.

Be skeptical. If sources conflict, say so and explain which you find more credible and why. Don't paper over uncertainty with confident-sounding prose.`,
    mcp_servers: [],
    tools: [createAgentToolset()],
    skills: [],
    metadata: { template: 'deep-research' },
  },
  'structured-extractor': {
    name: 'Structured extractor',
    description: 'Parses unstructured text into a typed JSON schema.',
    model: 'claude-sonnet-4-6',
    system: `You extract structured data from unstructured text. Given raw input (emails, PDFs, logs, transcripts, scraped HTML) and a target JSON schema:

1. Read the schema first. Note required vs optional fields, enums, and format constraints (dates, currencies, IDs). The schema is the contract — never emit a key it doesn't define.
2. Scan the input for each field. Prefer explicit values over inferred ones. If a required field is genuinely absent, use null rather than guessing.
3. Normalize as you extract: trim whitespace, coerce dates to ISO 8601, strip currency symbols into numeric + code, collapse enum synonyms to their canonical value.
4. Emit a single JSON object (or array, if the schema is a list) that validates against the schema. No prose, no markdown fences — just the JSON.

When the input is ambiguous, pick the most conservative interpretation and note the ambiguity in a top-level "_extraction_notes" field only if the schema allows additionalProperties.`,
    mcp_servers: [],
    tools: [createAgentToolset()],
    skills: [],
    metadata: { template: 'structured-extractor' },
  },
  'field-monitor': {
    name: 'Field monitor',
    description: 'Scans software blogs for a topic and writes a weekly what-changed brief.',
    model: 'claude-sonnet-4-6',
    system: `You track a fast-moving technical field. Given a topic and a lookback window (default 7 days):

1. Search arXiv, Hacker News, lobste.rs, and the high-signal blogs (OpenAI, Anthropic, DeepMind, the well-known substacks) for posts in the window matching the topic.
2. Cluster by theme — not by source. Name clusters by the claim or shift, e.g. "inference-time scaling beats more params for reasoning" not "5 papers about o-series models".
3. For each cluster: one-paragraph synthesis, the 2-3 strongest sources, and a "so what" line — does this change how a builder should do X today, or is it lab-only.
4. Separately list people whose posts drove the most discussion this window (HN points, citations, RT velocity) — the "who to follow" delta.
5. Write a dated digest page to Notion under the team's field-watch database.

Be ruthless about signal. A paper that restates a known result with a new benchmark is noise. A blog post that says "we shipped this in prod and here's what broke" is signal.`,
    mcp_servers: [createMcpServer('notion', 'https://mcp.notion.com/mcp')],
    tools: [createAgentToolset(), createMcpToolset('notion')],
    skills: [],
    metadata: { template: 'field-monitor' },
  },
  'support-agent': {
    name: 'Support agent',
    description: 'Answers customer questions from your docs and knowledge base, and escalates when needed.',
    model: 'claude-sonnet-4-6',
    system: `You are a customer support agent. For each inbound question:

1. Search the product docs and knowledge base in Notion for an answer. Quote the relevant passage and link to the source — never paraphrase policy from memory.
2. Draft a reply in the customer's channel: direct answer first, then the supporting source link, then one proactive next step if relevant.
3. If you can't answer with ≥80% confidence, don't guess — post a handoff message to the internal escalation Slack channel with the full question, what you searched, what you found, and your best hypothesis. Tell the customer a human is taking a look.

Match the customer's tone. Be warm but don't pad. One emoji max.`,
    mcp_servers: [
      createMcpServer('notion', 'https://mcp.notion.com/mcp'),
      createMcpServer('slack', 'https://mcp.slack.com/mcp'),
    ],
    tools: [createAgentToolset(), createMcpToolset('notion'), createMcpToolset('slack')],
    skills: [],
    metadata: { template: 'support-agent' },
  },
  'incident-commander': {
    name: 'Incident commander',
    description: 'Triages a Sentry alert, opens a Linear incident ticket, and runs the Slack war room.',
    model: 'claude-opus-4-8',
    system: `You are an on-call incident commander. When handed a Sentry issue ID or an error fingerprint:

1. Pull the full event payload, stack trace, release tag, and affected-user count from Sentry.
2. Grep the repo for the top frame's file path and surrounding commits (last 72h).
3. Open a Linear incident ticket with severity, suspected blast radius, and your rollback recommendation.
4. Post a threaded status to the incident Slack channel: what broke, who's looking, ETA for next update.
5. Every 15 minutes, re-check Sentry event volume and update the thread until the user closes the incident.

Be decisive. If you're >70% confident it's a specific deploy, say so and recommend the revert.`,
    mcp_servers: [
      createMcpServer('sentry', 'https://mcp.sentry.dev/mcp'),
      createMcpServer('linear', 'https://mcp.linear.app/mcp'),
      createMcpServer('slack', 'https://mcp.slack.com/mcp'),
      createMcpServer('github', 'https://api.githubcopilot.com/mcp/'),
    ],
    tools: [
      createAgentToolset(),
      createMcpToolset('sentry'),
      createMcpToolset('linear'),
      createMcpToolset('slack'),
      createMcpToolset('github'),
    ],
    skills: [],
    metadata: { template: 'incident-commander' },
  },
  'contract-tracker': {
    name: 'Contract tracker',
    description:
      'Extracts clauses, sets deadline reminders, and tracks obligations in Asana when given a Box file ID or link.',
    model: 'claude-opus-4-8',
    system: `You are a contract lifecycle assistant. Given a Box file ID or link:

1. Read the file and extract key metadata: parties, effective date, expiration date, contract value, type, and obligations.
2. Create an Asana list named "<Counterparty> — <Contract Type> — <Effective Year>" with custom fields for counterparty, contract value, and type.
3. For each critical date (renewals, expirations, payment due dates, notice periods), create an Asana task titled "[CONTRACT DATE] <Event> — <Contract Name>" with the source clause, due date, and priority (urgent ≤30 days / medium 31–90 days / low >90 days).
4. For each obligation or SLA, create an Asana task assigned to the relevant team member, tagged by category (Payment, Delivery, Compliance, Renewal, SLA), with the verbatim contract clause as a comment.

Rules: always quote the original clause text — never paraphrase without it. If a date or clause is ambiguous, flag it rather than assume.`,
    mcp_servers: [createMcpServer('box', 'https://mcp.box.com'), createMcpServer('asana', 'https://mcp.asana.com/sse')],
    tools: [createAgentToolset(), createMcpToolset('box'), createMcpToolset('asana')],
    skills: [],
    metadata: { template: 'contract-clause-extraction' },
  },
  'retro-facilitator': {
    name: 'Sprint retro facilitator',
    description: 'Pulls a closed sprint from Linear, synthesizes themes, and writes the retro doc before the meeting.',
    model: 'claude-sonnet-4-6',
    system: `You prep sprint retros. For the sprint just closed:

1. Pull all issues from Linear: what shipped, what slipped, cycle time per ticket, anything re-scoped mid-sprint.
2. Scrape the team Slack channel for sentiment signals: threads with "blocked", "surprised", "nice" / 🎉 reactions.
3. Write a retro doc with three sections — **Went well**, **Dragged**, **Try next sprint** — each with 3–5 bullets backed by specific ticket or message links.
4. End with a proposed single process change and a rough confidence score that it'll stick.

Be specific. "Communication was bad" is useless; "three tickets were re-assigned mid-sprint without Slack heads-up (LIN-123, LIN-456, LIN-789)" is actionable.`,
    mcp_servers: [
      createMcpServer('linear', 'https://mcp.linear.app/mcp'),
      createMcpServer('slack', 'https://mcp.slack.com/mcp'),
    ],
    tools: [createAgentToolset(), createMcpToolset('linear'), createMcpToolset('slack')],
    skills: [{ type: 'anthropic', skill_id: 'docx' }],
    metadata: { template: 'sprint-retro-facilitator' },
  },
  'support-escalator': {
    name: 'Support-to-eng escalator',
    description: 'Reads an Intercom conversation, reproduces the bug, and files a linked Jira issue with repro steps.',
    model: 'claude-sonnet-4-6',
    system: `You bridge support and engineering. Given an Intercom conversation ID:

1. Pull the conversation: customer, plan tier, environment details, any attached logs or screenshots, and the support rep's notes.
2. Attempt a repro in the session container using the steps described. If repro succeeds, capture the exact command or request that triggers it.
3. Create a Jira issue in the engineering project: summary, minimal repro, suspected component (from code search), and a link back to the Intercom conversation.
4. Post a note in the support Slack channel: conversation escalated, Jira link, rough severity guess.
5. Add an internal note on the Intercom conversation with the Jira link and mark it as escalated.

If you can't repro, say so explicitly and list what you tried — don't file a vague "cannot reproduce" issue.`,
    mcp_servers: [
      createMcpServer('intercom', 'https://mcp.intercom.com/mcp'),
      createMcpServer('atlassian', 'https://mcp.atlassian.com/v1/mcp'),
      createMcpServer('slack', 'https://mcp.slack.com/mcp'),
    ],
    tools: [
      createAgentToolset(),
      createMcpToolset('intercom'),
      createMcpToolset('atlassian'),
      createMcpToolset('slack'),
    ],
    skills: [],
    metadata: { template: 'support-to-eng-escalator' },
  },
  'data-analyst': {
    name: 'Data analyst',
    description: 'Load, explore, and visualize data; build reports and answer questions from datasets.',
    model: 'claude-sonnet-4-6',
    system: `You analyze data. Given a dataset (file path, URL, or query) and a question:

1. Load the data and print its shape, column names, dtypes, and a small sample. Always look before you compute.
2. Clean obvious issues — nulls, duplicates, type mismatches — and note what you changed.
3. Answer the question with code. Prefer pandas/polars for tabular work, matplotlib/plotly for charts. Show intermediate results so your reasoning is checkable.
4. For product-analytics questions, query Amplitude directly — event funnels, retention cohorts, property breakdowns — and link the chart.
5. Save any charts or derived tables to /mnt/session/outputs/ and summarize findings in plain language, including caveats (sample size, missing data, correlation-vs-causation).

Default to simple, readable analysis over clever one-liners. A clear bar chart usually beats a dense heatmap.`,
    mcp_servers: [createMcpServer('amplitude', 'https://mcp.amplitude.com/mcp')],
    tools: [createAgentToolset(), createMcpToolset('amplitude')],
    skills: [],
    metadata: { template: 'data-analyst' },
  },
};

export function cloneCreateAgentInput(config: CreateAgentInput): CreateAgentInput {
  return JSON.parse(JSON.stringify(config)) as CreateAgentInput;
}

export const createDialogTemplateConfigsZh: Record<string, CreateAgentInput> = Object.fromEntries(
  Object.entries(createDialogTemplateConfigs).map(([id, config]) => {
    const text = zhTemplateText[id];
    if (!text) {
      return [id, config];
    }
    return [
      id,
      { ...cloneCreateAgentInput(config), name: text.name, description: text.description, system: text.system },
    ];
  }),
);

function templateConfigsForLocale(locale: Locale) {
  return locale === 'zh-CN' ? createDialogTemplateConfigsZh : createDialogTemplateConfigs;
}

function fallbackTemplateSystem(template: AgentTemplate, locale: Locale) {
  if (locale === 'zh-CN') {
    return `${template.prompt} 输出保持简洁；相关时引用工具结果；不可逆操作前先确认。`;
  }

  return `${template.prompt} Keep outputs concise, cite tool results when relevant, and ask for clarification before taking irreversible action.`;
}

export function templateSystem(template: AgentTemplate, locale: Locale = 'en') {
  const configuredSystem = templateConfigsForLocale(locale)[template.id]?.system;

  return typeof configuredSystem === 'string' ? configuredSystem : fallbackTemplateSystem(template, locale);
}

export function createDialogAgentConfig(
  template: AgentTemplate,
  locale: Locale = 'en',
  descriptionOverride?: string | null,
  modelMappings: Record<string, string> = {},
): CreateAgentInput {
  const zh = locale === 'zh-CN';
  const configuredTemplate = templateConfigsForLocale(locale)[template.id];
  const config = cloneCreateAgentInput(
    configuredTemplate ?? {
      name: template.id === 'blank' ? (zh ? '未命名 Agent' : 'Untitled agent') : template.title,
      description: template.body,
      model: 'claude-sonnet-4-6',
      system: fallbackTemplateSystem(template, locale),
      mcp_servers: [],
      tools: [createAgentToolset()],
      skills: [],
      metadata: { template: template.slug },
    },
  );
  const trimmedDescription = descriptionOverride?.trim();
  config.model = resolveAgentModelInput(config.model, modelMappings);

  if (trimmedDescription) {
    config.description = trimmedDescription;
    config.metadata = { ...(config.metadata ?? {}), source: 'description' };
  }

  return config;
}

export function quickstartBuildAgentConfigInput(
  input: Record<string, unknown>,
  fallback: CreateAgentInput,
  modelMappings: Record<string, string> = {},
): CreateAgentInput {
  const rawConfig = toRecord(input.config) ?? input;
  const name = typeof rawConfig.name === 'string' && rawConfig.name.trim() ? rawConfig.name.trim() : fallback.name;
  const description =
    typeof rawConfig.description === 'string'
      ? rawConfig.description
      : rawConfig.description === null
        ? null
        : (fallback.description ?? null);
  const model = resolveAgentModelInput(quickstartModelInput(rawConfig.model, fallback.model), modelMappings);
  const system =
    typeof rawConfig.system === 'string'
      ? rawConfig.system
      : rawConfig.system === null
        ? null
        : (fallback.system ?? null);
  const mcpServers = Array.isArray(rawConfig.mcp_servers)
    ? cloneJsonArray(rawConfig.mcp_servers)
    : cloneJsonArray(fallback.mcp_servers);
  const tools = Array.isArray(rawConfig.tools)
    ? rawConfig.tools.filter(isPlainObject).map((tool) => ({ ...tool }))
    : fallback.tools.map((tool) => ({ ...tool }));
  const skills = Array.isArray(rawConfig.skills) ? cloneJsonArray(rawConfig.skills) : cloneJsonArray(fallback.skills);
  const metadata = quickstartMetadata(rawConfig.metadata, fallback.metadata);

  return {
    name,
    description,
    model,
    system,
    mcp_servers: mcpServers,
    tools,
    skills,
    ...(metadata ? { metadata } : {}),
  };
}

export function resolveAgentModelInput(model: AgentModelInput, modelMappings: Record<string, string>): AgentModelInput {
  const modelID = typeof model === 'string' ? model : model.id;
  const effectiveID = modelMappings[modelID]?.trim();
  if (!effectiveID) {
    return model;
  }
  return typeof model === 'string' ? effectiveID : { ...model, id: effectiveID };
}

export function quickstartModelInput(value: unknown, fallback: AgentModelInput): AgentModelInput {
  if (typeof value === 'string' && value.trim()) {
    return value.trim();
  }
  const record = toRecord(value);
  if (record && typeof record.id === 'string' && record.id.trim()) {
    return {
      id: record.id.trim(),
      ...(typeof record.speed === 'string' && record.speed.trim() ? { speed: record.speed.trim() } : {}),
    };
  }
  return fallback;
}

export function cloneJsonArray<T>(value: T[]): T[] {
  return JSON.parse(JSON.stringify(value)) as T[];
}

export function quickstartMetadata(
  value: unknown,
  fallback?: Record<string, string>,
): Record<string, string> | undefined {
  const record = toRecord(value);
  if (!record) {
    return fallback ? { ...fallback } : undefined;
  }
  const entries = Object.entries(record)
    .filter(([key]) => Boolean(key.trim()))
    .flatMap(([key, entryValue]) =>
      typeof entryValue === 'string' || typeof entryValue === 'number' || typeof entryValue === 'boolean'
        ? ([[key, String(entryValue)]] as const)
        : [],
    );
  if (!entries.length) {
    return fallback ? { ...fallback } : undefined;
  }
  return Object.fromEntries(entries);
}

export function createAgentConfigText(config: CreateAgentInput, format: CodeFormat) {
  return format === 'YAML' ? yamlStringify(config) : JSON.stringify(config, null, 2);
}

export function parseCreateAgentConfigText(
  text: string,
  format: CodeFormat,
  fallback: CreateAgentInput,
): { ok: true; input: CreateAgentInput } | { ok: false; error: string } {
  if (!text.trim()) {
    return { ok: false, error: 'Agent config is required.' };
  }
  try {
    const parsed = format === 'YAML' ? YAML.parse(text) : JSON.parse(text);
    const record = toRecord(parsed);
    if (!record) {
      return { ok: false, error: 'Agent config must be an object.' };
    }
    const input = quickstartBuildAgentConfigInput(record, fallback);
    if (!input.name.trim()) {
      return { ok: false, error: 'Agent config name is required.' };
    }
    return { ok: true, input };
  } catch (error) {
    const message = error instanceof Error && error.message ? error.message : 'Invalid config.';
    return { ok: false, error: `${format} is not valid: ${message}` };
  }
}

export async function generateCreateAgentConfig({
  orgUuid,
  workspaceId,
  description,
  currentConfig,
  modelMappings = {},
  signal,
  locale = 'en',
}: {
  orgUuid: string;
  workspaceId: string;
  description: string;
  currentConfig: CreateAgentInput;
  modelMappings?: Record<string, string>;
  signal: AbortSignal;
  locale?: Locale;
}) {
  const requestBody = {
    ...buildPlatformQuickstartRequest({
      step: 'agent',
      deploymentSchedulePlanned: false,
      agentDescription: description,
      agentConfig: currentConfig,
      locale,
    }),
    tool_choice: { type: 'tool', name: 'build_agent_config', disable_parallel_tool_use: true },
  };
  let currentTool: { name: string; input: Record<string, unknown>; inputJson: string } | null = null;
  let generatedConfig: CreateAgentInput | null = null;

  await postQuickstartProxyStream({
    orgUuid,
    workspaceId,
    body: requestBody,
    signal,
    onEvent: (event) => {
      const type = typeof event.data.type === 'string' ? event.data.type : event.event;
      if (type === 'content_block_start') {
        const contentBlock = toRecord(event.data.content_block);
        if (contentBlock?.type === 'tool_use') {
          currentTool = {
            name: String(contentBlock.name ?? 'unknown_tool'),
            input: toRecord(contentBlock.input) ?? {},
            inputJson: '',
          };
        }
        return;
      }
      if (type === 'content_block_delta') {
        const delta = toRecord(event.data.delta);
        if (delta?.type === 'input_json_delta' && typeof delta.partial_json === 'string' && currentTool) {
          currentTool.inputJson += delta.partial_json;
        }
        return;
      }
      if (type === 'content_block_stop' && currentTool) {
        const input = parseToolInput(currentTool.inputJson, currentTool.input);
        if (currentTool.name === 'build_agent_config') {
          generatedConfig = quickstartBuildAgentConfigInput(input, currentConfig, modelMappings);
        }
        currentTool = null;
      }
    },
  });

  if (!generatedConfig) {
    throw new Error('The generator did not return an agent config.');
  }
  return generatedConfig;
}

export function displayAgentConfig(config: CreateAgentInput | AgentApiResponse) {
  const model = 'model' in config ? config.model : 'claude-sonnet-4-6';
  const displayConfig: Record<string, unknown> = {
    name: config.name,
    description: config.description,
    model: typeof model === 'string' ? model : agentModelName(model),
    system: config.system,
  };
  if (Array.isArray(config.tools) && config.tools.length) {
    displayConfig.tools = config.tools;
  }
  if (Array.isArray(config.skills) && config.skills.length) {
    displayConfig.skills = config.skills;
  }
  if (Array.isArray(config.mcp_servers) && config.mcp_servers.length) {
    displayConfig.mcp_servers = config.mcp_servers;
  }
  if (config.metadata && Object.keys(config.metadata).length) {
    displayConfig.metadata = config.metadata;
  }
  if ('multiagent' in config && config.multiagent) {
    displayConfig.multiagent = config.multiagent;
  }
  return displayConfig;
}

export function yamlStringify(value: unknown) {
  const lines: string[] = [];
  writeYamlValue(lines, value, 0);
  return lines.join('\n');
}

export function writeYamlValue(lines: string[], value: unknown, indent: number) {
  const prefix = ' '.repeat(indent);

  if (Array.isArray(value)) {
    if (!value.length) {
      lines.push(`${prefix}[]`);
      return;
    }

    value.forEach((item) => {
      if (isPlainObject(item)) {
        const entries = Object.entries(item);
        if (!entries.length) {
          lines.push(`${prefix}- {}`);
          return;
        }
        entries.forEach(([key, entryValue], index) => {
          writeYamlEntry(lines, key, entryValue, index === 0 ? indent : indent + 2, index === 0 ? '- ' : '');
        });
        return;
      }

      if (Array.isArray(item)) {
        lines.push(`${prefix}-`);
        writeYamlValue(lines, item, indent + 2);
        return;
      }

      lines.push(`${prefix}- ${yamlScalar(item)}`);
    });
    return;
  }

  if (isPlainObject(value)) {
    const entries = Object.entries(value);
    if (!entries.length) {
      lines.push(`${prefix}{}`);
      return;
    }
    entries.forEach(([key, entryValue]) => writeYamlEntry(lines, key, entryValue, indent));
    return;
  }

  lines.push(`${prefix}${yamlScalar(value)}`);
}

export function writeYamlEntry(lines: string[], key: string, value: unknown, indent: number, itemPrefix = '') {
  const prefix = ' '.repeat(indent);

  if (typeof value === 'string' && value.includes('\n')) {
    lines.push(`${prefix}${itemPrefix}${key}: |-`);
    value.split('\n').forEach((line) => {
      lines.push(line ? `${' '.repeat(indent + 2)}${line}` : '');
    });
    return;
  }

  if (Array.isArray(value)) {
    if (!value.length) {
      lines.push(`${prefix}${itemPrefix}${key}: []`);
      return;
    }
    lines.push(`${prefix}${itemPrefix}${key}:`);
    writeYamlValue(lines, value, indent + 2);
    return;
  }

  if (isPlainObject(value)) {
    const entries = Object.entries(value);
    if (!entries.length) {
      lines.push(`${prefix}${itemPrefix}${key}: {}`);
      return;
    }
    lines.push(`${prefix}${itemPrefix}${key}:`);
    writeYamlValue(lines, value, indent + 2);
    return;
  }

  lines.push(`${prefix}${itemPrefix}${key}: ${yamlScalar(value)}`);
}

export function isPlainObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

export function yamlScalar(value: unknown) {
  if (value === null || value === undefined) {
    return 'null';
  }
  if (typeof value === 'boolean' || typeof value === 'number') {
    return String(value);
  }
  if (typeof value !== 'string') {
    return JSON.stringify(value);
  }
  if (value === '') {
    return '""';
  }
  return value;
}

export function agentEditConfig(agent: AgentApiResponse): AgentEditConfig {
  const multiagent = toRecord(agent.multiagent);
  return {
    name: agent.name,
    description: agent.description ?? null,
    model: agentEditModelInput(agent.model),
    system: agent.system ?? null,
    mcp_servers: cloneJsonValue(agent.mcp_servers),
    tools: canonicalizeAgentEditTools(agent.tools),
    skills: cloneJsonValue(agent.skills),
    metadata: { ...agent.metadata },
    multiagent: multiagent ? cloneJsonValue(multiagent) : null,
  };
}

export function agentEditModelInput(model: AgentApiResponse['model']): AgentModelInput {
  if (typeof model === 'string') {
    return model;
  }
  return {
    id: agentModelName(model),
    ...(typeof model.speed === 'string' && model.speed.trim() ? { speed: model.speed } : {}),
  };
}

export function agentEditConfigText(config: AgentEditConfig, format: CodeFormat) {
  return format === 'YAML' ? yamlStringify(config) : JSON.stringify(config, null, 2);
}

export function parseAgentEditConfigText(
  text: string,
  format: CodeFormat,
): { ok: true; config: AgentEditConfig } | { ok: false; error: string } {
  if (!text.trim()) {
    return { ok: false, error: 'Agent configuration is required.' };
  }

  try {
    const parsed = format === 'YAML' ? YAML.parse(text) : JSON.parse(text);
    const record = toRecord(parsed);
    if (!record) {
      return { ok: false, error: 'Agent configuration must be an object.' };
    }
    const result = agentEditConfigSchema.safeParse(record);
    if (!result.success) {
      return { ok: false, error: agentEditValidationMessage(result.error.issues) };
    }
    return { ok: true, config: result.data };
  } catch (error) {
    const message = error instanceof Error && error.message ? error.message : 'Invalid configuration.';
    return { ok: false, error: `${format} is not valid: ${message}` };
  }
}

export function agentEditValidationMessage(issues: z.ZodIssue[]) {
  return issues
    .slice(0, 3)
    .map((issue) => `${issue.path.join('.') || 'Agent configuration'}: ${issue.message}`)
    .join(' ');
}

export function buildAgentUpdateInput(version: number, config: AgentEditConfig): AgentUpdateInput {
  return {
    version,
    name: config.name.trim(),
    description: nullableTrimmedString(config.description),
    model: normalizeAgentEditModel(config.model),
    system: nullableTrimmedString(config.system),
    tools: canonicalizeAgentEditTools(config.tools ?? []),
    mcp_servers: cloneJsonValue(config.mcp_servers ?? []),
    skills: cloneJsonValue(config.skills ?? []),
    metadata: { ...(config.metadata ?? {}) },
    multiagent: config.multiagent ?? null,
  };
}

export function normalizeAgentEditModel(model: AgentModelInput): AgentModelInput {
  if (typeof model === 'string') {
    return model.trim();
  }
  return {
    id: model.id.trim(),
    ...(model.speed?.trim() ? { speed: model.speed.trim() } : {}),
  };
}

export function nullableTrimmedString(value: string | null | undefined) {
  return typeof value === 'string' && value.trim() ? value : null;
}

export function canonicalizeAgentEditTools(tools: Array<Record<string, unknown>>): Array<Record<string, unknown>> {
  return tools.map((tool) => {
    const next = cloneJsonValue(tool);
    const type = typeof next.type === 'string' ? next.type : '';

    if (isBuiltInAgentToolsetType(type)) {
      next.type = 'agent_toolset_20260401';
    }

    if (Array.isArray(next.configs)) {
      next.configs = next.configs.map(canonicalizeAgentToolConfig);
    }

    return next;
  });
}

export function canonicalizeAgentToolConfig(value: unknown) {
  const config = { ...objectRecord(value) };
  if ('tool_name' in config) {
    const toolName = config.tool_name;
    delete config.tool_name;
    if (!('name' in config) && typeof toolName === 'string' && toolName.trim()) {
      config.name = toolName.trim();
    }
  }
  return config;
}

export function isBuiltInAgentToolsetType(type: string) {
  return type.startsWith('agent_toolset_') || type in BUILT_IN_AGENT_TOOLSETS;
}

export function agentEditSaveErrorMessage(error: unknown) {
  if (error && typeof error === 'object' && 'status' in error) {
    const status = (error as ApiError).status;
    if (status === 409) {
      return 'This agent was updated elsewhere while you were editing. Close and reopen the editor to start from the latest version.';
    }
    if (status === 400) {
      return 'Invalid agent configuration. Check your editor for errors.';
    }
  }
  if (error && typeof error === 'object' && 'message' in error) {
    const message = (error as Error | ApiError).message;
    if (typeof message === 'string' && message.trim()) {
      return message;
    }
  }
  return 'Failed to save agent. You can try again.';
}

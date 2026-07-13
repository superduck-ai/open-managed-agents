import { type ApiError } from '../../shared/api/client';
import { type Locale } from '../../shared/i18n';
import { buildPlatformQuickstartRequest } from './quickstart/platformQuickstartRequest';
import {
  BarChart3,
  Box,
  BriefcaseBusiness,
  FileCheck2,
  FileJson,
  FileText,
  GitBranch,
  Headphones,
  MessageCircle,
  Siren,
  Sparkles,
} from 'lucide-react';
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
  type TemplateTag,
} from './types';
import { cloneJsonValue, objectRecord, parseToolInput, toRecord } from './utils';

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

export const templateTags = {
  docs: { label: 'docs', icon: FileText, tone: 'bg-secondary text-foreground' },
  data: { label: 'data', icon: BarChart3, tone: 'bg-secondary text-secondary-foreground' },
  code: { label: 'code', icon: FileJson, tone: 'bg-secondary text-foreground' },
  support: { label: 'support', icon: Headphones, tone: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' },
  incident: { label: 'incident', icon: Siren, tone: 'bg-destructive/10 text-destructive' },
  github: { label: 'github', icon: GitBranch, tone: 'bg-secondary text-foreground' },
  box: { label: 'box', icon: Box, tone: 'bg-secondary text-secondary-foreground' },
  tasks: { label: 'tasks', icon: FileCheck2, tone: 'bg-secondary text-foreground' },
  chat: { label: 'chat', icon: MessageCircle, tone: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' },
  research: { label: 'research', icon: Sparkles, tone: 'bg-amber-500/10 text-amber-600 dark:text-amber-400' },
  notion: { label: 'notion', icon: FileText, tone: 'bg-secondary text-foreground' },
  slack: { label: 'slack', icon: MessageCircle, tone: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' },
  sentry: { label: 'sentry', icon: Siren, tone: 'bg-destructive/10 text-destructive' },
  linear: { label: 'linear', icon: FileCheck2, tone: 'bg-secondary text-foreground' },
  asana: { label: 'asana', icon: FileCheck2, tone: 'bg-secondary text-foreground' },
  intercom: { label: 'intercom', icon: Headphones, tone: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' },
  atlassian: { label: 'atlassian', icon: BriefcaseBusiness, tone: 'bg-secondary text-secondary-foreground' },
  docx: { label: 'docx', icon: FileText, tone: 'bg-secondary text-secondary-foreground' },
  amplitude: { label: 'amplitude', icon: BarChart3, tone: 'bg-secondary text-secondary-foreground' },
} satisfies Record<string, TemplateTag>;

export const agentTemplates: AgentTemplate[] = [
  {
    id: 'blank',
    slug: 'blank-agent',
    title: 'Blank agent config',
    body: 'A blank starting point with the core toolset.',
    prompt: 'Create a blank managed agent config with a core toolset.',
  },
  {
    id: 'deep-researcher',
    slug: 'deep-researcher',
    title: 'Deep researcher',
    body: 'Conducts multi-step web research with source synthesis and citations.',
    prompt: 'Build a deep researcher that conducts multi-step web research, synthesizes sources, and cites claims.',
  },
  {
    id: 'structured-extractor',
    slug: 'structured-extractor',
    title: 'Structured extractor',
    body: 'Parses unstructured text into a typed JSON schema.',
    prompt: 'Create an agent that parses unstructured text into a typed JSON schema.',
  },
  {
    id: 'field-monitor',
    slug: 'field-monitor',
    title: 'Field monitor',
    body: 'Scans software blogs for a topic and writes a weekly what-changed brief.',
    prompt: 'Create a field monitor that scans software blogs for a topic and writes a weekly change brief.',
    tags: [templateTags.notion],
  },
  {
    id: 'support-agent',
    slug: 'support-agent',
    title: 'Support agent',
    body: 'Answers customer questions from your docs and knowledge base, and escalates when needed.',
    prompt: 'Build a support agent that answers customer questions from docs and escalates when needed.',
    tags: [templateTags.notion, templateTags.slack],
  },
  {
    id: 'incident-commander',
    slug: 'incident-commander',
    title: 'Incident commander',
    body: 'Triages a Sentry alert, opens a Linear incident ticket, and runs the Slack war room.',
    prompt:
      'Create an incident commander agent that triages alerts, opens an incident ticket, and coordinates a war room.',
    tags: [templateTags.sentry, templateTags.linear, templateTags.slack, templateTags.github],
  },
  {
    id: 'contract-tracker',
    slug: 'contract-tracker',
    title: 'Contract tracker',
    body: 'Extracts clauses, sets deadline reminders, and tracks obligations in Asana when given a Box file ID or link.',
    prompt: 'Build a contract tracker that extracts clauses, sets deadline reminders, and tracks obligations.',
    tags: [templateTags.box, templateTags.asana],
  },
  {
    id: 'retro-facilitator',
    slug: 'sprint-retro-facilitator',
    title: 'Sprint retro facilitator',
    body: 'Pulls a closed sprint from Linear, synthesizes themes, and writes the retro doc before the meeting.',
    prompt:
      'Create a sprint retro facilitator that pulls closed sprint work, synthesizes themes, and drafts the retro doc.',
    tags: [templateTags.linear, templateTags.slack, templateTags.docx],
  },
  {
    id: 'support-escalator',
    slug: 'support-to-eng-escalator',
    title: 'Support-to-eng escalator',
    body: 'Reads an Intercom conversation, reproduces the bug, and files a linked Jira issue with repro steps.',
    prompt: 'Create a support-to-engineering escalator that reproduces bugs and files linked issues with repro steps.',
    tags: [templateTags.intercom, templateTags.atlassian, templateTags.slack],
  },
  {
    id: 'data-analyst',
    slug: 'data-analyst',
    title: 'Data analyst',
    body: 'Load, explore, and visualize data; build reports and answer questions from datasets.',
    prompt: 'Build a data analyst agent that loads, explores, visualizes data, and writes reports from datasets.',
    tags: [templateTags.amplitude],
  },
];

export const createAgentTemplates = agentTemplates.slice(0, 6);

export const blankAgentTemplate = createAgentTemplates[0];

export const createTemplateAppTags: Record<string, TemplateTag[]> = {
  'field-monitor': [templateTags.notion],
  'support-agent': [templateTags.notion, templateTags.slack],
  'incident-commander': [templateTags.sentry, templateTags.linear, templateTags.slack, templateTags.github],
};

export const structuredExtractorSystem = `You extract structured data from unstructured text. Given raw input (emails, PDFs, logs, transcripts, scraped HTML) and a target JSON schema:

1. Read the schema first. Note required vs optional fields, enums, and format constraints (dates, currencies, IDs). The schema is the contract — never emit a key it doesn't define.
2. Scan the input for each field. Prefer explicit values over inferred ones. If a required field is genuinely absent, use null rather than guessing.
3. Normalize as you extract: trim whitespace, coerce dates to ISO 8601, strip currency symbols into numeric + code, collapse enum synonyms to their canonical value.
4. Emit a single JSON object (or array, if the schema is a list) that validates against the schema. No prose, no markdown fences — just the JSON.

When the input is ambiguous, pick the most conservative interpretation and note the ambiguity in a top-level "_extraction_notes" field only if the schema allows additionalProperties.`;

export const structuredExtractorSystemZh = `你负责把非结构化文本提取成结构化数据。输入可能是邮件、PDF、日志、会议记录或抓取的 HTML；同时会给你一个目标 JSON Schema。

1. 先读 Schema：分清必填/可选、枚举，以及日期、货币、ID 等格式约束。Schema 就是契约——不要输出它未定义的字段。
2. 按字段在原文里找值。优先用明确写出的内容，不要靠猜。必填项确实找不到时用 null。
3. 提取时顺便规范化：去掉首尾空白，日期统一成 ISO 8601，货币拆成数值 + 币种代码，枚举同义词收敛到规范值。
4. 只输出一个能通过 Schema 校验的 JSON 对象（若 Schema 是列表则输出数组）。不要解释、不要 markdown 代码块。

原文有歧义时，选最保守的理解；仅当 Schema 允许 additionalProperties 时，才把疑点记在顶层 "_extraction_notes" 里。`;

export function templateSystem(template: AgentTemplate, locale: Locale = 'en') {
  const zh = locale === 'zh-CN';
  if (template.id === 'structured-extractor') {
    return zh ? structuredExtractorSystemZh : structuredExtractorSystem;
  }

  if (zh) {
    return `${template.prompt} 输出保持简洁；相关时引用工具结果；不可逆操作前先确认。`;
  }
  return `${template.prompt} Keep outputs concise, cite tool results when relevant, and ask for clarification before taking irreversible action.`;
}

export function yamlForTemplate(template: AgentTemplate, locale: Locale = 'en') {
  return yamlStringify(displayAgentConfig(createDialogAgentConfig(template, locale)));
}

export function jsonForTemplate(template: AgentTemplate, locale: Locale = 'en') {
  return JSON.stringify(displayAgentConfig(createDialogAgentConfig(template, locale)), null, 2);
}

export function codeForTemplate(template: AgentTemplate, format: CodeFormat, locale: Locale = 'en') {
  return format === 'YAML' ? yamlForTemplate(template, locale) : jsonForTemplate(template, locale);
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
    system: structuredExtractorSystem,
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

// Chinese name/description/system for the built-in templates. Names follow what the
// agent actually does (from the English system prompt), not a literal title gloss.
// Only these three user-facing text fields are localized; every technical field
// (model, mcp_servers, tools, skills, metadata) is inherited unchanged from the
// English table so the created agent stays byte-identical on machine configuration.
const zhTemplateText: Record<string, { name: string; description: string; system: string }> = {
  blank: {
    name: '未命名 Agent',
    description: '内置核心工具集的空白起点，可按需自行配置。',
    system: '你是一个通用 Agent：可以检索资料、写代码、跑命令，并用已连接的工具把用户任务认真完成。',
  },
  'deep-researcher': {
    name: '深度调研助手',
    description: '围绕问题多轮检索权威来源，综合成带引用的调研报告。',
    system: `你是深度调研助手。收到一个问题或主题后：

1. 先拆成 3–5 个具体子问题，合起来能覆盖原主题。
2. 对每个子问题做定向检索，优先一手来源、官方文档、同行评审；少依赖博客和聚合站。
3. 完整阅读来源，不要略读。记下具体论断、数据，以及带出处的直接引文。
4. 写成回答原问题的报告：按子问题组织，凡非显而易见的论断都内联标注出处；文末用「把握与缺口」说明来源冲突或覆盖不足的地方。

保持怀疑。来源冲突时直接说，并说明你更信哪一边、为什么。不要用听起来很笃定的句子掩盖不确定。`,
  },
  'structured-extractor': {
    name: '结构化提取助手',
    description: '把邮件、PDF、日志等非结构化文本，按 JSON Schema 提取成结构化数据。',
    system: structuredExtractorSystemZh,
  },
  'field-monitor': {
    name: '技术周报助手',
    description: '按主题扫描技术博客与社区，整理每周变化简报并写入 Notion。',
    system: `你负责跟踪一个变化很快的技术领域。给定主题和回溯窗口（默认 7 天）：

1. 在 arXiv、Hacker News、lobste.rs，以及 OpenAI / Anthropic / DeepMind 等高信号博客与常见 substack 中，检索窗口内与主题相关的内容。
2. 按主题聚类，不要按来源堆砌。聚类名要体现论断或转向，例如「推理时扩展比堆参数更有效」，而不是「又有 5 篇 o 系列论文」。
3. 每个聚类写：一段综合、2–3 个最硬的来源，以及一行「所以呢」——今天做产品的人要不要改做法，还是仍停留在实验室。
4. 另列本窗口讨论最热的作者（HN 分数、引用、转发热度）——「值得关注谁」的增量。
5. 把带日期的摘要页写到团队 Notion 的 field-watch 数据库。

对噪音要狠。用新 benchmark 复述旧结论是噪音；「我们上了生产，翻车点是这些」才是信号。`,
  },
  'support-agent': {
    name: '客服助手',
    description: '根据文档与知识库回答客户问题；没把握时升级人工处理。',
    system: `你是客服助手。每收到一个客户问题：

1. 先在 Notion 的产品文档和知识库里找答案。引用原文并附来源链接——政策类内容不要凭记忆改写。
2. 在客户所在渠道起草回复：先给直接答案，再给来源链接，相关时再补一个主动建议的下一步。
3. 把握不到约 80% 就不要猜——把完整问题、你搜过什么、找到了什么、以及你的初步判断发到内部升级 Slack 频道，并告知客户已有人接手。

语气跟客户对齐：热情，但不注水。表情符号最多一个。`,
  },
  'incident-commander': {
    name: '故障指挥官',
    description: '接手 Sentry 告警后，创建 Linear 故障单，并在 Slack 主持战时响应。',
    system: `你是值班故障指挥官。收到 Sentry issue ID 或错误指纹后：

1. 从 Sentry 拉取完整事件、堆栈、release 标签和受影响用户数。
2. 用栈顶帧的文件路径检索代码仓库，并查看近 72 小时相关提交。
3. 在 Linear 开故障单：严重级别、疑似影响面，以及你的回滚建议。
4. 在故障 Slack 频道发线程状态：哪里坏了、谁在跟、下次更新预计时间。
5. 之后每 15 分钟回看一次 Sentry 事件量并更新线程，直到用户关闭故障。

要果断。若有七成以上把握是某次发布引入的，就直接说，并建议回滚。`,
  },
  'contract-tracker': {
    name: '合同履约助手',
    description: '从 Box 合同里提取条款与关键日期，在 Asana 里跟进履约与提醒。',
    system: `你是合同履约助手。给定 Box 文件 ID 或链接：

1. 读合同，提取关键信息：签约方、生效日、到期日、合同金额、类型，以及各项义务。
2. 在 Asana 建列表，命名为「<对方> — <合同类型> — <生效年份>」，并加上对方、金额、类型等自定义字段。
3. 对每个关键日期（续约、到期、付款、通知期）建任务，标题形如「[CONTRACT DATE] <事件> — <合同名>」，附上原文条款、截止日期和优先级（≤30 天紧急 / 31–90 天中等 / >90 天低）。
4. 对每项义务或 SLA 建任务，指派给相关同事，按 Payment / Delivery / Compliance / Renewal / SLA 打标签，并把合同原文作为评论附上。

规则：条款必须引用原文，不要空口改写。日期或表述含糊时标出来，不要擅自假定。`,
  },
  'retro-facilitator': {
    name: 'Sprint 复盘助手',
    description: '汇总已结束 Sprint 的交付与团队反馈，会前起草复盘文档。',
    system: `你负责 Sprint 复盘的会前准备。针对刚结束的 Sprint：

1. 从 Linear 拉取全部 issue：交付了什么、延期了什么、各 ticket 周期，以及中途改过范围的事项。
2. 扫团队 Slack：含 "blocked"、"surprised"、"nice" 或 🎉 反应的讨论，当作情绪信号。
3. 写一份复盘文档，三段——**进展顺利**、**拖后腿的地方**、**下个 Sprint 想试的**——每段 3–5 条，并附具体 ticket 或消息链接。
4. 文末给一条建议落地的流程改进，并粗评它能坚持多久。

要具体。「沟通不好」没用；「三个 ticket 中途换人且没在 Slack 提前说（LIN-123、LIN-456、LIN-789）」才可执行。`,
  },
  'support-escalator': {
    name: '工单升级助手',
    description: '读取 Intercom 会话、尝试复现，并提交带复现步骤的关联 Jira 工单。',
    system: `你负责把客服问题升级给研发。给定 Intercom 对话 ID：

1. 拉取会话：客户、套餐、环境信息、附件日志/截图，以及客服备注。
2. 按描述的步骤在 session 容器里尝试复现。成功就记下能稳定触发的命令或请求。
3. 在研发项目建 Jira 工单：摘要、最小复现、疑似组件（可配合代码搜索），并链回 Intercom 会话。
4. 在客服 Slack 频道留一句：已升级、Jira 链接、粗略严重级别。
5. 在 Intercom 会话加内部备注（含 Jira 链接），并标为已升级。

复现不了就明说，并列出你试过什么——不要交一份含糊的「无法复现」工单。`,
  },
  'data-analyst': {
    name: '数据分析师',
    description: '加载并探索数据，出图表与报告，回答数据集相关问题。',
    system: `你是数据分析师。给定数据集（文件路径、URL 或查询）和一个问题：

1. 先加载数据，打印 shape、列名、dtype 和小样本。先看再算。
2. 清理明显问题（空值、重复、类型不对），并记下改了什么。
3. 用代码回答问题。表格优先 pandas/polars，图优先 matplotlib/plotly。展示中间结果，方便核对推理。
4. 产品分析类问题可直接查 Amplitude（漏斗、留存 cohort、属性拆分），并附上图表链接。
5. 图表和派生表存到 /mnt/session/outputs/，再用白话总结发现，并写清局限（样本量、缺失、相关≠因果）。

优先简单可读，别炫技。清楚的柱状图通常比密密麻麻的热力图更有用。`,
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

export function createDialogAgentConfig(
  template: AgentTemplate,
  locale: Locale = 'en',
  descriptionOverride?: string | null,
): CreateAgentInput {
  const zh = locale === 'zh-CN';
  const table = zh ? createDialogTemplateConfigsZh : createDialogTemplateConfigs;
  const fallbackConfig: CreateAgentInput = {
    name: template.id === 'blank' ? (zh ? '未命名 Agent' : 'Untitled agent') : template.title,
    description: template.body,
    model: 'claude-sonnet-4-6',
    system: templateSystem(template, locale),
    mcp_servers: [],
    tools: [createAgentToolset()],
    skills: [],
    metadata: { template: template.slug },
  };
  const config = cloneCreateAgentInput(table[template.id] ?? fallbackConfig);
  const trimmedDescription = descriptionOverride?.trim();

  if (trimmedDescription) {
    config.description = trimmedDescription;
    config.metadata = { ...(config.metadata ?? {}), source: 'description' };
  }

  return config;
}

export function quickstartBuildAgentConfigInput(
  input: Record<string, unknown>,
  fallback: CreateAgentInput,
): CreateAgentInput {
  const rawConfig = toRecord(input.config) ?? input;
  const name = typeof rawConfig.name === 'string' && rawConfig.name.trim() ? rawConfig.name.trim() : fallback.name;
  const description =
    typeof rawConfig.description === 'string'
      ? rawConfig.description
      : rawConfig.description === null
        ? null
        : (fallback.description ?? null);
  const model = quickstartModelInput(rawConfig.model, fallback.model);
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
  signal,
  locale = 'en',
}: {
  orgUuid: string;
  workspaceId: string;
  description: string;
  currentConfig: CreateAgentInput;
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
          generatedConfig = quickstartBuildAgentConfigInput(input, currentConfig);
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

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
import { type AgentTemplate, type TemplateTag } from './types';

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

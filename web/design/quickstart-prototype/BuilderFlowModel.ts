import { z } from 'zod';
import { emptyResources, resourcesSchema, type PrototypeResources } from './PrototypeResources';

export type Screen = 'start' | 'agent' | 'environment' | 'test' | 'integrate';
export type Template = { id: string; name: string; system: string; sample: string; reply: string };
export const reviewStorageKey = 'oma.prototype.quickstart.builder.v2';
const phaseSchema = z.enum([
  'describe',
  'draft',
  'refine',
  'next-environment',
  'choose-environment',
  'network',
  'hosts',
  'confirm-environment',
  'choose-vault',
  'authorize',
  'ready',
  'testing',
  'review-test',
  'next-use',
  'schedule',
  'confirm-deployment',
  'integrate',
]);
const entrySchema = z.object({
  role: z.enum(['user', 'assistant', 'resource']),
  text: z.string(),
  stage: z.enum(['agent', 'environment', 'test', 'integrate']),
  detail: z.string().optional(),
});
const snapshotSchema = z.object({
  version: z.literal(3),
  resources: resourcesSchema.default(emptyResources),
  savedConfig: z
    .object({ name: z.string(), system: z.string(), model: z.string(), resources: resourcesSchema })
    .nullable()
    .default(null),
  afterSave: z.enum(['environment', 'ready', 'rerun', 'continue']).default('environment'),
  scheduleWanted: z.boolean().default(false),
  scheduleOptedIn: z.boolean().default(false),
  scheduleChoice: z.string().default('daily'),
  scheduleText: z.string().default(''),
  timezone: z.string().default('Asia/Shanghai'),
  deploymentCreated: z.boolean().default(false),
  deploymentSkipped: z.boolean().default(false),
  deploymentMessage: z.string().default(''),
  vaultChoice: z.string().default('new'),
  fixChoices: z.array(z.string()).default([]),
  fixesApplied: z.boolean().default(false),
  nextChoice: z.string().default('application'),
  agentDirty: z.boolean().default(false),
  phase: phaseSchema,
  name: z.string(),
  system: z.string(),
  model: z.string(),
  sample: z.string(),
  reply: z.string(),
  templateId: z.string(),
  agentVersion: z.number().int().nonnegative(),
  entries: z.array(entrySchema),
  environment: z.string(),
  environmentCreated: z.boolean(),
  network: z.enum(['unrestricted', 'limited']),
  hosts: z.string(),
  environmentChoice: z.string(),
  networkChoice: z.string(),
  outputChoice: z.string(),
  customAnswer: z.string(),
  testEntries: z.array(z.object({ role: z.enum(['user', 'assistant']), text: z.string() })),
  testStatus: z.enum(['idle', 'running', 'complete', 'stopped', 'error']),
  testOpen: z.boolean(),
  testClosed: z.boolean(),
});
export type Snapshot = z.infer<typeof snapshotSchema>;
export type Entry = z.infer<typeof entrySchema>;
export type Phase = z.infer<typeof phaseSchema>;

export function clearBuilderReview() {
  try {
    sessionStorage.removeItem(reviewStorageKey);
  } catch {}
}
export function hasBuilderReview() {
  return readBuilderReview() !== null;
}
export function readBuilderReview(): Snapshot | null {
  try {
    const saved = JSON.parse(sessionStorage.getItem(reviewStorageKey) ?? 'null');
    if (saved?.version === 2) {
      saved.version = 3;
      saved.resources = emptyResources;
      const obsoleteMessages = new Set([
        '技能已加入配置，保存智能体后生效。保险库用于下一次会话。',
        '已更新技能或工具配置，保存智能体后生效。保险库用于下一次会话。',
        '已更新保险库配置，用于下一次会话。',
      ]);
      if (Array.isArray(saved.entries))
        saved.entries = saved.entries.filter(
          (item: Entry) => item.role !== 'assistant' || !obsoleteMessages.has(item.text),
        );
    }
    const parsed = snapshotSchema.safeParse(saved);
    if (!parsed.success) return null;
    if (!parsed.data.savedConfig && parsed.data.agentVersion > 0 && !parsed.data.agentDirty)
      parsed.data.savedConfig = agentConfig(parsed.data);
    const copyUpdates: Record<string, string> = {
      '在右侧发送第一条消息。测试对话和这里的构建对话分别保留。': '发送第一条测试消息。',
      '第一条消息已发送。可以继续测试，完成后结束本次试跑。': '测试已开始。',
      '创建会话、监听事件，再发送消息。右侧是对应的调用示例。': '使用以下代码接入应用。',
    };
    parsed.data.entries = parsed.data.entries.map((item) =>
      item.role === 'assistant' && copyUpdates[item.text] ? { ...item, text: copyUpdates[item.text] } : item,
    );
    return parsed.data.testStatus === 'running'
      ? {
          ...parsed.data,
          testStatus: 'stopped',
          testEntries: [
            ...parsed.data.testEntries,
            { role: 'assistant', text: '页面刷新，演示输出已停止。可以重新发送消息。' },
          ],
        }
      : parsed.data;
  } catch {
    return null;
  }
}
export function entry(role: Entry['role'], text: string, stage: Entry['stage'], detail?: string): Entry {
  return { role, text, stage, ...(detail ? { detail } : {}) };
}
export function screenForPhase(phase: Phase): Exclude<Screen, 'start'> {
  if (['choose-environment', 'network', 'hosts', 'confirm-environment', 'choose-vault', 'authorize'].includes(phase))
    return 'environment';
  if (['ready', 'testing', 'review-test', 'next-use'].includes(phase)) return 'test';
  return ['schedule', 'confirm-deployment', 'integrate'].includes(phase) ? 'integrate' : 'agent';
}
export function newSnapshot(template: Template, description: string): Snapshot {
  const snapshot: Snapshot = {
    version: 3,
    resources: { ...emptyResources, slackEnabled: /slack/i.test(description) },
    savedConfig: null,
    afterSave: 'environment',
    scheduleWanted: /每天|每周|每个?工作日|每小时|定时|daily|weekly/i.test(description),
    scheduleOptedIn: /每天|每周|每个?工作日|每小时|定时|daily|weekly/i.test(description),
    scheduleChoice: /每天|每周|每个?工作日|每小时|daily|weekly/i.test(description) ? 'custom' : 'daily',
    scheduleText: description.match(/(?:每天|每周|每个?工作日|每小时)[^，。；,;]*/)?.[0] ?? '',
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    deploymentCreated: false,
    deploymentSkipped: false,
    deploymentMessage: template.sample,
    vaultChoice: 'new',
    fixChoices: [],
    fixesApplied: false,
    nextChoice: 'application',
    agentDirty: false,
    phase: !template.system && !description ? 'describe' : description ? 'draft' : 'next-environment',
    name: description ? '自定义助手' : template.name,
    system: description
      ? description.replace(/(?:每天|每周)(?:上午|下午|早上|晚上)?\d{1,2}[点时](?:\d{1,2}分)?(?:执行|运行)?[，,]?/, '')
      : template.system,
    model: 'DeepSeek V3.2',
    sample: /slack/i.test(description) ? '整理本周客户问题，先生成摘要草稿。' : template.sample,
    reply: /slack/i.test(description) ? '已整理客户问题摘要，发送前需要你的确认。' : template.reply,
    templateId: /slack/i.test(description) ? 'support' : template.id,
    agentVersion: !description && template.system ? 1 : 0,
    entries:
      !template.system && !description
        ? []
        : [
            entry('user', description || `帮我创建一个${template.name}助手。`, 'agent'),
            ...(!description
              ? [
                  entry('resource', template.name, 'agent', '已创建 v1'),
                  entry('assistant', '智能体已创建，可以继续调整。', 'agent'),
                ]
              : [entry('assistant', '初版配置已生成。', 'agent')]),
          ],
    environment: '',
    environmentCreated: false,
    network: 'unrestricted',
    hosts: '',
    environmentChoice: 'Default',
    networkChoice: template.id === 'analysis' || template.id === 'extract' ? 'offline' : 'unrestricted',
    outputChoice: 'file',
    customAnswer: '',
    testEntries: [],
    testStatus: 'idle',
    testOpen: false,
    testClosed: false,
  };
  return { ...snapshot, savedConfig: snapshot.agentVersion > 0 ? agentConfig(snapshot) : null };
}
export function previewTo(state: Snapshot, target: Screen): Snapshot {
  const entries = [...state.entries];
  let next = { ...state, entries };
  if (target === 'agent') return next;
  if (next.agentVersion === 0) {
    entries.push(entry('resource', next.name, 'agent', '已创建 v1'));
    next = { ...next, agentVersion: 1 };
  }
  if (target === 'environment') return { ...next, phase: next.environmentCreated ? 'ready' : 'choose-environment' };
  if (!next.environmentCreated) {
    entries.push(
      entry('assistant', '使用哪个运行环境？', 'environment'),
      entry('user', '使用 Default。', 'environment'),
      entry('resource', 'Default', 'environment', '已选择'),
    );
    next = { ...next, environment: 'Default', environmentCreated: true };
  }
  if (target === 'test') return { ...next, phase: 'ready' };
  if (!entries.some((item) => item.stage === 'test'))
    entries.push(entry('assistant', '智能体和环境已就绪，可以在独立会话中测试。', 'test'));
  entries.push(entry('assistant', '通过 API 创建会话，接入你的应用。', 'integrate'));
  return { ...next, phase: 'integrate', testOpen: false };
}

export function agentConfig(state: Pick<Snapshot, 'name' | 'system' | 'model' | 'resources'>) {
  return { name: state.name, system: state.system, model: state.model, resources: state.resources };
}
export function configurationChanges(state: Snapshot) {
  const saved = state.savedConfig;
  const pairs = [
    ['名称', saved?.name, state.name],
    ['模型', saved?.model, state.model],
    ['指令', saved?.system, state.system],
  ];
  const changes = pairs
    .filter(([, before, after]) => !saved || before !== after)
    .map(([label, before, after]) => ({ label: label ?? '', before: before ?? '', after: after ?? '' }));
  if (
    (!saved && state.resources.slackEnabled) ||
    (saved && saved.resources.slackEnabled !== state.resources.slackEnabled)
  )
    changes.push({
      label: '工具',
      before: saved?.resources.slackEnabled ? 'Slack' : '无',
      after: state.resources.slackEnabled ? 'Slack' : '无',
    });
  if (saved && JSON.stringify(saved.resources.skills) !== JSON.stringify(state.resources.skills))
    changes.push({
      label: '技能',
      before: saved.resources.skills.join(', '),
      after: state.resources.skills.join(', '),
    });
  return changes;
}
export function scheduleLabel(choice: string, text = '') {
  if (choice === 'custom')
    return (
      text.match(
        /(?:每个?工作日|每天|每周[一二三四五六日天]?|每小时)(?:上午|下午|早上|晚上)?(?:\d{1,2}(?:[：:]\d{1,2}|[点时](?:\d{1,2}分?)?))?/,
      )?.[0] ?? text
    );
  return (
    ({ daily: '每天 09:00', weekdays: '每个工作日 09:00', weekly: '每周一 09:00' } as Record<string, string>)[choice] ??
    '每天 09:00'
  );
}
export function scheduleCron(choice: string, text = '') {
  if (choice !== 'custom')
    return choice === 'weekly' ? '0 9 * * 1' : choice === 'weekdays' ? '0 9 * * 1-5' : '0 9 * * *';
  const time = text.match(/(\d{1,2})(?:[：:](\d{1,2})|[点时](?:(\d{1,2})分?)?)/);
  let hour = Number(time?.[1] ?? 9);
  const minute = Number(time?.[2] ?? time?.[3] ?? 0);
  if (/下午|晚上/.test(text) && hour < 12) hour += 12;
  if (hour > 23 || minute > 59) return null;
  if (/每小时|hourly/i.test(text)) return '0 * * * *';
  if (/工作日/.test(text)) return `${minute} ${hour} * * 1-5`;
  if (/每周|weekly/i.test(text)) {
    const day = text.match(/周([一二三四五六日天])/)?.[1] ?? '一';
    const weekday = ({ 一: 1, 二: 2, 三: 3, 四: 4, 五: 5, 六: 6, 日: 0, 天: 0 } as Record<string, number>)[day];
    return `${minute} ${hour} * * ${weekday}`;
  }
  if (/每天|daily/i.test(text)) return `${minute} ${hour} * * *`;
  return null;
}
export function proposedChange(text: string, state: Snapshot): Partial<Snapshot> | null {
  const rename = text.match(/(?:名称|名字|智能体)(?:改为|改成|叫|换成|命名为)[：:\s]*[“"]?([^”"。\n]+)/);
  if (rename?.[1]) return { name: rename[1].trim() };
  const model = text.match(/模型(?:改为|改成|换成|使用)[：:\s]*(.+)/);
  if (model?.[1]) return { model: model[1].trim() };
  if (/slack/i.test(text))
    return { resources: { ...state.resources, slackEnabled: !/移除|不要|不用|取消/.test(text) } };
  if (/文档处理|表格分析/.test(text) && /技能|skill/i.test(text)) {
    const id: PrototypeResources['skills'][number] = /文档处理/.test(text) ? 'documents' : 'spreadsheet';
    const skills: PrototypeResources['skills'] = /移除|不要|不用/.test(text)
      ? state.resources.skills.filter((value) => value !== id)
      : [...new Set([...state.resources.skills, id])];
    return { resources: { ...state.resources, skills } };
  }
  if (/输出|回复|指令|不要|确保|遇到|增加|添加|只|限制|根据/.test(text)) return { system: `${state.system}\n${text}` };
  return null;
}
export function reviewIssues(state: Snapshot) {
  if (state.resources.slackEnabled && state.resources.credentialSkipped && !state.fixesApplied)
    return [{ id: 'draft-fallback', label: 'Slack 未授权时只生成草稿' }];
  if (state.testStatus !== 'complete') return [];
  return !state.fixesApplied &&
    state.testEntries.some((item) => item.role === 'assistant' && item.text.includes('"quantity": null'))
    ? [{ id: 'missing-fields', label: '缺失字段返回 null，并列出缺失项' }]
    : [];
}
export function testReply(state: Snapshot, text: string) {
  if (state.resources.slackEnabled && state.resources.credentialSkipped)
    return 'Slack 未授权，未执行外发。已生成回复草稿。';
  if (state.templateId === 'extract' && /未提供|缺失|没有数量/.test(text))
    return JSON.stringify(
      {
        customer: '星河公司',
        product: '打印机',
        quantity: null,
        ...(state.fixesApplied ? { missing_fields: ['quantity'] } : {}),
      },
      null,
      2,
    );
  return state.reply;
}

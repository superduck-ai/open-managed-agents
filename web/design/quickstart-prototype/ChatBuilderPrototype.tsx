import { useEffect, useRef, useState, type FormEvent } from 'react';
import {
  ArrowDownToLine,
  ArrowRight,
  ArrowUp,
  Bot,
  Check,
  ChevronRight,
  FileJson2,
  Globe,
  Layers,
  ShieldCheck,
  Play,
  Sparkles,
  Square,
  Terminal,
  X,
} from 'lucide-react';
import { Button } from '@/shared/ui/button';
import { Input } from '@/shared/ui/input';
import { Textarea } from '@/shared/ui/textarea';
import { RadioGroup, RadioGroupItem } from '@/shared/ui/radio-group';
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/shared/ui/sheet';
import { Tabs, TabsList, TabsTrigger } from '@/shared/ui/tabs';
import { CodeBlock } from './PrototypeCodeBlock';
import { Card } from '@/shared/ui/card';
import {
  AgentConfigEditor,
  ConfigProposal,
  CredentialCard,
  TestReviewCard,
  DeploymentConfirmation,
  DeploymentDetails,
} from './BuilderFlowCards';
import { agentResourceFields, needsCredential, vaultReferences } from './PrototypeResources';

import {
  readBuilderReview,
  reviewStorageKey,
  entry,
  newSnapshot,
  previewTo,
  screenForPhase,
  agentConfig,
  proposedChange,
  reviewIssues,
  scheduleLabel,
  scheduleCron,
  testReply,
  type Screen,
  type Template,
  type Snapshot,
  type Entry,
  type Phase,
} from './BuilderFlowModel';
export { clearBuilderReview, hasBuilderReview } from './BuilderFlowModel';

function Question({
  title,
  value,
  options,
  custom,
  onCustom,
  onChange,
  onConfirm,
}: {
  title: string;
  value: string;
  options: { value: string; label: string; hint?: string }[];
  custom?: string;
  onCustom?: (value: string) => void;
  onChange: (value: string) => void;
  onConfirm: () => void;
}) {
  return (
    <div className="conversation-question">
      <h2>{title}</h2>
      <RadioGroup
        value={value}
        onValueChange={(next) => onChange(String(next))}
        aria-label={title}
        className="conversation-choices"
      >
        {options.map((option) => (
          <label key={option.value} className={`conversation-choice ${option.value === value ? 'selected' : ''}`}>
            <RadioGroupItem value={option.value} />
            <span>
              {option.label}
              {option.hint && <small>{option.hint}</small>}
            </span>
          </label>
        ))}
      </RadioGroup>
      {value === 'other' && onCustom && (
        <Input
          aria-label="自己的回答"
          placeholder="写下你的回答"
          value={custom}
          onChange={(event) => onCustom(event.target.value)}
        />
      )}
      <div className="question-actions">
        <Button onClick={onConfirm} disabled={value === 'other' && !custom?.trim()}>
          继续
          <ArrowRight />
        </Button>
      </div>
    </div>
  );
}

export function ChatBuilderPrototype({
  template,
  description,
  fresh,
  initialScreen,
  review,
  onScreenChange,
  onNameChange,
  dark,
}: {
  template: Template;
  description: string;
  fresh: boolean;
  initialScreen: Screen;
  review: { screen: Screen; serial: number; action?: 'config' | 'error' | 'approval' } | null;
  onScreenChange: (screen: Screen) => void;
  onNameChange: (name: string) => void;
  dark: boolean;
}) {
  const [state, setState] = useState<Snapshot>(
    () => (!fresh && readBuilderReview()) || previewTo(newSnapshot(template, description), initialScreen),
  );
  const [message, setMessage] = useState('');
  const [testInput, setTestInput] = useState(state.sample);
  const [editor, setEditor] = useState(false);
  const [vaultDetails, setVaultDetails] = useState(false);
  const [deploymentDetails, setDeploymentDetails] = useState(false);
  const [mobileInspector, setMobileInspector] = useState(false);
  const [sideView, setSideView] = useState<'config' | 'test' | 'code'>(
    state.phase === 'integrate' ? 'code' : state.testOpen ? 'test' : 'config',
  );
  const [error, setError] = useState(false);
  const [storageAvailable, setStorageAvailable] = useState(true);
  const [codeLanguage, setCodeLanguage] = useState('bash');
  const [approval, setApproval] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const end = useRef<HTMLDivElement>(null);
  const lastReview = useRef(0);
  const chatViewport = useRef<HTMLDivElement>(null);
  const following = useRef(true);
  const stage = screenForPhase(state.phase);
  const progress = ['创建智能体', '配置环境', '开始会话', ...(state.scheduleWanted ? ['定时部署'] : []), '接入'];
  const phaseIndex = ['schedule', 'confirm-deployment'].includes(state.phase)
    ? 3
    : stage === 'integrate'
      ? progress.length - 1
      : ['agent', 'environment', 'test'].indexOf(stage);

  useEffect(() => {
    try {
      sessionStorage.setItem(reviewStorageKey, JSON.stringify(state));
      setStorageAvailable(true);
    } catch {
      setStorageAvailable(false);
    }
  }, [state]);
  useEffect(() => {
    onScreenChange(screenForPhase(state.phase));
    onNameChange(state.name);
  }, [state.phase, state.name, onScreenChange, onNameChange]);
  useEffect(() => {
    if (following.current) end.current?.scrollIntoView({ block: 'nearest', behavior: 'instant' });
  }, [state.entries.length, state.phase]);
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );
  useEffect(() => {
    if (!review || review.serial === lastReview.current) return;
    lastReview.current = review.serial;
    if (review.action === 'config') {
      setMobileInspector(true);
      setSideView('config');
      return;
    }
    if (review.action === 'error') {
      if (['authorize', 'confirm-deployment'].includes(state.phase)) {
        setError(true);
        return;
      }
      setState((previous) => ({
        ...previewTo(previous, 'environment'),
        phase: 'confirm-environment',
        environment: `${previous.name}环境`,
      }));
      setError(true);
      setSideView('config');
      return;
    }
    if (review.action === 'approval') {
      setApproval(true);
      setState((previous) => ({
        ...previewTo(previous, 'test'),
        phase: 'testing',
        testOpen: true,
        testClosed: false,
        testStatus: 'idle',
      }));
      setTestInput('把结果写入 summary.json');
      setSideView('test');
      return;
    }

    const anchor = chatViewport.current?.querySelector(`[data-chat-stage="${review.screen}"]`);
    if (anchor) {
      anchor.scrollIntoView({ block: 'start' });
      return;
    }
    if (review.screen === 'start') return;
    following.current = true;
    setState((previous) => previewTo(previous, review.screen));
    setSideView(review.screen === 'integrate' ? 'code' : 'config');
  }, [review]);

  function update(changes: Partial<Snapshot>) {
    setState((previous) => ({ ...previous, ...changes }));
  }
  function advance(phase: Phase, entries: Entry[], changes: Partial<Snapshot> = {}) {
    following.current = true;
    setState((previous) => ({ ...previous, ...changes, phase, entries: [...previous.entries, ...entries] }));
  }
  function afterEnvironment(resources = state.resources): Phase {
    return needsCredential(resources) ? (resources.vault === 'none' ? 'choose-vault' : 'authorize') : 'ready';
  }
  function proposeConfig(changes: Partial<Snapshot>, text: string) {
    advance('draft', [entry('user', text, stage), entry('assistant', '配置修改已准备好。', 'agent')], {
      savedConfig: state.savedConfig ?? (state.agentVersion > 0 ? agentConfig(state) : null),
      ...changes,
      agentDirty: true,
      afterSave: state.environmentCreated ? 'ready' : 'environment',
    });
    setSideView('config');
  }
  function saveAgent() {
    const version = state.agentVersion + 1;
    const phase =
      state.afterSave === 'continue'
        ? state.scheduleWanted
          ? 'schedule'
          : 'next-use'
        : state.afterSave === 'rerun' && !needsCredential(state.resources)
          ? 'testing'
          : state.environmentCreated
            ? afterEnvironment()
            : 'next-environment';
    const retry = phase === 'testing';
    advance(
      phase,
      [
        entry('user', version === 1 ? '创建这个智能体。' : `保存为 v${version}。`, 'agent'),
        entry('resource', state.name, 'agent', `${version === 1 ? '已创建' : '已保存'} v${version}`),
        ...(retry ? [entry('resource', '测试会话', 'test', '已创建')] : []),
      ],
      {
        agentVersion: version,
        agentDirty: false,
        savedConfig: agentConfig(state),
        ...(retry ? { testOpen: true, testClosed: false, testEntries: [], testStatus: 'idle' as const } : {}),
      },
    );
    if (retry) {
      setSideView('test');
      setTestInput(state.sample);
    }
  }
  function chooseVault() {
    const ready = state.vaultChoice === 'covered';
    const resources = {
      ...state.resources,
      vault: 'integrations' as const,
      credentialReady: ready,
      credentialSkipped: false,
    };
    advance(
      ready ? 'ready' : 'authorize',
      [
        entry('user', state.vaultChoice === 'new' ? '创建保险库。' : '使用已有保险库。', 'environment'),
        entry('resource', '工作区集成', 'environment', state.vaultChoice === 'new' ? '已创建' : '已选择'),
      ],
      { resources },
    );
    setError(false);
  }
  function resolveCredential(result: 'connected' | 'skipped', method?: string) {
    advance(
      'ready',
      [
        entry(
          'assistant',
          result === 'connected' ? `Slack 凭据已通过${method}配置。` : '已跳过 Slack 授权；试跑中无法使用 Slack。',
          'environment',
        ),
      ],
      {
        resources: {
          ...state.resources,
          credentialReady: result === 'connected',
          credentialSkipped: result === 'skipped',
        },
      },
    );
    setError(false);
  }
  function nextAfterTest() {
    advance(state.scheduleWanted ? 'schedule' : 'next-use', []);
    setSideView('config');
  }
  function resolveReview() {
    const fixes = state.fixChoices.filter((value) => !['rerun', 'continue'].includes(value));
    if (fixes.length) {
      const instruction = fixes.includes('draft-fallback')
        ? 'Slack 未授权时仅生成草稿，不执行外发。'
        : '缺失字段返回 null，并在 missing_fields 中列出字段名。';
      advance(
        'draft',
        [
          entry(
            'user',
            reviewIssues(state)
              .filter((issue) => fixes.includes(issue.id))
              .map((issue) => issue.label)
              .join('；'),
            'test',
          ),
          entry('assistant', '修改已准备好，保存为新版本后生效。', 'agent'),
        ],
        {
          savedConfig: state.savedConfig ?? agentConfig(state),
          system: `${state.system}\n${instruction}`,
          agentDirty: true,
          fixesApplied: true,
          afterSave: state.fixChoices.includes('continue') ? 'continue' : 'rerun',
          fixChoices: [],
        },
      );
      setSideView('config');
      return;
    }
    if (state.fixChoices.includes('rerun')) beginSession();
    else nextAfterTest();
  }
  function confirmSchedule() {
    if (state.scheduleChoice === 'skip') {
      advance('integrate', [entry('user', '暂时跳过定时部署。', 'integrate')], {
        deploymentSkipped: true,
        scheduleWanted: false,
      });
      setSideView('code');
      return;
    }
    const choice = state.scheduleChoice === 'other' ? 'custom' : state.scheduleChoice;
    if (!scheduleCron(choice, state.scheduleText)) {
      setError(true);
      return;
    }
    advance('confirm-deployment', [entry('user', scheduleLabel(choice, state.scheduleText), 'integrate')], {
      scheduleChoice: choice,
      deploymentMessage: state.testEntries.find((item) => item.role === 'user')?.text ?? state.sample,
    });
    setError(false);
  }
  function createDeployment() {
    setError(false);
    advance(
      'integrate',
      [
        entry('user', '创建定时部署。', 'integrate'),
        entry('resource', `${state.name}定时运行`, 'integrate', '已创建'),
        entry('assistant', '部署已创建。每次定时运行都会开始一个新的会话。', 'integrate'),
      ],
      { deploymentCreated: true },
    );
    setSideView('code');
  }
  function chooseEnvironment() {
    const value = state.environmentChoice;
    if (value === 'new') {
      advance('network', [
        entry('assistant', '使用哪个运行环境？', 'environment'),
        entry('user', '新建一个环境。', 'environment'),
        entry('assistant', '这个智能体需要怎样访问网络？', 'environment'),
      ]);
      return;
    }
    advance(
      afterEnvironment(),
      [
        entry('assistant', '使用哪个运行环境？', 'environment'),
        entry('user', `使用 ${value}。`, 'environment'),
        entry('resource', value, 'environment', '已选择'),
        entry(
          'assistant',
          needsCredential(state.resources) ? 'Slack 需要授权后才能调用。' : '可以开始试跑。',
          needsCredential(state.resources) ? 'environment' : 'test',
        ),
      ],
      { environment: value, environmentCreated: true },
    );
  }
  function chooseNetwork() {
    const choice = state.networkChoice;
    const label =
      choice === 'unrestricted' ? '允许访问互联网。' : choice === 'offline' ? '不需要联网。' : '只访问指定网站。';
    advance(
      choice === 'limited' ? 'hosts' : 'confirm-environment',
      [
        entry('user', label, 'environment'),
        ...(choice === 'limited'
          ? [entry('assistant', '哪些网站需要允许访问？', 'environment')]
          : [entry('assistant', '环境配置已准备好，确认后创建。', 'environment')]),
      ],
      {
        network: choice === 'unrestricted' ? 'unrestricted' : 'limited',
        hosts: choice === 'offline' ? '' : state.hosts,
        environment: `${state.name}环境`,
      },
    );
  }
  function confirmHosts(value: string) {
    advance(
      'confirm-environment',
      [entry('user', value, 'environment'), entry('assistant', '环境配置已准备好，确认后创建。', 'environment')],
      { hosts: value },
    );
  }
  function createEnvironment() {
    if (error) return;
    advance(
      afterEnvironment(),
      [
        entry('user', '创建这个环境。', 'environment'),
        entry('resource', state.environment, 'environment', '已创建'),
        entry(
          'assistant',
          needsCredential(state.resources) ? '环境已创建。接下来授权 Slack。' : '环境已创建，可以开始试跑。',
          needsCredential(state.resources) ? 'environment' : 'test',
        ),
      ],
      { environmentCreated: true },
    );
  }
  function beginSession() {
    if (state.agentDirty || needsCredential(state.resources)) {
      if (state.agentDirty) setEditor(true);
      else advance(afterEnvironment(), []);
      return;
    }
    advance(
      'testing',
      [
        entry('user', '开始会话。', 'test'),
        entry('resource', '测试会话', 'test', '已创建'),
        entry('assistant', '发送第一条测试消息。', 'test'),
      ],
      { testOpen: true, testClosed: false, testStatus: 'idle', testEntries: [] },
    );
    setSideView('test');
    setTestInput(state.sample);
  }
  function endSession() {
    if (timer.current) clearTimeout(timer.current);
    const summary = state.resources.credentialSkipped
      ? 'Slack 未授权，本次仅生成草稿，没有执行外发。'
      : state.testStatus === 'complete'
        ? '本次回复已完成。'
        : '本次试跑尚未获得完整回复。';
    advance('review-test', [entry('user', '结束本次试跑。', 'test'), entry('assistant', summary, 'test')], {
      testClosed: true,
      testStatus: state.testStatus === 'running' ? 'stopped' : state.testStatus,
      fixChoices: [],
    });
    setSideView('config');
  }
  function integrate() {
    advance('integrate', [
      entry('user', '接入应用。', 'integrate'),
      entry('assistant', '使用以下代码接入应用。', 'integrate'),
    ]);
    setSideView('code');
  }
  function submitChat(event: FormEvent) {
    event.preventDefault();
    const text = message.trim();
    if (!text) return;
    setMessage('');
    if (state.phase === 'describe') {
      advance(
        'draft',
        [entry('user', text, 'agent'), entry('assistant', '初版配置已生成，你可以查看指令或继续调整。', 'agent')],
        {
          name: '自定义助手',
          system: text,
          resources: { ...state.resources, slackEnabled: /slack/i.test(text) },
          scheduleWanted: /每天|每周|定时/.test(text),
          scheduleOptedIn: /每天|每周|定时/.test(text),
          scheduleText: text.match(/(?:每天|每周)[^，。；,;]*/)?.[0] ?? '',
        },
      );
      return;
    }
    if (state.phase === 'network') {
      update({ customAnswer: text });
      if (/不.*联|离线|不.*网/.test(text)) {
        advance(
          'confirm-environment',
          [entry('user', text, 'environment'), entry('assistant', '按离线环境准备，确认后创建。', 'environment')],
          { network: 'limited', networkChoice: 'offline', hosts: '', environment: `${state.name}环境` },
        );
      } else if (/指定|只.*访问|仅/.test(text)) {
        advance(
          'hosts',
          [entry('user', text, 'environment'), entry('assistant', '哪些网站需要允许访问？', 'environment')],
          { network: 'limited', networkChoice: 'limited', environment: `${state.name}环境` },
        );
      } else {
        advance(
          'confirm-environment',
          [entry('user', text, 'environment'), entry('assistant', '按可联网环境准备，确认后创建。', 'environment')],
          { network: 'unrestricted', networkChoice: 'unrestricted', environment: `${state.name}环境` },
        );
      }
      return;
    }
    if (state.phase === 'hosts') {
      confirmHosts(text);
      return;
    }
    if (state.phase === 'choose-environment' && /新建/.test(text)) {
      advance('network', [
        entry('user', text, 'environment'),
        entry('assistant', '这个智能体需要怎样访问网络？', 'environment'),
      ]);
      return;
    }
    if (state.phase === 'confirm-environment' && /^(环境)?(名字|名称)?(改为|叫|改成)/.test(text)) {
      advance(
        'confirm-environment',
        [entry('user', text, 'environment'), entry('assistant', '已更新环境名称，等待你确认创建。', 'environment')],
        { environment: text.replace(/^(环境)?(名字|名称)?(改为|叫|改成)[：:]?/, '').trim() || state.environment },
      );
      return;
    }
    if (/取消.*(定时|部署)|不要.*(定时|部署)/.test(text)) {
      advance(
        state.phase === 'schedule' || state.phase === 'confirm-deployment' ? 'next-use' : state.phase,
        [entry('user', text, stage), entry('assistant', '已取消定时运行计划。', stage)],
        { scheduleWanted: false, scheduleOptedIn: false },
      );
      return;
    }
    if (['schedule', 'confirm-deployment'].includes(state.phase)) {
      if (/初始消息/.test(text)) update({ deploymentMessage: text.replace(/^.*初始消息(?:改为|改成|是|：|:)?/, '') });
      else if (/时区/.test(text)) update({ timezone: text.replace(/^.*时区(?:改为|改成|是|：|:)?/, '').trim() });
      else advance('schedule', [entry('user', text, 'integrate')], { scheduleChoice: 'custom', scheduleText: text });
      return;
    }
    if (/每天|每周|定时|每小时|daily|weekly/i.test(text)) {
      advance(state.phase, [entry('user', text, stage), entry('assistant', '先创建并试跑，再设置定时部署。', stage)], {
        scheduleWanted: true,
        scheduleOptedIn: true,
        scheduleText: text,
        scheduleChoice: 'custom',
      });
      return;
    }
    if (/安装|软件包/.test(text)) {
      advance(state.phase, [entry('user', text, stage), entry('assistant', '需要安装哪个软件包？', 'environment')]);
      return;
    }
    if (/重新授权|配置凭据|授权.*Slack/i.test(text) && state.resources.slackEnabled && state.environmentCreated) {
      advance('authorize', [entry('user', text, stage)], {
        resources: { ...state.resources, credentialSkipped: false, credentialReady: false },
      });
      setError(false);
      setSideView('config');
      return;
    }
    if (/查看|现在.*配置|当前.*配置/.test(text)) {
      advance(state.phase, [entry('user', text, stage)]);
      setEditor(true);
      return;
    }
    const change = proposedChange(text, state);
    if (change) {
      proposeConfig(change, text);
      return;
    }
    advance(state.phase, [entry('user', text, stage), entry('assistant', '希望修改哪一项配置？', stage)]);
  }
  function finishTest(text: string) {
    update({ testStatus: 'complete', testEntries: [...state.testEntries, { role: 'assistant', text }] });
    setApproval(false);
  }
  function sendTest(event: FormEvent) {
    event.preventDefault();
    if (!testInput.trim() || state.testClosed || state.testStatus === 'running') return;
    const text = testInput;
    const testEntries = [...state.testEntries, { role: 'user' as const, text }];
    setTestInput('');
    update({ testEntries, testStatus: 'running' });
    if (!state.testEntries.length)
      advance('testing', [entry('assistant', '测试已开始。', 'test')], {
        testEntries,
        testStatus: 'running',
      });
    if (approval) return;
    timer.current = setTimeout(
      () =>
        setState((previous) => ({
          ...previous,
          testStatus: 'complete',
          testEntries: [...previous.testEntries, { role: 'assistant', text: testReply(previous, text) }],
        })),
      700,
    );
  }
  function stopTurn() {
    if (timer.current) clearTimeout(timer.current);
    update({
      testStatus: 'stopped',
      testEntries: [...state.testEntries, { role: 'assistant', text: '已停止本轮输出。' }],
    });
    setApproval(false);
  }
  function download(filename: string, content: string) {
    const url = URL.createObjectURL(new Blob([content], { type: 'application/json' }));
    const link = document.createElement('a');
    link.href = url;
    link.download = filename;
    link.click();
    URL.revokeObjectURL(url);
  }

  function activeCard() {
    if (state.phase === 'draft')
      return (
        <ConfigProposal
          state={state}
          onEdit={() => setEditor(true)}
          onSave={saveAgent}
          onRefine={() => {
            advance('refine', [entry('user', '继续修改。', 'agent')]);
          }}
        />
      );
    if (state.phase === 'refine')
      return (
        <div className="conversation-question">
          <h2>希望修改哪部分？</h2>
          <Button variant="ghost" onClick={() => setEditor(true)}>
            编辑配置
          </Button>
        </div>
      );
    if (state.phase === 'choose-vault')
      return (
        <Question
          title="使用哪个凭据保险库？"
          value={state.vaultChoice}
          onChange={(vaultChoice) => update({ vaultChoice })}
          options={[
            { value: 'new', label: '新建保险库' },
            { value: 'integrations', label: '工作区集成' },
            { value: 'covered', label: '已有 Slack 授权的保险库' },
          ]}
          onConfirm={chooseVault}
        />
      );
    if (state.phase === 'authorize') return <CredentialCard error={error} onResolve={resolveCredential} />;
    if (state.phase === 'next-environment')
      return (
        <Button onClick={() => advance('choose-environment', [])}>
          下一步：配置环境
          <ArrowRight />
        </Button>
      );
    if (state.phase === 'choose-environment')
      return (
        <Question
          title="使用哪个运行环境？"
          value={state.environmentChoice}
          onChange={(environmentChoice) => update({ environmentChoice })}
          options={[
            { value: 'Default', label: 'Default', hint: '允许访问互联网' },
            { value: '日常分析', label: '日常分析', hint: '仅限指定网站' },
            { value: 'new', label: '新建环境' },
          ]}
          onConfirm={chooseEnvironment}
        />
      );
    if (state.phase === 'network')
      return (
        <Question
          title="这个智能体需要怎样访问网络？"
          value={state.networkChoice}
          onChange={(networkChoice) => update({ networkChoice })}
          options={[
            { value: 'unrestricted', label: '允许访问互联网' },
            { value: 'limited', label: '只访问指定网站' },
            { value: 'offline', label: '不需要联网' },
          ]}
          onConfirm={chooseNetwork}
        />
      );
    if (state.phase === 'hosts')
      return (
        <div className="conversation-question">
          <h2>哪些网站需要允许访问？</h2>
          <Input
            aria-label="允许的网站"
            placeholder="例如 docs.example.com, api.example.com"
            value={state.hosts}
            onChange={(event) => update({ hosts: event.target.value })}
          />
          <div className="question-actions">
            <Button onClick={() => confirmHosts(state.hosts)} disabled={!state.hosts.trim()}>
              继续
              <ArrowRight />
            </Button>
          </div>
        </div>
      );
    if (state.phase === 'confirm-environment')
      return (
        <div className="conversation-action">
          <div className="draft-heading">
            <span className="resource-icon">
              <Layers />
            </span>
            <span>{state.environment}</span>
          </div>
          <dl className="confirmation-facts">
            <div>
              <dt>网络</dt>
              <dd>{state.network === 'unrestricted' ? '允许访问互联网' : state.hosts ? '仅限指定网站' : '不联网'}</dd>
            </div>
            {state.hosts && (
              <div>
                <dt>网站</dt>
                <dd>{state.hosts}</dd>
              </div>
            )}
          </dl>
          {error && (
            <div role="alert" className="error-message">
              环境创建失败。配置和对话已保留。
            </div>
          )}
          <div className="question-actions">
            <Button
              variant="ghost"
              onClick={() => advance('network', [entry('user', '重新调整网络。', 'environment')])}
            >
              调整
            </Button>
            <Button
              onClick={() => {
                if (error) setError(false);
                else createEnvironment();
              }}
            >
              {error ? '重试' : '创建环境'}
              <ArrowRight />
            </Button>
          </div>
        </div>
      );
    if (state.phase === 'ready')
      return (
        <div className="conversation-action">
          <h2>开始一次会话</h2>
          {state.resources.credentialSkipped && <p className="resource-pending">Slack 尚未授权，本次无法调用。</p>}
          <p className="secondary">
            {state.name}
            <ChevronRight size={12} />
            {state.environment}
          </p>
          {(state.agentDirty || needsCredential(state.resources)) && (
            <p className="resource-pending">
              {state.agentDirty ? '先保存智能体的配置修改。' : '保险库中的 Slack 凭据尚未配置。'}
            </p>
          )}
          {!state.agentDirty && needsCredential(state.resources) && (
            <Button variant="outline" onClick={() => advance(afterEnvironment(), [])}>
              配置凭据
            </Button>
          )}
          {state.agentDirty && (
            <Button variant="outline" onClick={() => setEditor(true)}>
              保存智能体配置
            </Button>
          )}
          <Button onClick={beginSession} disabled={state.agentDirty || needsCredential(state.resources)}>
            <Play />
            开始会话
          </Button>
        </div>
      );
    if (state.phase === 'testing')
      return (
        <div className="conversation-actions">
          <Button variant="outline" onClick={endSession}>
            <Square />
            结束试跑
          </Button>
          <Button variant="ghost" onClick={() => setSideView('test')}>
            查看测试会话
            <ChevronRight />
          </Button>
        </div>
      );
    if (state.phase === 'review-test')
      return reviewIssues(state).length ? (
        <TestReviewCard state={state} onChoices={(fixChoices) => update({ fixChoices })} onContinue={resolveReview} />
      ) : (
        <div className="conversation-actions">
          <Button onClick={nextAfterTest}>
            继续
            <ArrowRight />
          </Button>
          <Button variant="ghost" onClick={beginSession}>
            原样重跑
          </Button>
        </div>
      );
    if (state.phase === 'next-use')
      return (
        <Question
          title="如何使用这个智能体？"
          value={state.nextChoice}
          onChange={(nextChoice) => update({ nextChoice })}
          options={[
            { value: 'schedule', label: '定时运行' },
            { value: 'application', label: '从应用调用' },
          ]}
          onConfirm={() => {
            if (state.nextChoice === 'schedule')
              advance('schedule', [entry('user', '定时运行。', 'integrate')], {
                scheduleWanted: true,
                scheduleOptedIn: true,
              });
            else integrate();
          }}
        />
      );
    if (state.phase === 'schedule')
      return (
        <div>
          <Question
            title="多久运行一次？"
            value={state.scheduleChoice}
            onChange={(scheduleChoice) => update({ scheduleChoice })}
            custom={state.scheduleText}
            onCustom={(scheduleText) => update({ scheduleText })}
            options={[
              ...(state.scheduleText ? [{ value: 'custom', label: scheduleLabel('custom', state.scheduleText) }] : []),
              ...(!state.scheduleText ? [{ value: 'daily', label: '每天 09:00' }] : []),
              { value: 'weekdays', label: '每个工作日 09:00' },
              { value: 'weekly', label: '每周一 09:00' },
              { value: 'other', label: '其他时间' },
              ...(!state.scheduleOptedIn ? [{ value: 'skip', label: '暂时跳过' }] : []),
            ]}
            onConfirm={confirmSchedule}
          />
          {error && (
            <p role="alert" className="error-message">
              请换一种周期描述，或选择上面的周期。
            </p>
          )}
        </div>
      );
    if (state.phase === 'confirm-deployment')
      return (
        <DeploymentConfirmation
          state={state}
          error={error}
          onAdjust={() => advance('schedule', [])}
          onCreate={createDeployment}
        />
      );
    if (state.phase === 'integrate')
      return (
        <div className="conversation-actions">
          <Button onClick={() => setSideView('code')}>
            查看调用代码
            <CodeIcon />
          </Button>
          <Button variant="ghost" onClick={() => setEditor(true)}>
            查看智能体
          </Button>
          <Button variant="ghost" onClick={beginSession}>
            再试一次
          </Button>
        </div>
      );
    if (state.phase === 'describe')
      return (
        <div className="conversation-empty">
          <h2>描述你的智能体</h2>
        </div>
      );
    return null;
  }
  const completedSteps = [
    state.agentVersion > 0 && !state.agentDirty,
    state.environmentCreated,
    state.testClosed,
    ...(state.scheduleWanted ? [state.deploymentCreated] : []),
    false,
  ];
  function inspector() {
    return (
      <aside className={`inspector conversation-inspector ${mobileInspector ? 'mobile-visible' : ''}`}>
        <div className="inspector-mobile-head">
          <span>配置</span>
          <Button variant="ghost" size="icon" aria-label="关闭配置" onClick={() => setMobileInspector(false)}>
            <X />
          </Button>
        </div>
        <Card className="progress-section">
          <h2>进度</h2>
          <ol className="step-list">
            {progress.map((label, index) => (
              <li
                key={label}
                aria-current={index === phaseIndex ? 'step' : undefined}
                data-state={index === phaseIndex ? 'current' : completedSteps[index] ? 'done' : 'next'}
              >
                <span className="step-number">
                  {completedSteps[index] && index !== phaseIndex ? <Check size={12} /> : index + 1}
                </span>
                <span>{label}</span>
              </li>
            ))}
          </ol>
        </Card>
        <Card className="resource-section">
          <h2>智能体</h2>
          <Button variant="ghost" className="resource-row" onClick={() => setEditor(true)}>
            <span className="resource-icon">
              <Bot />
            </span>
            <span>{state.name}</span>
            <small className={state.agentVersion ? '' : 'draft-status'}>
              {state.agentDirty && state.agentVersion
                ? '未保存'
                : state.agentVersion
                  ? `v${state.agentVersion}`
                  : '草稿'}
            </small>
            <ChevronRight />
          </Button>
        </Card>
        <Card className="resource-section">
          <h2>资源</h2>
          {state.environmentCreated ? (
            <>
              <Button
                variant="ghost"
                className="resource-row"
                onClick={() => {
                  setSideView('config');
                  setMobileInspector(false);
                  const target = chatViewport.current?.querySelector('[data-chat-stage="environment"]');
                  target?.scrollIntoView({ block: 'start' });
                }}
              >
                <span className="resource-icon">
                  <Layers />
                </span>
                <span>{state.environment}</span>
                <ChevronRight />
              </Button>
            </>
          ) : (
            <div className="empty-resource">
              <Layers size={30} strokeWidth={1} />
              <span>暂无资源</span>
            </div>
          )}
          {state.resources.slackEnabled && state.resources.vault !== 'none' && (
            <Button variant="ghost" className="resource-row" onClick={() => setVaultDetails(true)}>
              <span className="resource-icon">
                <ShieldCheck />
              </span>
              <span title="工作区集成">工作区集成</span>
              <small className={state.resources.credentialReady ? 'resource-ready' : 'resource-pending'}>
                {state.resources.credentialReady ? '已配置' : state.resources.credentialSkipped ? '已跳过' : '待授权'}
              </small>
              <ChevronRight />
            </Button>
          )}
          {state.deploymentCreated && (
            <Button variant="ghost" className="resource-row" onClick={() => setDeploymentDetails(true)}>
              <span className="resource-icon">
                <Play />
              </span>
              <span>{state.name}定时运行</span>
              <ChevronRight />
            </Button>
          )}
        </Card>

        <Button
          variant="secondary"
          className="export-button"
          onClick={() =>
            download(
              'agent.json',
              JSON.stringify(
                {
                  agent: {
                    name: state.name,
                    model: state.model,
                    system: state.system,
                    ...agentResourceFields(state.resources),
                  },
                  session: { environment_id: 'ENVIRONMENT_ID', vault_ids: vaultReferences(state.resources) },
                },
                null,
                2,
              ),
            )
          }
        >
          <ArrowDownToLine />
          导出配置
        </Button>
      </aside>
    );
  }
  const integrationCode = state.deploymentCreated
    ? codeLanguage === 'python'
      ? `from anthropic import Anthropic\n\nclient = Anthropic(base_url="OMA_API_URL", api_key="OMA_API_KEY")\nruns = client.beta.deployment_runs.list(deployment_id="DEPLOYMENT_ID")\nrun = next((run for run in runs.data if run.session_id), None)\nif run is not None:\n    with client.beta.sessions.events.stream(session_id=run.session_id) as stream:\n        for event in stream:\n            print(event)`
      : `curl "$OMA_API_URL/v1/deployment_runs?deployment_id=DEPLOYMENT_ID" \\\n  -H "x-api-key: $OMA_API_KEY"\n\n# 选择已有 session_id 的运行记录，再监听对应会话\ncurl -N "$OMA_API_URL/v1/sessions/SESSION_ID/events/stream" \\\n  -H "x-api-key: $OMA_API_KEY"`
    : codeLanguage === 'python'
      ? `from anthropic import Anthropic\n\nclient = Anthropic(base_url="OMA_API_URL", api_key="OMA_API_KEY")\nsession = client.beta.sessions.create(\n    agent={"type": "agent", "id": "AGENT_ID", "version": ${state.agentVersion}},\n    environment_id="ENVIRONMENT_ID",\n    vault_ids=${JSON.stringify(vaultReferences(state.resources))},\n)\n# 监听事件后再发送消息`
      : `curl -X POST "$OMA_API_URL/v1/sessions" \\\n  -H "x-api-key: $OMA_API_KEY" \\\n  -H 'content-type: application/json' \\\n  -d '{\n    "agent": {"type": "agent", "id": "AGENT_ID", "version": ${state.agentVersion}},\n    "environment_id": "ENVIRONMENT_ID",\n    "vault_ids": ${JSON.stringify(vaultReferences(state.resources))}\n  }'\n\n# 在终端 A 监听\ncurl -N "$OMA_API_URL/v1/sessions/SESSION_ID/events/stream" \\\n  -H "x-api-key: $OMA_API_KEY"\n\n# 然后在终端 B 发送消息\ncurl -X POST "$OMA_API_URL/v1/sessions/SESSION_ID/events" \\\n  -H "x-api-key: $OMA_API_KEY" \\\n  -H 'content-type: application/json' \\\n  -d '{"events":[{"type":"user.message","content":[\n    {"type":"text","text":"帮我分析这份数据"}\n  ]}]}'`;

  const pendingQuestion = {
    network: '这个智能体需要怎样访问网络？',
    hosts: '哪些网站需要允许访问？',
    'choose-environment': '使用哪个运行环境？',
    refine: '希望修改哪部分？',
  }[state.phase as 'network' | 'hosts' | 'choose-environment' | 'refine'];
  return (
    <div
      className={`builder-layout continuous-builder ${sideView !== 'config' ? 'with-workspace' : ''}`}
      data-theme-mode={dark ? 'dark' : 'light'}
    >
      <section className="builder-chat">
        <div
          className="chat-scroll"
          ref={chatViewport}
          onScroll={() => {
            const viewport = chatViewport.current;
            if (viewport) following.current = viewport.scrollHeight - viewport.clientHeight - viewport.scrollTop < 160;
          }}
        >
          <div className="stage-content conversation-history" aria-label="构建对话历史">
            {state.entries.map((item, index) =>
              index === state.entries.length - 1 &&
              item.role === 'assistant' &&
              item.text === pendingQuestion ? null : (
                <div key={index} data-chat-stage={item.stage} className={`conversation-entry entry-${item.role}`}>
                  {item.role === 'assistant' && (
                    <span className="assistant-mark">
                      <Sparkles size={16} />
                    </span>
                  )}
                  {item.role === 'resource' ? (
                    <div className="history-resource">
                      <span className="resource-icon">
                        {item.stage === 'agent' ? <Bot /> : item.stage === 'environment' ? <Layers /> : <Terminal />}
                      </span>
                      <span>
                        {item.text}
                        <small>{item.detail}</small>
                      </span>
                      <Check className="resource-check" size={16} />
                    </div>
                  ) : (
                    <p>{item.text}</p>
                  )}
                </div>
              ),
            )}
            <div className="active-conversation-action">{activeCard()}</div>
            <div ref={end} />
          </div>
        </div>
        <form className="chat-composer" onSubmit={submitChat}>
          <Textarea
            aria-label="补充构建要求"
            placeholder={state.phase === 'hosts' ? '输入需要访问的网站…' : '回复或继续调整…'}
            value={message}
            onChange={(event) => setMessage(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
                event.preventDefault();
                submitChat(event);
              }
            }}
          />
          <Button type="submit" size="icon" aria-label="发送构建要求" disabled={!message.trim()}>
            <ArrowUp />
          </Button>
        </form>
        {!storageAvailable && <p className="storage-warning">本地保存不可用，刷新会丢失本次对话。</p>}
      </section>
      {sideView === 'config' ? (
        inspector()
      ) : (
        <section className="conversation-workspace">
          <header>
            <span>
              {sideView === 'test' ? <Terminal size={16} /> : <CodeIcon />}
              {sideView === 'test' ? '测试会话' : '接入应用'}
            </span>
            <Button variant="ghost" size="icon-sm" aria-label="收起面板" onClick={() => setSideView('config')}>
              <X />
            </Button>
          </header>
          {sideView === 'test' ? (
            <>
              <div className="test-messages" aria-label="测试会话历史">
                {state.testEntries.length ? (
                  state.testEntries.map((item, index) => (
                    <div key={index} className={item.role === 'user' ? 'test-user' : 'test-assistant'}>
                      {item.role === 'assistant' && (
                        <div className="test-role">
                          <Bot size={14} />
                          {state.name}
                        </div>
                      )}
                      <div className="message-text">{item.text}</div>
                      {item.role === 'assistant' && item.text === state.reply && state.templateId === 'analysis' && (
                        <Button
                          variant="outline"
                          className="file-download"
                          onClick={() =>
                            download('summary.json', JSON.stringify({ count: 3, total: 60, mean: 20 }, null, 2))
                          }
                        >
                          <FileJson2 />
                          <span>summary.json</span>
                          <ArrowDownToLine />
                        </Button>
                      )}
                    </div>
                  ))
                ) : (
                  <div className="test-empty">
                    <Bot size={28} strokeWidth={1} />
                    <span>发一条消息，看看效果。</span>
                  </div>
                )}
                {state.testStatus === 'running' && !approval && <p className="secondary">正在运行…</p>}
                {state.testStatus === 'running' && approval && (
                  <div className="approval-card">
                    <h2>允许写入 summary.json？</h2>
                    <div className="conversation-actions">
                      <Button variant="outline" onClick={() => finishTest('已拒绝写入，没有生成文件。')}>
                        拒绝
                      </Button>
                      <Button onClick={() => finishTest(state.reply)}>允许</Button>
                    </div>
                  </div>
                )}
                {state.testClosed && (
                  <div className="test-complete">
                    <Check size={14} />
                    会话已结束
                  </div>
                )}
              </div>
              <div className="test-session-actions">
                {state.testStatus === 'running' && (
                  <Button variant="ghost" size="sm" onClick={stopTurn}>
                    <Square />
                    停止本轮
                  </Button>
                )}
                {!state.testClosed && (
                  <Button variant="ghost" size="sm" onClick={endSession}>
                    结束试跑
                  </Button>
                )}
              </div>
              <form className="test-composer" onSubmit={sendTest}>
                <Textarea
                  aria-label="测试消息"
                  placeholder={state.testClosed ? '会话已结束' : '发送一条测试消息…'}
                  value={testInput}
                  onChange={(event) => setTestInput(event.target.value)}
                  disabled={state.testStatus === 'running' || state.testClosed}
                />
                <Button
                  type="submit"
                  size="icon"
                  aria-label="发送测试消息"
                  disabled={!testInput.trim() || state.testStatus === 'running' || state.testClosed}
                >
                  <ArrowUp />
                </Button>
              </form>
            </>
          ) : (
            <div className="conversation-code">
              <Tabs value={codeLanguage} onValueChange={(value) => setCodeLanguage(String(value))}>
                <TabsList>
                  <TabsTrigger value="bash">curl</TabsTrigger>
                  <TabsTrigger value="python">Python</TabsTrigger>
                </TabsList>
              </Tabs>
              <CodeBlock code={integrationCode} language={codeLanguage} />
              <p className="secondary">替换 API 地址、密钥和资源 ID 后使用。</p>
            </div>
          )}
        </section>
      )}
      <AgentConfigEditor
        open={editor}
        state={state}
        onClose={() => setEditor(false)}
        onPropose={(config) => proposeConfig(config, '修改配置。')}
      />
      <DeploymentDetails open={deploymentDetails} state={state} onClose={() => setDeploymentDetails(false)} />
      <Sheet open={vaultDetails} onOpenChange={setVaultDetails}>
        <SheetContent className="config-sheet">
          <SheetHeader>
            <SheetTitle>凭据保险库</SheetTitle>
          </SheetHeader>
          <dl className="confirmation-facts">
            {state.resources.vault !== 'none' && (
              <div>
                <dt>名称</dt>
                <dd>工作区集成</dd>
              </div>
            )}
            <div>
              <dt>服务</dt>
              <dd>Slack</dd>
            </div>
            <div>
              <dt>状态</dt>
              <dd>{state.resources.credentialReady ? '已配置' : '待授权'}</dd>
            </div>
          </dl>
        </SheetContent>
      </Sheet>
    </div>
  );
}
function CodeIcon() {
  return <Globe size={16} />;
}

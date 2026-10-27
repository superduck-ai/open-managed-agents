import { useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import {
  ArrowDownToLine,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  Bot,
  ChartNoAxesCombined,
  Check,
  ChevronDown,
  Code2,
  FileJson2,
  FolderOpen,
  Globe,
  Layers,
  LifeBuoy,
  Menu,
  Moon,
  Play,
  Plus,
  RotateCcw,
  Search,
  Settings2,
  ShieldCheck,
  Sparkles,
  Square,
  Sun,
  Terminal,
  Workflow,
  X,
} from 'lucide-react';
import { Button } from '@/shared/ui/button';
import { Input } from '@/shared/ui/input';
import { Textarea } from '@/shared/ui/textarea';
import { Label } from '@/shared/ui/label';
import { Tabs, TabsList, TabsTrigger } from '@/shared/ui/tabs';
import { RadioGroup, RadioGroupItem } from '@/shared/ui/radio-group';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/shared/ui/sheet';
import { BrandLogo } from '@/shared/brand/BrandLogo';
import { I18nContext } from '@/shared/i18n/context';
import { CodeBlock } from './PrototypeCodeBlock';
import { emptyResources, agentResourceFields, needsCredential, vaultReferences } from './PrototypeResources';
import { ChatBuilderPrototype, clearBuilderReview, hasBuilderReview } from './ChatBuilderPrototype';
import '@fontsource-variable/geist/index.css';
import '@fontsource-variable/geist-mono/index.css';
import '../../src/styles.css';
import './prototype.css';

type Screen = 'start' | 'agent' | 'environment' | 'test' | 'integrate';
type Mode = 'chat' | 'api';
type Template = {
  id: string;
  name: string;
  description: string;
  icon: typeof Bot;
  system: string;
  sample: string;
  reply: string;
};
const templates: Template[] = [
  {
    id: 'research',
    name: '深度研究',
    description: '研究一个问题，整理有来源的结论。',
    icon: Search,
    system: '围绕用户的问题研究，优先引用原始来源，区分事实、推断和未知。',
    sample: '研究一下企业知识库接入 AI 助手的常见方式。',
    reply: '可以从文件上传、检索知识库和工具连接三个方向比较。我会先确定资料范围，再整理来源与结论。',
  },
  {
    id: 'extract',
    name: '结构化提取',
    description: '把邮件、文档和文字转成 JSON。',
    icon: FileJson2,
    system: '按用户提供的字段提取信息。缺失值填 null，只输出合法 JSON，不猜测。',
    sample: '提取客户、商品和数量：星河公司想买 3 台打印机。',
    reply: '{\n  "customer": "星河公司",\n  "product": "打印机",\n  "quantity": 3\n}',
  },
  {
    id: 'tracking',
    name: '领域追踪',
    description: '收集近期变化，形成一份简报。',
    icon: Globe,
    system: '按指定主题和时间范围整理变化，附上来源，资料不足时明确说明。',
    sample: '整理过去一周我们产品公告的变化。',
    reply: '请提供公告网址或内容。我会按过去一周的时间范围整理变化，不补造没有来源的信息。',
  },
  {
    id: 'support',
    name: '客服助手',
    description: '依据知识库回答，未知问题交给人工。',
    icon: LifeBuoy,
    system: '依据用户提供的资料回答客户问题。没有依据时说明未知，先生成回复草稿，不直接外发。',
    sample: '标准版每月 99 元，支持 3 人。我们有 5 人，够用吗？',
    reply: '标准版支持 3 人，无法覆盖 5 人。当前资料没有提供其他套餐的人数和价格，建议确认后再选择。',
  },
  {
    id: 'analysis',
    name: '数据分析',
    description: '分析表格，用数据回答问题。',
    icon: ChartNoAxesCombined,
    system: '分析用户提供的数据，使用代码核对计算，说明缺失与限制，并交付可下载结果。',
    sample: '订单 A 12 元，B 18 元，C 30 元。统计订单数、总额和平均值。',
    reply: '共 3 笔订单，总额 60 元，平均每单 20 元。',
  },
  {
    id: 'review',
    name: '代码评审',
    description: '检查代码，只报告能确认的问题。',
    icon: Code2,
    system: '审阅用户提供的代码，报告可确认的错误、复现方式和建议，不直接修改原文件。',
    sample: 'def average(values): return sum(values) / len(values)\n这个函数需要支持空列表。',
    reply: '空列表会触发除零错误。复现：average([])。建议在列表为空时返回 0；原代码未修改。',
  },
];
const screens: Screen[] = ['start', 'agent', 'environment', 'test', 'integrate'];
const stepNames = ['创建智能体', '配置环境', '试跑', '接入'];
const reviewNames = ['模板', '构建', '环境', '试跑', '接入'];
const labels: Record<string, string> = {
  'select.search': '搜索…',
  'select.empty': '没有匹配结果',
  'common.close': '关闭',
};
const defaultTemplate = templates[4];
const currentSearch = new URLSearchParams(location.search);
const initialScreen = screens.find((value) => value === currentSearch.get('screen')) ?? 'start';

function Choice({
  value,
  options,
  onChange,
  name,
}: {
  value: string;
  options: { value: string; title: string; detail?: string; icon?: typeof Bot }[];
  onChange: (value: string) => void;
  name: string;
}) {
  return (
    <RadioGroup
      value={value}
      onValueChange={(next) => onChange(String(next))}
      aria-label={name}
      className="choice-list"
    >
      {options.map((option) => (
        <label key={option.value} className={`choice ${value === option.value ? 'chosen' : ''}`}>
          <RadioGroupItem value={option.value} />
          {option.icon && <option.icon size={16} className="secondary" />}
          <span className="choice-text">
            <span>{option.title}</span>
            {option.detail && <small>{option.detail}</small>}
          </span>
        </label>
      ))}
    </RadioGroup>
  );
}

function App() {
  const [notice, setNotice] = useState('');
  const resources = emptyResources;
  const [chatRevision, setChatRevision] = useState(0);
  const [chatReview, setChatReview] = useState<{
    screen: Screen;
    serial: number;
    action?: 'config' | 'error' | 'approval';
  } | null>(null);
  const [screen, setScreen] = useState<Screen>(initialScreen);
  const [mode, setMode] = useState<Mode>(currentSearch.get('mode') === 'api' ? 'api' : 'chat');
  const [dark, setDark] = useState(currentSearch.get('theme') !== 'light');
  const [template, setTemplate] = useState<Template>(defaultTemplate);
  const [description, setDescription] = useState('');
  const [name, setName] = useState(defaultTemplate.name);
  const [system, setSystem] = useState(defaultTemplate.system);
  const [model, setModel] = useState('DeepSeek V3.2');
  const [saved, setSaved] = useState(initialScreen !== 'start' && initialScreen !== 'agent');
  const [environment, setEnvironment] = useState('Default');
  const [newEnvironment, setNewEnvironment] = useState(false);
  const [environmentName, setEnvironmentName] = useState('分析环境');
  const [network, setNetwork] = useState('unrestricted');
  const [hosts, setHosts] = useState('');
  const [error, setError] = useState(false);
  const [editor, setEditor] = useState(false);
  const [mobileMenu, setMobileMenu] = useState(false);
  const [testInput, setTestInput] = useState(defaultTemplate.sample);
  const [testHistory, setTestHistory] = useState<{ role: 'user' | 'assistant'; text: string }[]>([]);
  const [running, setRunning] = useState(false);
  const [complete, setComplete] = useState(false);
  const [approval, setApproval] = useState(false);
  const [testError, setTestError] = useState(false);
  const [language, setLanguage] = useState('bash');
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const phase = Math.max(0, screens.indexOf(screen) - 1);
  const environmentLabel = newEnvironment ? environmentName : environment;

  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark);
    document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  }, [dark]);
  useEffect(() => {
    const url = new URL(location.href);
    url.searchParams.set('screen', screen);
    url.searchParams.set('mode', mode);
    url.searchParams.set('theme', dark ? 'dark' : 'light');
    window.history.replaceState(null, '', url);
    console.info('Quickstart prototype', { screen, mode, saved, environment: environmentLabel });
  }, [screen, mode, dark, saved, environmentLabel]);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  function chooseTemplate(next: Template) {
    setDescription('');
    setChatRevision((value) => value + 1);
    setChatReview(null);
    setTemplate(next);
    setName(next.name);
    setSystem(next.system);
    setTestInput(next.sample);
    setSaved(false);
    setComplete(false);
    setTestHistory([]);
    setScreen('agent');
  }
  function start(event?: FormEvent) {
    event?.preventDefault();
    if (!description.trim()) return;
    setChatRevision((value) => value + 1);
    setChatReview(null);
    const next = templates.find((item) => description.includes(item.name)) ?? defaultTemplate;
    setTemplate(next);
    setName(next === defaultTemplate ? '销售分析助手' : next.name);
    setSystem(`${next.system}\n\n${description.trim()}`);
    setTestInput(next.sample);
    setSaved(false);
    setComplete(false);
    setTestHistory([]);
    setScreen('agent');
  }
  function saveAgent() {
    setSaved(true);
    setScreen('environment');
  }
  function selectEnvironment() {
    if (newEnvironment && !environmentName.trim()) return;
    if (error) return;
    setComplete(false);
    setScreen('test');
  }
  function finishTest(text: string) {
    setTestHistory((items) => [...items, { role: 'assistant', text }]);
    setRunning(false);
    setApproval(false);
    setComplete(true);
  }
  function runTest(event?: FormEvent) {
    event?.preventDefault();
    if (!testInput.trim() || running || needsCredential(resources)) return;
    setTestHistory((items) => [...items, { role: 'user', text: testInput }]);
    setTestInput('');
    setRunning(true);
    setComplete(false);
    if (approval) return;
    timer.current = setTimeout(() => {
      if (testError) {
        setRunning(false);
        return;
      }
      finishTest(template.reply);
    }, 900);
  }
  function stopTest() {
    if (timer.current) clearTimeout(timer.current);
    setRunning(false);
    setApproval(false);
    setTestHistory((items) => [...items, { role: 'assistant', text: '本次试跑已停止。' }]);
  }
  function reset() {
    setChatRevision((value) => value + 1);
    setChatReview(null);
    if (timer.current) clearTimeout(timer.current);
    setScreen('start');
    setRunning(false);
    setTestHistory([]);
    setSaved(false);
    setComplete(false);
    setError(false);
    setTestError(false);
    setApproval(false);
    setDescription('');
  }
  function download(filename: string, content: string, type = 'application/json') {
    const url = URL.createObjectURL(new Blob([content], { type }));
    const link = document.createElement('a');
    link.href = url;
    link.download = filename;
    link.click();
    URL.revokeObjectURL(url);
  }

  const createCode = `curl -X POST "$OMA_API_URL/v1/agents" \\\n  -H "x-api-key: $OMA_API_KEY" \\\n  -H 'content-type: application/json' \\\n  -d '${JSON.stringify({ name, model: 'YOUR_MODEL_ID', system, ...agentResourceFields(resources) }, null, 2).replaceAll("'", "'\\''")}'`;
  const environmentCode = newEnvironment
    ? `curl -X POST "$OMA_API_URL/v1/environments" \\\n  -H "x-api-key: $OMA_API_KEY" \\\n  -H 'content-type: application/json' \\\n  -d '${JSON.stringify(
        {
          name: environmentName,
          config: {
            type: 'cloud',
            networking: {
              type: network,
              ...(network === 'limited'
                ? {
                    allowed_hosts: hosts
                      .split(',')
                      .map((host) => host.trim())
                      .filter(Boolean),
                  }
                : {}),
            },
          },
        },
        null,
        2,
      )}'`
    : `curl "$OMA_API_URL/v1/environments/ENVIRONMENT_ID" \\\n  -H "x-api-key: $OMA_API_KEY"`;
  const integrationCode =
    language === 'python'
      ? `import os\nfrom anthropic import Anthropic\n\nclient = Anthropic(\n    api_key=os.environ["OMA_API_KEY"],\n    base_url=os.environ["OMA_API_URL"],\n)\n\nsession = client.beta.sessions.create(\n    agent={"type": "agent", "id": "AGENT_ID", "version": 1},\n    environment_id="ENVIRONMENT_ID",\n    vault_ids=${JSON.stringify(vaultReferences(resources))},\n)\nstream = client.beta.sessions.events.stream(session.id)\nclient.beta.sessions.events.send(session.id,\n    events=[{"type": "user.message", "content": [\n        {"type": "text", "text": "帮我分析这份数据"}\n    ]}],\n)\nfor event in stream:\n    print(event)\n    if event.type == "session.status_idle":\n        break`
      : `# 1. 创建会话\ncurl -X POST "$OMA_API_URL/v1/sessions" \\\n  -H "x-api-key: $OMA_API_KEY" \\\n  -H 'content-type: application/json' \\\n  -d '{\n    "agent": {"type": "agent", "id": "AGENT_ID", "version": 1},\n    "environment_id": "ENVIRONMENT_ID",\n    "vault_ids": ${JSON.stringify(vaultReferences(resources))}\n  }'\n\n# 2. 在终端 A 监听\ncurl -N "$OMA_API_URL/v1/sessions/SESSION_ID/events/stream" \\\n  -H "x-api-key: $OMA_API_KEY"\n\n# 3. 在终端 B 发送消息\ncurl -X POST "$OMA_API_URL/v1/sessions/SESSION_ID/events" \\\n  -H "x-api-key: $OMA_API_KEY" \\\n  -H 'content-type: application/json' \\\n  -d '{"events":[{"type":"user.message","content":[\n    {"type":"text","text":"帮我分析这份数据"}\n  ]}]}'`;

  function field(label: string, child: ReactNode) {
    return (
      <div className="field">
        <Label>
          {label}
          {child}
        </Label>
      </div>
    );
  }
  function agentFields() {
    return (
      <>
        {field('名称', <Input value={name} onChange={(event) => setName(event.target.value)} />)}
        {field(
          '模型',
          <Select
            value={model}
            onValueChange={(value) => value && setModel(value)}
            items={['DeepSeek V3.2', 'Claude Sonnet', 'GPT'].map((value) => ({ value, label: value }))}
          >
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {['DeepSeek V3.2', 'Claude Sonnet', 'GPT'].map((value) => (
                <SelectItem key={value} value={value}>
                  {value}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>,
        )}
        {field(
          '指令',
          <Textarea className="system-input" value={system} onChange={(event) => setSystem(event.target.value)} />,
        )}
      </>
    );
  }
  function environmentChoices() {
    return (
      <>
        <Choice
          name="选择环境"
          value={newEnvironment ? 'new' : environment}
          onChange={(value) => {
            setNewEnvironment(value === 'new');
            if (value !== 'new') setEnvironment(value);
            setError(false);
          }}
          options={[
            { value: 'Default', title: 'Default', detail: '允许访问互联网', icon: Globe },
            { value: '日常分析', title: '日常分析', detail: '仅限指定网站', icon: ShieldCheck },
            { value: 'new', title: '新建环境', icon: Plus },
          ]}
        />
        {newEnvironment && (
          <div className="new-environment">
            {field(
              '名称',
              <Input value={environmentName} onChange={(event) => setEnvironmentName(event.target.value)} />,
            )}
            {field(
              '网络',
              <Select
                value={network}
                onValueChange={(value) => value && setNetwork(value)}
                items={[
                  { value: 'unrestricted', label: '不限制' },
                  { value: 'limited', label: '仅限指定网站' },
                ]}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="unrestricted">不限制</SelectItem>
                  <SelectItem value="limited">仅限指定网站</SelectItem>
                </SelectContent>
              </Select>,
            )}
            {network === 'limited' &&
              field(
                '允许的网站',
                <Input
                  placeholder="example.com, api.example.com"
                  value={hosts}
                  onChange={(event) => setHosts(event.target.value)}
                />,
              )}
          </div>
        )}

        {error && (
          <div role="alert" className="error-message">
            <span>环境创建失败，配置已保留。</span>
            <Button variant="ghost" size="sm" onClick={() => setError(false)}>
              重试
            </Button>
          </div>
        )}
      </>
    );
  }
  function apiCompletionIntro() {
    return (
      <div className="stage-content">
        <p>
          {screen === 'test' ? '发送一条消息，检查智能体的运行效果。' : '使用已保存的智能体创建会话，接入你的应用。'}
        </p>

        <div className="integration-actions">
          {screen === 'test' ? (
            <Button disabled={!complete} onClick={() => setScreen('integrate')}>
              继续接入
              <ArrowRight />
            </Button>
          ) : (
            <Button variant="outline" onClick={() => setScreen('test')}>
              返回试跑
            </Button>
          )}
        </div>
      </div>
    );
  }
  function testPanel() {
    return (
      <section className="test-panel">
        <header>
          <div>
            <Terminal size={16} />
            <span>试跑</span>
            <small>{name}</small>
          </div>
          {running ? (
            <Button variant="ghost" size="sm" onClick={stopTest}>
              <Square />
              停止
            </Button>
          ) : (
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="清空试跑"
              onClick={() => {
                setTestHistory([]);
                setComplete(false);
                setTestInput(template.sample);
              }}
            >
              <RotateCcw />
            </Button>
          )}
        </header>
        <div className="test-messages" aria-live="polite">
          {testHistory.length === 0 ? (
            <div className="test-empty">
              <Bot size={28} strokeWidth={1} />
              <span>发一条消息，看看效果。</span>
            </div>
          ) : (
            testHistory.map((item, index) => (
              <div key={index} className={item.role === 'user' ? 'test-user' : 'test-assistant'}>
                {item.role === 'assistant' && (
                  <div className="test-role">
                    <Bot size={14} />
                    {name}
                  </div>
                )}
                <div className="message-text">{item.text}</div>
                {item.role === 'assistant' &&
                  complete &&
                  item.text === template.reply &&
                  template.id === 'analysis' && (
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
          )}
          {running && !approval && <p className="secondary">正在运行…</p>}
          {running && approval && (
            <div className="approval-card">
              <h2>允许写入结果文件？</h2>
              <code>summary.json</code>
              <div className="inline-actions">
                <Button variant="outline" onClick={() => finishTest('已拒绝写入，本次没有生成文件。')}>
                  拒绝
                </Button>
                <Button onClick={() => finishTest(template.reply)}>允许</Button>
              </div>
            </div>
          )}
          {testError && !running && testHistory.length > 0 && (
            <div className="error-message">
              <span>环境启动失败，未完成试跑。</span>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setTestError(false);
                  setTestInput(template.sample);
                }}
              >
                重试
              </Button>
            </div>
          )}
          {complete && (
            <div className="test-complete">
              <Check size={14} />
              本轮已结束
            </div>
          )}
        </div>
        <form className="test-composer" onSubmit={runTest}>
          <Textarea
            aria-label="测试消息"
            value={testInput}
            onChange={(event) => setTestInput(event.target.value)}
            placeholder="发送一条测试消息…"
            disabled={running}
          />
          <Button
            type="submit"
            size="icon"
            aria-label="发送测试消息"
            disabled={running || !testInput.trim() || needsCredential(resources)}
          >
            <ArrowUp />
          </Button>
        </form>
      </section>
    );
  }

  return (
    <I18nContext.Provider
      value={{ locale: 'zh-CN', setLocale: () => {}, msg: (id, fallback) => labels[id] ?? fallback }}
    >
      <div className="prototype-shell">
        <aside className={`app-sidebar ${mobileMenu ? 'menu-open' : ''}`}>
          <div className="brand-row">
            <BrandLogo />
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="收起导航"
              className="mobile-only"
              onClick={() => setMobileMenu(false)}
            >
              <X />
            </Button>
          </div>
          <Select value="Default" items={[{ value: 'Default', label: 'Default' }]}>
            <SelectTrigger className="workspace-select">
              <span className="workspace-square" />
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="Default">Default</SelectItem>
            </SelectContent>
          </Select>
          <nav aria-label="主导航">
            <Button variant="ghost" onClick={reset}>
              <Layers />
              概览
            </Button>
            <Button variant="ghost" className="active" onClick={reset}>
              <Plus />
              创建
            </Button>
            {[
              { label: '智能体', icon: Bot },
              { label: '会话', icon: Terminal },
              { label: '部署', icon: Workflow },
              { label: '环境', icon: Globe },
              { label: '记忆', icon: FolderOpen },
              { label: '凭据保险库', icon: ShieldCheck },
            ].map((item) => (
              <Button
                key={item.label}
                variant="ghost"
                onClick={() => {
                  if (item.label === '智能体' && mode === 'chat')
                    setChatReview((previous) => ({ screen, serial: (previous?.serial ?? 0) + 1, action: 'config' }));
                  else if (item.label === '智能体') setEditor(true);
                  else if (item.label === '环境') {
                    setScreen('environment');
                    setSaved(true);
                  } else setNotice(`${item.label}不在本次交互稿范围内`);
                }}
              >
                <item.icon />
                {item.label}
              </Button>
            ))}
          </nav>
          <div className="account-row">
            <span className="avatar">T</span>
            <span>
              <strong>Tom</strong>
              <small>OMA</small>
            </span>
            <ChevronDown size={14} />
          </div>
        </aside>
        <main className="workspace-main">
          <header className="page-header">
            <div>
              <Button
                className="mobile-only"
                size="icon"
                variant="ghost"
                aria-label="展开导航"
                onClick={() => setMobileMenu(true)}
              >
                <Menu />
              </Button>
              {screen === 'start' ? (
                <span>创建智能体</span>
              ) : (
                <>
                  <Button variant="ghost" size="icon-sm" aria-label="返回模板" onClick={reset}>
                    <ArrowLeft />
                  </Button>
                  <span>{name}</span>
                  {saved && mode === 'api' && <small>v1</small>}
                </>
              )}
            </div>
            <div className="header-actions">
              {screen !== 'start' && mode === 'chat' && (
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label="查看配置"
                  className="inspector-toggle"
                  onClick={() =>
                    setChatReview((previous) => ({ screen, serial: (previous?.serial ?? 0) + 1, action: 'config' }))
                  }
                >
                  <Settings2 />
                </Button>
              )}
              <Button
                variant="ghost"
                size="icon"
                aria-label={dark ? '切换浅色' : '切换深色'}
                onClick={() => setDark(!dark)}
              >
                {dark ? <Sun /> : <Moon />}
              </Button>
            </div>
          </header>
          {screen === 'start' ? (
            <div className="start-scroll">
              <section className="start-content">
                <h1>你想构建什么？</h1>
                <Tabs value={mode} onValueChange={(value) => setMode(value as Mode)}>
                  <TabsList aria-label="创建方式">
                    <TabsTrigger value="chat">问答构建</TabsTrigger>
                    <TabsTrigger value="api">API 创建</TabsTrigger>
                  </TabsList>
                </Tabs>
                {mode === 'chat' ? (
                  <form className="start-composer" onSubmit={start}>
                    <Textarea
                      aria-label="描述你的智能体"
                      placeholder="一个客服助手，根据我们的文档回答问题，遇到无法解决的问题时交给人工…"
                      value={description}
                      onChange={(event) => setDescription(event.target.value)}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
                          event.preventDefault();
                          start();
                        }
                      }}
                    />
                    <div className="composer-bottom">
                      <Sparkles size={16} />
                      <Button type="submit" aria-label="开始构建" size="icon" disabled={!description.trim()}>
                        <ArrowUp />
                      </Button>
                    </div>
                  </form>
                ) : null}
                <div className="template-grid">
                  {templates.map((item) => (
                    <Button
                      key={item.id}
                      variant="ghost"
                      className="template-card"
                      onClick={() => chooseTemplate(item)}
                    >
                      <item.icon size={19} strokeWidth={1.5} />
                      <strong>{item.name}</strong>
                      <span>{item.description}</span>
                      <ArrowUp className="template-arrow" size={15} />
                    </Button>
                  ))}
                </div>
                <Button
                  variant="link"
                  className="blank-link"
                  onClick={() => {
                    setChatRevision((value) => value + 1);
                    setChatReview(null);
                    setTemplate({ ...defaultTemplate, id: 'blank', name: '未命名智能体', system: '' });
                    setName('未命名智能体');
                    setSystem('');
                    setDescription('');
                    setSaved(false);
                    setScreen('agent');
                    setEditor(mode === 'api');
                  }}
                >
                  从空白智能体开始
                  <ArrowRight size={14} />
                </Button>
                {mode === 'chat' && hasBuilderReview() && (
                  <Button
                    variant="ghost"
                    className="resume-builder"
                    onClick={() => {
                      setChatRevision(0);
                      setChatReview(null);
                      setScreen('agent');
                    }}
                  >
                    继续上次构建
                    <ArrowRight size={14} />
                  </Button>
                )}
              </section>
            </div>
          ) : mode === 'chat' ? (
            <ChatBuilderPrototype
              key={chatRevision}
              template={template}
              description={description}
              fresh={chatRevision > 0}
              initialScreen={screen}
              review={chatReview}
              onScreenChange={setScreen}
              onNameChange={setName}
              dark={dark}
            />
          ) : screen !== 'test' && screen !== 'integrate' ? (
            <div className="api-wizard">
              <div className="api-stepper">
                {stepNames.map((step, index) => (
                  <div key={step} className={index === phase ? 'current' : ''}>
                    <span className="step-number">{index < phase ? <Check size={12} /> : index + 1}</span>
                    {step}
                  </div>
                ))}
              </div>
              <div className="api-columns">
                <section className="api-fields">
                  <h1>{screen === 'agent' ? '配置智能体' : '配置环境'}</h1>
                  {screen === 'agent' ? agentFields() : environmentChoices()}
                </section>
                <section className="api-preview">
                  <CodeBlock code={screen === 'agent' ? createCode : environmentCode} />
                </section>
              </div>
              <footer className="wizard-footer">
                <Button variant="ghost" onClick={() => setScreen(screen === 'agent' ? 'start' : 'agent')}>
                  <ArrowLeft />
                  返回
                </Button>
                <Button
                  onClick={screen === 'agent' ? saveAgent : selectEnvironment}
                  disabled={screen === 'agent' ? !name.trim() || !system.trim() : error}
                >
                  {screen === 'agent' ? '保存并继续' : newEnvironment ? '创建并继续' : '继续试跑'}
                  <ArrowRight />
                </Button>
              </footer>
            </div>
          ) : (
            <div className="builder-layout expanded api-completion-layout">
              <section className="builder-chat">
                <div className="chat-scroll">{apiCompletionIntro()}</div>
              </section>
              {screen === 'test' ? (
                testPanel()
              ) : (
                <section className="integration-panel">
                  <header>
                    <h2>调用示例</h2>
                    <span className="secondary">{name}</span>
                  </header>
                  <Tabs value={language} onValueChange={(value) => setLanguage(String(value))}>
                    <TabsList>
                      <TabsTrigger value="bash">curl</TabsTrigger>
                      <TabsTrigger value="python">Python</TabsTrigger>
                    </TabsList>
                  </Tabs>
                  <p className="integration-hint">设置 OMA_API_URL 和 OMA_API_KEY，并替换资源 ID。</p>
                  <CodeBlock code={integrationCode} language={language} />
                  <Button
                    variant="outline"
                    onClick={() => download('agent.json', JSON.stringify({ name, model, system }, null, 2))}
                  >
                    <ArrowDownToLine />
                    导出配置
                  </Button>
                </section>
              )}
            </div>
          )}
        </main>
        <Sheet open={editor} onOpenChange={setEditor}>
          <SheetContent className="config-sheet">
            <SheetHeader>
              <SheetTitle>智能体配置</SheetTitle>
            </SheetHeader>
            <div className="sheet-fields">{agentFields()}</div>
            <div className="sheet-footer">
              <Button onClick={() => setEditor(false)}>完成</Button>
            </div>
          </SheetContent>
        </Sheet>
        <div className="review-dock">
          <span>交互稿</span>
          <div className="review-scenes">
            {screens.map((value, index) => (
              <Button
                key={value}
                size="sm"
                variant={screen === value ? 'secondary' : 'ghost'}
                onClick={() => {
                  if (mode !== 'chat' || screen === 'start' || value === 'start') setScreen(value);
                  if (mode === 'chat')
                    setChatReview((previous) => ({ screen: value, serial: (previous?.serial ?? 0) + 1 }));
                  setSaved(index > 1);
                  if (value !== 'test') setComplete(false);
                }}
                aria-label={`审阅${reviewNames[index]}`}
              >
                {reviewNames[index]}
              </Button>
            ))}
          </div>
          <span className="dock-divider" />
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label="显示错误状态"
            title="显示错误状态"
            onClick={() => {
              if (mode === 'chat') {
                if (screen === 'start') setScreen('environment');
                setChatReview((previous) => ({
                  screen: 'environment',
                  serial: (previous?.serial ?? 0) + 1,
                  action: 'error',
                }));
                return;
              }
              if (screen === 'test') {
                setTestError(!testError);
                setRunning(false);
              } else {
                setScreen('environment');
                setError(!error);
              }
            }}
          >
            <ShieldCheck />
          </Button>
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label="显示工具确认"
            title="显示工具确认"
            onClick={() => {
              if (mode === 'chat') {
                setScreen('test');
                setChatReview((previous) => ({
                  screen: 'test',
                  serial: (previous?.serial ?? 0) + 1,
                  action: 'approval',
                }));
                return;
              }
              setScreen('test');
              setApproval(true);
              setRunning(false);
              setTestInput('把结果写入 summary.json');
            }}
          >
            <Play />
          </Button>
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label="重置交互稿"
            title="重置"
            onClick={() => {
              clearBuilderReview();
              reset();
            }}
          >
            <RotateCcw />
          </Button>
        </div>

        {notice && (
          <div role="status" className="prototype-notice">
            {notice}
            <Button variant="ghost" size="icon-xs" aria-label="关闭提示" onClick={() => setNotice('')}>
              <X />
            </Button>
          </div>
        )}
      </div>
    </I18nContext.Provider>
  );
}

function Prototype() {
  return <App />;
}

createRoot(document.getElementById('root')!).render(<Prototype />);

import { type Locale } from '../../../shared/i18n';
import { platformQuickstartOfficialRequest } from './platformQuickstartOfficialRequest.generated';

// The Agent Builder system prompt ships as two English text blocks captured from the
// official Platform request:
//   - block 0 (~128KB): Managed Agents API reference — pure technical documentation
//     (endpoints, JSON schemas, enums, code). It is the model's knowledge base, never
//     shown to the user, and is intentionally kept in English for both locales.
//   - block 1 (~38KB): Agent Builder behavior instructions. This is what we localize.
// We derive the localized system prompt from the generated English blocks instead of
// hand-editing the generated file.

const englishSystem = platformQuickstartOfficialRequest.system;

function systemBlockText(index: number): string {
  const block = englishSystem[index] as { text?: unknown } | undefined;
  return typeof block?.text === 'string' ? block.text : '';
}

const englishBlock1Text = systemBlockText(1);

// The MCP catalog (~330 lines of "- \"name\" (url: \"...\") — Label") is a pure list of
// technical identifiers that must NOT be translated and must stay identical across
// locales. We slice it out of the English block 1 so there is a single source of truth
// for the catalog and no drift between the English and Chinese prompts.
const MCP_CATALOG_START = 'Known servers (URLs on file):\n';
const MCP_CATALOG_END = '\n  If the user names a service not in this list';

function extractMcpCatalog(): string {
  const start = englishBlock1Text.indexOf(MCP_CATALOG_START);
  const end = englishBlock1Text.indexOf(MCP_CATALOG_END);
  if (start === -1 || end === -1) {
    return '';
  }
  return englishBlock1Text.slice(start + MCP_CATALOG_START.length, end);
}

const mcpCatalog = extractMcpCatalog();

// Semantically equivalent Chinese rewrite of Agent Builder block 1. Keeps the same
// capabilities, flow, and constraints. All technical tokens stay untranslated: tool
// names, step keys (agent/environment/session/deploy/integrate), model IDs, JSON field
// names, the MCP catalog, and enum values. Includes an integrated language directive
// rather than a crude "please answer in Chinese" appended to the English prompt.
const agentBuilderBlock1Zh = `你是一位专业的 agent 构建助手，正在引导用户构建他们的第一个 Claude Managed Agent。

用户已经创建了 agent —— 一份初始配置已保存并显示在上下文中。请按顺序、用尽可能少的提问，带领用户完成剩余步骤。界面上有一个步骤条；用户通过点击 "Next" 推进步骤 —— 每次推进都会通过 tool_result 通知你。

语言约定：请全程用简体中文与用户对话，包括你的说明性文字与通过 ask_user_questions 提出的问题。生成 agent 配置时，name、description、system 的语言默认跟随用户输入的语言（用户用中文描述通常生成中文配置，用户用英文描述通常生成英文配置）；如果用户明确要求某种输出语言，优先遵循用户要求。model ID、工具名、MCP server 名称与 URL、JSON 字段、metadata key、协议枚举等技术标识一律保持原样，不做翻译。

═══ STEP 1: CREATE AGENT（已完成 —— 仅需复核）═══
只针对以下阻塞项复核配置：
- 未确认的集成 —— 如果描述隐含了某种能力（工单、文档、issue 跟踪），但没有指明具体服务，就询问用户使用哪个服务，并从 MCP 目录中给出可能的选项。如果被点名的服务在目录中，它应该已经在配置里 —— 不要重复确认。如果被点名的服务不在目录中，就询问其 MCP server URL（目录是已知服务器，并非全部服务器）。
- agent 无法自行推断的范围 —— 例如 "summarize PRs" 需要知道是哪些仓库，"monitor Slack" 需要知道是哪些频道。只有当描述确实没有覆盖时才询问。
- 护栏 —— 仅当 agent 具备破坏性能力（写入、删除、发送消息、做出更改）时才询问。询问它在行动前是否应先确认。
不要主动询问：语气、人设、输出格式、详略程度、边界情况处理、model 选择。这些是精修项，不是阻塞项 —— 用户之后可以自行提出。
上下文中显示的配置已经在编辑器里生效 —— 不要为了原样重新发出而调用 build_agent_config。如果需要修复阻塞项，再调用 build_agent_config。
复核完成后，调用 offer_next_step。它会阻塞并显示 "Next: Configure environment" 按钮。点击后 tool_result 会告诉你开始 step 2。

如果用户表示 agent 应按计划运行 —— "每天"、"每周"、"每天早上"、任何频率，无论是在初始描述中还是之后的对话中 —— 调用 flag_schedule_intent 并传 wants_schedule: true（这会给流程增加一个 "Schedule deployment" 步骤），并在你下一条文字消息中告诉用户：排程不属于 agent 本身；agent 会先被创建并测试，然后 Schedule deployment 步骤再把它设置为按该排程自动运行。如果用户撤回排程，则用 false 调用。不要把频率当作阻塞项，也不要在部署步骤之前追问频率。

═══ STEP 2: CONFIGURE ENVIRONMENT ═══
当你被告知用户推进到此步骤时触发。用一句话解释什么是环境。然后调用 list_environments。
如果返回了已有环境，先通过 ask_user_questions 提供它们：一个问题，选项最多为 3 个已有环境名（最近的在前；描述中标注 networking 类型）加上 "Create a new one" —— 总选项数不超过 4 个。如果用户选择某个已有环境，仅用 reuse_environment_id 调用 create_environment。
如果用户选择 "Create a new one"（或没有返回任何已有环境），通过 ask_user_questions 询问 1-2 个 networking 问题：
- agent 需要访问开放互联网，还是只需访问特定主机？（提供："Unrestricted" 对比 "Limited to [你从 MCP servers + system prompt 推断出的主机]"）
- 如果是受限：确认你推断出的主机列表，并让用户补充更多。
然后用 name + config 调用 create_environment。优先选择受限 networking —— 只允许 agent 确实需要的主机。
create_environment 在环境创建/选择后立即完成。收到 tool_result 后，用一句简短的话确认，然后调用 offer_next_step。它会阻塞并显示 "Next: Start session" 按钮；tool_result 会告诉你开始 step 3。

═══ STEP 3: START SESSION ═══
当你被告知用户推进到此步骤时触发。先用一句话解释什么是 session：你的 agent 在其环境中的一个运行实例 —— 你向它发送事件并观察它工作。然后检查 agent 是否已准备好运行 —— system prompt 是否具备 agent 行动所需的具体取值？（一个没有频道的 Slack 发送器、一个没有仓库的 GitHub agent。）如果确实缺少某些东西，通过 ask_user_questions 询问，然后用 build_agent_config 把它写进配置。只有当缺了它 agent 就无法做有意义的测试时才询问。
如果配置里有 mcp_servers，为凭据选择一个 vault（见下方 SELECTING A VAULT），以便测试 session 能进行认证。
然后简短地（一句话）说明 agent 已就绪，并调用 agent_ready，附带 suggested_first_message。它会阻塞；界面显示 Test run。用户点击后会创建一个 session，tool_result 中包含 session_id 和请求 payload。如果返回的是一个精修请求，则处理它并再次调用 agent_ready。

session 创建之后：
1. 简短一行：你的 session 已就绪 —— 在右侧的 test run 面板中发送你的第一条消息。
2. 调用 await_test_run 并传 until="first_message"。它会阻塞并返回事件 payload。
3. 用一行话确认（提及 POST /v1/sessions/{id}/events）并鼓励用户继续交互。
4. 调用 await_test_run 并传 until="session_closed"。它会阻塞并返回一份 transcript 摘要。如果结果是「用户改为发送了消息："…"」，说明 session 仍在运行 —— 回答用户的问题（使用 turn context 中的事件），然后用相同的 "until" 再次调用 await_test_run。
5. 用一小段话分析这次运行。如果你看到具体的可修复项（缺失取值、错误频道、要补充的护栏、工具报错），调用 ask_user_questions，用一个 multiSelect 问题列出它们 —— 每个选项是你可以对配置做的一项具体更改。再加一个末尾选项 "Rerun as-is"，描述为 "Skip fixes and test again"。根据回答：通过 build_agent_config 应用所选修复，然后调用 agent_ready 再测一次；如果用户只选了 "Rerun as-is" 或什么都没选，则跳过配置调用，直接 agent_ready。从上面的第 1 步重复。
   如果这次运行看起来没问题（无需修复），跳过问题，直接进入第 6 步。
6. 只有在第 5 步分析之后 —— 绝不在测试运行仍打开时：检查 turn context 中的 "Deployment schedule planned" 行。如果是 "no"，先调用 ask_user_questions，恰好一个问题 —— "你想拿这个 agent 做什么？" —— 恰好两个选项："按排程运行它" 和 "从应用中调用它"。调用前最多一句简短的话。如果用户选择 "按排程运行它"，调用 flag_schedule_intent 并传 wants_schedule: true —— Schedule deployment 步骤会成为下一步（并且之后跳过部署步骤的 "Skip for now" 选项 —— 用户刚刚已经选择了排程）。
   如果是 "yes"，说明已经计划了排程 —— 完全跳过这个问题。
   然后调用 offer_next_step。它会阻塞并显示进入下一步的 Next 按钮。

═══ STEP: SCHEDULE DEPLOYMENT（key "deploy"）═══
只有在计划了排程时（模板自带排程，或你用 wants_schedule: true 调用过 flag_schedule_intent）才存在此步骤。当当前步骤为 "deploy" 时触发。首先用一小段通俗的话解释什么是 deployment：它把这次 quickstart 中的一切 —— agent、它的环境、以及它的凭据 vault —— 连同一条起始消息一起打包，并按排程自动运行；每次运行都会开启一个用户可以从 Console 监控的全新 session。假设用户从未听说过 deployment；2-3 句话，不要行话。在这段话之前不要调用任何工具。
然后调用 ask_user_questions，用一个问题询问它应多久运行一次 —— 提供 2-3 个符合此 agent 用途的具体排程（例如 "每个工作日上午 9 点"、"每小时"），加上末尾选项 "Skip for now"，描述为 "Run it on demand instead"。如果用户之前已经指定过某个频率，把它作为第一个选项。绝不要求用户提供 cron 表达式。
如果用户选择 "Skip for now"：用一句话确认并调用 offer_next_step 推进。
否则调用 create_deployment，附带一个简短的描述性 name、与用户选择匹配的 cron_expression，以及一个 initial_message —— 每次运行的起始消息；以测试运行的第一条消息为基础。省略 timezone（默认使用用户本地时区），除非用户指明。本次 quickstart 的 agent、环境和 vault 会被自动附加 —— 你不需要传入它们。
如果 tool_result 报告失败，用一句话转达并带上修复重试。成功后，用一句简短的话确认 —— 运行会按排程自动开始 —— 然后调用 offer_next_step。

═══ STEP: INTEGRATE（key "integrate"）═══
当当前步骤为 "integrate" 时触发。
如果此流程中先前创建了 deployment：用一小段文字说明 —— 每次排程运行都会自动创建一个 session；你的应用可以列出该 deployment 的运行记录，并像操作自己创建的 session 一样对任意一次运行的 session 进行流式读取或发送消息。不要写代码块 —— 下一个工具调用展示的代码卡片已经包含了列出运行记录和流式读取的示例。
否则：用一小段话说明 —— 要进行集成，通过 /v1/sessions 用你的 agent id + 环境创建一个 session，用 GET /v1/sessions/{id}/events/stream 流式读取事件，用 POST /v1/sessions/{id}/events 发送用户消息 —— 你的应用对 status_idle 作出反应并按需发送后续消息。
然后用你已知的 agent_id 和 environment_id 调用 show_integration_exits。它会阻塞；界面显示带标签页的代码片段和退出按钮。tool_result 会告诉你用户选择了哪个出口 —— 用一句话确认并停止。

SELECTING A VAULT FOR MCP CREDENTIALS：
当配置里有需要凭据的 mcp_servers（即在目录中未标记为 "authless" 的服务器）时，测试 session 需要一个 vault 来拉取凭据。Vault 在 session 创建时传入 —— 它们不属于 agent 配置。Authless 服务器不需要凭据也不需要 vault —— 检查覆盖情况时忽略它们，绝不要为它们调用 create_vault_credential。如果配置中每个 mcp_server 都是 authless，跳过整段并直接进入 agent_ready。
- 首先用一小段通俗的话解释什么是 vault：一个 workspace 级的 MCP server 凭据存储，session 在创建时通过 id 引用它，从而在不重复输入 token 的情况下跨 agent 复用同一个已授权连接。假设用户从未听说过 vault；控制在 2-3 句话内，避免行话。不要提及共享/安全，也不要在这段话之前调用任何工具。
- 然后调用 vault_sharing_notice（无输入）以呈现共享警告。
- 然后调用 list_vaults，查看用户的 vault，以及哪些 vault 已经拥有此 agent 的 MCP servers 所需的凭据。
- 通过 ask_user_questions 提供选择：一个问题，选项是各 vault 名称（在描述中标注覆盖情况）加上一个末尾选项用于创建新 vault。推荐那些已经覆盖所需服务器的 vault。
- 如果用户想要新 vault，用一个 name 调用 create_vault。它会自动选中，所以之后不要再调用 select_vault。
- 如果用户选择了已有 vault，用所选 vault id 调用 select_vault —— 它会阻塞以等待一次授权确认后再继续。一个 vault 可以覆盖多个服务器，用户也可以组合多个。
- select_vault 之后，对于所选 vault 未覆盖的每个 mcp_server（依据 list_vaults 的覆盖信息），调用 create_vault_credential 让用户授权。在 agent_ready 之前等待每一个完成。

当你需要用户输入时，调用 ask_user_questions。拿到答案后，简短地（一句话）说明你正在更新什么，然后用完整的更新后配置调用 build_agent_config。

build_agent_config 规则：
- 始终包含一份详细、写得好、贴合 agent 用途的 system prompt。
- 选择最合适的 model。使用完整的带版本 model ID："claude-sonnet-4-6" 适用于大多数任务，"claude-opus-4-8" 适用于复杂推理。
- 只添加用户明确指名或确认过的 MCP servers。绝不猜测用户使用哪个服务。已知服务器（URL 已在册）：
${mcpCatalog}
  如果用户指名的服务不在此列表中，询问其 MCP server URL —— 任何 MCP server 都可用，目录并不详尽。绝不要用 curl 或原始 API 调用来代替 MCP。
- 对于 tools，始终包含 {"type": "agent_toolset_20260401"}。为每个 MCP server 添加 {"type": "mcp_toolset", "mcp_server_name": "<name>", "default_config": {"permission_policy": {"type": "always_allow"}}}。
- name 应简短、有描述性、使用自然语言。
- 除非对话明确更改，否则保留现有配置值。
- quickstart 构建的是单个 agent。如果用户询问 multiagent、subagent、协调者或委派给其他 agent，不要在这里配置 —— 把用户指引到 multiagent 文档 https://platform.claude.com/docs/en/managed-agents/multi-agent，并继续构建用户描述的这个单个 agent。


  TURN CONTEXT：
  - 用户消息中可能包含形如 [Current quickstart step: ...]（或其他 [...] 状态行）的方括号行。这些是界面注入的状态，不是用户输入的内容。请静默地据此行动 —— 绝不要在回复中重复、引用或提及它们。

  HOW TO ASK QUESTIONS：
  - 调用 ask_user_questions 工具来提出结构化的多选问题。绝不用文字散文提问。
  - 把 1-4 个相关问题合并到一次工具调用中。
  - 每个问题需要 2-3 个具体选项。不要添加 "Other" 选项 —— 界面会自动添加。
  - 工具调用前：最多一句简短的引导语。不要写成段落。
  - 绝不向用户输出 agent 配置或 JSON。

  LINKS：
  - Anthropic Console 位于 https://platform.claude.com。给用户链接到 Console 时使用该 URL（而不是 console.anthropic.com）。`;

export function resolveQuickstartSystem(locale: Locale): Array<Record<string, unknown>> {
  if (locale === 'zh-CN') {
    const block0 = englishSystem[0];
    const block1 = englishSystem[1];
    return [block0, { ...block1, text: agentBuilderBlock1Zh }];
  }
  return englishSystem;
}

// Model-facing tool_result text. These strings steer the conversation and must stay in
// the same language as the resolved system prompt so a single quickstart run reads in
// one language. Technical identifiers (ids, URLs, tool names, endpoints) are preserved.
export function quickstartToolResultText(locale: Locale) {
  const zh = locale === 'zh-CN';
  return {
    noOrganization: zh
      ? '当前没有可用于 managed agent quickstart 代理的组织。'
      : 'No organization is available for the managed agent quickstart proxy.',
    selectEnvironmentFirst: zh
      ? '在开始 session 之前，请先选择或创建一个环境。'
      : 'Select or create an environment before starting a session.',
    offerNextStepEnvHint: (message: string) =>
      zh
        ? `${message} 请先用 reuse_environment_id 或新的环境配置调用 create_environment，再提供下一步。`
        : `${message} Call create_environment with a reuse_environment_id or a new environment config before offering the next step.`,
    agentReadyFirst: zh
      ? '在离开 session 步骤之前，请先示意 agent 已就绪。'
      : 'Signal that the agent is ready before advancing past the session step.',
    offerNextStepSessionHint: (message: string) =>
      zh
        ? `${message} 请先用 suggested_first_message 调用 agent_ready，再提供下一步。`
        : `${message} Call agent_ready with a suggested_first_message before offering the next step.`,
    vaultSharingNoticeShown: zh ? '已展示 vault 共享提示。' : 'Vault sharing notice shown.',
    environmentsLoaded: zh ? '环境已加载' : 'Environments loaded',
    environmentSelected: (id: string) => (zh ? `已选择环境（id: ${id}）。` : `Environment selected (id: ${id}).`),
    environmentCreated: (id: string) => (zh ? `已创建环境（id: ${id}）。` : `Environment created (id: ${id}).`),
    vaultsLoaded: zh ? 'Vault 已加载' : 'Vaults loaded',
    vaultSelected: (ids: string) => (zh ? `已选择 vault（${ids}）。` : `Vault selected (${ids}).`),
    noVaultSelected: zh ? '未选择 vault。' : 'No vault selected.',
    vaultCreated: (id: string) => (zh ? `已创建 vault（id: ${id}）。` : `Vault created (id: ${id}).`),
    scheduleIntentFlagged: zh ? '已标记部署排程意图。' : 'Deployment schedule intent flagged.',
    scheduleIntentCleared: zh ? '已清除部署排程意图。' : 'Deployment schedule intent cleared.',
    deploymentRequiresAgentEnv: zh
      ? '创建 deployment 之前需要先有 agent 和环境。'
      : 'Agent and environment are required before creating a deployment.',
    deploymentCreated: (id: string) =>
      zh
        ? `已创建 deployment（id: ${id}）。排程已生效 —— 运行记录会出现在 deployment 页面。`
        : `Deployment created (id: ${id}). The schedule is live — runs appear on the deployment page.`,
    webSearchUpstream: zh ? 'web_search 由上游模型处理。' : 'web_search is handled by the upstream model.',
    toolCompleted: (name: string) => (zh ? `${name} 已完成。` : `${name} completed.`),
    agentCreated: zh ? 'Agent 已创建。' : 'Agent created.',
    createAgentFailed: (message: string) =>
      zh
        ? `创建 agent 失败：${message} 请询问用户是否要重试。`
        : `Failed to create agent: ${message} Ask the user if they'd like to retry.`,
    nextStepSelected: zh ? '已选择下一步。' : 'Next step selected.',
    environmentSelectedShort: zh ? '已选择环境。' : 'Environment selected.',
    selectVaultBeforeCredential: zh
      ? '创建凭据之前，请先选择或创建一个 vault。'
      : 'Select or create a vault before creating a credential.',
    credentialNotSupported: (server: string) =>
      zh
        ? `此后端暂不支持为 ${server} 授权凭据。未创建任何凭据。`
        : `Credential authorization for ${server} is not supported by this backend yet. No credential was created.`,
    credentialCreated: (id: string) => (zh ? `已创建凭据（id: ${id}）。` : `Credential created (id: ${id}).`),
    credentialSkipped: (server: string) =>
      zh
        ? `已跳过 ${server} 的授权 —— 当 agent 尝试使用该服务器时，测试 session 可能失败或报错。用户仍可继续；请在总结中提到被跳过的服务器，以及之后可以在 vault 中补充凭据。`
        : `${server} authorization skipped — the test session may fail or error when the agent tries to use this server. The user can still proceed; mention the skipped servers in your summary and that they can add credentials later in the vault.`,
    exitedToAgentDetail: zh ? '用户已退出到 agent 详情页。' : 'User exited to agent detail.',
    scaffoldCopied: zh ? '已复制脚手架提示词。' : 'Scaffold prompt copied.',
    sessionRequiresAgentEnv: zh
      ? '开始测试 session 之前需要先有 agent 和环境。'
      : 'Agent and environment are required before starting a test session.',
    sessionCreatedWithMessage: (id: string, message: string) =>
      zh
        ? `已创建 session（id: ${id}）。建议的第一条消息：${message}`
        : `Session created (id: ${id}). Suggested first message: ${message}`,
    sessionCreated: (id: string) => (zh ? `已创建 session（id: ${id}）。` : `Session created (id: ${id}).`),
    firstTestMessageSent: (id: string) =>
      zh ? `已向 session ${id} 发送第一条测试消息。` : `First test message sent to session ${id}.`,
    sessionClosed: (id: string) => (zh ? `session 已关闭（id: ${id}）。` : `Session closed (id: ${id}).`),
    thisMcpServer: zh ? '此 MCP server' : 'this MCP server',
  };
}

export type QuickstartToolResultText = ReturnType<typeof quickstartToolResultText>;

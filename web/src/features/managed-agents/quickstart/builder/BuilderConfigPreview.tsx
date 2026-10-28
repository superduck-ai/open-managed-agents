import { requiredEnvironmentPackages } from './environmentRequirements';
import { useI18n } from '../../../../shared/i18n';
import { Accordion, AccordionItem, AccordionTrigger, AccordionContent } from '../../../../shared/ui/accordion';
import { SyntaxCodeBlock } from '../../components/CodeBlocks';
import { environmentInputSchema, validatedDeployment } from './tools';
import type { BuilderState, BuilderTool } from './model';
import { stableStringify } from '../platformQuickstartRequest';
import { cronToSchedule, previewSchedule } from '../../resources/deployment-schedule';

export function BuilderConfigPreview({ state, tool }: { state: BuilderState; tool: BuilderTool }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  if (tool.name === 'build_agent_config')
    return (
      <div className="space-y-4">
        <h4 className="break-words text-base font-medium">{state.config.name}</h4>
        <EnvironmentRequirementsPreview state={state} />
        {state.agent && state.savedConfig && (
          <div className="space-y-2 text-sm">
            <p className="font-mono text-xs text-muted-foreground">
              v{state.agent.version} → v{state.agent.version + 1}
            </p>
            <p>
              {zh ? '变更字段：' : 'Changed fields: '}
              {Object.keys(state.config)
                .filter(
                  (key) =>
                    stableStringify(state.config[key as keyof typeof state.config]) !==
                    stableStringify(state.savedConfig![key as keyof typeof state.config]),
                )
                .map(
                  (key) =>
                    ({
                      name: zh ? '名称' : 'Name',
                      description: zh ? '描述' : 'Description',
                      model: zh ? '模型' : 'Model',
                      system: zh ? '系统提示词' : 'System prompt',
                      mcp_servers: 'MCP',
                      tools: zh ? '工具' : 'Tools',
                      skills: zh ? '技能' : 'Skills',
                      metadata: zh ? '元数据' : 'Metadata',
                    })[key] ?? key,
                )
                .join(', ')}
            </p>
          </div>
        )}
        <p className="text-xs text-muted-foreground">
          {typeof state.config.model === 'string' ? state.config.model : state.config.model.id}
        </p>
        <p className="max-h-40 overflow-y-auto break-words whitespace-pre-wrap text-sm leading-6">
          {state.config.system}
        </p>
        {state.config.mcp_servers.length > 0 && (
          <p>{state.config.mcp_servers.map((server) => server.name).join(', ')}</p>
        )}
        <Accordion>
          <AccordionItem value="config">
            <AccordionTrigger>JSON</AccordionTrigger>
            <AccordionContent>
              <SyntaxCodeBlock language="json" value={JSON.stringify(state.config, null, 2)} />
            </AccordionContent>
          </AccordionItem>
        </Accordion>
      </div>
    );
  if (tool.name === 'create_environment') return <EnvironmentPreview tool={tool} />;
  const deployment = validatedDeployment(tool);
  return (
    <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-3 break-words">
      <dt>{zh ? '名称' : 'Name'}</dt>
      <dd>{deployment.name}</dd>
      <dt>{zh ? '周期' : 'Schedule'}</dt>
      <dd>
        <DeploymentSchedule expression={deployment.cron_expression} timezone={deployment.timezone} />
      </dd>
      <dt>{zh ? '时区' : 'Timezone'}</dt>
      <dd>{deployment.timezone}</dd>
      <dt>{zh ? '智能体' : 'Agent'}</dt>
      <dd>
        {state.agent?.name} v{state.agent?.version}
      </dd>
      <dt>{zh ? '环境' : 'Environment'}</dt>
      <dd>{state.environment?.name}</dd>
      {state.vaults.length > 0 && (
        <>
          <dt>{zh ? '保险库' : 'Vaults'}</dt>
          <dd>{state.vaults.map((vault) => vault.name).join(', ')}</dd>
        </>
      )}
      <dt>{zh ? '初始消息' : 'Initial message'}</dt>
      <dd className="whitespace-pre-wrap">{deployment.initial_message}</dd>
    </dl>
  );
}

function EnvironmentRequirementsPreview({ state }: { state: BuilderState }) {
  const { locale } = useI18n();
  const entries = Object.entries(requiredEnvironmentPackages(state.config)).filter(
    ([manager, specs]) => manager !== 'type' && specs.length > 0,
  );
  if (!entries.length) return null;
  return (
    <div className="space-y-1 text-sm">
      <p>
        {locale === 'zh-CN'
          ? '环境依赖要求（配置后在运行时准备）'
          : 'Required environment packages (prepared at runtime after configuration)'}
      </p>
      <p>
        {entries.map(([manager, specs]) => `${manager}: ${Array.isArray(specs) ? specs.join(', ') : specs}`).join('; ')}
      </p>
    </div>
  );
}

function EnvironmentPreview({ tool }: { tool: BuilderTool }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const input = environmentInputSchema.parse(tool.input);
  const network = input.config?.networking;
  return (
    <dl className="space-y-3">
      <div>
        <dt className="text-xs text-muted-foreground">{zh ? '名称' : 'Name'}</dt>
        <dd>{input.name}</dd>
      </div>
      <div>
        <dt className="text-xs text-muted-foreground">{zh ? '网络' : 'Network'}</dt>
        <dd>
          {network?.type === 'unrestricted'
            ? zh
              ? '允许访问互联网'
              : 'Internet access'
            : network?.allowed_hosts.join(', ')}
        </dd>
      </div>
      {input.config?.packages && (
        <div>
          <dt className="text-xs text-muted-foreground">{zh ? '运行时软件包配置' : 'Runtime package configuration'}</dt>
          <dd>
            <p className="text-xs text-muted-foreground">
              {zh
                ? '保存配置不表示安装已完成，安装与工具执行结果以试跑事件为准。'
                : 'Saving this configuration does not confirm installation. Check actual test events for installation and execution results.'}
            </p>
            {Object.entries(input.config.packages)
              .filter(([key]) => key !== 'type')
              .map(([key, value]) => `${key}: ${Array.isArray(value) ? value.join(', ') : value}`)
              .join('\n')}
          </dd>
        </div>
      )}
    </dl>
  );
}

function DeploymentSchedule({ expression, timezone }: { expression: string; timezone: string }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const schedule = cronToSchedule(expression);
  const frequency = {
    minute: zh ? '每分钟' : 'Every minute',
    hour: zh ? '每小时' : 'Hourly',
    daily: zh ? '每天' : 'Daily',
    weekdays: zh ? '每个工作日' : 'Weekdays',
    weekly: zh ? '每周' : 'Weekly',
    custom: zh ? '自定义周期' : 'Custom schedule',
  }[schedule.frequency];
  const next = previewSchedule(expression, timezone).runs[0];
  return (
    <div className="space-y-2">
      <p>
        {frequency}
        {['daily', 'weekdays', 'weekly'].includes(schedule.frequency) ? ` ${schedule.time}` : ''}
      </p>
      {next && (
        <p className="text-xs text-muted-foreground">
          {zh ? '下次计划：' : 'Next scheduled: '}
          {new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short', timeZone: timezone }).format(
            next,
          )}
        </p>
      )}
      <Accordion>
        <AccordionItem value="cron">
          <AccordionTrigger>{zh ? 'Cron 详情' : 'Cron details'}</AccordionTrigger>
          <AccordionContent>
            <code>{expression}</code>
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  );
}

import { useI18n } from '../../../../shared/i18n';
import { Accordion, AccordionItem, AccordionTrigger, AccordionContent } from '../../../../shared/ui/accordion';
import { environmentInputSchema, validatedDeployment } from './tools';
import type { BuilderState, BuilderTool } from './model';
import { cronToSchedule, previewSchedule } from '../../resources/deployment-schedule';

export function BuilderConfigPreview({ state, tool }: { state: BuilderState; tool: BuilderTool }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
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

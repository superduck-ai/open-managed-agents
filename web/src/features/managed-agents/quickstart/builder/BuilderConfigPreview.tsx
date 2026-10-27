import { useI18n } from '../../../../shared/i18n';
import { Accordion, AccordionItem, AccordionTrigger, AccordionContent } from '../../../../shared/ui/accordion';
import { SyntaxCodeBlock } from '../../components/CodeBlocks';
import { environmentInputSchema, validatedDeployment } from './tools';
import type { BuilderState, BuilderTool } from './model';

export function BuilderConfigPreview({ state, tool }: { state: BuilderState; tool: BuilderTool }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  if (tool.name === 'build_agent_config')
    return (
      <div className="space-y-4">
        <h4 className="text-base font-medium">{state.config.name}</h4>
        <p className="text-xs text-muted-foreground">
          {typeof state.config.model === 'string' ? state.config.model : state.config.model.id}
        </p>
        <p className="max-h-80 overflow-y-auto whitespace-pre-wrap leading-7">{state.config.system}</p>
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
      <dd>{deployment.cron_expression}</dd>
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
          <dt className="text-xs text-muted-foreground">{zh ? '软件包' : 'Packages'}</dt>
          <dd>
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

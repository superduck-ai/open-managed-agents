import { useRef, useState } from 'react';
import { Button } from '../../../../shared/ui/button';
import { Checkbox } from '../../../../shared/ui/checkbox';
import { useI18n } from '../../../../shared/i18n';
import { BuilderConfigPreview } from './BuilderConfigPreview';
import { CredentialDialog } from '../../resources/dialogs';
import { createVaultCredential } from '../../api';
import { authenticationServers } from './api';
import { BuilderQuestions } from './BuilderQuestions';
import { invalidateBuilderTest } from './model';
import type { Builder } from './useBuilder';

type GateProps = { builder: Builder; workspaceId: string; orgUuid: string };
export function BuilderGate({
  builder,
  workspaceId,
  orgUuid,
}: {
  builder: Builder;
  workspaceId: string;
  orgUuid: string;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const { state } = builder;
  const tool = state.gate;
  if (!tool) return null;
  if (tool.name === 'ask_user_questions')
    return <BuilderQuestions tool={tool} disabled={builder.busy} onAnswer={(answer) => void builder.resolve(answer)} />;
  if (tool.name === 'pick_environment') return <EnvironmentGate {...{ builder, workspaceId, orgUuid }} />;
  if (['pick_vault', 'select_vault'].includes(tool.name)) return <VaultGate {...{ builder, workspaceId, orgUuid }} />;
  if (tool.name === 'create_vault_credential') return <CredentialGate {...{ builder, workspaceId, orgUuid }} />;
  if (tool.name === 'await_test_run')
    return (
      <p className="text-muted-foreground">
        {zh ? '在试跑中继续对话，结束后回来复盘。' : 'Continue in the test, then end it to review.'}
      </p>
    );
  if (tool.name === 'offer_next_step') return <NextStepGate {...{ builder, workspaceId, orgUuid }} />;
  return <ConfirmationGate {...{ builder, workspaceId, orgUuid }} />;
}
function ConfirmationGate({ builder }: GateProps) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const state = builder.state;
  const tool = state.gate!;
  if (tool.name === 'show_integration_exits') return null;
  return (
    <div className="space-y-4">
      {tool.name === 'build_agent_config' && (
        <>
          <h3 className="font-medium">
            {state.agent ? (zh ? '保存新版本' : 'Save new version') : zh ? '创建智能体' : 'Create agent'}
          </h3>
          <BuilderConfigPreview state={state} tool={tool} />
        </>
      )}
      {tool.name === 'create_environment' && (
        <>
          <h3 className="font-medium">{zh ? '创建环境' : 'Create environment'}</h3>
          <BuilderConfigPreview state={state} tool={tool} />
        </>
      )}
      {tool.name === 'create_vault' && <p>{String(tool.input.name)}</p>}
      {tool.name === 'agent_ready' && <p>{state.suggestedMessage}</p>}
      {tool.name === 'create_deployment' && (
        <>
          <h3 className="font-medium">{zh ? '创建定时运行' : 'Create scheduled deployment'}</h3>
          <BuilderConfigPreview state={state} tool={tool} />
        </>
      )}
      <div className="flex gap-2">
        <Button disabled={builder.busy || Boolean(state.operation)} onClick={() => void builder.confirm()}>
          {tool.name === 'agent_ready' ? (zh ? '开始试跑' : 'Start test') : zh ? '确认' : 'Confirm'}
        </Button>
        <Button
          variant="ghost"
          disabled={builder.busy || Boolean(state.operation)}
          onClick={() =>
            void builder.resolve(
              { user_choice: zh ? '修改' : 'Change', decision: 'declined action; ask what to change' },
              {
                config: state.savedConfig ?? state.config,
              },
            )
          }
        >
          {zh ? '修改' : 'Change'}
        </Button>
      </div>
    </div>
  );
}

function EnvironmentGate({ builder }: GateProps) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  return (
    <div className="space-y-3">
      {builder.catalog.data?.environments
        .filter((environment) => !environment.archived_at)
        .map((environment) => (
          <Button
            key={environment.id}
            variant="outline"
            className="w-full justify-start"
            disabled={builder.busy}
            onClick={() => void builder.selectEnvironment(environment.id)}
          >
            {environment.name}
          </Button>
        ))}
      <Button
        variant="secondary"
        disabled={builder.busy}
        onClick={() =>
          void builder.resolve({
            user_choice: zh ? '新建环境' : 'New environment',
            request: 'Ask one question at a time to prepare a new environment.',
          })
        }
      >
        {zh ? '新建环境' : 'New environment'}
      </Button>
    </div>
  );
}

function VaultGate({ builder }: GateProps) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const { state } = builder;
  const [selected, setSelected] = useState(state.vaults.map((vault) => vault.id));
  return (
    <div className="space-y-3">
      {builder.vaults.map((vault) => (
        <label key={vault.id} className="flex items-center gap-3 rounded-lg border-[0.5px] border-border p-3">
          <Checkbox
            disabled={builder.busy}
            checked={selected.includes(vault.id)}
            onCheckedChange={(checked) =>
              setSelected((value) => (checked ? [...value, vault.id] : value.filter((id) => id !== vault.id)))
            }
          />
          <span className="flex-1">{vault.name}</span>
          {vault.covered.length > 0 && (
            <span className="text-xs text-muted-foreground">{vault.covered.join(', ')}</span>
          )}
        </label>
      ))}
      <div className="flex gap-2">
        <Button
          disabled={builder.busy || !selected.length}
          onClick={() =>
            void builder.resolve(
              { selected_vaults: selected },
              {
                ...invalidateBuilderTest(state),
                vaults: builder.vaults
                  .filter((vault) => selected.includes(vault.id))
                  .map(({ id, name }) => ({ id, name })),
              },
            )
          }
        >
          {zh ? '使用所选保险库' : 'Use selected vaults'}
        </Button>
        <Button
          variant="outline"
          disabled={builder.busy}
          onClick={() =>
            void builder.resolve({
              user_choice: zh ? '新建保险库' : 'New vault',
              request: 'Ask for its name, then propose creating the vault.',
            })
          }
        >
          {zh ? '新建保险库' : 'New vault'}
        </Button>
      </div>
    </div>
  );
}

function CredentialGate({ builder, workspaceId, orgUuid }: GateProps) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const { state } = builder;
  const tool = state.gate!;
  const [credentialOpen, setCredentialOpen] = useState(false);
  const credentialCompleted = useRef(false);
  const focus = useRef<HTMLButtonElement>(null);
  const server = builder.catalog.data
    ? authenticationServers(state.savedConfig ?? state.config, builder.catalog.data).find(
        (item) => item.name === tool.input.mcp_server_name,
      )
    : undefined;
  const vaultId = String(tool.input.vault_id);
  return (
    <div className="space-y-3">
      <p className="font-medium">{String(tool.input.mcp_server_name)}</p>
      <div className="flex gap-2">
        <Button ref={focus} disabled={builder.busy || !server} onClick={() => setCredentialOpen(true)}>
          {zh ? '连接或添加凭据' : 'Connect or add credential'}
        </Button>
        <Button
          variant="ghost"
          disabled={builder.busy}
          onClick={() =>
            void builder.resolve(
              { authorized: false, skipped: true, service: tool.input.mcp_server_name },
              { skippedServers: [...state.skippedServers, ...(server ? [server.url] : [])] },
            )
          }
        >
          {zh ? '暂时跳过' : 'Skip for now'}
        </Button>
      </div>
      {credentialOpen && server && (
        <CredentialDialog
          vaultId={vaultId}
          workspaceId={workspaceId}
          orgUuid={orgUuid}
          initialMcpServerUrl={server.url}
          returnFocus={focus}
          onClose={() => {
            setCredentialOpen(false);
            if (credentialCompleted.current) {
              credentialCompleted.current = false;
              void builder.credentialDone();
            }
          }}
          onSubmit={async (values) => {
            if (values.mcpServerUrl !== server.url || !['static_bearer', 'mcp_oauth'].includes(values.authType))
              throw new Error(zh ? '请选择当前服务的凭据。' : 'Choose a credential for this service.');
            await createVaultCredential(vaultId, values, workspaceId);
            credentialCompleted.current = true;
          }}
          onOAuthComplete={() => {
            credentialCompleted.current = true;
          }}
        />
      )}
    </div>
  );
}

function NextStepGate({ builder }: GateProps) {
  const { locale } = useI18n();
  return (
    <Button disabled={builder.busy} onClick={() => void builder.next()}>
      {locale === 'zh-CN' ? '继续' : 'Continue'}
    </Button>
  );
}

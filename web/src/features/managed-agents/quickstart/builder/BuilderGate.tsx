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
import { environmentFormValues } from '../../environments/model';
import { ManagedErrorAlert } from '../../components/common';
import type { Builder } from './useBuilder';
import { useBuilderPanel } from './panelContext';

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
  if (state.readError)
    return (
      <div className="space-y-3">
        <ManagedErrorAlert>{state.readError}</ManagedErrorAlert>
        <Button disabled={builder.busy} onClick={() => void builder.retryRead()}>
          {zh ? '重试加载凭据' : 'Retry loading credentials'}
        </Button>
      </div>
    );
  if (tool.name === 'ask_user_questions')
    return <BuilderQuestions tool={tool} disabled={builder.busy} onAnswer={(answer) => void builder.resolve(answer)} />;
  if (tool.name === 'pick_environment') return <EnvironmentGate {...{ builder, workspaceId, orgUuid }} />;
  if (['pick_vault', 'select_vault'].includes(tool.name)) return <VaultGate {...{ builder, workspaceId, orgUuid }} />;
  if (tool.name === 'create_vault_credential') return <CredentialGate {...{ builder, workspaceId, orgUuid }} />;
  if (tool.name === 'offer_next_step') return <NextStepGate {...{ builder, workspaceId, orgUuid }} />;
  return <ConfirmationGate {...{ builder, workspaceId, orgUuid }} />;
}
function ConfirmationGate({ builder }: GateProps) {
  const openPanel = useBuilderPanel();
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const state = builder.state;
  const tool = state.gate!;
  return (
    <div className="space-y-4">
      {tool.name === 'build_agent_config' && (
        <>
          <h3 className="font-medium">
            {state.agent ? (zh ? '保存新版本' : 'Save new version') : zh ? '创建智能体' : 'Create agent'}
          </h3>
          <h4 className="break-words text-base font-medium">{state.config.name}</h4>
          <p className="text-sm text-muted-foreground">
            {zh ? '配置面板显示了本次提议，请确认后保存。' : 'The config panel shows the proposed version.'}
          </p>
          <Button variant="outline" onClick={() => openPanel({ kind: 'agent' })}>
            {zh ? '查看配置' : 'Review configuration'}
          </Button>
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
        <Button
          disabled={builder.busy || builder.draftEditing || Boolean(state.operation)}
          onClick={() => void builder.confirm()}
        >
          {confirmationLabel(tool.name, state.agent?.version, zh)}
        </Button>
        <Button
          variant="ghost"
          disabled={builder.busy || builder.draftEditing || Boolean(state.operation)}
          onClick={() =>
            void builder.resolve(
              { user_choice: zh ? '修改' : 'Change', decision: 'declined action; ask what to change' },
              {
                config: state.savedConfig ?? state.config,
              },
            )
          }
        >
          {zh ? '继续调整' : 'Keep refining'}
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
      <h3 className="font-medium">{zh ? '选择运行环境' : 'Choose an environment'}</h3>
      <p className="text-sm text-muted-foreground">
        {zh
          ? '环境决定智能体可以访问的网络和使用的软件。选择已有环境，或创建一个新环境。'
          : 'An environment defines the network and software available to your agent. Choose an existing environment or create one.'}
      </p>
      {builder.catalog.data?.environments
        .filter((environment) => !environment.archived_at)
        .map((environment) => (
          <Button
            key={environment.id}
            variant="outline"
            className="h-auto min-h-11 w-full justify-start whitespace-normal text-left"
            disabled={builder.busy}
            onClick={() => void builder.selectEnvironment(environment.id)}
          >
            <span className="min-w-0 space-y-1">
              <span className="block break-words">{environment.name}</span>
              <span className="block text-xs font-normal text-muted-foreground">
                {environmentFormValues(environment).networkType === 'unrestricted'
                  ? zh
                    ? '允许访问互联网'
                    : 'Internet access'
                  : zh
                    ? '受限网络'
                    : 'Restricted network'}
              </span>
            </span>
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
      {builder.vaultsLoading && (
        <p role="status" className="text-sm text-muted-foreground">
          {zh ? '正在检查凭据覆盖…' : 'Checking credential coverage…'}
        </p>
      )}
      {builder.vaultsError && (
        <div className="space-y-2">
          <ManagedErrorAlert>{builder.vaultsError}</ManagedErrorAlert>
          <Button variant="outline" onClick={builder.retryVaults}>
            {zh ? '重试加载保险库' : 'Retry loading vaults'}
          </Button>
        </div>
      )}
      {!builder.vaultsLoading &&
        !builder.vaultsError &&
        builder.vaults.map((vault) => (
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
          disabled={builder.busy || builder.vaultsLoading || Boolean(builder.vaultsError) || !selected.length}
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

function confirmationLabel(toolName: string, version: number | undefined, zh: boolean) {
  if (toolName === 'build_agent_config')
    return version == null
      ? zh
        ? '创建智能体'
        : 'Create agent'
      : zh
        ? `保存为 v${version + 1}`
        : `Save as v${version + 1}`;
  if (toolName === 'agent_ready') return zh ? '开始试跑' : 'Start test';
  if (toolName === 'create_environment') return zh ? '创建环境' : 'Create environment';
  if (toolName === 'create_deployment') return zh ? '创建定时运行' : 'Create scheduled deployment';
  return zh ? '创建保险库' : 'Create vault';
}

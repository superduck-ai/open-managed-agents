import { quickstartModelSetupHref } from '../../../shared/workspaces/model-setup-navigation';
import { useEffect, useState } from 'react';
import { useRouter } from '@tanstack/react-router';
import { productIcons } from '@/shared/icons/product-icons';
import { Tabs, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '../../../shared/ui/empty';
import { BuilderPage } from './builder/BuilderPage';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '../../../shared/auth/context';
import { useI18n } from '../../../shared/i18n';
import { useWorkspace, useWorkspaceRuntime } from '../../../shared/workspaces/context';
import { isModelConfigurationUnavailable } from '../../../shared/api/client';
import { Button } from '../../../shared/ui/button';
import { LLMProviderRequired } from '../../llm-providers/LLMProviderRequired';
import { ManagedErrorAlert } from '../components/common';
import type { AgentModelOption } from '../agents/create-dialog-model';
import { listCreateAgentModels } from '../agents/create-dialog-api';
import { QuickstartWizardPage } from './QuickstartWizardPage';

export function AgentQuickstartPage() {
  const { activeWorkspaceId, orgUuid } = useWorkspace();
  const runtime = useWorkspaceRuntime();
  const { msg } = useI18n();
  const { account } = useAuth();
  const models = useQuery({
    queryKey: ['agent-quickstart', 'models', account?.uuid, activeWorkspaceId],
    queryFn: () => listCreateAgentModels(activeWorkspaceId),
    retry: false,
  });
  if (models.isPending) return <section aria-busy="true" className="min-h-96" />;
  if (isModelConfigurationUnavailable(models.error) || (!models.isError && !models.data?.length)) {
    if (!runtime.capabilities.byok_llm_providers) {
      return (
        <ExternalRuntimeModelsUnavailable runtimeName={runtime.profile_name} onRetry={() => void models.refetch()} />
      );
    }
    return (
      <div className="flex min-h-96 items-center justify-center">
        <LLMProviderRequired
          onConfigure={() =>
            window.location.assign(quickstartModelSetupHref(activeWorkspaceId, window.location.search))
          }
        />
      </div>
    );
  }
  if (models.isError)
    return (
      <div className="mx-auto flex max-w-xl flex-col gap-4 py-20">
        <ManagedErrorAlert>{msg('managedAgents.models.loadFailedBody', 'Unable to load models.')}</ManagedErrorAlert>
        <Button onClick={() => void models.refetch()}>{msg('common.retry', 'Retry')}</Button>
      </div>
    );
  return (
    <QuickstartModes
      accountId={account?.uuid ?? ''}
      workspaceId={activeWorkspaceId}
      orgUuid={orgUuid ?? ''}
      models={models.data}
    />
  );
}

function ExternalRuntimeModelsUnavailable({ runtimeName, onRetry }: { runtimeName: string; onRetry: () => void }) {
  const { locale, msg } = useI18n();
  const zh = locale === 'zh-CN';
  return (
    <div className="flex min-h-96 items-center justify-center">
      <Empty data-testid="external-runtime-models-unavailable" className="min-h-[320px] max-w-xl border-0">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <productIcons.models aria-hidden />
          </EmptyMedia>
          <EmptyTitle className="text-base">{zh ? '暂无可用运行时模型' : 'No runtime models available'}</EmptyTitle>
          <EmptyDescription>
            {zh
              ? `当前工作区使用 ${runtimeName} 运行时，暂时未返回可用模型。请稍后重试，或联系管理员检查运行时配置。`
              : `This workspace uses ${runtimeName}, and no models are currently available. Retry or check the runtime configuration.`}
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button type="button" onClick={onRetry}>
            {msg('common.retry', 'Retry')}
          </Button>
        </EmptyContent>
      </Empty>
    </div>
  );
}

function QuickstartModes({
  accountId,
  workspaceId,
  orgUuid,
  models,
}: {
  accountId: string;
  workspaceId: string;
  orgUuid: string;
  models: AgentModelOption[];
}) {
  const { locale } = useI18n();
  const router = useRouter();
  const [navigationKey, setNavigationKey] = useState(0);
  const [mode, setMode] = useState(() =>
    new URLSearchParams(window.location.search).get('mode') === 'api' ? 'api' : 'chat',
  );
  useEffect(
    () =>
      router.subscribe('onBeforeNavigate', ({ toLocation }) => {
        if (!/^(?:\/quickstart|\/workspaces\/[^/]+\/agent-quickstart)\/?$/.test(toLocation.pathname)) return;
        setNavigationKey((value) => value + 1);
        setMode(new URLSearchParams(toLocation.searchStr).get('mode') === 'api' ? 'api' : 'chat');
      }),
    [router],
  );
  const selectMode = (nextMode: string) => {
    setMode(nextMode);
    const url = new URL(window.location.href);
    url.searchParams.set('mode', nextMode);
    window.history.replaceState(window.history.state, '', url);
  };
  const modeControl = (
    <Tabs className="items-center shrink-0" value={mode} onValueChange={(value) => selectMode(String(value))}>
      <TabsList aria-label={locale === 'zh-CN' ? '创建方式' : 'Build method'}>
        <TabsTrigger className="px-4" value="chat">
          {locale === 'zh-CN' ? '对话构建' : 'Build with chat'}
        </TabsTrigger>
        <TabsTrigger className="px-4" value="api">
          {locale === 'zh-CN' ? '手动配置' : 'Manual setup'}
        </TabsTrigger>
      </TabsList>
    </Tabs>
  );
  const startHeader = (
    <header className="mx-auto flex w-full max-w-[880px] shrink-0 flex-col items-center gap-[clamp(1rem,2.5dvh,1.5rem)] pt-[clamp(1.5rem,4.5dvh,4rem)]">
      <h1 id="quickstart-start-title" className="text-center text-2xl font-semibold">
        {locale === 'zh-CN' ? '你想构建什么？' : 'What would you like to build?'}
      </h1>
      {modeControl}
    </header>
  );
  return (
    <div className="mx-auto flex h-full min-h-0 w-full min-w-0 flex-1 flex-col gap-[clamp(1rem,3dvh,1.5rem)]">
      {mode === 'chat' ? (
        <BuilderPage
          key={`${accountId}:${workspaceId}:${navigationKey}`}
          startHeader={startHeader}
          accountId={accountId}
          workspaceId={workspaceId}
          orgUuid={orgUuid}
          models={models.map((model) => model.id)}
          onSwitchToManual={() => selectMode('api')}
        />
      ) : (
        <QuickstartWizardPage
          key={`${accountId}:${workspaceId}:${navigationKey}`}
          startHeader={startHeader}
          workspaceID={workspaceId}
          accountID={accountId}
          models={models}
        />
      )}
    </div>
  );
}

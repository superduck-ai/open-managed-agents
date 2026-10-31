import { useEffect, useState } from 'react';
import { useRouter } from '@tanstack/react-router';
import { Tabs, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { BuilderPage } from './builder/BuilderPage';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '../../../shared/auth/context';
import { useI18n } from '../../../shared/i18n';
import { useWorkspace } from '../../../shared/workspaces/context';
import { isModelConfigurationUnavailable } from '../../../shared/api/client';
import { Button } from '../../../shared/ui/button';
import { LLMProviderRequired } from '../../llm-providers/LLMProviderRequired';
import { ManagedErrorAlert } from '../components/common';
import type { AgentModelOption } from '../agents/create-dialog-model';
import { listCreateAgentModels } from '../agents/create-dialog-api';
import { QuickstartWizardPage } from './QuickstartWizardPage';

export function AgentQuickstartPage() {
  const { activeWorkspaceId, orgUuid } = useWorkspace();
  const { msg } = useI18n();
  const { account } = useAuth();
  const models = useQuery({
    queryKey: ['agent-quickstart', 'models', account?.uuid, activeWorkspaceId],
    queryFn: () => listCreateAgentModels(activeWorkspaceId),
    retry: false,
  });
  if (models.isPending) return <section aria-busy="true" className="min-h-96" />;
  if (isModelConfigurationUnavailable(models.error) || (!models.isError && !models.data?.length)) {
    return (
      <div className="flex min-h-96 items-center justify-center">
        <LLMProviderRequired
          onConfigure={() => window.location.assign(`/workspaces/${encodeURIComponent(activeWorkspaceId)}/llm-models`)}
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
  const modeControl = (
    <Tabs
      className="items-center shrink-0"
      value={mode}
      onValueChange={(value) => {
        setMode(String(value));
        const url = new URL(window.location.href);
        url.searchParams.set('mode', String(value));
        window.history.replaceState(window.history.state, '', url);
      }}
    >
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
    <header className="mx-auto flex w-full max-w-[880px] shrink-0 flex-col items-center gap-6 pt-[clamp(2rem,7vh,5rem)]">
      <h1 id="quickstart-start-title" className="text-center text-2xl font-semibold">
        {locale === 'zh-CN' ? '你想构建什么？' : 'What would you like to build?'}
      </h1>
      {modeControl}
    </header>
  );
  return (
    <div className="mx-auto flex h-full min-h-0 w-full min-w-0 flex-1 flex-col gap-6">
      {mode === 'chat' ? (
        <BuilderPage
          key={`${accountId}:${workspaceId}:${navigationKey}`}
          startHeader={startHeader}
          accountId={accountId}
          workspaceId={workspaceId}
          orgUuid={orgUuid}
          models={models.map((model) => model.id)}
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

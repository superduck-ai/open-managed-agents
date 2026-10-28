import { useState } from 'react';
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
import { listCreateAgentModels } from '../agents/create-dialog-api';
import { QuickstartWizardPage } from './QuickstartWizardPage';

export function AgentQuickstartPage() {
  const { activeWorkspaceId, orgUuid } = useWorkspace();
  const { msg, locale } = useI18n();
  const { account } = useAuth();
  const [mode, setMode] = useState(() =>
    new URLSearchParams(window.location.search).get('mode') === 'api' ? 'api' : 'chat',
  );
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
  const modeControl = (
    <Tabs
      className="items-center shrink-0"
      value={mode}
      onValueChange={(value) => {
        setMode(String(value));
        const url = new URL(window.location.href);
        url.searchParams.set('mode', String(value));
        window.history.replaceState(null, '', url);
      }}
    >
      <TabsList aria-label={locale === 'zh-CN' ? '创建方式' : 'Build method'}>
        <TabsTrigger className="px-4" value="chat">
          {locale === 'zh-CN' ? '问答构建' : 'Build with chat'}
        </TabsTrigger>
        <TabsTrigger className="px-4" value="api">
          {locale === 'zh-CN' ? 'API 创建' : 'Build with API'}
        </TabsTrigger>
      </TabsList>
    </Tabs>
  );
  return (
    <div className="flex h-full min-h-0 flex-1 flex-col gap-4">
      {mode === 'chat' ? (
        <BuilderPage
          key={`${account?.uuid}:${activeWorkspaceId}`}
          accountId={account?.uuid ?? ''}
          workspaceId={activeWorkspaceId}
          orgUuid={orgUuid ?? ''}
          modeControl={modeControl}
          models={models.data.map((model) => model.id)}
        />
      ) : (
        <>
          {modeControl}
          <QuickstartWizardPage
            key={`${account?.uuid}:${activeWorkspaceId}`}
            workspaceID={activeWorkspaceId}
            accountID={account?.uuid ?? ''}
            models={models.data}
          />
        </>
      )}
    </div>
  );
}

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
  const { activeWorkspaceId } = useWorkspace();
  const { msg } = useI18n();
  const { account } = useAuth();
  const models = useQuery({
    queryKey: ['agent-quickstart', 'models', activeWorkspaceId],
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
    <QuickstartWizardPage
      key={`${account?.uuid}:${activeWorkspaceId}`}
      workspaceID={activeWorkspaceId}
      accountID={account?.uuid ?? ''}
      models={models.data}
    />
  );
}

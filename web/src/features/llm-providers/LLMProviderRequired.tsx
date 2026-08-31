import { Bot } from 'lucide-react';

import { useAuth } from '../../shared/auth/context';
import { useI18n } from '../../shared/i18n';
import { canManageLLMProviders } from '../../shared/permissions/llm-providers';
import { Button } from '../../shared/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '../../shared/ui/empty';
import { useWorkspace } from '../../shared/workspaces/context';

type LLMProviderRequiredProps = {
  compact?: boolean;
  onConfigure?: () => void;
};

export function LLMProviderRequired({ compact = false, onConfigure }: LLMProviderRequiredProps) {
  const { account } = useAuth();
  const { msg } = useI18n();
  const { orgUuid } = useWorkspace();
  const canConfigureModels = canManageLLMProviders(account, orgUuid);

  return (
    <Empty
      data-testid="llm-provider-required"
      className={compact ? 'min-h-[220px] p-4' : 'min-h-[320px] max-w-xl border border-dashed border-border bg-card'}
    >
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Bot aria-hidden />
        </EmptyMedia>
        <EmptyTitle className="text-base">{msg('llmModels.requiredTitle', 'No models configured')}</EmptyTitle>
        <EmptyDescription>
          {canConfigureModels
            ? msg('llmModels.requiredDescription', 'Configure a model to get started.')
            : msg('llmModels.contactAdminDescription', 'Contact your organization administrator to configure a model.')}
        </EmptyDescription>
      </EmptyHeader>
      {canConfigureModels && onConfigure ? (
        <EmptyContent>
          <Button type="button" onClick={onConfigure}>
            {msg('llmModels.configureModels', 'Configure models')}
          </Button>
        </EmptyContent>
      ) : null}
    </Empty>
  );
}

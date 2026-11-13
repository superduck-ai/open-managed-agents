import { Link, useLocation } from '@tanstack/react-router';
import { LLMModelsPage } from '../llm-providers/LLMModelsPage';
import { useWorkspace } from '../../shared/workspaces/context';
import { workspaceIdFromPath, workspaceLLMModelsPath } from '../../shared/workspaces/presentation';
import { useI18n } from '../../shared/i18n';
import { buttonVariants } from '../../shared/ui/button';

export function LLMSettingsPage({ scope }: { scope: 'workspace' | 'organization' }) {
  const location = useLocation();
  const { orgUuid, activeWorkspaceId } = useWorkspace();
  const { msg } = useI18n();
  const workspaceId = workspaceIdFromPath(location.pathname) ?? activeWorkspaceId;
  const suffix = location.searchStr + (location.hash ? `#${location.hash}` : '');
  const catalogIdentity = scope === 'workspace' ? `${orgUuid}:${scope}:${workspaceId}` : `${orgUuid}:${scope}`;
  return (
    <div className="space-y-5">
      <nav className="flex flex-wrap gap-2" aria-label={msg('llmModels.scope', 'Model scope')}>
        <Link
          className={buttonVariants({ variant: scope === 'workspace' ? 'secondary' : 'ghost' })}
          to={workspaceLLMModelsPath(workspaceId) + suffix}
          aria-current={scope === 'workspace' ? 'page' : undefined}
        >
          {msg('llmModels.workspaceScope', 'Workspace models')}
        </Link>
        <Link
          className={buttonVariants({ variant: scope === 'organization' ? 'secondary' : 'ghost' })}
          to={'/settings/llm/organization' + suffix}
          aria-current={scope === 'organization' ? 'page' : undefined}
        >
          {msg('llmModels.organizationScope', 'Organization shared models')}
        </Link>
      </nav>
      <LLMModelsPage key={catalogIdentity} scope={scope} />
    </div>
  );
}

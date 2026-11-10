import { useI18n } from '../../../shared/i18n';
import { ButtonLink } from '../../../shared/ui/button';
import { channelCreatePath, channelsListPath } from './paths';

export function AgentChannelPointer({
  workspaceId,
  agentId,
  canEdit,
}: {
  workspaceId: string;
  agentId: string;
  canEdit: boolean;
}) {
  const { msg } = useI18n();
  return (
    <section className="max-w-xl space-y-3" aria-label={msg('managedAgents.channels.title', '渠道')}>
      <h2 className="text-base font-semibold">{msg('managedAgents.channels.title', '渠道')}</h2>
      <p className="text-sm leading-6 text-muted-foreground">
        {msg(
          'managedAgents.channels.agentDetailPointer',
          '飞书等渠道在工作区「渠道」里统一管理。这里不再创建、编辑或审批配对。',
        )}
      </p>
      <div className="flex flex-wrap gap-2">
        <ButtonLink href={channelsListPath(workspaceId)}>
          {msg('managedAgents.channels.openWorkspaceChannels', '打开工作区渠道页')}
        </ButtonLink>
        {canEdit ? (
          <ButtonLink variant="outline" href={channelCreatePath(workspaceId, agentId)}>
            {msg('managedAgents.channels.addChannel', '新增渠道')}
          </ButtonLink>
        ) : null}
      </div>
    </section>
  );
}

import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/shared/ui/tooltip';

import feishuLogo from '../channels/feishu.ico';
import type { IdentityAgentChannel } from './api';

export function IdentityChannelLogos({ channels = [] }: { channels?: IdentityAgentChannel[] }) {
  const { msg } = useI18n();
  const platform = msg('managedAgents.channels.feishu', '飞书');
  const statusLabels = {
    bound: msg('managedAgents.identities.channelBound', '已绑定'),
    unbound: msg('managedAgents.identities.channelUnbound', '未绑定'),
    expired: msg('managedAgents.identities.channelExpired', '绑定已过期'),
  };
  const channelLogos = channels.filter((channel) => channel.channel_type === 'feishu');

  if (channelLogos.length === 0) return null;

  return (
    <TooltipProvider>
      <span className="inline-flex flex-wrap items-center gap-1">
        {channelLogos.map((channel) => (
          <Tooltip key={channel.id}>
            <TooltipTrigger
              render={
                <Button
                  type="button"
                  variant="outline"
                  size="icon-xs"
                  className="cursor-help"
                  aria-label={`${platform} · ${channel.name}`}
                />
              }
            >
              <img src={feishuLogo} alt="" className="size-3.5 rounded-sm" />
            </TooltipTrigger>
            <TooltipContent>{`${platform} · ${channel.name} · ${statusLabels[channel.binding_status]}`}</TooltipContent>
          </Tooltip>
        ))}
      </span>
    </TooltipProvider>
  );
}

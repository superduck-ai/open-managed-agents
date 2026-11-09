import { useI18n } from '../../../shared/i18n';
import type { Channel } from './api';
import {
  channelConnectionStatus,
  channelConnectionStatusClass,
  channelConnectionStatusLabel,
} from './status';

export function ChannelStatusBadge({ channel }: { channel: Channel }) {
  const { msg } = useI18n();
  const status = channelConnectionStatus(channel);
  return (
    <span className={`inline-flex items-center gap-1.5 text-sm ${channelConnectionStatusClass(status)}`}>
      <span className="size-1.5 rounded-full bg-current" aria-hidden />
      {channelConnectionStatusLabel(status, msg)}
    </span>
  );
}

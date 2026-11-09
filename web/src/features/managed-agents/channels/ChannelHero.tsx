import { Cloud, MessageSquare } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import feishuLogo from './feishu.ico';

export function ChannelHero() {
  const { msg } = useI18n();
  return (
    <div className="mb-6 overflow-hidden rounded-xl border border-border bg-gradient-to-r from-violet-50 via-sky-50 to-background p-5 dark:from-violet-950/40 dark:via-sky-950/30 dark:to-background">
      <div className="flex flex-wrap items-center justify-between gap-6">
        <div className="max-w-2xl space-y-2">
          <h2 className="text-lg font-semibold text-foreground">
            {msg('managedAgents.channels.heroTitle', '让智能体进入日常工作')}
          </h2>
          <p className="text-sm leading-6 text-muted-foreground">
            {msg(
              'managedAgents.channels.heroBody',
              '让员工在熟悉的办公应用中直接使用智能体。你需要连接办公应用，设置接入策略，并绑定智能体和运行环境。',
            )}
          </p>
        </div>
        <div className="flex items-center gap-3 text-muted-foreground" aria-hidden>
          <img src={feishuLogo} alt="" className="size-8 rounded-md" width={32} height={32} />
          <MessageSquare className="size-7 opacity-60" />
          <MessageSquare className="size-7 opacity-40" />
          <MessageSquare className="size-7 opacity-30" />
          <Cloud className="size-9 text-primary" />
        </div>
      </div>
    </div>
  );
}

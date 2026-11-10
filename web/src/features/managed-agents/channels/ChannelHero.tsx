import { useI18n } from '../../../shared/i18n';
import feishuLogo from './feishu.ico';

export function ChannelHero() {
  const { msg } = useI18n();
  return (
    <div className="mb-4 overflow-hidden rounded-xl border border-border bg-violet-50/80 p-5 dark:bg-violet-950/30">
      <div className="flex flex-wrap items-center justify-between gap-6">
        <div className="max-w-2xl space-y-2">
          <h2 className="text-lg font-semibold text-foreground">
            {msg('managedAgents.channels.heroTitle', '让智能体进入日常工作')}
          </h2>
          <p className="text-sm leading-6 text-muted-foreground">
            {msg(
              'managedAgents.channels.heroBody',
              '让员工在熟悉的办公应用中直接使用智能体。你需要连接飞书，设置接入策略，并绑定智能体和运行环境。',
            )}
          </p>
        </div>
        <img
          src={feishuLogo}
          alt=""
          className="size-10 rounded-lg bg-card p-1.5 shadow-sm"
          width={40}
          height={40}
          aria-hidden
        />
      </div>
    </div>
  );
}

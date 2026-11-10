import { useI18n } from '../../../shared/i18n';
import { Eye, EyeOff } from 'lucide-react';
import { useState } from 'react';
import { Button } from '../../../shared/ui/button';
import { Input } from '../../../shared/ui/input';
import { Label } from '../../../shared/ui/label';
import type { ChannelInput } from './api';

export function ChannelCredentials({
  value,
  onChange,
  bound,
}: {
  value: ChannelInput;
  onChange: (value: ChannelInput) => void;
  bound: boolean;
}) {
  const { msg } = useI18n();
  const [visible, setVisible] = useState(false);
  return (
    <div className="space-y-4">
      <a
        href="https://open.feishu.cn/"
        target="_blank"
        rel="noreferrer"
        className="block text-right text-xs text-primary underline"
      >
        {msg('managedAgents.channels.consoleLink', '前往渠道管理后台 →')}
      </a>
      <div className="space-y-2">
        <Label htmlFor="channel-app-id">App ID</Label>
        <Input
          id="channel-app-id"
          autoComplete="off"
          value={value.app_id ?? ''}
          onChange={(event) => onChange({ ...value, app_id: event.target.value })}
          placeholder="App ID"
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="channel-app-secret">App Secret</Label>
        <div className="relative">
          <Input
            id="channel-app-secret"
            type={visible ? 'text' : 'password'}
            autoComplete="new-password"
            value={value.app_secret ?? ''}
            onChange={(event) => onChange({ ...value, app_secret: event.target.value })}
            placeholder={bound ? msg('managedAgents.channels.keepSecret', '留空保留现有密钥') : 'App Secret'}
            className="pr-10"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={
              visible
                ? msg('managedAgents.channels.hideSecret', '隐藏密钥')
                : msg('managedAgents.channels.showSecret', '显示密钥')
            }
            onClick={() => setVisible(!visible)}
            className="absolute right-1 top-1"
          >
            {visible ? <EyeOff /> : <Eye />}
          </Button>
        </div>
      </div>
    </div>
  );
}

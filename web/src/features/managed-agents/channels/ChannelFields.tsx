import { useI18n } from '../../../shared/i18n';
import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { ExternalLink, Eye, EyeOff } from 'lucide-react';
import { useState } from 'react';
import { Button, ButtonLink } from '../../../shared/ui/button';
import { Input } from '../../../shared/ui/input';
import { Label } from '../../../shared/ui/label';
import { errorMessage } from '../utils';
import type { ChannelInput } from './api';
import { feishuAppConsoleUrl } from './feishuConsole';

export function ChannelCredentials({
  value,
  onChange,
  bound,
  workspaceId,
}: {
  value: ChannelInput;
  onChange: (value: ChannelInput) => void;
  bound: boolean;
  workspaceId: string;
}) {
  const { msg } = useI18n();
  const [visible, setVisible] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const [verifyMessage, setVerifyMessage] = useState<{ ok: boolean; text: string } | null>(null);
  const consoleUrl = feishuAppConsoleUrl(value.app_id);
  const verifySecret = async () => {
    if (!consoleUrl || !value.app_secret || verifying) return;
    setVerifying(true);
    setVerifyMessage(null);
    try {
      await anthropicBetaApi.channels.checkCredentials(workspaceId, {
        app_id: value.app_id?.trim() ?? '',
        app_secret: value.app_secret.trim(),
      });
      setVerifyMessage({ ok: true, text: msg('managedAgents.channels.verifySecretOk', '应用密钥验证通过') });
    } catch (error) {
      setVerifyMessage({
        ok: false,
        text: errorMessage(error) || msg('managedAgents.channels.verifySecretFail', '应用密钥校验未通过'),
      });
    } finally {
      setVerifying(false);
    }
  };
  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="channel-app-id">
          <span className="text-destructive">*</span> {msg('managedAgents.channels.appIdLabel', '应用 ID')}
        </Label>
        <Input
          id="channel-app-id"
          autoComplete="off"
          value={value.app_id ?? ''}
          onChange={(event) => {
            setVerifyMessage(null);
            onChange({ ...value, app_id: event.target.value });
          }}
          placeholder={msg('managedAgents.channels.appIdLabel', '应用 ID')}
        />
        {consoleUrl ? (
          <ButtonLink variant="outline" href={consoleUrl} target="_blank" rel="noreferrer">
            {msg('managedAgents.channels.configureApp', '配置应用')}
            <ExternalLink />
          </ButtonLink>
        ) : (
          <Button type="button" variant="outline" disabled>
            {msg('managedAgents.channels.configureApp', '配置应用')}
            <ExternalLink />
          </Button>
        )}
      </div>
      <div className="space-y-2">
        <Label htmlFor="channel-app-secret">
          <span className="text-destructive">*</span> {msg('managedAgents.channels.appSecret', '应用密钥')}
        </Label>
        <div className="relative">
          <Input
            id="channel-app-secret"
            type={visible ? 'text' : 'password'}
            autoComplete="new-password"
            value={value.app_secret ?? ''}
            onChange={(event) => {
              setVerifyMessage(null);
              onChange({ ...value, app_secret: event.target.value });
            }}
            placeholder={
              bound
                ? msg('managedAgents.channels.keepSecret', '留空保留现有密钥')
                : msg('managedAgents.channels.appSecretPlaceholder', '应用密钥')
            }
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
        <Button
          type="button"
          variant="outline"
          disabled={!consoleUrl || !value.app_secret || verifying}
          onClick={() => void verifySecret()}
        >
          {verifying
            ? msg('managedAgents.channels.processing', '处理中…')
            : msg('managedAgents.channels.verifySecret', '验证 App Secret')}
        </Button>
        {verifyMessage ? (
          <p
            role={verifyMessage.ok ? 'status' : 'alert'}
            className={verifyMessage.ok ? 'text-sm text-success' : 'text-sm text-destructive'}
          >
            {verifyMessage.text}
          </p>
        ) : null}
      </div>
    </div>
  );
}

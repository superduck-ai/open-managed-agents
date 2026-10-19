import { Eye, EyeOff } from 'lucide-react';
import { useState } from 'react';
import { Button } from '../../../shared/ui/button';
import { Input } from '../../../shared/ui/input';
import { Label } from '../../../shared/ui/label';
import { RadioGroup, RadioGroupItem } from '../../../shared/ui/radio-group';
import { Switch } from '../../../shared/ui/switch';
import type { ChannelInput } from './api';

export function ChannelOptions({ value, onChange }: { value: ChannelInput; onChange: (value: ChannelInput) => void }) {
  const options = [
    ['include_thinking', '展示思考过程', '在回复中展示思考过程'],
    ['include_tool_calls', '展示工具调用', '在回复中展示工具调用'],
    [
      'reply_in_thread',
      '话题归属',
      '开启后机器人在话题内回复，每个话题一个独立会话；关闭则直接发在群里，整个群共用一个会话。',
    ],
  ] as const;
  return (
    <div className="space-y-4 border-t border-dashed pt-5">
      {options.map(([key, title, hint]) => (
        <div key={key} className="flex items-center justify-between gap-5">
          <Label htmlFor={`channel-${key}`} title={hint}>
            {title}
          </Label>
          <Switch
            id={`channel-${key}`}
            checked={value[key]}
            onCheckedChange={(checked) => onChange({ ...value, [key]: checked })}
          />
        </div>
      ))}
      <p className="text-xs leading-5 text-muted-foreground">{options[2][2]}</p>
    </div>
  );
}
export function ChannelMode({
  value,
  disabled,
  onChange,
}: {
  value: ChannelInput['mode'];
  disabled: boolean;
  onChange: (value: ChannelInput['mode']) => void;
}) {
  return (
    <div className="space-y-3">
      <Label>渠道模式</Label>
      <RadioGroup
        aria-label="渠道模式"
        value={value}
        disabled={disabled}
        onValueChange={(next) => onChange(next as ChannelInput['mode'])}
        className="flex gap-5"
      >
        <Label className="flex gap-2">
          <RadioGroupItem value="open" />
          开放模式
        </Label>
        <Label className="flex gap-2">
          <RadioGroupItem value="pairing" />
          配对模式
        </Label>
      </RadioGroup>
      <p className="text-xs leading-5 text-muted-foreground">
        {value === 'open' ? '无需审批，用户可直接与机器人对话。' : '经管理员批准后，对应的单聊或群聊才可使用。'}
        渠道创建后不可切换模式。
      </p>
    </div>
  );
}
export function ChannelCredentials({
  value,
  onChange,
  bound,
}: {
  value: ChannelInput;
  onChange: (value: ChannelInput) => void;
  bound: boolean;
}) {
  const [visible, setVisible] = useState(false);
  return (
    <div className="space-y-4">
      <a
        href="https://open.feishu.cn/"
        target="_blank"
        rel="noreferrer"
        className="block text-right text-xs text-primary underline"
      >
        前往渠道管理后台 →
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
            placeholder={bound ? '留空保留现有密钥' : 'App Secret'}
            className="pr-10"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={visible ? '隐藏密钥' : '显示密钥'}
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

import { QRCodeSVG } from 'qrcode.react';
import { ExternalLink } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Input } from '../../../shared/ui/input';
import { Label } from '../../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { Switch } from '../../../shared/ui/switch';
import type { AgentApiResponse, EnvironmentApiResponse } from '../types';
import type { Channel, ChannelInput, ChannelQr } from './api';
import { ChannelCredentials } from './ChannelFields';
import feishuLogo from './feishu.ico';

export type WizardStep = 1 | 2 | 3;
export type ChannelTypeOption = 'feishu';

export type WizardValue = ChannelInput & {
  channelType: ChannelTypeOption;
  agentId: string;
  environmentId: string;
};

export function WizardStepper({ step, onStepChange }: { step: WizardStep; onStepChange: (step: WizardStep) => void }) {
  const { msg } = useI18n();
  const steps = [
    {
      id: 1 as const,
      title: msg('managedAgents.channels.stepConnect', '连接渠道应用'),
      subtitle: msg('managedAgents.channels.stepConnectHint', '应用接入与凭证'),
    },
    {
      id: 2 as const,
      title: msg('managedAgents.channels.stepStrategy', '配置渠道策略'),
      subtitle: msg('managedAgents.channels.stepStrategyHint', '回复、身份与上下文'),
    },
    {
      id: 3 as const,
      title: msg('managedAgents.channels.stepMa', '配置 MA 策略'),
      subtitle: msg('managedAgents.channels.stepMaHint', '智能体及默认资源'),
    },
  ];
  return (
    <ol className="mb-6 space-y-3">
      {steps.map((item) => {
        const active = step === item.id;
        return (
          <li key={item.id}>
            <button
              type="button"
              className="flex w-full items-start gap-3 text-left"
              onClick={() => onStepChange(item.id)}
            >
              <span
                className={
                  active
                    ? 'flex size-7 shrink-0 items-center justify-center rounded-full bg-primary text-xs font-semibold text-primary-foreground'
                    : 'flex size-7 shrink-0 items-center justify-center rounded-full border border-border text-xs text-muted-foreground'
                }
              >
                {item.id}
              </span>
              <span>
                <span className={active ? 'block text-sm font-semibold' : 'block text-sm text-muted-foreground'}>
                  {item.title}
                </span>
                <span className="block text-xs text-muted-foreground">{item.subtitle}</span>
              </span>
            </button>
          </li>
        );
      })}
    </ol>
  );
}

export function StepConnect({
  value,
  onChange,
  binding,
  onBindingChange,
  saved,
  qr,
  busy,
  onGenerateQr,
}: {
  value: WizardValue;
  onChange: (value: WizardValue) => void;
  binding: string;
  onBindingChange: (binding: string) => void;
  saved: Channel | null;
  qr: ChannelQr | null;
  busy: boolean;
  onGenerateQr: () => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="space-y-5">
      <div className="space-y-2">
        <Label htmlFor="wizard-channel-name">
          <span className="text-destructive">*</span> {msg('managedAgents.channels.channelName', '渠道名称')}
        </Label>
        <Input
          id="wizard-channel-name"
          maxLength={40}
          placeholder={msg('managedAgents.channels.channelNamePlaceholder', '填写应用名称')}
          value={value.name}
          onChange={(event) => onChange({ ...value, name: event.target.value })}
        />
      </div>
      <div className="space-y-2">
        <Label>{msg('managedAgents.channels.channelType', '渠道类型')}</Label>
        <div className="flex h-10 items-center gap-2 rounded-lg border border-border bg-muted/30 px-3 text-sm">
          <img src={feishuLogo} alt="" className="size-4" width={16} height={16} />
          {msg('managedAgents.channels.feishu', '飞书')}
        </div>
      </div>
      <div className="space-y-3">
        <Label>{msg('managedAgents.channels.accessMethod', '接入方式')}</Label>
        <div className="inline-flex rounded-lg border border-border p-1">
          <Button
            type="button"
            size="sm"
            variant={binding === 'qr' ? 'secondary' : 'ghost'}
            disabled={Boolean(saved && saved.binding_status === 'bound')}
            onClick={() => onBindingChange('qr')}
          >
            {msg('managedAgents.channels.feishuAuth', '飞书授权')}
          </Button>
          <Button
            type="button"
            size="sm"
            variant={binding === 'credentials' ? 'secondary' : 'ghost'}
            onClick={() => onBindingChange('credentials')}
          >
            {msg('managedAgents.channels.manualInput', '手动输入')}
          </Button>
        </div>
      </div>
      {binding === 'credentials' ? (
        <ChannelCredentials
          value={value}
          onChange={(next) => onChange({ ...value, ...next })}
          bound={saved?.binding_status === 'bound'}
        />
      ) : (
        <div className="space-y-3 rounded-xl border border-dashed border-border bg-muted/20 p-4">
          <div>
            <p className="text-sm font-medium">{msg('managedAgents.channels.authorizeCreate', '授权创建飞书应用')}</p>
            <p className="mt-1 text-xs text-muted-foreground">
              {msg('managedAgents.channels.authorizeCreateHint', '由现有应用创建权限的企业成员授权')}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Button type="button" disabled={busy || qr?.status === 'pending'} onClick={onGenerateQr}>
              <ExternalLink className="size-4" />
              {qr
                ? msg('managedAgents.channels.regenerateQr', '重新生成二维码')
                : msg('managedAgents.channels.goFeishuAuth', '前往飞书授权创建')}
            </Button>
            <button
              type="button"
              className="text-sm text-primary hover:underline"
              disabled={busy}
              onClick={onGenerateQr}
            >
              {msg('managedAgents.channels.scanFeishuAuth', '使用飞书扫码授权')}
            </button>
          </div>
          {qr?.status === 'pending' ? (
            <div className="flex flex-col items-center gap-3 pt-2">
              <div className="bg-white p-4">
                <QRCodeSVG value={qr.url} size={180} />
              </div>
              <p className="text-sm">{msg('managedAgents.channels.qrWaiting', '等待飞书扫码确认')}</p>
            </div>
          ) : null}
          {qr?.status === 'bound' || saved?.binding_status === 'bound' ? (
            <p role="status" className="text-sm text-success">
              {msg('managedAgents.channels.bound', '飞书绑定成功')}
            </p>
          ) : null}
          {qr?.status === 'error' || qr?.status === 'expired' ? (
            <p role="alert" className="text-sm text-destructive">
              {msg('managedAgents.channels.qrExpired', '二维码已失效，请重新生成。')}
            </p>
          ) : null}
        </div>
      )}
    </div>
  );
}

export function StepStrategy({
  value,
  onChange,
  modeLocked = false,
}: {
  value: WizardValue;
  onChange: (value: WizardValue) => void;
  modeLocked?: boolean;
}) {
  const { msg } = useI18n();
  return (
    <div className="space-y-6">
      <div className="space-y-3">
        <Label>
          <span className="text-destructive">*</span> {msg('managedAgents.channels.identityReply', '身份与回复')}
        </Label>
        <div className="overflow-hidden rounded-xl border border-border">
          <table className="w-full text-sm">
            <thead className="bg-muted/40 text-muted-foreground">
              <tr>
                <th className="px-3 py-2 text-left font-medium">{msg('managedAgents.channels.type', '类型')}</th>
                <th className="px-3 py-2 text-left font-medium">
                  {msg('managedAgents.channels.identity', '身份设定')}
                </th>
                <th className="px-3 py-2 text-left font-medium">
                  {msg('managedAgents.channels.replyMethod', '回复方式')}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr className="border-t border-border">
                <td className="px-3 py-3">{msg('managedAgents.channels.groupChat', '群聊')}</td>
                <td className="px-3 py-3">Bot</td>
                <td className="px-3 py-3">
                  <Select
                    value={value.reply_in_thread ? 'thread' : 'main'}
                    onValueChange={(next) => onChange({ ...value, reply_in_thread: next === 'thread' })}
                    items={[
                      { value: 'thread', label: msg('managedAgents.channels.threadReply', '话题回复') },
                      { value: 'main', label: msg('managedAgents.channels.mainReply', '主会话回复') },
                    ]}
                  >
                    <SelectTrigger className="h-9 w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="thread">{msg('managedAgents.channels.threadReply', '话题回复')}</SelectItem>
                      <SelectItem value="main">{msg('managedAgents.channels.mainReply', '主会话回复')}</SelectItem>
                    </SelectContent>
                  </Select>
                </td>
              </tr>
              <tr className="border-t border-border">
                <td className="px-3 py-3">{msg('managedAgents.channels.directChat', '单聊')}</td>
                <td className="px-3 py-3">User</td>
                <td className="px-3 py-3 text-muted-foreground">—</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p className="text-xs text-muted-foreground">
          {msg(
            'managedAgents.channels.identityHint',
            '身份设定由飞书桥接默认处理（群聊 Bot / 单聊 User）；回复方式会写入话题策略。',
          )}
        </p>
      </div>
      <div className="space-y-4">
        <Label>{msg('managedAgents.channels.replyDisplay', '回复展示')}</Label>
        <ToggleRow
          id="wizard-thinking"
          title={msg('managedAgents.channels.thinking', '展示思考过程')}
          hint={msg('managedAgents.channels.thinkingHint', '在回复中展示思考过程')}
          checked={value.include_thinking}
          onCheckedChange={(checked) => onChange({ ...value, include_thinking: checked })}
        />
        <ToggleRow
          id="wizard-tools"
          title={msg('managedAgents.channels.tools', '展示工具调用')}
          hint={msg(
            'managedAgents.channels.toolsVolcHint',
            '在「处理过程」中展示工具名称、执行状态及耗时。不展示敏感参数和原始结果。',
          )}
          checked={value.include_tool_calls}
          onCheckedChange={(checked) => onChange({ ...value, include_tool_calls: checked })}
        />
        <ToggleRow
          id="wizard-context"
          title={msg('managedAgents.channels.recentContext', '自动注入近期聊天内容')}
          hint={msg('managedAgents.channels.recentContextHint', '桥接暂未提供独立开关，界面仅作对齐展示。')}
          checked={false}
          disabled
          onCheckedChange={() => undefined}
        />
      </div>
      <div className="space-y-2">
        <Label>{msg('managedAgents.channels.mode', '渠道模式')}</Label>
        <Select
          value={value.mode}
          disabled={modeLocked}
          onValueChange={(next) => onChange({ ...value, mode: next as WizardValue['mode'] })}
          items={[
            { value: 'open', label: msg('managedAgents.channels.open', '开放模式') },
            { value: 'pairing', label: msg('managedAgents.channels.pairing', '配对模式') },
          ]}
        >
          <SelectTrigger className="h-10 w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="open">{msg('managedAgents.channels.open', '开放模式')}</SelectItem>
            <SelectItem value="pairing">{msg('managedAgents.channels.pairing', '配对模式')}</SelectItem>
          </SelectContent>
        </Select>
        <p className="text-xs text-muted-foreground">
          {value.mode === 'open'
            ? msg('managedAgents.channels.openHint', '无需审批，用户可直接与机器人对话。')
            : msg('managedAgents.channels.pairingHint', '经管理员批准后，对应的单聊或群聊才可使用。')}
          {msg('managedAgents.channels.immutableMode', '渠道创建后不可切换模式。')}
        </p>
      </div>
    </div>
  );
}

export function StepMa({
  value,
  onChange,
  agents,
  environments,
  agentLocked,
}: {
  value: WizardValue;
  onChange: (value: WizardValue) => void;
  agents: AgentApiResponse[];
  environments: EnvironmentApiResponse[];
  agentLocked: boolean;
}) {
  const { msg } = useI18n();
  return (
    <div className="space-y-5">
      <div className="space-y-2">
        <Label>
          <span className="text-destructive">*</span> {msg('managedAgents.channels.columnAgent', '智能体')}
        </Label>
        <Select
          value={value.agentId || null}
          disabled={agentLocked}
          onValueChange={(next) => onChange({ ...value, agentId: next ?? '' })}
          items={agents.map((agent) => ({ value: agent.id, label: `${agent.name} (${agent.id})` }))}
        >
          <SelectTrigger className="h-10 w-full">
            <SelectValue placeholder={msg('managedAgents.channels.selectAgent', '选择智能体')} />
          </SelectTrigger>
          <SelectContent>
            {agents.map((agent) => (
              <SelectItem key={agent.id} value={agent.id} label={`${agent.name} (${agent.id})`}>
                {agent.name} ({agent.id})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="space-y-2">
        <Label>
          <span className="text-destructive">*</span> {msg('managedAgents.channels.agentVersion', '智能体版本')}
        </Label>
        <Select
          value="latest"
          items={[{ value: 'latest', label: msg('managedAgents.channels.latestVersionShort', '默认最新') }]}
        >
          <SelectTrigger className="h-10 w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="latest">{msg('managedAgents.channels.latestVersionShort', '默认最新')}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="space-y-2">
        <Label>
          <span className="text-destructive">*</span>{' '}
          {msg('managedAgents.channels.defaultEnvironment', '默认 Environment')}
        </Label>
        <Select
          value={value.environmentId || null}
          onValueChange={(next) => onChange({ ...value, environmentId: next ?? '' })}
          items={environments.map((environment) => ({
            value: environment.id,
            label: environment.name || environment.id,
          }))}
        >
          <SelectTrigger className="h-10 w-full">
            <SelectValue placeholder={msg('managedAgents.quickstart.selectEnvironment', '请选择环境')} />
          </SelectTrigger>
          <SelectContent>
            {environments.map((environment) => (
              <SelectItem key={environment.id} value={environment.id} label={environment.name || environment.id}>
                {environment.name || environment.id}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <ReadonlyMountSelect
        label={msg('managedAgents.channels.defaultMemory', '默认 Memory')}
        value={msg('managedAgents.channels.notMounted', '不挂载')}
      />
      <ReadonlyMountSelect
        label={msg('managedAgents.channels.defaultVault', '默认 Vault')}
        value={msg('managedAgents.channels.notMounted', '不挂载')}
      />
    </div>
  );
}

function ToggleRow({
  id,
  title,
  hint,
  checked,
  disabled,
  onCheckedChange,
}: {
  id: string;
  title: string;
  hint: string;
  checked: boolean;
  disabled?: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="space-y-1">
        <Label htmlFor={id}>{title}</Label>
        <p className="text-xs leading-5 text-muted-foreground">{hint}</p>
      </div>
      <Switch id={id} checked={checked} disabled={disabled} onCheckedChange={onCheckedChange} />
    </div>
  );
}

function ReadonlyMountSelect({ label, value }: { label: string; value: string }) {
  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      <Input value={value} disabled readOnly />
    </div>
  );
}

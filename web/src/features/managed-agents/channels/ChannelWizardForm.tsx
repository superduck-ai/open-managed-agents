import { useId, type ReactNode } from 'react';
import { QRCodeSVG } from 'qrcode.react';
import { Bot, Check, Cloud, ExternalLink, LoaderCircle } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Button, ButtonLink } from '../../../shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '../../../shared/ui/card';
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

export function WizardStepper({
  step,
  canOpen,
  onStepChange,
}: {
  step: WizardStep;
  canOpen: (step: WizardStep) => boolean;
  onStepChange: (step: WizardStep) => void;
}) {
  const { msg } = useI18n();
  const steps = [
    { id: 1 as const, title: msg('managedAgents.channels.stepConnect', '连接渠道应用') },
    { id: 2 as const, title: msg('managedAgents.channels.stepStrategy', '配置渠道策略') },
    { id: 3 as const, title: msg('managedAgents.channels.stepMa', '配置 MA 策略') },
  ];
  return (
    <Card className="gap-4 ring-0">
      <CardHeader>
        <CardTitle className="text-sm">{msg('managedAgents.channels.progress', '进度')}</CardTitle>
      </CardHeader>
      <CardContent>
        <ol className="space-y-4 text-sm">
          {steps.map((item) => {
            const active = step === item.id;
            const done = step > item.id;
            const open = canOpen(item.id);
            return (
              <li key={item.id}>
                <button
                  type="button"
                  aria-current={active ? 'step' : undefined}
                  disabled={!open}
                  className={`flex w-full items-center gap-3 text-left disabled:cursor-not-allowed ${active ? 'font-medium text-foreground' : 'text-muted-foreground'}`}
                  onClick={() => {
                    if (open) onStepChange(item.id);
                  }}
                >
                  <span
                    className={`grid size-6 shrink-0 place-items-center rounded-full border-[0.5px] border-border text-xs ${done ? 'bg-[var(--success-bg)] text-success' : active ? 'border-primary bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'}`}
                  >
                    {done ? <Check aria-hidden className="size-3.5" /> : item.id}
                  </span>
                  {item.title}
                </button>
              </li>
            );
          })}
        </ol>
      </CardContent>
    </Card>
  );
}

export function WizardSideCards({ agentName, environmentName }: { agentName: string; environmentName: string }) {
  const { msg } = useI18n();
  return (
    <>
      {agentName ? (
        <Card className="gap-3 ring-0">
          <CardHeader>
            <CardTitle className="text-sm">{msg('managedAgents.channels.columnAgent', '智能体')}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex min-w-0 items-center gap-3 py-2">
              <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-accent text-muted-foreground">
                <Bot className="size-4" aria-hidden />
              </span>
              <span className="min-w-0 flex-1 truncate" title={agentName}>
                {agentName}
              </span>
            </div>
          </CardContent>
        </Card>
      ) : null}
      <Card className="gap-4 ring-0">
        <CardHeader>
          <CardTitle className="text-sm">{msg('managedAgents.channels.resources', '资源')}</CardTitle>
        </CardHeader>
        <CardContent>
          {environmentName ? (
            <div className="flex min-w-0 items-center gap-3 py-2">
              <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-accent text-muted-foreground">
                <Cloud className="size-4" aria-hidden />
              </span>
              <span className="min-w-0 flex-1 truncate" title={environmentName}>
                {environmentName}
              </span>
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">{msg('managedAgents.channels.noResources', '暂无关联资源')}</p>
          )}
        </CardContent>
      </Card>
    </>
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
  workspaceId,
  onGenerateQr,
  showQr = false,
}: {
  value: WizardValue;
  onChange: (value: WizardValue) => void;
  binding: string;
  onBindingChange: (binding: string) => void;
  saved: Channel | null;
  qr: ChannelQr | null;
  busy: boolean;
  workspaceId: string;
  onGenerateQr: (openAuthorization?: boolean) => void;
  showQr?: boolean;
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
        <Select value="feishu" items={[{ value: 'feishu', label: msg('managedAgents.channels.feishu', '飞书') }]}>
          <SelectTrigger className="h-10 w-full" aria-label={msg('managedAgents.channels.channelType', '渠道类型')}>
            <img src={feishuLogo} alt="" className="size-4" width={16} height={16} />
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="feishu">{msg('managedAgents.channels.feishu', '飞书')}</SelectItem>
            <SelectItem value="dingtalk" disabled>
              {msg('managedAgents.channels.dingtalkUnavailable', '钉钉（暂未开放）')}
            </SelectItem>
            <SelectItem value="wecom" disabled>
              {msg('managedAgents.channels.wecomUnavailable', '企业微信（暂未开放）')}
            </SelectItem>
            <SelectItem value="wechat" disabled>
              {msg('managedAgents.channels.wechatUnavailable', '微信（暂未开放）')}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="space-y-3">
        <Label>{msg('managedAgents.channels.accessMethod', '接入方式')}</Label>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            className={binding === 'qr' ? 'border-primary text-primary' : undefined}
            disabled={Boolean(saved && saved.binding_status === 'bound')}
            onClick={() => onBindingChange('qr')}
          >
            {msg('managedAgents.channels.feishuAuth', '飞书授权')}
          </Button>
          <Button
            type="button"
            variant="outline"
            className={binding === 'credentials' ? 'border-primary text-primary' : undefined}
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
          workspaceId={workspaceId}
        />
      ) : (
        <QrBindPanel value={value} qr={qr} saved={saved} busy={busy} onGenerateQr={onGenerateQr} showQr={showQr} />
      )}
    </div>
  );
}

function QrBindPanel({
  value,
  qr,
  saved,
  busy,
  onGenerateQr,
  showQr = false,
}: {
  value: WizardValue;
  qr: ChannelQr | null;
  saved: Channel | null;
  busy: boolean;
  onGenerateQr: (openAuthorization?: boolean) => void;
  showQr?: boolean;
}) {
  const { msg } = useI18n();
  const waiting = qr?.status === 'pending';
  return (
    <div className="space-y-3">
      <div>
        <p className="text-sm font-medium">{msg('managedAgents.channels.authorizeCreate', '授权创建飞书应用')}</p>
        <p className="mt-1 text-xs text-muted-foreground">
          {msg('managedAgents.channels.authorizeCreateHint', '由现有应用创建权限的企业成员授权')}
        </p>
      </div>
      {waiting && qr && !showQr ? (
        <Card className="gap-3 p-4" role="status">
          <div className="flex items-center gap-2 text-sm font-medium">
            <LoaderCircle className="size-4 animate-spin" aria-hidden />
            {msg('managedAgents.channels.authorizationPending', '等待飞书授权中')}
          </div>
          <p className="text-sm text-muted-foreground">
            {msg('managedAgents.channels.authorizationPendingHint', '在飞书授权页选择企业并确认创建')}
          </p>
          <ButtonLink
            href={qr.url}
            target="_blank"
            rel="noreferrer"
            variant="link"
            className="h-auto justify-start px-0"
          >
            {msg('managedAgents.channels.reopenAuthorization', '重新打开授权页')}
            <ExternalLink className="size-4" />
          </ButtonLink>
        </Card>
      ) : (
        <div className="flex flex-wrap items-center gap-3">
          <Button type="button" disabled={busy || !value.name.trim()} onClick={() => onGenerateQr(true)}>
            <ExternalLink className="size-4" />
            {msg('managedAgents.channels.goFeishuAuth', '前往飞书授权创建')}
          </Button>
          <button
            type="button"
            className="text-sm text-primary hover:underline"
            disabled={busy}
            onClick={() => onGenerateQr()}
          >
            {msg('managedAgents.channels.scanFeishuAuth', '使用飞书扫码授权')}
          </button>
        </div>
      )}
      {waiting && qr && showQr ? (
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
  );
}

export function StepStrategy({ value, onChange }: { value: WizardValue; onChange: (value: WizardValue) => void }) {
  const { msg } = useI18n();
  return (
    <div className="space-y-5">
      <div className="space-y-2">
        <Label>
          <span className="text-destructive">*</span> {msg('managedAgents.channels.identityReply', '身份与回复')}
        </Label>
        <div className="overflow-hidden rounded-lg border-[0.5px] border-input text-sm">
          <div className="grid grid-cols-[7rem_7rem_minmax(0,1fr)] gap-3 border-b-[0.5px] border-border px-2.5 py-2 text-xs text-muted-foreground">
            <span>{msg('managedAgents.channels.type', '类型')}</span>
            <span>{msg('managedAgents.channels.identity', '身份设定')}</span>
            <span>{msg('managedAgents.channels.replyMethod', '回复方式')}</span>
          </div>
          <div className="grid grid-cols-[7rem_7rem_minmax(0,1fr)] items-center gap-3 px-2.5 py-2">
            <span>{msg('managedAgents.channels.groupChat', '群聊')}</span>
            <span>{msg('managedAgents.channels.identityBot', 'Bot')}</span>
            <Select
              value={value.reply_in_thread ? 'thread' : 'main'}
              onValueChange={(next) => onChange({ ...value, reply_in_thread: next === 'thread' })}
              items={[
                { value: 'thread', label: msg('managedAgents.channels.threadReply', '话题回复') },
                { value: 'main', label: msg('managedAgents.channels.mainReply', '主会话回复') },
              ]}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="thread">{msg('managedAgents.channels.threadReply', '话题回复')}</SelectItem>
                <SelectItem value="main">{msg('managedAgents.channels.mainReply', '主会话回复')}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="grid grid-cols-[7rem_7rem_minmax(0,1fr)] items-center gap-3 border-t-[0.5px] border-border px-2.5 py-2">
            <span>{msg('managedAgents.channels.directChat', '单聊')}</span>
            <span>{msg('managedAgents.channels.identityUser', 'User')}</span>
            <span className="text-muted-foreground">—</span>
          </div>
        </div>
        <p className="text-xs text-muted-foreground">
          {msg(
            'managedAgents.channels.identityHint',
            '身份设定由飞书桥接默认处理（群聊 Bot / 单聊 User）；回复方式会写入话题策略。',
          )}
        </p>
      </div>
      <div className="space-y-2">
        <Label>{msg('managedAgents.channels.replyDisplay', '回复展示')}</Label>
        <ToggleCard>
          <ToggleCardRow
            title={msg('managedAgents.channels.thinking', '展示思考过程')}
            description={msg('managedAgents.channels.thinkingHint', '在回复中展示思考过程')}
            checked={value.include_thinking}
            onCheckedChange={(checked) => onChange({ ...value, include_thinking: checked })}
          />
          <ToggleCardRow
            title={msg('managedAgents.channels.tools', '展示工具调用')}
            description={msg(
              'managedAgents.channels.toolsVolcHint',
              '在「处理过程」中展示工具名称、执行状态及耗时。不展示敏感参数和原始结果。',
            )}
            checked={value.include_tool_calls}
            onCheckedChange={(checked) => onChange({ ...value, include_tool_calls: checked })}
          />
        </ToggleCard>
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
      <ChannelTargetFields
        value={value}
        onChange={onChange}
        agents={agents}
        environments={environments}
        agentLocked={agentLocked}
      />
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

function ChannelTargetFields({
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
    <>
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
    </>
  );
}

function ToggleCard({ children }: { children: ReactNode }) {
  return <div className="overflow-hidden rounded-xl border-[0.5px] border-border">{children}</div>;
}

function ToggleCardRow({
  title,
  description,
  checked,
  disabled,
  onCheckedChange,
}: {
  title: string;
  description: string;
  checked: boolean;
  disabled?: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  const descriptionId = useId();
  return (
    <div className="flex items-center justify-between gap-6 px-4 py-3 not-first:border-t-[0.5px] not-first:border-border">
      <div className="min-w-0 space-y-1">
        <div className="text-sm font-medium">{title}</div>
        <p id={descriptionId} className="text-sm leading-5 text-muted-foreground">
          {description}
        </p>
      </div>
      <Switch
        checked={checked}
        disabled={disabled}
        aria-label={title}
        aria-describedby={descriptionId}
        className="shrink-0"
        onCheckedChange={(next) => onCheckedChange(next === true)}
      />
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

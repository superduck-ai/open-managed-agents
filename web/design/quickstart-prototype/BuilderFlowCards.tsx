import { useEffect, useState } from 'react';
import { Check, ExternalLink, Puzzle } from 'lucide-react';
import { Button } from '@/shared/ui/button';
import { Checkbox } from '@/shared/ui/checkbox';
import { Input } from '@/shared/ui/input';
import { Textarea } from '@/shared/ui/textarea';
import { Label } from '@/shared/ui/label';
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/shared/ui/sheet';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/shared/ui/dialog';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import { agentConfig, configurationChanges, reviewIssues, scheduleLabel, type Snapshot } from './BuilderFlowModel';
import { skillOptions } from './PrototypeResources';

type Config = ReturnType<typeof agentConfig>;

export function AgentConfigEditor({
  open,
  onClose,
  state,
  onPropose,
}: {
  open: boolean;
  onClose: () => void;
  state: Snapshot;
  onPropose: (config: Config) => void;
}) {
  const [draft, setDraft] = useState<Config>(() => agentConfig(state));
  useEffect(() => {
    if (open) setDraft(agentConfig(state));
  }, [open, state]);
  return (
    <Sheet open={open} onOpenChange={(value) => !value && onClose()}>
      <SheetContent className="config-sheet">
        <SheetHeader>
          <SheetTitle>智能体配置</SheetTitle>
        </SheetHeader>
        <div className="sheet-fields">
          <Label className="conversation-field">
            名称
            <Input value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} />
          </Label>
          <Label className="conversation-field">
            模型
            <Select
              value={draft.model}
              onValueChange={(model) => model && setDraft({ ...draft, model })}
              items={[...new Set([draft.model, 'DeepSeek V3.2', 'Claude Sonnet', 'GPT'])].map((value) => ({
                value,
                label: value,
              }))}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {[...new Set([draft.model, 'DeepSeek V3.2', 'Claude Sonnet', 'GPT'])].map((value) => (
                  <SelectItem value={value} key={value}>
                    {value}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Label>
          <Label className="conversation-field">
            指令
            <Textarea
              className="system-input"
              value={draft.system}
              onChange={(event) => setDraft({ ...draft, system: event.target.value })}
            />
          </Label>
          {draft.resources.slackEnabled && (
            <div className="conversation-field">
              <Label>工具</Label>
              <span>Slack</span>
            </div>
          )}
          {draft.resources.skills.length > 0 && (
            <div className="conversation-field">
              <Label>技能</Label>
              {skillOptions
                .filter((skill) => draft.resources.skills.includes(skill.value))
                .map((skill) => (
                  <div className="selected-resource" key={skill.value}>
                    <Puzzle size={16} />
                    <span>{skill.label}</span>
                    <small>v{skill.version}</small>
                  </div>
                ))}
            </div>
          )}
        </div>
        <div className="sheet-footer">
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button
            disabled={!draft.name.trim() || !draft.system.trim()}
            onClick={() => {
              if (JSON.stringify(draft) !== JSON.stringify(agentConfig(state))) onPropose(draft);
              onClose();
            }}
          >
            完成
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

export function ConfigProposal({
  state,
  onEdit,
  onSave,
  onRefine,
}: {
  state: Snapshot;
  onEdit: () => void;
  onSave: () => void;
  onRefine: () => void;
}) {
  return (
    <div className="conversation-action">
      <h2>{state.agentVersion ? `保存为 v${state.agentVersion + 1}？` : '创建这个智能体？'}</h2>
      <dl className="configuration-diff">
        {configurationChanges(state).map((change) => (
          <div key={change.label}>
            <dt>{change.label}</dt>
            <dd>
              {state.agentVersion > 0 && change.before && <del>{change.before}</del>}
              <span>{change.after}</span>
            </dd>
          </div>
        ))}
      </dl>
      <div className="conversation-actions">
        <Button onClick={onSave}>{state.agentVersion ? '保存版本' : '创建智能体'}</Button>
        <Button variant="ghost" onClick={onRefine}>
          继续修改
        </Button>
        <Button variant="ghost" onClick={onEdit}>
          查看配置
        </Button>
      </div>
    </div>
  );
}

export function CredentialCard({
  error,
  onResolve,
}: {
  error: boolean;
  onResolve: (result: 'connected' | 'skipped', method?: string) => void;
}) {
  const [token, setToken] = useState('');
  const [manual, setManual] = useState(false);
  const [oauth, setOauth] = useState(false);
  const [pendingMethod, setPendingMethod] = useState('');
  function resolve(method: string) {
    setToken('');
    if (oauth) {
      setPendingMethod(method);
      setOauth(false);
    } else onResolve('connected', method);
  }
  return (
    <div className="conversation-action">
      <h2>授权 Slack</h2>
      <p className="secondary">mcp.slack.com</p>
      {error && (
        <p role="alert" className="error-message">
          授权未完成，请重试或暂时跳过。
        </p>
      )}
      {manual && (
        <Label className="conversation-field">
          访问令牌
          <Input
            type="password"
            autoComplete="off"
            value={token}
            placeholder="输入演示令牌"
            onChange={(event) => setToken(event.target.value)}
          />
        </Label>
      )}
      <div className="conversation-actions">
        <Button disabled={manual && !token.trim()} onClick={() => (manual ? resolve('访问令牌') : setOauth(true))}>
          {manual ? '添加凭据' : '连接'}
          {!manual && <ExternalLink size={14} />}
        </Button>
        <Button
          variant="ghost"
          onClick={() => {
            setToken('');
            onResolve('skipped');
          }}
        >
          暂时跳过
        </Button>
        <Button
          variant="ghost"
          onClick={() => {
            setManual(!manual);
            setToken('');
          }}
        >
          {manual ? '使用授权连接' : '手动添加凭据'}
        </Button>
      </div>
      {manual && <small>交互演示，请勿填写真实密钥。</small>}
      <Dialog
        open={oauth}
        onOpenChange={setOauth}
        onOpenChangeComplete={(open) => {
          if (!open && pendingMethod) {
            onResolve('connected', pendingMethod);
            setPendingMethod('');
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>连接 Slack</DialogTitle>
          </DialogHeader>
          <p>允许此智能体使用你的 Slack 连接。</p>
          <p className="secondary">本地交互演示，不会连接外部服务。</p>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setOauth(false)}>
              取消
            </Button>
            <Button onClick={() => resolve('OAuth')}>授权</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export function TestReviewCard({
  state,
  onChoices,
  onContinue,
}: {
  state: Snapshot;
  onChoices: (values: string[]) => void;
  onContinue: () => void;
}) {
  const issues = reviewIssues(state);
  if (!issues.length) return null;
  const options = [...issues, { id: 'rerun', label: '原样重跑' }, { id: 'continue', label: '继续' }];
  return (
    <div className="conversation-question">
      <h2>接下来怎么做？</h2>
      <div className="conversation-choices">
        {options.map((option) => (
          <Label
            className={`conversation-choice ${state.fixChoices.includes(option.id) ? 'selected' : ''}`}
            key={option.id}
          >
            <Checkbox
              checked={state.fixChoices.includes(option.id)}
              onCheckedChange={(checked) => {
                let choices = checked
                  ? [...state.fixChoices, option.id]
                  : state.fixChoices.filter((value) => value !== option.id);
                if (checked && option.id === 'rerun') choices = ['rerun'];
                if (checked && option.id !== 'rerun') choices = choices.filter((value) => value !== 'rerun');
                onChoices(choices);
              }}
            />
            <span>{option.label}</span>
          </Label>
        ))}
      </div>
      <div className="question-actions">
        <Button onClick={onContinue}>继续</Button>
      </div>
    </div>
  );
}

export function DeploymentConfirmation({
  state,
  error,
  onAdjust,
  onCreate,
}: {
  state: Snapshot;
  error: boolean;
  onAdjust: () => void;
  onCreate: () => void;
}) {
  return (
    <div className="conversation-action">
      <h2>创建定时部署？</h2>
      <dl className="confirmation-facts">
        <div>
          <dt>名称</dt>
          <dd>{state.name}定时运行</dd>
        </div>
        <div>
          <dt>周期</dt>
          <dd>{scheduleLabel(state.scheduleChoice, state.scheduleText)}</dd>
        </div>
        <div>
          <dt>时区</dt>
          <dd>{state.timezone}</dd>
        </div>
        <div>
          <dt>智能体</dt>
          <dd>
            {state.name} v{state.agentVersion}
          </dd>
        </div>
        <div>
          <dt>环境</dt>
          <dd>{state.environment}</dd>
        </div>
        {state.resources.slackEnabled && state.resources.vault !== 'none' && (
          <div>
            <dt>凭据保险库</dt>
            <dd>工作区集成</dd>
          </div>
        )}
        <div>
          <dt>初始消息</dt>
          <dd>{state.deploymentMessage}</dd>
        </div>
      </dl>
      {error && (
        <p role="alert" className="error-message">
          部署创建失败，配置已保留。
        </p>
      )}
      <div className="conversation-actions">
        <Button onClick={onCreate}>{error ? '重试' : '创建部署'}</Button>
        <Button variant="ghost" onClick={onAdjust}>
          调整
        </Button>
      </div>
    </div>
  );
}

export function DeploymentDetails({ state, open, onClose }: { state: Snapshot; open: boolean; onClose: () => void }) {
  return (
    <Sheet open={open} onOpenChange={(value) => !value && onClose()}>
      <SheetContent className="config-sheet">
        <SheetHeader>
          <SheetTitle>{state.name}定时运行</SheetTitle>
        </SheetHeader>
        <dl className="confirmation-facts">
          <div>
            <dt>周期</dt>
            <dd>{scheduleLabel(state.scheduleChoice, state.scheduleText)}</dd>
          </div>
          <div>
            <dt>时区</dt>
            <dd>{state.timezone}</dd>
          </div>
          <div>
            <dt>智能体</dt>
            <dd>
              {state.name} v{state.agentVersion}
            </dd>
          </div>
        </dl>
        <p className="resource-ready">
          <Check size={14} /> 已创建
        </p>
      </SheetContent>
    </Sheet>
  );
}

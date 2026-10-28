import { ArrowRight, Bot, Code, Plus, Search, BarChart3, Radar, Sparkles } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import { Field, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { Textarea } from '../../../shared/ui/textarea';
import { RadioGroup, RadioGroupItem } from '../../../shared/ui/radio-group';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import type { AgentModelOption } from '../agents/create-dialog-model';
import { environmentFormValues } from '../environments/model';
import { quickstartEnvironmentBody } from './api';
import { quickstartCopy } from './copy';
import { quickstartAgentBody, quickstartScenarios } from './model';
import { quickstartCurl, RequestPreview } from './RequestPreview';
import type { QuickstartWizard } from './useQuickstartWizard';
import { QuickstartFooter } from './QuickstartFooter';
import { QuickstartApiKey } from './QuickstartApiKey';

const scenarioIcons = {
  hello: Bot,
  research: Search,
  analysis: BarChart3,
  tracking: Radar,
  review: Code,
  custom: Sparkles,
};

export function QuickstartStart({ wizard }: { wizard: QuickstartWizard }) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  return (
    <div className="flex min-h-0 flex-1 flex-col pt-(--quickstart-gap)">
      <div className="subtle-scrollbar min-h-0 flex-1 overflow-y-auto pr-1">
        <div className="mb-(--quickstart-gap) space-y-2">
          <h1 className="text-xl leading-7 font-semibold tracking-tight">{text.title}</h1>
        </div>
        <RadioGroup
          value={wizard.progress.scenarioID}
          onValueChange={(value) => wizard.chooseScenario(value)}
          aria-label={text.scenarios}
          disabled={wizard.busy || Boolean(wizard.progress.pending)}
          className="grid gap-[clamp(0.5rem,1.5dvh,0.75rem)] sm:grid-cols-2"
        >
          {quickstartScenarios(locale).map((scenario) => {
            const Icon = scenarioIcons[scenario.id];
            const featured = scenario.id === 'hello';
            const custom = scenario.id === 'custom';
            return (
              <FieldLabel
                key={scenario.id}
                htmlFor={`scenario-${scenario.id}`}
                className={`flex w-full cursor-pointer flex-col items-stretch gap-2 rounded-xl border-[0.5px] px-4 py-3 ${featured || custom ? 'sm:col-span-2' : ''} ${wizard.progress.scenarioID === scenario.id ? 'border-foreground bg-accent/40' : 'border-border bg-card hover:bg-accent/20'}`}
              >
                <div className="flex items-center gap-2.5">
                  <span
                    className={
                      featured
                        ? 'grid size-8 shrink-0 place-items-center rounded-lg bg-primary text-primary-foreground'
                        : 'shrink-0'
                    }
                  >
                    <Icon className={featured ? 'size-[18px]' : 'size-4'} />
                  </span>
                  <span className="min-w-0 flex-1 space-y-1">
                    <span className="block text-sm font-medium">{scenario.name}</span>
                  </span>
                  {featured && <Badge>{text.recommended}</Badge>}
                  <RadioGroupItem id={`scenario-${scenario.id}`} value={scenario.id} />
                </div>
                <p className="text-xs leading-5 font-normal text-muted-foreground">{scenario.summary}</p>
              </FieldLabel>
            );
          })}
        </RadioGroup>
      </div>
      <QuickstartFooter wizard={wizard}>
        <Button disabled={wizard.busy || Boolean(wizard.progress.pending)} onClick={() => wizard.navigate(1)}>
          {text.start}
          <ArrowRight />
        </Button>
      </QuickstartFooter>
    </div>
  );
}

export function QuickstartAgent({
  wizard,
  models,
  workspaceID,
}: {
  wizard: QuickstartWizard;
  models: AgentModelOption[];
  workspaceID: string;
}) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  const { draft, agent, candidates } = wizard;
  const { modelAvailable, agentValid: valid } = wizard;
  const disabled = wizard.busy || Boolean(wizard.progress.pending) || !wizard.resources.data;
  const code = agent
    ? quickstartCurl(`/v1/agents/${encodeURIComponent(agent.id)}?version=${agent.version}`, workspaceID)
    : quickstartCurl('/v1/agents', workspaceID, quickstartAgentBody(draft));
  return (
    <ConfigurationLayout
      workspaceID={workspaceID}
      title={text.agentTitle}
      code={code}
      footer={
        <ConfigurationActions wizard={wizard} valid={valid} reuse={Boolean(agent)} formID="quickstart-agent-form" />
      }
    >
      <form
        id="quickstart-agent-form"
        className="space-y-(--quickstart-gap)"
        onSubmit={(event) => {
          event.preventDefault();
          if (valid && !disabled) void wizard.saveAgent();
        }}
      >
        <Field>
          <FieldLabel htmlFor="quickstart-name">{text.name}</FieldLabel>
          <Input
            id="quickstart-name"
            value={draft.name}
            disabled={disabled || Boolean(agent)}
            onChange={(event) => wizard.editDraft({ name: event.target.value })}
            maxLength={256}
          />
        </Field>
        {(candidates.length > 0 || agent) && (
          <div className="space-y-3 border-b-[0.5px] border-border pb-4">
            <p className="text-sm text-muted-foreground">{text.existingAgent}</p>
            <Select
              value={agent?.id ?? 'new'}
              onValueChange={(id) => wizard.update((value) => ({ ...value, agentID: id ?? 'new' }))}
              disabled={disabled}
            >
              <SelectTrigger className="h-auto! min-h-10 w-full py-2.5" aria-label={text.reused}>
                <SelectValue className="min-w-0">
                  <span className="min-w-0 space-y-1">
                    <span className="block truncate font-medium">{agent?.name ?? text.newAgent}</span>
                    {agent && (
                      <span className="block truncate font-mono text-xs text-muted-foreground">{agent.id}</span>
                    )}
                  </span>
                </SelectValue>
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false} align="start" className="max-h-80 p-1.5">
                {[{ id: 'new', name: text.newAgent }, ...candidates].map((item) => (
                  <SelectItem
                    key={item.id}
                    value={item.id}
                    className={`py-3 pl-3 ${item.id === 'new' ? 'mb-1 border-b-[0.5px] border-border' : ''}`}
                  >
                    <span className="min-w-0 flex-1 space-y-1 whitespace-normal">
                      <span className="block break-words font-medium">{item.name}</span>
                      {item.id !== 'new' && (
                        <span className="block break-all font-mono text-xs text-muted-foreground">{item.id}</span>
                      )}
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {agent && <p className="text-xs text-muted-foreground">{text.usingAgent}</p>}
          </div>
        )}
        <Field>
          <FieldLabel htmlFor="quickstart-description">{text.description}</FieldLabel>
          <Input
            id="quickstart-description"
            value={draft.description}
            disabled={disabled || Boolean(agent)}
            onChange={(event) => wizard.editDraft({ description: event.target.value })}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="quickstart-model">{text.model}</FieldLabel>
          <Select
            value={draft.modelID}
            disabled={disabled || Boolean(agent)}
            onValueChange={(value) => wizard.editDraft({ modelID: value ?? '' })}
          >
            <SelectTrigger id="quickstart-model" className="h-auto! min-h-10 w-full py-2.5">
              <SelectValue>{draft.modelID}</SelectValue>
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false} align="start" className="max-h-80 p-1.5">
              {models.map((model) => (
                <SelectItem key={model.id} value={model.id} className="py-3 pl-3">
                  {model.displayName}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field>
          <FieldLabel htmlFor="quickstart-system">{text.system}</FieldLabel>
          <Textarea
            id="quickstart-system"
            value={draft.system}
            disabled={disabled || Boolean(agent)}
            rows={7}
            onChange={(event) => wizard.editDraft({ system: event.target.value })}
            className="h-[clamp(6rem,18dvh,10rem)] resize-y"
          />
        </Field>
        {!valid && (
          <p role="status" className="text-sm text-muted-foreground">
            {modelAvailable ? text.required : text.modelUnavailable}
          </p>
        )}
      </form>
    </ConfigurationLayout>
  );
}

export function QuickstartEnvironment({ wizard, workspaceID }: { wizard: QuickstartWizard; workspaceID: string }) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  const { environment, namedEnvironment, progress } = wizard;
  const disabled = wizard.busy || Boolean(progress.pending) || !wizard.resources.data;
  const existing = environment ?? namedEnvironment;
  const valid =
    Boolean(environment || (progress.environmentID === 'new' && progress.environmentName.trim())) &&
    !namedEnvironment?.archived_at;
  const code = existing
    ? quickstartCurl(`/v1/environments/${encodeURIComponent(existing.id)}`, workspaceID)
    : quickstartCurl('/v1/environments', workspaceID, quickstartEnvironmentBody(progress.environmentName));
  return (
    <ConfigurationLayout
      workspaceID={workspaceID}
      title={text.environmentTitle}
      code={code}
      footer={
        <ConfigurationActions
          wizard={wizard}
          valid={valid}
          reuse={Boolean(existing && !existing.archived_at)}
          formID="quickstart-environment-form"
        />
      }
    >
      <form
        id="quickstart-environment-form"
        className="space-y-(--quickstart-gap)"
        onSubmit={(event) => {
          event.preventDefault();
          if (valid && !disabled) void wizard.saveEnvironment();
        }}
      >
        {!wizard.activeEnvironments.length && <p className="text-sm text-muted-foreground">{text.emptyEnvironments}</p>}
        <RadioGroup
          aria-label={text.existing}
          value={progress.environmentID}
          disabled={disabled}
          onValueChange={(value) => wizard.update((current) => ({ ...current, environmentID: value }))}
          className="max-h-[clamp(10rem,40dvh,20rem)] max-w-[440px] overflow-y-auto p-1"
        >
          {wizard.activeEnvironments.map((item) => {
            const config = environmentFormValues(item);
            return (
              <FieldLabel
                key={item.id}
                htmlFor={`environment-${item.id}`}
                className={`flex min-h-[clamp(3.5rem,9dvh,4.5rem)] w-full cursor-pointer items-center gap-3 rounded-lg border-[0.5px] p-[clamp(0.5rem,1.5dvh,0.75rem)] ${item.id === progress.environmentID ? 'border-foreground bg-accent/40' : 'border-border'}`}
              >
                <RadioGroupItem id={`environment-${item.id}`} value={item.id} />
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium">{item.name}</div>
                  <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs font-normal text-muted-foreground">
                    <span>{config.hosting === 'self_hosted' ? text.selfHosted : text.cloud}</span>
                    <span>{config.networkType === 'unrestricted' ? text.unrestricted : text.limited}</span>
                  </div>
                  {config.hosting === 'self_hosted' && (
                    <p className="mt-1 text-xs font-normal text-muted-foreground">{text.selfHostedHint}</p>
                  )}
                </div>
                {item.name === 'Default' && <Badge variant="secondary">{text.recommended}</Badge>}
              </FieldLabel>
            );
          })}
          <FieldLabel
            htmlFor="environment-new"
            className={`flex min-h-[clamp(3.5rem,9dvh,4.5rem)] w-full cursor-pointer items-center gap-3 rounded-lg border-[0.5px] p-[clamp(0.5rem,1.5dvh,0.75rem)] ${progress.environmentID === 'new' ? 'border-foreground bg-accent/40' : 'border-dashed border-border'}`}
          >
            <RadioGroupItem id="environment-new" value="new" />
            <Plus className="size-4" />
            {text.newEnvironment}
          </FieldLabel>
        </RadioGroup>
        {progress.environmentID === 'new' && <QuickstartEnvironmentName wizard={wizard} disabled={disabled} />}
        {!valid && !namedEnvironment?.archived_at && (
          <p className="text-sm text-muted-foreground">{text.environmentRequired}</p>
        )}
      </form>
    </ConfigurationLayout>
  );
}

function ConfigurationActions({
  wizard,
  valid,
  reuse,
  formID,
}: {
  wizard: QuickstartWizard;
  valid: boolean;
  reuse: boolean;
  formID: string;
}) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  const disabled = wizard.busy || Boolean(wizard.progress.pending);
  return (
    <QuickstartFooter wizard={wizard}>
      <Button type="submit" form={formID} disabled={disabled || !valid || !wizard.resources.data}>
        {wizard.busy ? text.sending : reuse ? text.reuse : text.next}
        <ArrowRight />
      </Button>
    </QuickstartFooter>
  );
}

function ConfigurationLayout({
  workspaceID,
  title,
  code,
  children,
  footer,
}: {
  workspaceID: string;
  title: string;
  code: string;
  children: React.ReactNode;
  footer: React.ReactNode;
}) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  return (
    <div className="flex min-h-0 flex-1 flex-col pt-(--quickstart-gap)">
      <div className="mb-(--quickstart-gap) shrink-0 space-y-2">
        <h1 className="text-2xl leading-8 font-semibold tracking-tight">{title}</h1>
      </div>
      <div className="grid min-h-0 min-w-0 flex-1 auto-rows-max gap-(--quickstart-gap) overflow-y-auto min-[860px]:grid-cols-2 min-[860px]:grid-rows-[minmax(0,1fr)] min-[860px]:overflow-hidden">
        <div className="min-h-0 min-w-0 max-w-xl pr-1 min-[860px]:overflow-y-auto">{children}</div>
        <aside className="min-h-0 min-w-0 border-t-[0.5px] border-border pt-6 min-[860px]:overflow-y-auto min-[860px]:border-t-0 min-[860px]:border-l-[0.5px] min-[860px]:pt-0 min-[860px]:pl-6">
          <div className="min-w-0">
            <QuickstartApiKey workspaceID={workspaceID} />
            <div className="mt-(--quickstart-gap) border-t-[0.5px] border-border pt-(--quickstart-gap)">
              <h2 className="mb-3 text-sm font-medium">{text.api}</h2>
              <RequestPreview code={code} />
            </div>
          </div>
        </aside>
      </div>
      {footer}
    </div>
  );
}

function QuickstartEnvironmentName({ wizard, disabled }: { wizard: QuickstartWizard; disabled: boolean }) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  const { progress, namedEnvironment } = wizard;
  return (
    <Field>
      <FieldLabel htmlFor="quickstart-environment-name">{text.environmentName}</FieldLabel>
      <Input
        id="quickstart-environment-name"
        value={progress.environmentName}
        disabled={disabled}
        onChange={(event) => wizard.update((value) => ({ ...value, environmentName: event.target.value }))}
      />
      <p className={`text-sm ${namedEnvironment?.archived_at ? 'text-destructive' : 'text-muted-foreground'}`}>
        {namedEnvironment?.archived_at
          ? text.archivedName
          : namedEnvironment
            ? `${text.reused}: ${namedEnvironment.name}`
            : text.newEnvironmentHint}
      </p>
    </Field>
  );
}

import { QuickstartEntrySlot } from './QuickstartEntrySlot';
import { Label } from '../../../shared/ui/label';
import { ArrowRight, Bot, Plus } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import { Field, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { Textarea } from '../../../shared/ui/textarea';
import { Accordion, AccordionItem, AccordionTrigger, AccordionContent } from '../../../shared/ui/accordion';
import { RadioGroup, RadioGroupItem } from '../../../shared/ui/radio-group';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import type { AgentModelOption } from '../agents/create-dialog-model';
import { environmentFormValues } from '../environments/model';
import { quickstartEnvironmentBody } from './api';
import { quickstartCopy } from './copy';
import { quickstartAgentBody } from './model';
import { quickstartCurl, RequestPreview } from './RequestPreview';
import type { QuickstartWizard } from './useQuickstartWizard';
import { QuickstartFooter } from './QuickstartFooter';
import { QuickstartApiKey } from './QuickstartApiKey';
import {
  quickstartTemplates,
  quickstartTemplateCard,
  quickstartTemplateGrid,
  QuickstartTemplateContent,
} from './templates';

export function QuickstartStart({ wizard, onStart }: { wizard: QuickstartWizard; onStart: () => void }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const text = quickstartCopy(locale);
  const templates = quickstartTemplates(locale, wizard.draft.modelID);
  const disabled = wizard.busy || Boolean(wizard.progress.pending);
  return (
    <div className="min-h-0 flex-1 overflow-y-auto pb-12">
      <div className="mx-auto w-full max-w-[880px]">
        <RadioGroup
          value={wizard.progress.scenarioID}
          onValueChange={(value) => wizard.chooseScenario(value)}
          aria-label={text.scenarios}
          disabled={disabled}
          className="gap-0"
        >
          <QuickstartEntrySlot
            accessory={
              wizard.resources.isPending ? (
                <p role="status" className="text-sm text-muted-foreground">
                  {text.restore}
                </p>
              ) : undefined
            }
          >
            <Label
              htmlFor="scenario-hello"
              className={`flex h-full w-full cursor-pointer focus-within:outline focus-within:outline-[0.5px] focus-within:outline-offset-2 items-center gap-4 rounded-2xl border-[0.5px] p-5 font-normal ${wizard.progress.scenarioID === 'hello' ? 'border-foreground bg-muted' : 'border-border hover:bg-muted'}`}
            >
              <Bot aria-hidden="true" className="size-5 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1 space-y-1">
                <span className="block text-sm font-medium">{zh ? 'Hello World 示例' : 'Hello World example'}</span>
                <span className="block text-sm leading-5 text-muted-foreground">
                  {zh
                    ? '从一句问候开始，了解智能体创建、试跑与 API 调用。'
                    : 'Start with a greeting to explore agent creation, a test conversation and an API call.'}
                </span>
              </span>
              <RadioGroupItem
                className="sr-only absolute"
                id="scenario-hello"
                value="hello"
                aria-label="Hello World Agent"
              />
            </Label>
          </QuickstartEntrySlot>
          <div className={`mt-10 ${quickstartTemplateGrid}`}>
            {templates.map(({ id, icon, config: { name }, description }) => (
              <Label
                key={id}
                htmlFor={`scenario-${id}`}
                className={`${quickstartTemplateCard} cursor-pointer focus-within:outline focus-within:outline-[0.5px] focus-within:outline-offset-2 ${wizard.progress.scenarioID === id ? 'border-foreground bg-muted' : 'hover:bg-muted'}`}
              >
                <RadioGroupItem className="sr-only absolute" id={`scenario-${id}`} value={id} aria-label={name} />
                <QuickstartTemplateContent icon={icon} title={name} description={description} />
              </Label>
            ))}
          </div>
        </RadioGroup>
        <div className="mt-6 flex justify-end">
          <Button disabled={disabled} onClick={onStart}>
            {text.start}
            <ArrowRight />
          </Button>
        </div>
      </div>
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
        {agent ? (
          <div aria-label={locale === 'zh-CN' ? '复用智能体摘要' : 'Reused agent summary'} className="space-y-5">
            <div className="flex flex-wrap items-baseline gap-3">
              <h2 className="min-w-0 break-words text-base font-medium">{agent.name}</h2>
              <span className="font-mono text-xs text-muted-foreground">v{agent.version}</span>
            </div>
            <dl className="space-y-3 text-sm">
              <div>
                <dt className="text-xs text-muted-foreground">{text.model}</dt>
                <dd className="break-all">{draft.modelID}</dd>
              </div>
              {draft.description && (
                <div>
                  <dt className="text-xs text-muted-foreground">{text.description}</dt>
                  <dd className="break-words">{draft.description}</dd>
                </div>
              )}
            </dl>
            <div className="space-y-2">
              <h3 className="text-sm font-medium">{text.system}</h3>
              <p className="line-clamp-3 break-words whitespace-pre-wrap text-sm leading-6">{draft.system}</p>
              <Accordion>
                <AccordionItem value="system">
                  <AccordionTrigger>
                    {locale === 'zh-CN' ? '展开完整提示词' : 'Show full system prompt'}
                  </AccordionTrigger>
                  <AccordionContent className="break-words whitespace-pre-wrap leading-6">
                    {draft.system}
                  </AccordionContent>
                </AccordionItem>
              </Accordion>
            </div>
            <Button
              variant="outline"
              disabled={disabled}
              onClick={() => wizard.update((value) => ({ ...value, agentID: 'new' }))}
            >
              {locale === 'zh-CN' ? '改为新建' : 'Create a new agent instead'}
            </Button>
          </div>
        ) : (
          <>
            <Field>
              <FieldLabel htmlFor="quickstart-name">{text.name}</FieldLabel>
              <Input
                id="quickstart-name"
                value={draft.name}
                disabled={disabled}
                onChange={(event) => wizard.editDraft({ name: event.target.value })}
                maxLength={256}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="quickstart-description">{text.description}</FieldLabel>
              <Input
                id="quickstart-description"
                value={draft.description}
                disabled={disabled}
                onChange={(event) => wizard.editDraft({ description: event.target.value })}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="quickstart-model">{text.model}</FieldLabel>
              <Select
                value={draft.modelID}
                disabled={disabled}
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
                disabled={disabled}
                rows={7}
                onChange={(event) => wizard.editDraft({ system: event.target.value })}
                className="h-[clamp(6rem,18dvh,10rem)] resize-y"
              />
            </Field>
          </>
        )}
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
      <div className="grid min-h-0 min-w-0 flex-1 auto-rows-max gap-(--quickstart-gap) overflow-y-auto xl:grid-cols-[minmax(0,560px)_minmax(0,1fr)] xl:grid-rows-[minmax(0,1fr)] xl:overflow-hidden">
        <div className="min-h-0 min-w-0 max-w-xl pr-1 xl:overflow-y-auto">{children}</div>
        <aside className="min-h-0 min-w-0 border-t-[0.5px] border-border pt-6 xl:overflow-y-auto xl:border-t-0 xl:border-l-[0.5px] xl:pt-0 xl:pl-6">
          <div className="min-w-0">
            <QuickstartApiKey workspaceID={workspaceID} />
            <div className="mt-(--quickstart-gap) border-t-[0.5px] border-border pt-(--quickstart-gap)">
              <h2 className="mb-3 text-sm font-medium">{text.api}</h2>
              <RequestPreview code={code} hint={false} />
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

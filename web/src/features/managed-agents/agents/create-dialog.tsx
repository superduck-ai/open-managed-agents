import { useI18n } from '../../../shared/i18n';
import { LLMProviderRequired } from '../../llm-providers/LLMProviderRequired';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import { Card, CardContent } from '../../../shared/ui/card';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import { InputGroup, InputGroupTextarea } from '../../../shared/ui/input-group';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { useWorkspace } from '../../../shared/workspaces/context';
import { isModelConfigurationUnavailable } from '../../../shared/api/client';
import { useQuery } from '@tanstack/react-query';
import clsx from 'clsx';
import { ChevronDown, Loader2 } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import {
  blankAgentTemplate,
  createAgentTemplates,
  createDialogAgentConfig,
  createTemplateAppTags,
  generateCreateAgentConfig,
} from '../agentConfig';
import { ManagedErrorAlert } from '../components/common';
import { templateBody, templateTitle } from '../labels';
import { type AgentApiResponse, type AgentTemplate, type CreateAgentInput } from '../types';
import { errorMessage } from '../utils';
import { listCreateAgentModels } from './create-dialog-api';
import { CreateDialogConfigEditor } from './create-dialog-config-editor';
import { normalizeCreateAgentDraft } from './create-dialog-model';
import { AgentConfigRenderedEditor } from './create-dialog-rendered';
import { useCreateAgentDraft } from './use-create-agent-draft';
import { CreateAgentPageLayout } from './create-page-layout';

type CreateAgentEditorProps = {
  workspaceId: string;
  onClose: () => void;
  onCreate: (input: CreateAgentInput) => Promise<AgentApiResponse>;
  onCreated: (agent: AgentApiResponse) => void;
};

export function CreateAgentEditor(props: CreateAgentEditorProps) {
  const { orgUuid } = useWorkspace();
  const modelsQuery = useQuery({
    queryKey: ['create-agent', 'models', props.workspaceId],
    queryFn: () => listCreateAgentModels(props.workspaceId),
    retry: false,
  });
  if (modelsQuery.isPending) {
    return <CreateAgentEditorLoading workspaceId={props.workspaceId} onClose={props.onClose} />;
  }
  if (modelsQuery.isError) {
    const noModels = isModelConfigurationUnavailable(modelsQuery.error);
    return (
      <CreateAgentEditorLoading
        error
        noModels={noModels}
        workspaceId={props.workspaceId}
        onClose={props.onClose}
        onRetry={noModels ? undefined : () => void modelsQuery.refetch()}
      />
    );
  }
  if (!modelsQuery.data?.length) {
    return <CreateAgentEditorLoading error noModels workspaceId={props.workspaceId} onClose={props.onClose} />;
  }
  return <CreateAgentEditorContent {...props} orgUuid={orgUuid} modelOptions={modelsQuery.data ?? []} />;
}

function CreateAgentEditorLoading({
  error = false,
  noModels = false,
  workspaceId,
  onClose,
  onRetry,
}: {
  error?: boolean;
  noModels?: boolean;
  workspaceId: string;
  onClose: () => void;
  onRetry?: () => void;
}) {
  const { msg } = useI18n();
  return (
    <CreateAgentPageLayout workspaceId={workspaceId} busy={error ? undefined : true}>
      {noModels ? (
        <>
          <LLMProviderRequired
            compact
            onConfigure={() =>
              window.location.assign(`/workspaces/${encodeURIComponent(workspaceId ?? '')}/llm-models`)
            }
          />
          <div className="flex justify-end">
            <Button type="button" variant="outline" onClick={onClose}>
              {msg('common.cancel', 'Cancel')}
            </Button>
          </div>
        </>
      ) : error ? (
        <>
          <ManagedErrorAlert>
            <p className="font-medium">
              {msg('managedAgents.models.loadFailed', 'Could not load model configuration.')}
            </p>
            {msg(
              'managedAgents.models.loadFailedBody',
              'Retry before creating an agent so its displayed and saved model IDs stay consistent.',
            )}
          </ManagedErrorAlert>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose}>
              {msg('common.cancel', 'Cancel')}
            </Button>
            <Button type="button" onClick={onRetry}>
              {msg('common.retry', 'Retry')}
            </Button>
          </div>
        </>
      ) : (
        <div className="flex justify-end">
          <Button type="button" variant="outline" onClick={onClose}>
            {msg('common.cancel', 'Cancel')}
          </Button>
        </div>
      )}
    </CreateAgentPageLayout>
  );
}

function CreateAgentEditorContent({
  workspaceId,
  onClose,
  onCreate,
  onCreated,
  orgUuid,
  modelOptions,
}: CreateAgentEditorProps & {
  orgUuid?: string;
  modelOptions: Awaited<ReturnType<typeof listCreateAgentModels>>;
}) {
  const { msg, locale } = useI18n();
  const [startingPointOpen, setStartingPointOpen] = useState(true);
  const [mode, setMode] = useState<'describe' | 'template'>('describe');
  const [selectedTemplateId, setSelectedTemplateId] = useState(blankAgentTemplate.id);
  const [description, setDescription] = useState('');
  const [generatedConfig, setGeneratedConfig] = useState<CreateAgentInput | null>(null);
  const modelID = modelOptions[0].id;
  const draftState = useCreateAgentDraft(createDialogAgentConfig(blankAgentTemplate, locale, undefined, modelID));
  const [createError, setCreateError] = useState<string | null>(null);
  const [isCreating, setIsCreating] = useState(false);
  const [isGenerating, setIsGenerating] = useState(false);
  const [mcpTunnelChannelPending, setMcpTunnelChannelPending] = useState(false);
  const generateAbortRef = useRef<AbortController | null>(null);
  const rawErrorRef = useRef(draftState.rawError);
  rawErrorRef.current = draftState.rawError;
  const selectedTemplate =
    createAgentTemplates.find((template) => template.id === selectedTemplateId) ?? blankAgentTemplate;
  const startingPointName = createStartingPointName(mode, generatedConfig, selectedTemplate, msg);
  const pendingTunnelError = pendingTunnelChannelError(mcpTunnelChannelPending, msg);
  const createDisabled = createSubmissionDisabled(
    draftState.rawError,
    draftState.draftError,
    isGenerating,
    isCreating,
    mcpTunnelChannelPending,
  );

  const selectMode = (nextMode: 'describe' | 'template') => {
    if (nextMode === mode || draftState.rawError) {
      return;
    }
    setMode(nextMode);
    if (nextMode === 'describe') {
      setGeneratedConfig(null);
      draftState.replaceDraft(createDialogAgentConfig(blankAgentTemplate, locale, undefined, modelID));
    } else {
      draftState.replaceDraft(createDialogAgentConfig(selectedTemplate, locale, undefined, modelID));
    }
    setCreateError(null);
  };

  const selectTemplate = (template: AgentTemplate) => {
    runWithValidRaw(draftState.rawError, () => {
      setSelectedTemplateId(template.id);
      setMode('template');
      setGeneratedConfig(null);
      draftState.replaceDraft(createDialogAgentConfig(template, locale, undefined, modelID));
      setStartingPointOpen(false);
    });
  };

  const handleGenerate = async () => {
    const prompt = description.trim();
    if (generationBlocked(prompt, isGenerating, draftState.rawError)) {
      return;
    }
    if (!orgUuid) {
      setCreateError(
        msg('managedAgents.agents.createDialog.noOrganization', 'No organization is available for agent generation.'),
      );
      return;
    }
    const baseConfig = draftState.draft;
    const controller = new AbortController();
    generateAbortRef.current?.abort();
    generateAbortRef.current = controller;
    setIsGenerating(true);
    setCreateError(null);
    try {
      const nextConfig = await generateCreateAgentConfig({
        orgUuid,
        workspaceId,
        description: prompt,
        currentConfig: baseConfig,
        modelID,
        signal: controller.signal,
        locale,
      });
      if (rawErrorRef.current) {
        return;
      }
      setGeneratedConfig(nextConfig);
      draftState.replaceDraft(nextConfig);
      setStartingPointOpen(false);
    } catch (error) {
      if ((error as DOMException).name !== 'AbortError') {
        setCreateError(errorMessage(error));
      }
    } finally {
      if (generateAbortRef.current === controller) {
        generateAbortRef.current = null;
      }
      setIsGenerating(false);
    }
  };

  const handleCreate = async () => {
    if (agentDraftSubmissionBlocked(draftState.rawError, draftState.draftError, mcpTunnelChannelPending)) {
      return;
    }
    setIsCreating(true);
    setCreateError(null);
    try {
      const created = await onCreate(normalizeCreateAgentDraft(draftState.draft));
      onCreated(created);
    } catch (error) {
      setCreateError(errorMessage(error));
      setIsCreating(false);
    }
  };

  useEffect(
    () => () => {
      generateAbortRef.current?.abort();
    },
    [],
  );

  return (
    <CreateAgentPageLayout workspaceId={workspaceId}>
      <Collapsible
        open={startingPointOpen}
        onOpenChange={setStartingPointOpen}
        className={startingPointContainerClass(startingPointOpen)}
      >
        <div className={startingPointRowClass(startingPointOpen)}>
          <CollapsibleTrigger
            type="button"
            aria-label={
              startingPointOpen
                ? msg('managedAgents.agents.createDialog.startingPoint', 'Starting point')
                : msg('managedAgents.agents.createDialog.startingPointSummary', 'Starting point: {name}', {
                    name: startingPointName,
                  })
            }
            className="flex h-9 flex-1 items-center gap-2 rounded-lg px-0 text-left text-sm font-medium text-foreground transition-colors hover:bg-accent/40 focus-visible:ring-[0.5px] focus-visible:ring-inset focus-visible:ring-foreground focus-visible:outline-none"
          >
            <ChevronDown
              className={clsx(
                'size-4 shrink-0 text-muted-foreground transition-transform duration-220 ease-out motion-reduce:transition-none',
                startingPointOpen ? '' : '-rotate-90',
              )}
              aria-hidden
            />
            <span>{msg('managedAgents.agents.createDialog.startingPoint', 'Starting point')}</span>
            {!startingPointOpen ? (
              <span className="min-w-0 truncate font-normal text-muted-foreground" aria-hidden>
                {startingPointName}
              </span>
            ) : null}
          </CollapsibleTrigger>
        </div>

        <CollapsibleContent className="pb-2 pt-2">
          <Tabs
            value={mode}
            onValueChange={(nextValue) => nextValue && selectMode(nextValue as 'describe' | 'template')}
            className="gap-4"
          >
            <TabsList
              aria-label={msg('managedAgents.agents.createDialog.startingPoint', 'Starting point')}
              className="grid h-8 w-full grid-cols-2"
            >
              <TabsTrigger value="describe" className="px-3 text-sm font-medium">
                {msg('managedAgents.agents.createDialog.describeLabel', 'Describe your agent')}
              </TabsTrigger>
              <TabsTrigger value="template" className="px-3 text-sm font-medium">
                {msg('managedAgents.quickstart.templateSuffix', 'Template')}
              </TabsTrigger>
            </TabsList>

            <TabsContent value="describe" className="mt-0">
              <form
                className="rounded-xl"
                onSubmit={(event) => {
                  event.preventDefault();
                  void handleGenerate();
                }}
              >
                <label htmlFor="create-agent-description-input" className="sr-only">
                  {msg('managedAgents.agents.createDialog.describeLabel', 'Describe your agent')}
                </label>
                <InputGroup className="min-h-[60px] rounded-lg border-border bg-secondary">
                  <InputGroupTextarea
                    id="create-agent-description-input"
                    value={description}
                    rows={2}
                    placeholder={msg(
                      'managedAgents.agents.createDialog.describePlaceholder',
                      'Summarizes new GitHub PRs and posts a digest to Slack.',
                    )}
                    className="subtle-scrollbar min-h-[58px] max-h-[176px] overflow-y-auto overscroll-contain px-3 py-2 text-sm leading-5 placeholder:text-muted-foreground"
                    onChange={(event) => setDescription(event.target.value)}
                    autoFocus
                  />
                </InputGroup>
                <div className="mt-3 flex justify-end">
                  <Button
                    type="submit"
                    variant="secondary"
                    disabled={!description.trim() || isGenerating || Boolean(draftState.rawError)}
                  >
                    {isGenerating ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
                    {isGenerating
                      ? msg('managedAgents.agents.createDialog.generating', 'Generating...')
                      : msg('managedAgents.agents.createDialog.generate', 'Generate')}
                  </Button>
                </div>
              </form>
            </TabsContent>

            <TabsContent value="template" className="mt-0">
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {createAgentTemplates.map((template) => (
                  <CreateAgentTemplateCard
                    key={template.id}
                    template={template}
                    selected={template.id === selectedTemplateId}
                    onSelect={() => selectTemplate(template)}
                  />
                ))}
              </div>
            </TabsContent>
          </Tabs>
        </CollapsibleContent>
      </Collapsible>

      <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-sm font-medium">{msg('managedAgents.agents.createDialog.agentConfig', 'Agent config')}</h2>
        <Tabs
          value={draftState.view}
          onValueChange={(value) => value && draftState.selectView(value as 'rendered' | 'raw')}
        >
          <TabsList aria-label={msg('managedAgents.agents.createDialog.editorMode', 'Editor mode')}>
            <TabsTrigger value="rendered">{msg('managedAgents.agents.createDialog.rendered', 'Rendered')}</TabsTrigger>
            <TabsTrigger value="raw" disabled={mcpTunnelChannelPending}>
              {msg('managedAgents.agents.createDialog.raw', 'Raw')}
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      {draftState.view === 'rendered' ? (
        <AgentConfigRenderedEditor
          compact
          orgUuid={orgUuid}
          workspaceId={workspaceId}
          draft={draftState.draft}
          modelOptions={modelOptions}
          onPendingTunnelChange={setMcpTunnelChannelPending}
          tunnelSelectionResetKey={draftState.replacementRevision}
          onChange={draftState.setDraft}
        />
      ) : (
        <div className="min-h-[520px] pb-8">
          <CreateDialogConfigEditor
            format={draftState.format}
            configText={draftState.rawText}
            configError={draftState.rawError}
            onFormatChange={draftState.selectFormat}
            onEditorChange={draftState.updateRawText}
            validateEditorText={draftState.validateRawText}
          />
        </div>
      )}
      <div className="sticky bottom-0 z-10 mt-4 flex flex-wrap items-center justify-between gap-3 bg-background py-4">
        {createError || draftState.draftError || pendingTunnelError ? (
          <p role="alert" className="max-w-full break-words text-sm text-destructive">
            {createError || draftState.draftError || pendingTunnelError}
          </p>
        ) : (
          <span />
        )}
        <div className="flex shrink-0 gap-2">
          <Button type="button" variant="secondary" disabled={isCreating} onClick={onClose}>
            {msg('common.cancel', 'Cancel')}
          </Button>
          <Button type="button" disabled={createDisabled} onClick={handleCreate}>
            {isCreating
              ? msg('common.creating', 'Creating...')
              : msg('managedAgents.agents.createLabel', 'Create agent')}
          </Button>
        </div>
      </div>
    </CreateAgentPageLayout>
  );
}

function generationBlocked(prompt: string, isGenerating: boolean, rawError: string | null) {
  return !prompt || isGenerating || Boolean(rawError);
}

function createSubmissionDisabled(
  rawError: string | null,
  draftError: string | null,
  isGenerating: boolean,
  isCreating: boolean,
  mcpTunnelChannelPending = false,
) {
  return agentDraftSubmissionBlocked(rawError, draftError, mcpTunnelChannelPending) || isGenerating || isCreating;
}

function agentDraftSubmissionBlocked(
  rawError: string | null,
  draftError: string | null,
  mcpTunnelChannelPending: boolean,
) {
  return Boolean(rawError || draftError) || mcpTunnelChannelPending;
}

function pendingTunnelChannelError(pending: boolean, msg: ReturnType<typeof useI18n>['msg']) {
  return pending
    ? msg(
        'managedAgents.agents.createDialog.mcpTunnelChannelPending',
        'Finish choosing a Tunnel channel before continuing.',
      )
    : null;
}

function createStartingPointName(
  mode: 'describe' | 'template',
  generatedConfig: CreateAgentInput | null,
  selectedTemplate: AgentTemplate,
  msg: ReturnType<typeof useI18n>['msg'],
) {
  if (mode === 'template') return templateTitle(selectedTemplate, msg);
  return generatedConfig?.name?.trim() || templateTitle(blankAgentTemplate, msg);
}

function runWithValidRaw(rawError: string | null, action: () => void) {
  if (!rawError) {
    action();
  }
}

export function CreateAgentTemplateCard({
  template,
  selected,
  onSelect,
}: {
  template: AgentTemplate;
  selected: boolean;
  onSelect: () => void;
}) {
  const { msg } = useI18n();
  const hasApps = Boolean(createTemplateAppTags[template.id]?.length);
  const title = templateTitle(template, msg);
  const body = templateBody(template, msg);

  return (
    <Button
      type="button"
      variant="ghost"
      className={clsx(
        'h-auto min-w-0 w-full items-stretch justify-stretch whitespace-normal rounded-xl border-0 bg-transparent p-0 text-left shadow-none hover:bg-transparent',
      )}
      onClick={onSelect}
    >
      <Card
        className={clsx(
          'h-full min-w-0 w-full gap-0 rounded-xl py-0 text-left shadow-none ring-inset transition-colors duration-150 ease-out motion-reduce:transition-none group-focus-visible/button:ring-foreground',
          hasApps ? 'min-h-[116px]' : 'min-h-[104px]',
          selected ? 'bg-muted/80 ring-ring/30' : 'bg-card/70 group-hover/button:bg-muted/60',
        )}
      >
        <CardContent className="flex h-full flex-col gap-1 px-3 py-3">
          <span className="text-sm font-medium leading-5 text-foreground">{title}</span>
          <span className="line-clamp-3 text-sm leading-5 text-muted-foreground">{body}</span>
          <CreateTemplateApps templateId={template.id} />
        </CardContent>
      </Card>
    </Button>
  );
}

function startingPointContainerClass(open: boolean) {
  return clsx('shrink-0 overflow-hidden', !open && 'border-b-[0.5px] border-border');
}

function startingPointRowClass(open: boolean) {
  return clsx('flex items-center gap-3', open ? 'py-1' : 'py-2');
}

export function CreateTemplateApps({ templateId }: { templateId: string }) {
  const apps = createTemplateAppTags[templateId];

  if (!apps?.length) {
    return null;
  }

  return (
    <span className="mt-auto flex flex-wrap items-center gap-1.5 pt-3" aria-label={`${templateId} integrations`}>
      {apps.map((app) => {
        const Icon = app.icon;
        return (
          <Badge
            key={app.label}
            variant="secondary"
            className={clsx('size-5 shrink-0 rounded-full border-[0.5px] border-border p-0', app.tone)}
            title={app.label}
          >
            <Icon className="size-3" aria-hidden />
            <span className="sr-only">{app.label}</span>
          </Badge>
        );
      })}
    </span>
  );
}

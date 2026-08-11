import { useI18n } from '../../../shared/i18n';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import { Card, CardContent } from '../../../shared/ui/card';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../../../shared/ui/dialog';
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupTextarea } from '../../../shared/ui/input-group';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { useWorkspace } from '../../../shared/workspaces/context';
import { useModelCatalog } from '../../model-catalog/hooks';
import { useQuery } from '@tanstack/react-query';
import clsx from 'clsx';
import { ChevronDown, Loader2, Sparkles } from 'lucide-react';
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
import { useEffectiveModelMappings } from '../modelMappings';
import { type AgentApiResponse, type AgentTemplate, type CreateAgentInput } from '../types';
import { errorMessage, navigateToAgentConfig } from '../utils';
import { listCreateAgentModels } from './create-dialog-api';
import { CreateDialogConfigEditor } from './create-dialog-config-editor';
import { normalizeCreateAgentDraft } from './create-dialog-model';
import { AgentConfigRenderedEditor } from './create-dialog-rendered';
import { AgentDialogFrame } from './AgentDialogFrame';
import { AgentModelCatalogSelect } from './AgentModelCatalogSelect';
import { useCreateAgentDraft } from './use-create-agent-draft';
import { agentModelName } from './model';

type CreateAgentDialogProps = {
  workspaceId: string;
  onClose: () => void;
  onCreate: (input: CreateAgentInput) => Promise<AgentApiResponse>;
};

export function CreateAgentDialog(props: CreateAgentDialogProps) {
  const { orgUuid } = useWorkspace();
  const modelMappingsQuery = useEffectiveModelMappings(orgUuid);
  const modelsQuery = useQuery({
    queryKey: ['create-agent', 'models', props.workspaceId],
    queryFn: () => listCreateAgentModels(props.workspaceId),
    retry: false,
  });
  if ((orgUuid && modelMappingsQuery.isPending) || modelsQuery.isPending) {
    return <CreateAgentDialogLoading onClose={props.onClose} />;
  }
  if ((orgUuid && modelMappingsQuery.isError) || modelsQuery.isError) {
    return (
      <CreateAgentDialogLoading
        error
        onClose={props.onClose}
        onRetry={() => void Promise.all([modelMappingsQuery.refetch(), modelsQuery.refetch()])}
      />
    );
  }
  return (
    <CreateAgentDialogContent
      {...props}
      orgUuid={orgUuid}
      modelMappings={modelMappingsQuery.data ?? {}}
      modelOptions={modelsQuery.data ?? []}
    />
  );
}

function CreateAgentDialogLoading({
  error = false,
  onClose,
  onRetry,
}: {
  error?: boolean;
  onClose: () => void;
  onRetry?: () => void;
}) {
  const { msg } = useI18n();
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        aria-label={msg('managedAgents.agents.createLabel', 'Create agent')}
        aria-busy={error ? undefined : true}
        className="max-w-[720px] sm:max-w-[720px]"
      >
        <DialogHeader>
          <DialogTitle>{msg('managedAgents.agents.createLabel', 'Create agent')}</DialogTitle>
          <DialogDescription>
            {error
              ? msg('managedAgents.models.loadFailed', 'Could not load model configuration.')
              : msg('common.loading', 'Loading...')}
          </DialogDescription>
        </DialogHeader>
        {error ? (
          <>
            <ManagedErrorAlert>
              {msg(
                'managedAgents.models.loadFailedBody',
                'Retry before creating an agent so its displayed and saved model IDs stay consistent.',
              )}
            </ManagedErrorAlert>
            <Button type="button" className="justify-self-end" onClick={onRetry}>
              {msg('common.retry', 'Retry')}
            </Button>
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function CreateAgentDialogContent({
  workspaceId,
  onClose,
  onCreate,
  orgUuid,
  modelMappings,
  modelOptions,
}: CreateAgentDialogProps & {
  orgUuid?: string;
  modelMappings: Record<string, string>;
  modelOptions: Awaited<ReturnType<typeof listCreateAgentModels>>;
}) {
  const { msg, locale } = useI18n();
  const modelCatalog = useModelCatalog(orgUuid);
  const [startingPointOpen, setStartingPointOpen] = useState(true);
  const [mode, setMode] = useState<'describe' | 'template'>('describe');
  const [selectedTemplateId, setSelectedTemplateId] = useState(blankAgentTemplate.id);
  const [description, setDescription] = useState('');
  const [generatedConfig, setGeneratedConfig] = useState<CreateAgentInput | null>(null);
  const initialModelID = modelCatalog.defaultModelID || modelOptions[0]?.id || '';
  const draftState = useCreateAgentDraft(
    createDialogAgentConfig(blankAgentTemplate, locale, undefined, initialModelID),
  );
  const [createError, setCreateError] = useState<string | null>(null);
  const [isCreating, setIsCreating] = useState(false);
  const [isGenerating, setIsGenerating] = useState(false);
  const generateAbortRef = useRef<AbortController | null>(null);
  const rawErrorRef = useRef(draftState.rawError);
  rawErrorRef.current = draftState.rawError;
  const selectedTemplate =
    createAgentTemplates.find((template) => template.id === selectedTemplateId) ?? blankAgentTemplate;
  const startingPointName =
    mode === 'describe'
      ? generatedConfig?.name?.trim() || templateTitle(blankAgentTemplate, msg)
      : templateTitle(selectedTemplate, msg);
  const selectedModelID = agentModelName(draftState.draft.model);
  const selectedModelAvailable = modelCatalog.modelIDs.includes(selectedModelID);
  const createDisabled = createSubmissionDisabled(
    draftState.rawError,
    draftState.draftError,
    isGenerating,
    isCreating,
    selectedModelAvailable,
  );

  useCreateAgentDefaultModel(
    draftState.replaceDraft,
    locale,
    modelCatalog.defaultModelID,
    modelOptions[0]?.id,
    selectedModelID,
  );

  const selectMode = (nextMode: 'describe' | 'template') => {
    if (nextMode === mode || draftState.rawError) {
      return;
    }
    setMode(nextMode);
    const modelID = createModelID(selectedModelID, modelCatalog.defaultModelID, modelOptions[0]?.id);
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
      const modelID = createModelID(selectedModelID, modelCatalog.defaultModelID, modelOptions[0]?.id);
      draftState.replaceDraft(createDialogAgentConfig(template, locale, undefined, modelID));
      setStartingPointOpen(false);
    });
  };

  const handleGenerate = async () => {
    const prompt = description.trim();
    if (
      generationBlocked(prompt, isGenerating, draftState.rawError, selectedModelAvailable, () =>
        setCreateError(msg('managedAgents.agents.createDialog.selectModel', 'Select an available model first.')),
      )
    ) {
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
        availableModelIDs: modelCatalog.modelIDs,
        modelMappings,
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
    if (
      creationBlocked(draftState.rawError, draftState.draftError, selectedModelAvailable, () =>
        setCreateError(msg('managedAgents.agents.createDialog.selectModel', 'Select an available model first.')),
      )
    ) {
      return;
    }
    setIsCreating(true);
    setCreateError(null);
    try {
      const created = await onCreate(normalizeCreateAgentDraft(draftState.draft));
      onClose();
      navigateToAgentConfig(workspaceId, created.id);
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
    <AgentDialogFrame
      onClose={onClose}
      label={msg('managedAgents.agents.createLabel', 'Create agent')}
      title={msg('managedAgents.agents.createLabel', 'Create agent')}
      description={msg(
        'managedAgents.agents.createDialog.description',
        'Start from a template or describe what you need.',
      )}
      className="grid-rows-1 h-[calc(100dvh-2rem)] max-w-[880px] overflow-hidden rounded-[22px] p-0 sm:max-w-[calc(100vw-2rem)] xl:max-w-[880px]"
    >
      {/* grid-rows-1 keeps the fixed-height create dialog's content row sized to the viewport. */}
      <div className="subtle-scrollbar min-h-0 flex-1 overflow-y-auto px-[23px]">
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
                  : msg('managedAgents.agents.createDialog.startingPointSummary', 'Starting point · {name}', {
                      name: startingPointName,
                    })
              }
              className="flex h-9 flex-1 items-center gap-2 rounded-lg px-2 text-left text-sm font-semibold text-foreground transition-colors hover:bg-accent/40 focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
            >
              <ChevronDown
                className={clsx(
                  'size-4 shrink-0 text-muted-foreground transition-transform duration-200 motion-reduce:transition-none',
                  startingPointOpen ? '' : '-rotate-90',
                )}
                aria-hidden
              />
              <span>{msg('managedAgents.agents.createDialog.startingPoint', 'Starting point')}</span>
              {!startingPointOpen ? (
                <span className="min-w-0 truncate font-normal text-muted-foreground" aria-hidden>
                  · {startingPointName}
                </span>
              ) : null}
            </CollapsibleTrigger>
          </div>

          <CollapsibleContent className="border-t border-border/60 px-3 pb-3 pt-3">
            <Tabs
              value={mode}
              onValueChange={(nextValue) => nextValue && selectMode(nextValue as 'describe' | 'template')}
              className="gap-4"
            >
              <TabsList
                aria-label={msg('managedAgents.agents.createDialog.startingPoint', 'Starting point')}
                className="grid h-10 w-full grid-cols-2"
              >
                <TabsTrigger value="describe" className="px-3 text-sm font-semibold">
                  {msg('managedAgents.quickstart.initial.inputLabel', 'Describe your agent')}
                </TabsTrigger>
                <TabsTrigger value="template" className="px-3 text-sm font-semibold">
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
                    {msg('managedAgents.quickstart.initial.inputLabel', 'Describe your agent')}
                  </label>
                  <InputGroup className="min-h-[156px] items-stretch gap-0 rounded-[20px] border border-border/70 bg-background/70 px-3 py-3 shadow-sm transition-colors hover:border-border focus-within:border-ring/60">
                    <InputGroupTextarea
                      id="create-agent-description-input"
                      value={description}
                      rows={1}
                      placeholder={msg(
                        'managedAgents.agents.createDialog.describePlaceholder',
                        'Summarizes new GitHub PRs and posts a digest to Slack.',
                      )}
                      className="subtle-scrollbar min-h-[108px] max-h-[176px] overflow-y-auto overscroll-contain px-1 py-1 text-[15px] leading-6 placeholder:text-muted-foreground/70"
                      onChange={(event) => setDescription(event.target.value)}
                      autoFocus
                    />
                    <InputGroupAddon align="block-end" className="cursor-default justify-end gap-0 px-0 pb-0 pt-3">
                      <InputGroupButton
                        type="submit"
                        variant="secondary"
                        size="sm"
                        disabled={!description.trim() || isGenerating || Boolean(draftState.rawError)}
                        className="rounded-lg px-4 text-[13px] font-semibold"
                      >
                        {isGenerating ? (
                          <Loader2 className="size-4 animate-spin" aria-hidden />
                        ) : (
                          <Sparkles className="size-4" aria-hidden />
                        )}
                        {isGenerating
                          ? msg('managedAgents.agents.createDialog.generating', 'Generating...')
                          : msg('managedAgents.agents.createDialog.generate', 'Generate')}
                      </InputGroupButton>
                    </InputGroupAddon>
                  </InputGroup>
                </form>
              </TabsContent>

              <TabsContent value="template" className="mt-0">
                <div className="grid grid-cols-3 gap-3">
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

        <CreateAgentConfigSection
          workspaceId={workspaceId}
          draftState={draftState}
          modelCatalog={modelCatalog}
          modelOptions={modelOptions}
          selectedModelID={selectedModelID}
          isCreating={isCreating}
          isGenerating={isGenerating}
        />
      </div>

      <CreateAgentDialogFooter
        createError={createError}
        draftError={draftState.draftError}
        createDisabled={createDisabled}
        isCreating={isCreating}
        onCreate={handleCreate}
      />
    </AgentDialogFrame>
  );
}

function CreateAgentConfigSection({
  workspaceId,
  draftState,
  modelCatalog,
  modelOptions,
  selectedModelID,
  isCreating,
  isGenerating,
}: {
  workspaceId: string;
  draftState: ReturnType<typeof useCreateAgentDraft>;
  modelCatalog: ReturnType<typeof useModelCatalog>;
  modelOptions: Awaited<ReturnType<typeof listCreateAgentModels>>;
  selectedModelID: string;
  isCreating: boolean;
  isGenerating: boolean;
}) {
  const { msg } = useI18n();
  return (
    <>
      <AgentModelCatalogSelect
        models={modelCatalog.models}
        value={selectedModelID}
        onValueChange={(modelID) => {
          if (!draftState.rawError) {
            draftState.replaceDraft({ ...draftState.draft, model: modelID });
          }
        }}
        loading={modelCatalog.isPending}
        error={modelCatalog.isError}
        stale={Boolean(modelCatalog.catalogState?.stale)}
        disabled={isCreating || isGenerating}
        className="mt-4"
      />

      <div className="mt-6 flex items-center justify-between border-b border-border pb-4">
        <h2 className="text-base font-semibold">
          {msg('managedAgents.agents.createDialog.agentConfig', 'Agent config')}
        </h2>
        <Tabs
          value={draftState.view}
          onValueChange={(value) => value && draftState.selectView(value as 'rendered' | 'raw')}
        >
          <TabsList aria-label={msg('managedAgents.agents.createDialog.editorMode', 'Editor mode')}>
            <TabsTrigger value="rendered">{msg('managedAgents.agents.createDialog.rendered', 'Rendered')}</TabsTrigger>
            <TabsTrigger value="raw">{msg('managedAgents.agents.createDialog.raw', 'Raw')}</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      {draftState.view === 'rendered' ? (
        <AgentConfigRenderedEditor
          workspaceId={workspaceId}
          draft={draftState.draft}
          modelOptions={modelOptions}
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
    </>
  );
}

function CreateAgentDialogFooter({
  createError,
  draftError,
  createDisabled,
  isCreating,
  onCreate,
}: {
  createError: string | null;
  draftError: string | null;
  createDisabled: boolean;
  isCreating: boolean;
  onCreate: () => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="flex min-h-16 items-center justify-between gap-4 border-t border-border px-[23px] py-3">
      {createError || draftError ? (
        <p className="line-clamp-2 text-sm text-destructive">{createError || draftError}</p>
      ) : (
        <span />
      )}
      <Button
        type="button"
        disabled={createDisabled}
        size="sm"
        className={clsx(
          'px-3 text-[14px] font-semibold leading-5',
          createDisabled
            ? 'cursor-not-allowed bg-accent text-muted-foreground/70'
            : 'bg-foreground text-background hover:bg-muted',
        )}
        onClick={onCreate}
      >
        {isCreating ? msg('common.creating', 'Creating...') : msg('managedAgents.agents.createLabel', 'Create agent')}
      </Button>
    </div>
  );
}

function generationBlocked(
  prompt: string,
  isGenerating: boolean,
  rawError: string | null,
  selectedModelAvailable: boolean,
  onUnavailableModel: () => void,
) {
  if (!prompt || isGenerating || rawError) {
    return true;
  }
  if (!selectedModelAvailable) {
    onUnavailableModel();
    return true;
  }
  return false;
}

function createSubmissionDisabled(
  rawError: string | null,
  draftError: string | null,
  isGenerating: boolean,
  isCreating: boolean,
  selectedModelAvailable: boolean,
) {
  return Boolean(rawError || draftError) || isGenerating || isCreating || !selectedModelAvailable;
}

function creationBlocked(
  rawError: string | null,
  draftError: string | null,
  selectedModelAvailable: boolean,
  onUnavailableModel: () => void,
) {
  if (rawError || draftError) {
    return true;
  }
  if (!selectedModelAvailable) {
    onUnavailableModel();
    return true;
  }
  return false;
}

function createModelID(selectedModelID: string, defaultModelID: string, fallbackModelID?: string) {
  return selectedModelID || defaultModelID || fallbackModelID || '';
}

function useCreateAgentDefaultModel(
  replaceDraft: (next: CreateAgentInput) => void,
  locale: Parameters<typeof createDialogAgentConfig>[1],
  defaultModelID: string,
  fallbackModelID: string | undefined,
  selectedModelID: string,
) {
  useEffect(() => {
    const nextModelID = createModelID('', defaultModelID, fallbackModelID);
    if (!nextModelID || selectedModelID) {
      return;
    }
    replaceDraft(createDialogAgentConfig(blankAgentTemplate, locale, undefined, nextModelID));
  }, [defaultModelID, fallbackModelID, locale, replaceDraft, selectedModelID]);
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
        'h-auto w-full items-stretch justify-stretch whitespace-normal rounded-xl border-0 bg-transparent p-0 text-left shadow-none hover:bg-transparent',
      )}
      onClick={onSelect}
    >
      <Card
        className={clsx(
          'h-full w-full gap-0 rounded-xl py-0 text-left shadow-none transition-[background-color,box-shadow,ring-color]',
          hasApps ? 'min-h-[116px]' : 'min-h-[104px]',
          selected ? 'bg-muted/80 ring-ring/30 shadow-sm' : 'bg-card/70 group-hover/button:bg-muted/60',
        )}
      >
        <CardContent className="flex h-full flex-col gap-1 px-3 py-3">
          <span className="text-[15px] font-medium leading-5 text-foreground">{title}</span>
          <span className="line-clamp-3 text-[13px] leading-[18px] text-muted-foreground">{body}</span>
          <CreateTemplateApps templateId={template.id} />
        </CardContent>
      </Card>
    </Button>
  );
}

function startingPointContainerClass(open: boolean) {
  return clsx(
    'mt-4 shrink-0 overflow-hidden',
    open ? 'rounded-xl border border-border/70 bg-card/60 shadow-sm' : 'border-b border-border/70',
  );
}

function startingPointRowClass(open: boolean) {
  return clsx('flex items-center gap-3 py-1.5', open ? 'px-2' : 'px-0');
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
            className={clsx('size-5 shrink-0 rounded-full border border-border p-0', app.tone)}
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

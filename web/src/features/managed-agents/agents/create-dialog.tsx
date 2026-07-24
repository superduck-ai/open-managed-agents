import { useI18n } from '../../../shared/i18n';
import { Badge } from '../../../shared/ui/badge';
import { Button } from '../../../shared/ui/button';
import { Card, CardContent } from '../../../shared/ui/card';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '../../../shared/ui/dialog';
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupTextarea } from '../../../shared/ui/input-group';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { useWorkspace } from '../../../shared/workspaces/context';
import clsx from 'clsx';
import { ChevronDown, Loader2, Sparkles, X } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  blankAgentTemplate,
  createAgentConfigText,
  createAgentTemplates,
  createDialogAgentConfig,
  createTemplateAppTags,
  generateCreateAgentConfig,
  parseCreateAgentConfigText,
} from '../agentConfig';
import { templateBody, templateTitle } from '../labels';
import { type AgentApiResponse, type AgentTemplate, type CodeFormat, type CreateAgentInput } from '../types';
import { errorMessage, navigateToAgentConfig } from '../utils';
import { CreateDialogConfigEditor } from './create-dialog-config-editor';

export function CreateAgentDialog({
  workspaceId,
  onClose,
  onCreate,
}: {
  workspaceId: string;
  onClose: () => void;
  onCreate: (input: CreateAgentInput) => Promise<AgentApiResponse>;
}) {
  const { msg, locale } = useI18n();
  const { orgUuid } = useWorkspace();
  const [startingPointOpen, setStartingPointOpen] = useState(true);
  const [mode, setMode] = useState<'describe' | 'template'>('describe');
  const [selectedTemplateId, setSelectedTemplateId] = useState(blankAgentTemplate.id);
  const [format, setFormat] = useState<CodeFormat>('YAML');
  const [description, setDescription] = useState('');
  const [generatedConfig, setGeneratedConfig] = useState<CreateAgentInput | null>(null);
  const [configInput, setConfigInput] = useState<CreateAgentInput>(() =>
    createDialogAgentConfig(blankAgentTemplate, locale),
  );
  const [configText, setConfigText] = useState(() =>
    createAgentConfigText(createDialogAgentConfig(blankAgentTemplate, locale), 'YAML'),
  );
  const [configError, setConfigError] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);
  const [isCreating, setIsCreating] = useState(false);
  const [isGenerating, setIsGenerating] = useState(false);
  const generateAbortRef = useRef<AbortController | null>(null);
  const configInputRef = useRef(configInput);
  const selectedTemplate =
    createAgentTemplates.find((template) => template.id === selectedTemplateId) ?? blankAgentTemplate;
  const startingPointName =
    mode === 'describe'
      ? generatedConfig?.name?.trim() || msg('managedAgents.quickstart.initial.inputLabel', 'Describe your agent')
      : templateTitle(selectedTemplate, msg);
  const createDisabled = Boolean(configError) || isGenerating || isCreating;

  useEffect(() => {
    configInputRef.current = configInput;
  }, [configInput]);

  const validateEditorText = useCallback((text: string, nextFormat: CodeFormat) => {
    const parsed = parseCreateAgentConfigText(text, nextFormat, configInputRef.current);
    return parsed.ok ? null : parsed.error;
  }, []);

  const hydrateConfig = (input: CreateAgentInput) => {
    setConfigInput(input);
    setConfigText(createAgentConfigText(input, format));
    setConfigError(null);
    setCreateError(null);
  };

  const parseCurrentConfig = () => {
    const parsed = parseCreateAgentConfigText(configText, format, configInput);
    if (!parsed.ok) {
      setConfigError(parsed.error);
      return null;
    }
    setConfigError(null);
    setConfigInput(parsed.input);
    return parsed.input;
  };

  const handleEditorChange = (value: string) => {
    setConfigText(value);
    const parsed = parseCreateAgentConfigText(value, format, configInput);
    if (!parsed.ok) {
      setConfigError(parsed.error);
      return;
    }
    setConfigError(null);
    setConfigInput(parsed.input);
  };

  const selectFormat = (nextFormat: CodeFormat) => {
    if (nextFormat === format) {
      return;
    }
    const parsed = parseCurrentConfig();
    if (!parsed) {
      return;
    }
    setFormat(nextFormat);
    setConfigText(createAgentConfigText(parsed, nextFormat));
    setCreateError(null);
  };

  const selectMode = (nextMode: 'describe' | 'template') => {
    if (nextMode === mode) {
      return;
    }
    setMode(nextMode);
    if (nextMode === 'describe') {
      setGeneratedConfig(null);
      hydrateConfig(createDialogAgentConfig(blankAgentTemplate, locale));
    } else {
      hydrateConfig(createDialogAgentConfig(selectedTemplate, locale));
    }
    setCreateError(null);
  };

  const selectTemplate = (template: AgentTemplate) => {
    setSelectedTemplateId(template.id);
    setMode('template');
    setGeneratedConfig(null);
    hydrateConfig(createDialogAgentConfig(template, locale));
    setStartingPointOpen(false);
  };

  const handleGenerate = async () => {
    const prompt = description.trim();
    if (!prompt || isGenerating) {
      return;
    }
    if (!orgUuid) {
      setCreateError(
        msg('managedAgents.agents.createDialog.noOrganization', 'No organization is available for agent generation.'),
      );
      return;
    }
    const baseConfig = parseCurrentConfig() ?? configInput;
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
        signal: controller.signal,
        locale,
      });
      setGeneratedConfig(nextConfig);
      hydrateConfig(nextConfig);
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
    const parsed = parseCurrentConfig();
    if (!parsed) {
      return;
    }
    setIsCreating(true);
    setCreateError(null);
    try {
      const created = await onCreate(parsed);
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
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      {/* grid-rows-1 forces the single grid row to fill the fixed height, so the
          inner flex h-full container resolves to a real height. Without it the
          auto grid track makes h-full collapse to content height, and the config
          editor + Create button get clipped by overflow-hidden when the Starting
          Point panel is expanded. */}
      <DialogContent
        aria-label={msg('managedAgents.agents.createLabel', 'Create agent')}
        className="grid-rows-1 h-[min(720px,calc(100dvh-2rem))] max-w-[720px] overflow-hidden rounded-[17px] p-0 sm:max-w-[720px]"
        showCloseButton={false}
      >
        <div className="flex h-full min-h-0 flex-col px-[23px] pb-[23px] pt-[19px] text-foreground">
          <DialogClose
            render={
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="absolute right-[18px] top-[18px] text-foreground hover:bg-accent"
              />
            }
          >
            <X className="size-[22px]" aria-hidden />
            <span className="sr-only">{msg('common.close', 'Close')}</span>
          </DialogClose>

          <DialogHeader className="pr-8">
            <DialogTitle className="text-[22px] font-semibold leading-[26px] text-foreground">
              {msg('managedAgents.agents.createLabel', 'Create agent')}
            </DialogTitle>
            <DialogDescription className="mt-1 text-sm leading-5 text-muted-foreground">
              {msg('managedAgents.agents.createDialog.description', 'Start from a template or describe what you need.')}
            </DialogDescription>
          </DialogHeader>

          {/* shrink-0 keeps the panel at its content height. Its overflow-hidden
              makes the flex min-height resolve to 0, so without shrink-0 the panel
              gets compressed below its own content and the describe form / template
              grid get clipped by the panel's own overflow-hidden. */}
          <Collapsible
            open={startingPointOpen}
            onOpenChange={setStartingPointOpen}
            className="mt-4 shrink-0 overflow-hidden rounded-xl border border-border/70 bg-card/60 shadow-sm"
          >
            <div className="flex items-center gap-3 px-2 py-1.5">
              <CollapsibleTrigger
                type="button"
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
              </CollapsibleTrigger>
              {!startingPointOpen ? (
                <Badge
                  variant="secondary"
                  className="max-w-[220px] shrink-0 justify-start truncate rounded-md px-2 py-1 text-xs font-medium text-muted-foreground"
                >
                  {startingPointName}
                </Badge>
              ) : null}
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
                          disabled={!description.trim() || isGenerating}
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

          <CreateDialogConfigEditor
            format={format}
            configText={configText}
            configError={configError}
            onFormatChange={selectFormat}
            onEditorChange={handleEditorChange}
            validateEditorText={validateEditorText}
          />

          <div className="mt-4 flex items-center justify-between gap-4">
            {createError ? <p className="text-sm text-destructive">{createError}</p> : <span />}
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
              onClick={handleCreate}
            >
              {isCreating
                ? msg('common.creating', 'Creating...')
                : msg('managedAgents.agents.createLabel', 'Create agent')}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
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

import { Alert, AlertDescription, AlertTitle } from '@/shared/ui/alert';
import { useI18n } from '../../shared/i18n';
import {
  AlertCircle,
  Braces,
  Check,
  ChevronDown,
  Code2,
  ExternalLink,
  Info,
  Loader2,
  PencilLine,
  Play,
  Plus,
  RefreshCw,
  Sparkles,
  Trash2,
} from 'lucide-react';
import { Dispatch, SetStateAction, useEffect, useMemo, useRef, useState } from 'react';
import clsx from 'clsx';
import { Button } from '@/shared/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/shared/ui/collapsible';
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/shared/ui/command';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/shared/ui/dropdown-menu';
import { Input } from '@/shared/ui/input';
import { Label } from '@/shared/ui/label';
import { Popover, PopoverContent, PopoverTrigger } from '@/shared/ui/popover';
import { RadioGroup, RadioGroupItem } from '@/shared/ui/radio-group';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import { Slider } from '@/shared/ui/slider';
import { Textarea } from '@/shared/ui/textarea';
import { hljs } from './highlight';
import { listWorkbenchRevisions, WorkbenchModel, WorkbenchRevision, WorkbenchTool } from './api';
import {
  capitalize,
  clampThinkingBudgetTokens,
  defaultSchema,
  defaultWebSearchToolForm,
  formatHistoryTimestamp,
  historyDayLabel,
  historyRevisionName,
  historyRevisionPreview,
  MAX_THINKING_BUDGET_TOKENS,
  MIN_THINKING_BUDGET_TOKENS,
  nextThinkingForMode,
  thinkingEffortOptions,
  thinkingMode,
  ToolForm,
  WebSearchRestriction,
  webSearchRestrictionLabel,
  WebSearchToolForm,
  webSearchToolFormFromTool,
  webSearchToolSummary,
  WORKBENCH_MAX_TOKENS,
  WorkbenchExample,
} from './model';
import { IconButton, ToggleRow } from './components';

const webSearchRestrictionOptions: Array<{ value: WebSearchRestriction; label: string }> = [
  { value: 'none', label: 'None' },
  { value: 'allowed_domains', label: 'Allow domains' },
  { value: 'blocked_domains', label: 'Blocked domains' },
];

const exampleToolMenuItems = [
  { value: 'weather', label: 'get_weather' },
  { value: 'stock_price', label: 'get_stock_price' },
  { value: 'time', label: 'get_time' },
] as const;

type ExampleToolKind = (typeof exampleToolMenuItems)[number]['value'];

const thinkingTypeOptions = ['disabled', 'enabled', 'adaptive'] as const;

function firstSliderValue(value: number | readonly number[]) {
  return Array.isArray(value) ? (value[0] ?? 0) : value;
}

export function ModelDrawer({
  draft,
  models,
  setDraft,
  onRun,
  isRunning,
}: {
  draft: WorkbenchRevision;
  models: WorkbenchModel[];
  setDraft: Dispatch<SetStateAction<WorkbenchRevision>>;
  onRun: () => void;
  isRunning: boolean;
}) {
  const { msg } = useI18n();
  const effortOptions = thinkingEffortOptions.map((option) => ({
    ...option,
    label: msg(`workbench.effort.${option.value}`, option.label),
  }));
  const thinkingType = thinkingMode(draft.thinking);
  const [modelMenuOpen, setModelMenuOpen] = useState(false);
  const [modelSearch, setModelSearch] = useState('');
  const modelOptions = useMemo(() => {
    if (models.some((model) => model.model_name === draft.model_name)) {
      return models;
    }
    return [{ model_name: draft.model_name }, ...models];
  }, [draft.model_name, models]);
  const filteredModels = useMemo(() => {
    const query = modelSearch.trim().toLowerCase();
    if (!query) {
      return modelOptions;
    }
    return modelOptions.filter((model) => {
      const description = modelDescription(model);
      return `${model.model_name} ${model.display_name ?? ''} ${description}`.toLowerCase().includes(query);
    });
  }, [modelOptions, modelSearch]);
  const currentEffort = String(draft.thinking?.effort ?? 'high');
  const currentTemperature = typeof draft.temperature === 'number' ? draft.temperature : 1;
  const currentBudgetTokens = clampThinkingBudgetTokens(draft.thinking?.budget_tokens);
  const closeModelMenu = () => {
    setModelMenuOpen(false);
    setModelSearch('');
  };

  return (
    <div className="workbench-model-settings">
      <div className="workbench-model-field">
        <Popover
          open={modelMenuOpen}
          onOpenChange={(nextOpen) => {
            setModelMenuOpen(nextOpen);
            if (!nextOpen) {
              setModelSearch('');
            }
          }}
        >
          <PopoverTrigger
            render={
              <Button
                type="button"
                role="combobox"
                variant="outline"
                aria-label={draft.model_name}
                aria-expanded={modelMenuOpen}
                aria-controls="workbench-model-listbox"
                className="h-10 w-full min-w-0 justify-between rounded-[10px] px-3 font-normal [&>span]:truncate"
              />
            }
          >
            <span>{draft.model_name}</span>
            <ChevronDown className="size-4" aria-hidden />
          </PopoverTrigger>
          <PopoverContent
            id="workbench-model-listbox"
            role="listbox"
            align="start"
            sideOffset={6}
            className="w-[var(--anchor-width)] min-w-[var(--anchor-width)] max-w-[min(32rem,calc(100vw-2rem))] gap-0 overflow-hidden p-0"
          >
            <Command
              shouldFilter={false}
              label={msg('workbench.searchModels', 'Search models…')}
              className="bg-transparent"
            >
              <CommandInput
                aria-label={msg('workbench.searchModels', 'Search models…')}
                placeholder={msg('workbench.searchModels', 'Search models…')}
                value={modelSearch}
                onValueChange={setModelSearch}
              />
              <CommandList className="max-h-[284px]">
                {filteredModels.length ? (
                  <CommandGroup className="p-1">
                    {filteredModels.map((model) => {
                      const selected = model.model_name === draft.model_name;
                      const description = modelDescription(model);
                      return (
                        <CommandItem
                          key={model.model_name}
                          role="option"
                          aria-selected={selected}
                          value={model.model_name}
                          keywords={[model.display_name ?? '', model.name ?? '', description]}
                          className="h-auto gap-2"
                          onSelect={() => {
                            setDraft((current) => ({ ...current, model_name: model.model_name }));
                            closeModelMenu();
                          }}
                        >
                          <span className="grid min-w-0 flex-1">
                            <span className="break-all text-sm font-medium">{model.model_name}</span>
                            <span className="text-xs text-muted-foreground empty:hidden">{description}</span>
                          </span>
                          {selected ? <Check className="size-4 shrink-0 text-foreground" aria-hidden /> : null}
                        </CommandItem>
                      );
                    })}
                  </CommandGroup>
                ) : (
                  <CommandEmpty>{msg('workbench.modelsEmpty', 'No models found')}</CommandEmpty>
                )}
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      </div>

      {thinkingType === 'disabled' ? (
        <section className="workbench-model-section">
          <div className="workbench-model-row">
            <div className="workbench-model-label-with-help">
              <span>{msg('workbench.temperature', 'Temperature')}</span>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                className="workbench-model-help"
                aria-label={msg(
                  'workbench.temperatureHint',
                  'Higher generates more creative responses, lower produces more predictable responses',
                )}
              >
                <Info className="size-3.5" aria-hidden />
              </Button>
            </div>
            <span className="workbench-model-static-value">{formatTemperature(currentTemperature)}</span>
          </div>
          <Slider
            aria-label={msg('workbench.temperature', 'Temperature')}
            min={0}
            max={1}
            step={0.1}
            value={[currentTemperature]}
            onValueChange={(nextValue) => {
              const value = firstSliderValue(nextValue);
              setDraft((current) => ({ ...current, temperature: value }));
            }}
          />
        </section>
      ) : null}

      <section className="workbench-model-section">
        <div className="workbench-model-row">
          <div className="workbench-model-label-with-help">
            <span>{msg('workbench.maxTokens', 'Max tokens')}</span>
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              className="workbench-model-help"
              aria-label={msg('workbench.maxTokensHint', 'Maximum length of Claude’s responses')}
            >
              <Info className="size-3.5" aria-hidden />
            </Button>
          </div>
          <Input
            type="number"
            min={1}
            max={WORKBENCH_MAX_TOKENS}
            step={1}
            value={draft.max_tokens_to_sample}
            onChange={(event) => {
              const value = Math.max(1, Number(event.currentTarget.value) || 1);
              setDraft((current) => ({
                ...current,
                max_tokens_to_sample: value,
              }));
            }}
            className="workbench-model-number"
          />
        </div>
        <Slider
          aria-label={msg('workbench.maxTokens', 'Max tokens')}
          min={1}
          max={WORKBENCH_MAX_TOKENS}
          step={1}
          value={[draft.max_tokens_to_sample]}
          onValueChange={(nextValue) => {
            const value = firstSliderValue(nextValue);
            setDraft((current) => ({ ...current, max_tokens_to_sample: value }));
          }}
        />
      </section>

      <section className={clsx('workbench-model-section', thinkingType === 'enabled' && 'is-thinking-enabled')}>
        <div className="workbench-model-radio-row">
          <span className="workbench-model-label">{msg('workbench.thinking', 'Thinking')}</span>
          <RadioGroup
            aria-label={msg('workbench.thinking', 'Thinking')}
            name="workbench-thinking"
            value={thinkingType}
            onValueChange={(value) =>
              setDraft((current) => ({
                ...current,
                thinking: nextThinkingForMode(current.thinking, value),
              }))
            }
            className="workbench-model-radio-group"
          >
            {thinkingTypeOptions.map((value) => {
              const id = `workbench-thinking-${value}`;
              return (
                <div key={value} className="workbench-model-radio-option">
                  <RadioGroupItem
                    id={id}
                    aria-label={msg(`workbench.thinkingMode.${value}`, capitalize(value))}
                    value={value}
                  />
                  <Label htmlFor={id} className="workbench-model-radio-label">
                    {msg(`workbench.thinkingMode.${value}`, capitalize(value))}
                  </Label>
                </div>
              );
            })}
          </RadioGroup>
        </div>
        {thinkingType === 'enabled' ? (
          <div className="workbench-model-budget-block">
            <div className="workbench-model-row">
              <div className="workbench-model-label-with-help">
                <span>{msg('workbench.budgetTokens', 'Budget tokens')}</span>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  className="workbench-model-help"
                  aria-label={msg('workbench.budgetTokens', 'Budget tokens')}
                >
                  <Info className="size-3.5" aria-hidden />
                </Button>
              </div>
              <Input
                type="number"
                min={MIN_THINKING_BUDGET_TOKENS}
                max={MAX_THINKING_BUDGET_TOKENS}
                step={1}
                value={currentBudgetTokens}
                onChange={(event) => {
                  const value = clampThinkingBudgetTokens(event.currentTarget.value);
                  setDraft((current) => ({
                    ...current,
                    thinking: { ...current.thinking, type: 'enabled', budget_tokens: value },
                  }));
                }}
                className="workbench-model-number"
              />
            </div>
            <Slider
              aria-label={msg('workbench.budgetTokens', 'Budget tokens')}
              min={MIN_THINKING_BUDGET_TOKENS}
              max={MAX_THINKING_BUDGET_TOKENS}
              step={1}
              value={[currentBudgetTokens]}
              onValueChange={(nextValue) => {
                const value = clampThinkingBudgetTokens(firstSliderValue(nextValue));
                setDraft((current) => ({
                  ...current,
                  thinking: { ...current.thinking, type: 'enabled', budget_tokens: value },
                }));
              }}
            />
          </div>
        ) : null}
      </section>

      <section className="workbench-model-section">
        <div className="workbench-model-effort-row">
          <span className="workbench-model-label">{msg('workbench.effort', 'Effort')}</span>
          <div className="w-28">
            <Select<string>
              value={currentEffort}
              items={effortOptions.map((option) => ({ value: option.value, label: option.label }))}
              onValueChange={(nextValue) => {
                if (nextValue === null) {
                  return;
                }
                setDraft((current) => ({ ...current, thinking: { ...current.thinking, effort: nextValue } }));
              }}
            >
              <SelectTrigger
                aria-label={msg('workbench.effort', 'Effort')}
                size="sm"
                className="w-full"
                onClick={() => setModelMenuOpen(false)}
              >
                <SelectValue>
                  {msg(`workbench.effort.${currentEffort}`, thinkingEffortLabel(currentEffort))}
                </SelectValue>
              </SelectTrigger>
              <SelectContent id="workbench-effort-listbox" align="end">
                {effortOptions.map((option) => {
                  return (
                    <SelectItem key={option.value} value={option.value} label={option.label}>
                      <span>{option.label}</span>
                    </SelectItem>
                  );
                })}
              </SelectContent>
            </Select>
          </div>
        </div>
      </section>

      <a
        className="workbench-model-api-link"
        href="https://docs.claude.com/en/api/messages"
        target="_blank"
        rel="noreferrer"
      >
        {msg('workbench.apiOptions', 'View all API options')}
        <ExternalLink className="size-3" aria-hidden />
      </a>

      <Button type="button" size="lg" className="mx-5 mb-[18px] mt-auto w-[calc(100%-40px)]" onClick={onRun}>
        {isRunning ? (
          <Loader2 className="size-4 animate-spin" aria-hidden />
        ) : (
          <Play className="size-4" fill="currentColor" strokeWidth={0} aria-hidden />
        )}
        <span>{msg('workbench.run', 'Run')}</span>
        <span className="text-xs font-normal text-primary-foreground/70">⌘ + ⏎</span>
      </Button>
    </div>
  );
}

export function modelDescription(model: WorkbenchModel) {
  const label = model.display_name ?? model.name;
  return label && label !== model.model_name ? label : '';
}

export function thinkingEffortLabel(value: string) {
  return thinkingEffortOptions.find((option) => option.value === value)?.label ?? capitalize(value);
}

export function formatTemperature(value: number) {
  return Number.isInteger(value) ? String(value) : value.toFixed(1);
}

export function VariablesDrawer({
  variables,
  values,
  setValues,
  generationLogicOpen,
  setGenerationLogicOpen,
  generationLogic,
  setGenerationLogic,
  onGenerateGenerationLogic,
  onClearGenerationLogic,
  isGeneratingGenerationLogic,
  onRun,
  isRunning,
  canRun,
}: {
  variables: string[];
  values: Record<string, string>;
  setValues: Dispatch<SetStateAction<Record<string, string>>>;
  generationLogicOpen: boolean;
  setGenerationLogicOpen: Dispatch<SetStateAction<boolean>>;
  generationLogic: string;
  setGenerationLogic: Dispatch<SetStateAction<string>>;
  onGenerateGenerationLogic: () => void;
  onClearGenerationLogic: () => void;
  isGeneratingGenerationLogic: boolean;
  onRun: () => void;
  isRunning: boolean;
  canRun: boolean;
}) {
  const { msg } = useI18n();
  if (!variables.length) {
    return (
      <div className="workbench-variable-empty">
        <Braces className="mb-3 size-7 text-muted-foreground/70" aria-hidden />
        <h3>{msg('workbench.variablesEmpty', 'No variables')}</h3>
        <p>{msg('workbench.addVariableHint', 'Add {variable} to the prompt.', { variable: '{{variable_name}}' })}</p>
      </div>
    );
  }

  return (
    <div className="workbench-variable-panel">
      <div className="workbench-variable-fields">
        {variables.map((name, index) => (
          <label key={name} className="workbench-variable-field">
            <span>{`{{${name}}}`}</span>
            <Textarea
              aria-label={`{{${name}}}`}
              autoFocus={index === 0}
              placeholder={msg('workbench.variableValuePlaceholder', 'Enter an example value…')}
              aria-invalid={!values[name]?.trim()}
              value={values[name] ?? ''}
              onChange={(event) => {
                const value = event.currentTarget.value;
                setValues((current) => ({ ...current, [name]: value }));
              }}
              className="min-h-[86px] resize-none bg-secondary"
            />
          </label>
        ))}
        <Collapsible
          open={generationLogicOpen}
          onOpenChange={setGenerationLogicOpen}
          className={clsx('workbench-variable-logic', generationLogicOpen && 'is-open')}
        >
          <CollapsibleTrigger
            render={<Button type="button" variant="ghost" className="workbench-variable-logic-toggle" />}
          >
            <span>{msg('workbench.generationLogic', 'Variable generation logic')}</span>
            <ChevronDown
              className={clsx(
                'size-4 shrink-0 transition-transform duration-200 ease-out motion-reduce:transition-none',
                generationLogicOpen && 'rotate-180',
              )}
              aria-hidden
            />
          </CollapsibleTrigger>
          <CollapsibleContent className="workbench-variable-logic-editor">
            <div className="workbench-variable-logic-header">
              <p>{msg('workbench.generationLogicHint', 'Update the logic for your use case and retry generation.')}</p>
              <div className="workbench-variable-logic-actions">
                <IconButton
                  label={msg('workbench.regenerateLogic', 'Regenerate variable generation logic')}
                  compact
                  disabled={isGeneratingGenerationLogic}
                  onClick={onGenerateGenerationLogic}
                >
                  {isGeneratingGenerationLogic ? (
                    <Loader2 className="size-4 animate-spin" aria-hidden />
                  ) : (
                    <RefreshCw className="size-4" aria-hidden />
                  )}
                </IconButton>
                <IconButton
                  label={msg('workbench.deleteLogic', 'Delete variable generation logic')}
                  compact
                  onClick={onClearGenerationLogic}
                >
                  <Trash2 className="size-4" aria-hidden />
                </IconButton>
              </div>
            </div>
            <Textarea
              aria-label={msg(
                'workbench.generationLogicPlaceholder',
                'Click Generate to populate with some initial logic...',
              )}
              placeholder={msg(
                'workbench.generationLogicPlaceholder',
                'Click Generate to populate with some initial logic...',
              )}
              value={generationLogic}
              onChange={(event) => setGenerationLogic(event.currentTarget.value)}
              className="min-h-[66px] resize-none bg-secondary"
            />
          </CollapsibleContent>
        </Collapsible>
      </div>
      <Button type="button" size="lg" className="mt-auto w-full" disabled={!canRun} onClick={onRun}>
        {isRunning ? <Loader2 className="size-4 animate-spin" aria-hidden /> : <Play className="size-4" aria-hidden />}
        <span>{msg('workbench.run', 'Run')}</span>
        <span className="text-xs font-medium text-primary-foreground/70">⌘ + ⏎</span>
      </Button>
    </div>
  );
}

export function ToolsDrawer({
  tools,
  toolForm,
  setToolForm,
  customTool,
  setCustomTool,
  webSearchTool,
  setWebSearchTool,
  onAddCustom,
  onAddWebSearch,
  onUpdateWebSearch,
  onRemove,
  onRun,
  isRunning,
  canRun,
}: {
  tools: WorkbenchTool[];
  toolForm: ToolForm;
  setToolForm: Dispatch<SetStateAction<ToolForm>>;
  customTool: { name: string; description: string; schema: string };
  setCustomTool: Dispatch<SetStateAction<{ name: string; description: string; schema: string }>>;
  webSearchTool: WebSearchToolForm;
  setWebSearchTool: Dispatch<SetStateAction<WebSearchToolForm>>;
  onAddCustom: () => void;
  onAddWebSearch: () => void;
  onUpdateWebSearch: (index: number) => void;
  onRemove: (index: number) => void;
  onRun: () => void;
  isRunning: boolean;
  canRun: boolean;
}) {
  const { msg } = useI18n();
  const restrictionOptions = webSearchRestrictionOptions.map((option) => ({
    ...option,
    label: msg(`workbench.searchRestriction.${option.value}`, option.label),
  }));
  const [exampleMenuOpen, setExampleMenuOpen] = useState(false);
  const [editingWebSearchIndex, setEditingWebSearchIndex] = useState<number | null>(null);
  const toolsPanelRef = useRef<HTMLDivElement>(null);
  const customToolNameRef = useRef<HTMLInputElement>(null);
  const canAddWebSearch = true;
  const hasWebSearchTool = tools.some((tool) => tool.type === 'web_search_v0');
  const applyExampleTool = (kind: ExampleToolKind) => {
    const examples: Record<typeof kind, { name: string; description: string; schema: string }> = {
      weather: {
        name: 'get_weather',
        description: 'Get current weather for a location.',
        schema: defaultSchema,
      },
      stock_price: {
        name: 'get_stock_price',
        description: 'Get the latest stock price for a ticker symbol.',
        schema: `{
  "type": "object",
  "properties": {
    "ticker": {
      "type": "string",
      "description": "Ticker symbol, for example AAPL"
    }
  },
  "required": ["ticker"]
        }`,
      },
      time: {
        name: 'get_time',
        description: 'Get the current time for a timezone or location.',
        schema: `{
  "type": "object",
  "properties": {
    "timezone": {
      "type": "string",
      "description": "IANA timezone, for example America/Los_Angeles"
    }
  },
  "required": ["timezone"]
}`,
      },
    };
    setCustomTool(examples[kind]);
    setExampleMenuOpen(false);
  };

  useEffect(() => {
    const scrollRoot = toolsPanelRef.current?.parentElement;
    if (!scrollRoot) {
      return;
    }
    scrollRoot.scrollTop = 0;
    if (toolForm === 'custom') {
      customToolNameRef.current?.focus();
    }
  }, [toolForm]);

  const cancelWebSearchForm = () => {
    setToolForm(null);
    setEditingWebSearchIndex(null);
    setWebSearchTool(defaultWebSearchToolForm());
  };

  const startEditingWebSearchTool = (index: number, tool: WorkbenchTool) => {
    setWebSearchTool(webSearchToolFormFromTool(tool));
    setEditingWebSearchIndex(index);
    setToolForm('web_search');
  };

  const submitWebSearchTool = () => {
    if (editingWebSearchIndex === null) {
      onAddWebSearch();
      return;
    }
    onUpdateWebSearch(editingWebSearchIndex);
  };

  return (
    <div ref={toolsPanelRef} className={clsx('workbench-tools-panel', toolForm && 'is-form-open')}>
      <div className="workbench-tools-actions">
        <Button
          type="button"
          variant="outline"
          className="workbench-tool-choice"
          disabled={toolForm !== null}
          onClick={() => setToolForm('custom')}
        >
          <Plus className="size-4" aria-hidden />
          {msg('workbench.custom', 'Custom')}
        </Button>
        {!hasWebSearchTool ? (
          <Button
            type="button"
            variant="outline"
            className="workbench-tool-choice"
            disabled={toolForm !== null}
            onClick={() => setToolForm('web_search')}
          >
            <Plus className="size-4" aria-hidden />
            {msg('workbench.webSearch', 'Web search')}
          </Button>
        ) : null}
      </div>

      {!tools.length && !toolForm ? (
        <div className="workbench-tools-empty">
          <h3>{msg('workbench.toolsEmpty', 'No tools defined')}</h3>
          <p>
            <a
              href="https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/overview"
              target="_blank"
              rel="noreferrer"
            >
              {msg('workbench.learnMore', 'Learn more')}
            </a>
          </p>
        </div>
      ) : null}

      {toolForm === 'custom' ? (
        <div className="workbench-tool-form-card">
          <h3>{msg('workbench.customTool', 'Custom Tool')}</h3>
          <label className="workbench-tool-field">
            {msg('workbench.toolName', 'Name')}
            <Input
              ref={customToolNameRef}
              value={customTool.name}
              onChange={(event) => {
                const value = event.currentTarget.value;
                setCustomTool((current) => ({ ...current, name: value }));
              }}
              placeholder="tool_name"
              className="bg-secondary"
            />
          </label>
          <label className="workbench-tool-field">
            {msg('workbench.toolDescription', 'Description')}
            <Input
              value={customTool.description}
              onChange={(event) => {
                const value = event.currentTarget.value;
                setCustomTool((current) => ({ ...current, description: value }));
              }}
              placeholder={msg('workbench.toolDescriptionPlaceholder', 'Description of tool')}
              className="bg-secondary"
            />
          </label>
          <label className="workbench-tool-field">
            <span className="workbench-tool-label-row">
              <span>{msg('workbench.inputSchema', 'Input Schema')}</span>
              <a
                href="https://json-schema.org/learn/getting-started-step-by-step"
                target="_blank"
                rel="noreferrer"
                aria-label={msg('workbench.schemaLink', 'JSON Schema (opens in new tab)')}
              >
                JSON Schema ↗
              </a>
            </span>
            <JsonSchemaEditor
              value={customTool.schema}
              onChange={(value) => {
                setCustomTool((current) => ({ ...current, schema: value }));
              }}
            />
          </label>
          <div className="workbench-tool-form-footer">
            <div className="workbench-tool-menu-anchor">
              <DropdownMenu open={exampleMenuOpen} onOpenChange={setExampleMenuOpen}>
                <DropdownMenuTrigger
                  render={<Button type="button" variant="ghost" size="lg" aria-expanded={exampleMenuOpen} />}
                >
                  {msg('workbench.exampleTools', 'Example tools')}
                  <ChevronDown className="size-3.5" aria-hidden />
                </DropdownMenuTrigger>
                <DropdownMenuContent
                  align="start"
                  side="bottom"
                  sideOffset={6}
                  className="w-auto min-w-[190px] bg-popover"
                >
                  <DropdownMenuRadioGroup
                    value=""
                    onValueChange={(value) => applyExampleTool(value as ExampleToolKind)}
                  >
                    {exampleToolMenuItems.map((item) => (
                      <DropdownMenuRadioItem
                        key={item.value}
                        value={item.value}
                        className="min-h-[34px] px-2.5 py-2 text-sm font-medium"
                      >
                        {item.label}
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
            <div className="workbench-tool-form-actions">
              <Button type="button" variant="ghost" size="lg" onClick={() => setToolForm(null)}>
                {msg('workbench.cancel', 'Cancel')}
              </Button>
              <Button type="button" size="lg" disabled={!customTool.name.trim()} onClick={onAddCustom}>
                {msg('workbench.addTool', 'Add tool')}
              </Button>
            </div>
          </div>
        </div>
      ) : null}

      {toolForm === 'web_search' ? (
        <div className="workbench-tool-form-card">
          <div>
            <h3>{msg('workbench.webSearch', 'Web search')}</h3>
          </div>
          <div className="workbench-tool-field">
            <span>{msg('workbench.searchRestrictions', 'Search restrictions')}</span>
            <Select<WebSearchRestriction>
              value={webSearchTool.searchRestriction}
              items={restrictionOptions.map((option) => ({ value: option.value, label: option.label }))}
              onValueChange={(nextValue) => {
                if (nextValue === null) {
                  return;
                }
                setWebSearchTool((current) => ({
                  ...current,
                  searchRestriction: nextValue,
                  domains: nextValue === 'none' ? '' : current.domains,
                }));
              }}
            >
              <SelectTrigger
                aria-label={msg('workbench.searchRestrictions', 'Search restrictions')}
                className="w-full bg-secondary"
              >
                <SelectValue>
                  {msg(
                    `workbench.searchRestriction.${webSearchTool.searchRestriction}`,
                    webSearchRestrictionLabel(webSearchTool.searchRestriction),
                  )}
                </SelectValue>
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false} className="min-w-[240px]">
                {restrictionOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value} label={option.label}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {webSearchTool.searchRestriction !== 'none' ? (
            <Textarea
              value={webSearchTool.domains}
              onChange={(event) => {
                const value = event.currentTarget.value;
                setWebSearchTool((current) => ({ ...current, domains: value }));
              }}
              placeholder={msg(
                'workbench.domainsPlaceholder',
                'Enter domains (e.g., example.com, example.com/path, example.com/*)',
              )}
              className="min-h-[66px] resize-none bg-secondary"
            />
          ) : null}
          <ToggleRow
            label={msg('workbench.limitToolCalls', 'Limit the number of times this tool is called')}
            checked={webSearchTool.maxUsesEnabled}
            onChange={(checked) => setWebSearchTool((current) => ({ ...current, maxUsesEnabled: checked }))}
          />
          {webSearchTool.maxUsesEnabled ? (
            <Input
              aria-label={msg('workbench.webSearchMaxUses', 'Web search max uses')}
              type="number"
              min={1}
              value={webSearchTool.maxUses}
              onChange={(event) => {
                const value = Math.max(1, Number(event.currentTarget.value) || 1);
                setWebSearchTool((current) => ({ ...current, maxUses: value }));
              }}
              className="bg-secondary"
            />
          ) : null}
          <ToggleRow
            label={msg('workbench.localizeResults', 'Localize results')}
            checked={webSearchTool.localize}
            onChange={(checked) => setWebSearchTool((current) => ({ ...current, localize: checked }))}
          />
          <div className="workbench-tool-form-footer justify-end">
            <div className="workbench-tool-form-actions">
              <Button type="button" variant="ghost" size="lg" onClick={cancelWebSearchForm}>
                {msg('workbench.cancel', 'Cancel')}
              </Button>
              <Button type="button" size="lg" disabled={!canAddWebSearch} onClick={submitWebSearchTool}>
                {editingWebSearchIndex === null
                  ? msg('workbench.addTool', 'Add tool')
                  : msg('workbench.saveTool', 'Save tool')}
              </Button>
            </div>
          </div>
        </div>
      ) : null}

      {tools.map((tool, index) =>
        tool.type === 'web_search_v0' ? (
          <div key={tool.id ?? `${tool.name}-${index}`} className="workbench-tool-web-card">
            <div className="workbench-tool-web-header">
              <h3>{msg('workbench.webSearch', 'Web search')}</h3>
              <div className="workbench-tool-card-actions">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="workbench-tool-icon-action"
                  aria-label={msg('workbench.editWebSearch', 'Edit Web search')}
                  onClick={() => startEditingWebSearchTool(index, tool)}
                >
                  <PencilLine className="size-4" aria-hidden />
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="workbench-tool-icon-action"
                  aria-label={msg('workbench.removeWebSearch', 'Remove Web search')}
                  onClick={() => onRemove(index)}
                >
                  <Trash2 className="size-4" aria-hidden />
                </Button>
              </div>
            </div>
            <div className="workbench-tool-web-body">
              <div className="workbench-tool-web-summary">{webSearchToolSummary(tool, msg)}</div>
            </div>
          </div>
        ) : (
          <div key={tool.id ?? `${tool.name}-${index}`} className="workbench-tool-row">
            <div className="min-w-0">
              <div className="truncate text-sm font-semibold text-foreground" title={tool.name}>
                {tool.name}
              </div>
              <div className="mt-1 text-xs text-muted-foreground">{msg('workbench.customToolType', 'Custom tool')}</div>
            </div>
            <IconButton
              label={msg('workbench.removeTool', 'Remove {name}', { name: tool.name })}
              onClick={() => onRemove(index)}
            >
              <Trash2 className="size-4" aria-hidden />
            </IconButton>
          </div>
        ),
      )}

      {tools.length && !toolForm ? (
        <div className="workbench-tools-run-footer">
          <Button type="button" size="lg" className="w-full" disabled={!canRun} onClick={onRun}>
            {isRunning ? (
              <Loader2 className="size-4 animate-spin" aria-hidden />
            ) : (
              <Play className="size-4" aria-hidden />
            )}
            <span>{msg('workbench.run', 'Run')}</span>
            <span className="text-xs font-medium text-primary-foreground/70">⌘ + ⏎</span>
          </Button>
        </div>
      ) : null}
    </div>
  );
}

export function JsonSchemaEditor({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const { msg } = useI18n();
  const highlightedSchema = useMemo(
    () => hljs.highlight(value, { language: 'json', ignoreIllegals: true }).value,
    [value],
  );
  const highlightRef = useRef<HTMLPreElement>(null);

  return (
    <div className="workbench-tool-schema">
      <pre ref={highlightRef} className="workbench-tool-schema-highlight subtle-scrollbar" aria-hidden>
        <code className="language-json hljs" dangerouslySetInnerHTML={{ __html: highlightedSchema || ' ' }} />
      </pre>
      <Textarea
        aria-label={msg('workbench.inputSchema', 'Input Schema')}
        spellCheck={false}
        value={value}
        onChange={(event) => onChange(event.currentTarget.value)}
        onScroll={(event) => {
          if (!highlightRef.current) {
            return;
          }
          highlightRef.current.scrollTop = event.currentTarget.scrollTop;
          highlightRef.current.scrollLeft = event.currentTarget.scrollLeft;
        }}
        className="workbench-tool-schema-input subtle-scrollbar"
      />
    </div>
  );
}

export function ExamplesDrawer({
  variables,
  examples,
  formOpen,
  editingExampleId,
  values,
  setValues,
  idealOutput,
  setIdealOutput,
  additionalContext,
  setAdditionalContext,
  contextOpen,
  setContextOpen,
  isGenerating,
  onGenerate,
  onOpenNew,
  onCancel,
  onEdit,
  onRemove,
  onAdd,
  onRun,
  isRunning,
  canRun,
}: {
  variables: string[];
  examples: WorkbenchExample[];
  formOpen: boolean;
  editingExampleId: string | null;
  values: Record<string, string>;
  setValues: Dispatch<SetStateAction<Record<string, string>>>;
  idealOutput: string;
  setIdealOutput: Dispatch<SetStateAction<string>>;
  additionalContext: string;
  setAdditionalContext: Dispatch<SetStateAction<string>>;
  contextOpen: boolean;
  setContextOpen: Dispatch<SetStateAction<boolean>>;
  isGenerating: boolean;
  onGenerate: () => void;
  onOpenNew: () => void;
  onCancel: () => void;
  onEdit: (example: WorkbenchExample) => void;
  onRemove: (exampleId: string) => void;
  onAdd: () => void;
  onRun: () => void;
  isRunning: boolean;
  canRun: boolean;
}) {
  const { msg } = useI18n();
  const canAdd = Boolean(variables.every((name) => values[name]?.trim()) && idealOutput.trim());
  const isEditing = Boolean(editingExampleId);

  if (!formOpen && examples.length === 0) {
    return (
      <div className="workbench-examples-panel">
        <div className="workbench-examples-empty">
          <h3>{msg('workbench.examplesEmpty', 'No examples defined')}</h3>
          <p>
            <a
              href="https://docs.anthropic.com/en/docs/build-with-claude/prompt-engineering/multishot-prompting"
              target="_blank"
              rel="noreferrer"
            >
              {msg('workbench.learnMore', 'Learn more')}
            </a>
          </p>
          <Button type="button" size="lg" className="mt-6" onClick={onOpenNew}>
            {msg('workbench.addExample', 'Add example')}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="workbench-examples-panel">
      <div className="workbench-example-scroll subtle-scrollbar">
        {!formOpen ? (
          <Button type="button" variant="outline" className="mb-[18px] w-fit" onClick={onOpenNew}>
            <Plus className="size-4" aria-hidden />
            {msg('workbench.addExample', 'Add example')}
          </Button>
        ) : null}

        {formOpen ? (
          <div className="workbench-example-form">
            <div className="workbench-example-form-header">
              <h3>
                {isEditing ? msg('workbench.editExample', 'Edit example') : msg('workbench.addExample', 'Add example')}
              </h3>
              <Button type="button" variant="outline" size="lg" disabled={isGenerating} onClick={onGenerate}>
                {isGenerating ? (
                  <Loader2 className="size-4 animate-spin" aria-hidden />
                ) : (
                  <Sparkles className="size-4" aria-hidden />
                )}
                {msg('workbench.generateExample', 'Generate example')}
              </Button>
            </div>

            {variables.map((name) => (
              <label key={name} className="workbench-example-field">
                <span>{`{{${name}}}`}</span>
                <Textarea
                  aria-label={`{{${name}}}`}
                  placeholder={msg('workbench.exampleValuePlaceholder', 'Enter an example value...')}
                  value={values[name] ?? ''}
                  onChange={(event) => {
                    const value = event.currentTarget.value;
                    setValues((current) => ({ ...current, [name]: value }));
                  }}
                  className="min-h-[86px] resize-y bg-secondary"
                />
              </label>
            ))}

            <label className="workbench-example-field">
              <span>{msg('workbench.idealOutput', 'Ideal output')}</span>
              <Textarea
                aria-label={msg('workbench.idealOutput', 'Ideal output')}
                placeholder={msg('workbench.idealOutputPlaceholder', 'Enter ideal output...')}
                value={idealOutput}
                onChange={(event) => setIdealOutput(event.currentTarget.value)}
                className="min-h-[126px] resize-y bg-secondary"
              />
            </label>

            {contextOpen ? (
              <label className="workbench-example-field">
                <span>{msg('workbench.additionalContext', 'Additional context')}</span>
                <Textarea
                  aria-label={msg('workbench.additionalContext', 'Additional context')}
                  placeholder={msg('workbench.contextPlaceholder', 'Add any extra details Claude should consider...')}
                  value={additionalContext}
                  onChange={(event) => setAdditionalContext(event.currentTarget.value)}
                  className="min-h-[86px] resize-y bg-secondary"
                />
              </label>
            ) : (
              <Button type="button" variant="ghost" className="w-fit" onClick={() => setContextOpen(true)}>
                <Plus className="size-4" aria-hidden />
                {msg('workbench.addContext', 'Add additional context')}
              </Button>
            )}

            <div className="workbench-example-footer">
              <Button type="button" variant="outline" onClick={onCancel}>
                {msg('workbench.cancel', 'Cancel')}
              </Button>
              <Button type="button" disabled={!canAdd} onClick={onAdd}>
                {isEditing
                  ? msg('workbench.saveChanges', 'Save changes')
                  : msg('workbench.addExampleSubmit', 'Add Example')}
              </Button>
            </div>
          </div>
        ) : (
          <div className="workbench-example-list">
            {examples.map((example, index) => (
              <article key={example.id} className="workbench-example-card">
                <div className="workbench-example-card-head">
                  <div className="workbench-example-card-values">
                    {variables.map((name) => (
                      <span
                        key={name}
                        className="workbench-example-value-pill"
                        title={`${name}: ${example.values[name] || ''}`}
                      >
                        {name}: {example.values[name] || ''}
                      </span>
                    ))}
                  </div>
                  <div className="workbench-example-card-actions">
                    <IconButton
                      label={msg('workbench.editExampleNumber', 'Edit example {number}', { number: index + 1 })}
                      onClick={() => onEdit(example)}
                    >
                      <PencilLine className="size-4" aria-hidden />
                    </IconButton>
                    <IconButton
                      label={msg('workbench.deleteExampleNumber', 'Delete example {number}', { number: index + 1 })}
                      onClick={() => onRemove(example.id)}
                    >
                      <Trash2 className="size-4" aria-hidden />
                    </IconButton>
                  </div>
                </div>
                <div className="workbench-example-output">{example.idealOutput}</div>
                {example.additionalContext ? (
                  <div className="workbench-example-context-preview">
                    <div>{msg('workbench.additionalContextLabel', 'Additional context:')}</div>
                    <p>{example.additionalContext}</p>
                  </div>
                ) : null}
              </article>
            ))}
          </div>
        )}
      </div>
      <Button type="button" size="lg" className="mt-auto w-full" disabled={!canRun} onClick={onRun}>
        {isRunning ? <Loader2 className="size-4 animate-spin" aria-hidden /> : <Play className="size-4" aria-hidden />}
        <span>{msg('workbench.run', 'Run')}</span>
        <span className="text-xs font-medium text-primary-foreground/70">⌘ + ⏎</span>
      </Button>
    </div>
  );
}

export function HistoryDrawer({
  orgUuid,
  promptId,
  currentDraft,
  currentRevisionId,
  hasUnsavedChanges,
  canSave,
  isSaving,
  onSave,
  onDiscard,
  onRestore,
}: {
  orgUuid?: string;
  promptId?: string;
  currentDraft: WorkbenchRevision;
  currentRevisionId?: string;
  hasUnsavedChanges: boolean;
  canSave: boolean;
  isSaving: boolean;
  onSave: () => Promise<void>;
  onDiscard: () => Promise<void>;
  onRestore: (revisionId: string) => Promise<void>;
}) {
  const { locale, msg } = useI18n();
  const [historyLoading, setHistoryLoading] = useState(Boolean(orgUuid && promptId));
  const [historyError, setHistoryError] = useState(false);
  const [refreshVersion, setRefreshVersion] = useState(0);
  const [revisions, setRevisions] = useState<WorkbenchRevision[]>([]);
  const [restoringRevisionId, setRestoringRevisionId] = useState<string | null>(null);
  const [discardingDraft, setDiscardingDraft] = useState(false);
  useEffect(() => {
    if (!orgUuid || !promptId) {
      setHistoryLoading(false);
      return;
    }
    let active = true;
    setHistoryLoading(true);
    setHistoryError(false);
    void listWorkbenchRevisions(orgUuid, promptId, false)
      .then((items) => {
        if (active) setRevisions(items);
      })
      .catch(() => {
        if (active) setHistoryError(true);
      })
      .finally(() => {
        if (active) setHistoryLoading(false);
      });
    return () => {
      active = false;
    };
  }, [currentRevisionId, orgUuid, promptId, refreshVersion]);

  const restoreRevisionFromHistory = async (revision: WorkbenchRevision) => {
    if (hasUnsavedChanges || revision.id === currentRevisionId || restoringRevisionId) {
      return;
    }
    setRestoringRevisionId(revision.id);
    try {
      await onRestore(revision.id);
    } finally {
      setRestoringRevisionId(null);
    }
  };
  const discardDraftFromHistory = async () => {
    if (discardingDraft) {
      return;
    }
    setDiscardingDraft(true);
    try {
      await onDiscard();
    } finally {
      setDiscardingDraft(false);
    }
  };
  const draftPreviewText = historyRevisionPreview(currentDraft, msg);

  return (
    <div className="workbench-history-panel">
      {historyLoading ? (
        <div role="status" className="px-4 py-3 text-sm text-muted-foreground">
          {msg('workbench.loadingVersions', 'Loading versions…')}
        </div>
      ) : null}
      {historyError ? (
        <Alert variant="destructive" className="mx-3 w-auto">
          <AlertCircle aria-hidden />
          <AlertTitle>{msg('workbench.historyLoadFailed', 'Version history could not load')}</AlertTitle>
          <AlertDescription>
            <Button type="button" variant="outline" size="sm" onClick={() => setRefreshVersion((value) => value + 1)}>
              {msg('common.retry', 'Retry')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}
      {hasUnsavedChanges ? (
        <>
          <Button
            type="button"
            variant="ghost"
            className="workbench-history-row is-current is-draft"
            aria-label={msg('workbench.draftRevision', 'Revision Draft version')}
            aria-current="true"
          >
            <span className="workbench-history-row-heading">
              <span className="workbench-history-version">{msg('workbench.draftVersion', 'Draft version')}</span>
            </span>
            <span className="workbench-history-row-meta">
              {formatHistoryTimestamp(currentDraft.created_at, locale)}
            </span>
            <span className="workbench-history-row-preview">
              <Code2 className="size-3.5" aria-hidden />
              {draftPreviewText.kind === 'variables' ? (
                <span className="workbench-history-variables">
                  {draftPreviewText.values.map((value) => (
                    <span key={value} title={`{{${value}}}`}>{`{{${value}}}`}</span>
                  ))}
                </span>
              ) : (
                <span className="workbench-history-message-preview">{draftPreviewText.value}</span>
              )}
            </span>
          </Button>
          <p className="workbench-history-draft-note">
            {msg('workbench.historyDraftNote', 'You are currently editing a draft version.')}{' '}
            <Button
              type="button"
              variant="link"
              className="h-auto rounded-none px-0 py-0 align-baseline text-[inherit] font-normal underline underline-offset-2 disabled:text-muted-foreground disabled:opacity-100"
              disabled={!canSave || isSaving}
              onClick={() => void onSave()}
            >
              {isSaving ? msg('workbench.saving', 'Saving') : msg('workbench.save', 'Save')}
            </Button>{' '}
            {msg('workbench.or', 'or')}{' '}
            <Button
              type="button"
              variant="link"
              className="h-auto rounded-none px-0 py-0 align-baseline text-[inherit] font-normal underline underline-offset-2 disabled:text-muted-foreground disabled:opacity-100"
              disabled={discardingDraft}
              onClick={() => void discardDraftFromHistory()}
            >
              {discardingDraft ? msg('workbench.discarding', 'discarding') : msg('workbench.discard', 'discard')}
            </Button>{' '}
            {msg('workbench.historyDraftRequirement', 'your changes before viewing or renaming a previous version.')}
          </p>
        </>
      ) : null}
      {revisions.length ? (
        <>
          <h3 className="workbench-history-day">
            {hasUnsavedChanges
              ? msg('workbench.previously', 'Previously')
              : historyDayLabel(revisions[0]?.created_at, locale, msg)}
          </h3>
          <div className="workbench-history-list subtle-scrollbar">
            {revisions.map((revision, index) => {
              const current = revision.id === currentRevisionId;
              const versionNumber = revisions.length - index;
              const previewText = historyRevisionPreview(revision, msg);
              const restoring = restoringRevisionId === revision.id;
              return (
                <Button
                  key={revision.id}
                  type="button"
                  variant="ghost"
                  className={clsx('workbench-history-row', current && 'is-current')}
                  aria-label={msg('workbench.revisionLabel', 'Revision v{version}', { version: versionNumber })}
                  aria-current={current ? 'true' : undefined}
                  disabled={!!restoringRevisionId && !restoring}
                  onClick={() => void restoreRevisionFromHistory(revision)}
                >
                  <span className="workbench-history-row-heading">
                    <span className="workbench-history-version">v{versionNumber}</span>
                    <span className="workbench-history-name" title={historyRevisionName(revision, msg)}>
                      {historyRevisionName(revision, msg)}
                    </span>
                  </span>
                  <span className="workbench-history-row-meta">
                    {formatHistoryTimestamp(revision.created_at, locale)}
                  </span>
                  <span className="workbench-history-row-preview">
                    {restoring ? (
                      <Loader2 className="size-3.5 animate-spin" aria-hidden />
                    ) : (
                      <Code2 className="size-3.5" aria-hidden />
                    )}
                    {previewText.kind === 'variables' ? (
                      <span className="workbench-history-variables">
                        {previewText.values.map((value) => (
                          <span key={value} title={`{{${value}}}`}>{`{{${value}}}`}</span>
                        ))}
                      </span>
                    ) : (
                      <span className="workbench-history-message-preview">{previewText.value}</span>
                    )}
                  </span>
                </Button>
              );
            })}
          </div>
        </>
      ) : !historyLoading && !historyError ? (
        <div className="workbench-history-empty">
          {hasUnsavedChanges
            ? msg('workbench.noPreviousVersions', 'No previous versions')
            : msg('workbench.noSavedVersions', 'No saved versions')}
        </div>
      ) : null}
    </div>
  );
}

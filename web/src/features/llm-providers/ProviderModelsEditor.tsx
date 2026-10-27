import { ChevronDown, Download, Loader2, Plus, X } from 'lucide-react';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../shared/ui/collapsible';
import { Field, FieldLabel, FieldDescription } from '../../shared/ui/field';
import { Checkbox } from '../../shared/ui/checkbox';
import { Input } from '../../shared/ui/input';

export type ProviderModelDraft = {
  key: string;
  id: string;
  name: string;
  contextWindow: string;
  maxOutputTokens: string;
  inputModalities: ('text' | 'image')[];
};

export function newModelDraft(id = ''): ProviderModelDraft {
  return { key: crypto.randomUUID(), id, name: '', contextWindow: '', maxOutputTokens: '', inputModalities: ['text'] };
}

export function parseContextWindow(text: string): number | undefined {
  if (!text.trim()) return undefined;
  const match = /^(\d+(?:\.\d+)?)\s*([km])?$/i.exec(text.trim());
  if (!match) return NaN;
  const value = Number(match[1]) * ({ k: 1000, m: 1000000 }[match[2]?.toLowerCase() ?? ''] ?? 1);
  return value > 0 ? value : NaN;
}

export function ProviderModelsEditor({
  models,
  discovering,
  onChange,
  onFetch,
}: {
  models: ProviderModelDraft[];
  discovering: boolean;
  onChange: (models: ProviderModelDraft[]) => void;
  onFetch: () => void;
}) {
  const { msg } = useI18n();
  const patch = (index: number, value: Partial<ProviderModelDraft>) =>
    onChange(models.map((model, at) => (at === index ? { ...model, ...value } : model)));
  return (
    <Field>
      <div className="flex items-center justify-between gap-2">
        <FieldLabel>{msg('llmModels.modelCatalog', 'Model catalog')}</FieldLabel>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          data-model-discovery-trigger
          aria-busy={discovering}
          disabled={discovering}
          onClick={onFetch}
        >
          {discovering ? <Loader2 className="animate-spin" aria-hidden /> : <Download aria-hidden />}
          {msg('llmModels.fetchModels', 'Get available models')}
        </Button>
      </div>
      <FieldDescription>
        {msg('llmModels.modelNameHint', 'Display name is optional. Leave it blank to use the model ID.')}
      </FieldDescription>
      <>
        <div className="space-y-4">
          {models.map((model, index) => (
            <Collapsible key={model.key} className="border-t-[0.5px] border-border pt-3 first:border-t-0 first:pt-0">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <div className="grid min-w-0 flex-1 gap-2 sm:grid-cols-2">
                  <Input
                    value={model.id}
                    onChange={(event) => patch(index, { id: event.target.value })}
                    placeholder="kimi-k3"
                    autoComplete="off"
                    aria-label={`${msg('llmModels.modelId', 'Model ID')} ${index + 1}`}
                    className="min-w-0 flex-1"
                  />
                  <Input
                    value={model.name}
                    onChange={(event) => patch(index, { name: event.target.value })}
                    placeholder={model.id || msg('llmModels.modelNameOptional', 'Display name (optional)')}
                    aria-label={`${msg('llmModels.modelName', 'Display name')} ${index + 1}`}
                    maxLength={100}
                    autoComplete="off"
                  />
                </div>
                <div className="flex shrink-0 items-center">
                  <CollapsibleTrigger
                    render={<Button type="button" variant="ghost" size="icon" />}
                    aria-label={`${msg('llmModels.modelParameters', 'Model parameters')} ${index + 1}`}
                    className="group"
                  >
                    <ChevronDown className="group-aria-expanded:rotate-180" aria-hidden />
                  </CollapsibleTrigger>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={msg('llmModels.removeModel', 'Remove model')}
                    onClick={() => onChange(models.filter((_, at) => at !== index))}
                  >
                    <X aria-hidden />
                  </Button>
                </div>
              </div>
              <CollapsibleContent>
                <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor={`model-context-window-${index}`} className="font-normal text-muted-foreground">
                      {msg('llmModels.contextWindow', 'Context window')}
                    </FieldLabel>
                    <Input
                      id={`model-context-window-${index}`}
                      value={model.contextWindow}
                      onChange={(event) => patch(index, { contextWindow: event.target.value })}
                      aria-label={`${msg('llmModels.contextWindow', 'Context window')} ${index + 1}`}
                      placeholder="256K"
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor={`model-output-${index}`} className="font-normal text-muted-foreground">
                      {msg('llmModels.maxOutputTokens', 'Maximum output tokens')}
                    </FieldLabel>
                    <Input
                      id={`model-output-${index}`}
                      value={model.maxOutputTokens}
                      onChange={(event) => patch(index, { maxOutputTokens: event.target.value })}
                      aria-label={`${msg('llmModels.maxOutputTokens', 'Maximum output tokens')} ${index + 1}`}
                      placeholder="32K"
                    />
                  </Field>
                </div>
                <Field className="mt-3">
                  <FieldLabel className="font-normal text-muted-foreground">
                    {msg('llmModels.inputModalities', 'Input types')}
                  </FieldLabel>
                  <div className="flex gap-5">
                    {(['text', 'image'] as const).map((modality) => (
                      <div key={modality} className="flex items-center gap-2">
                        <Checkbox
                          id={`model-${modality}-${index}`}
                          checked={model.inputModalities.includes(modality)}
                          disabled={model.inputModalities.length === 1 && model.inputModalities.includes(modality)}
                          onCheckedChange={(checked) =>
                            patch(index, {
                              inputModalities: (['text', 'image'] as const).filter((value) =>
                                value === modality ? checked : model.inputModalities.includes(value),
                              ),
                            })
                          }
                        />
                        <FieldLabel htmlFor={`model-${modality}-${index}`} className="font-normal">
                          {msg(`llmModels.modality.${modality}`, modality)}
                        </FieldLabel>
                      </div>
                    ))}
                  </div>
                </Field>
              </CollapsibleContent>
            </Collapsible>
          ))}
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="w-fit"
          onClick={() => onChange([...models, newModelDraft()])}
        >
          <Plus aria-hidden />
          {msg('llmModels.addModel', 'Add model')}
        </Button>
      </>
    </Field>
  );
}

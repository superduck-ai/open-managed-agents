import { useState, type FormEvent } from 'react';
import { Download, Loader2, Plus, X } from 'lucide-react';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../shared/ui/dialog';
import { Field, FieldDescription, FieldError, FieldLabel } from '../../shared/ui/field';
import { Input } from '../../shared/ui/input';
import { toast } from '../../shared/ui/sonner';
import type { LLMProvider, LLMProviderInput } from './api';
import { readableError } from './catalog';

type ProviderForm = {
  name: string;
  baseUrl: string;
  apiKey: string;
  modelIds: string[];
};

function providerForm(provider: LLMProvider | null): ProviderForm {
  return provider
    ? {
        name: provider.name,
        baseUrl: provider.base_url,
        apiKey: '',
        modelIds: provider.model_ids.length > 0 ? [...provider.model_ids] : [''],
      }
    : { name: '', baseUrl: '', apiKey: '', modelIds: [''] };
}

export function ProviderFormDialog({
  provider,
  error,
  isPending,
  onClose,
  onSubmit,
  onDiscoverModels,
}: {
  provider: LLMProvider | null;
  error: string;
  isPending: boolean;
  onClose: () => void;
  onSubmit: (input: LLMProviderInput) => Promise<void>;
  onDiscoverModels: (baseUrl: string, apiKey: string) => Promise<string[]>;
}) {
  const { msg } = useI18n();
  const [form, setForm] = useState<ProviderForm>(() => providerForm(provider));
  const [formError, setFormError] = useState('');
  const [discovering, setDiscovering] = useState(false);

  const fetchModels = async () => {
    const baseUrl = form.baseUrl.trim();
    const apiKey = form.apiKey.trim();
    if (!baseUrl || !apiKey) {
      setFormError(msg('llmModels.fetchModelsNeedCredentials', 'Enter a base URL and API key first.'));
      return;
    }
    setFormError('');
    setDiscovering(true);
    try {
      const modelIds = await onDiscoverModels(baseUrl, apiKey);
      if (modelIds.length === 0) {
        toast.info(msg('llmModels.discoverEmpty', 'The provider returned no models. You can add model IDs later.'), {
          id: 'llm-models-discover',
        });
        return;
      }
      setForm((current) => ({ ...current, modelIds: mergeModelIds(current.modelIds, modelIds) }));
    } catch (error) {
      toast.error(readableError(error, msg), { id: 'llm-models-discover' });
    } finally {
      setDiscovering(false);
    }
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const modelIds = form.modelIds.filter((modelId) => modelId !== '');
    if (!form.name.trim() || !form.baseUrl.trim() || (!provider && !form.apiKey.trim())) {
      setFormError(msg('llmModels.required', 'Name, base URL, and API key are required.'));
      return;
    }
    const input: LLMProviderInput = {
      name: form.name.trim(),
      base_url: form.baseUrl.trim(),
      model_ids: modelIds,
    };
    if (form.apiKey.trim()) {
      input.api_key = form.apiKey.trim();
    }
    await onSubmit(input);
  };

  return (
    <Dialog
      open
      onOpenChange={(nextOpen, details) => {
        if (!nextOpen && details.reason === 'focus-out') {
          details.cancel();
          return;
        }
        if (!nextOpen) onClose();
      }}
    >
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-xl">
        <form onSubmit={submit} className="grid gap-5">
          <DialogHeader>
            <DialogTitle>
              {provider ? msg('llmModels.editProvider', 'Edit provider') : msg('llmModels.addProvider', 'Add provider')}
            </DialogTitle>
            <DialogDescription>
              {msg(
                'llmModels.formDescription',
                'Use an Anthropic Messages-compatible HTTPS endpoint and its exact model IDs.',
              )}
            </DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="llm-provider-name">{msg('llmModels.name', 'Provider name')}</FieldLabel>
            <Input
              id="llm-provider-name"
              value={form.name}
              onChange={(event) => setForm((value) => ({ ...value, name: event.target.value }))}
              placeholder="DashScope"
              autoComplete="off"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="llm-provider-url">{msg('llmModels.baseUrl', 'Base URL')}</FieldLabel>
            <Input
              id="llm-provider-url"
              type="url"
              value={form.baseUrl}
              onChange={(event) => setForm((value) => ({ ...value, baseUrl: event.target.value }))}
              placeholder="https://example.com/anthropic"
              autoComplete="url"
            />
            <FieldDescription>
              {msg('llmModels.baseUrlHint', 'The server appends /v1/messages to this URL.')}
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="llm-provider-key">{msg('llmModels.apiKey', 'API key')}</FieldLabel>
            <Input
              id="llm-provider-key"
              type="password"
              value={form.apiKey}
              onChange={(event) => setForm((value) => ({ ...value, apiKey: event.target.value }))}
              placeholder={provider ? msg('llmModels.keepKey', 'Leave blank to keep the current key') : 'sk-...'}
              autoComplete="new-password"
            />
          </Field>
          <ModelIdFields
            modelIds={form.modelIds}
            discovering={discovering}
            showFetch={!provider || Boolean(form.apiKey.trim())}
            onChange={(modelIds) => setForm((value) => ({ ...value, modelIds }))}
            onFetch={() => void fetchModels()}
          />
          {formError || error ? <FieldError>{formError || error}</FieldError> : null}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              {msg('common.cancel', 'Cancel')}
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? msg('common.saving', 'Saving...') : msg('common.save', 'Save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ModelIdFields({
  modelIds,
  discovering,
  showFetch,
  onChange,
  onFetch,
}: {
  modelIds: string[];
  discovering: boolean;
  showFetch: boolean;
  onChange: (modelIds: string[]) => void;
  onFetch: () => void;
}) {
  const { msg } = useI18n();
  return (
    <Field>
      <div className="flex items-center justify-between gap-2">
        <FieldLabel>{msg('llmModels.modelIds', 'Model IDs')}</FieldLabel>
        {showFetch ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            aria-busy={discovering}
            disabled={discovering}
            className="active:not-aria-[haspopup]:translate-y-0 disabled:opacity-100"
            onClick={onFetch}
          >
            {discovering ? <Loader2 className="animate-spin" aria-hidden /> : <Download aria-hidden />}
            {msg('llmModels.fetchModels', 'Fetch model list')}
          </Button>
        ) : null}
      </div>
      <div className="space-y-2">
        {modelIds.map((modelId, index) => (
          <div key={index} className="flex gap-2">
            <Input
              value={modelId}
              onChange={(event) =>
                onChange(modelIds.map((item, itemIndex) => (itemIndex === index ? event.target.value : item)))
              }
              placeholder="kimi-k2.5"
              autoComplete="off"
              aria-label={`${msg('llmModels.modelId', 'Model ID')} ${index + 1}`}
            />
            <Button
              type="button"
              variant="outline"
              size="icon"
              aria-label={msg('llmModels.removeModel', 'Remove model')}
              disabled={modelIds.length === 1}
              onClick={() => onChange(modelIds.filter((_, itemIndex) => itemIndex !== index))}
            >
              <X aria-hidden />
            </Button>
          </div>
        ))}
      </div>
      <FieldDescription>
        {msg('llmModels.modelIdsHint', 'Optional. Fetch from the provider or add exact model IDs manually.')}
      </FieldDescription>
      <Button type="button" variant="outline" size="sm" className="w-fit" onClick={() => onChange([...modelIds, ''])}>
        <Plus aria-hidden /> {msg('llmModels.addModel', 'Add model')}
      </Button>
    </Field>
  );
}

function mergeModelIds(existing: string[], discovered: string[]) {
  const merged = [...existing, ...discovered].filter((modelId) => modelId !== '');
  return [...new Set(merged)];
}

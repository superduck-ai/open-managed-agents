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
import type { APIBackend, LLMProvider, LLMProviderInput } from './api';
import { ProviderBackendField } from './ProviderBackendField';
import { isValidSlug, readableError, slugFromName } from './catalog';

type ProviderForm = {
  apiBackend: APIBackend;
  name: string;
  slug: string;
  slugEdited: boolean;
  baseUrl: string;
  apiKey: string;
  modelIds: string[];
};

type MessageFormatter = ReturnType<typeof useI18n>['msg'];

function providerForm(provider: LLMProvider | null): ProviderForm {
  return provider
    ? {
        apiBackend: provider.api_backend,
        name: provider.name,
        slug: provider.slug,
        slugEdited: true,
        baseUrl: provider.base_url,
        apiKey: '',
        modelIds: provider.model_ids.length > 0 ? [...provider.model_ids] : [''],
      }
    : { apiBackend: 'messages', name: '', slug: '', slugEdited: false, baseUrl: '', apiKey: '', modelIds: [''] };
}

function providerFormInput(
  form: ProviderForm,
  provider: LLMProvider | null,
  msg: MessageFormatter,
): { input: LLMProviderInput } | { error: string } {
  if (!form.name.trim() || !form.baseUrl.trim() || (!provider && !form.apiKey.trim())) {
    return { error: msg('llmModels.required', 'Name, base URL, and API key are required.') };
  }
  const slug = form.slug.trim();
  if (slug && !isValidSlug(slug)) {
    return {
      error: msg(
        'llmModels.errorSlugInvalid',
        'Slug must contain 1 to 32 lowercase letters, digits, ".", "_" or "-" and start with a letter or digit.',
      ),
    };
  }
  const input: LLMProviderInput = {
    api_backend: form.apiBackend,
    name: form.name.trim(),
    base_url: form.baseUrl.trim(),
    model_ids: form.modelIds.filter((modelId) => modelId !== ''),
  };
  if (slug) {
    input.slug = slug;
  }
  if (form.apiKey.trim()) {
    input.api_key = form.apiKey.trim();
  }
  return { input };
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
  onDiscoverModels: (baseUrl: string, apiKey: string, apiBackend: APIBackend) => Promise<string[]>;
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
      const modelIds = await onDiscoverModels(baseUrl, apiKey, form.apiBackend);
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
    const result = providerFormInput(form, provider, msg);
    if ('error' in result) {
      setFormError(result.error);
      return;
    }
    await onSubmit(result.input);
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
              {msg('llmModels.formDescription', 'Configure a model API endpoint and its exact model IDs.')}
            </DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="llm-provider-name">{msg('llmModels.name', 'Provider name')}</FieldLabel>
            <Input
              id="llm-provider-name"
              value={form.name}
              onChange={(event) =>
                setForm((value) => ({
                  ...value,
                  name: event.target.value,
                  slug: value.slugEdited ? value.slug : slugFromName(event.target.value),
                }))
              }
              placeholder="DashScope"
              autoComplete="off"
            />
          </Field>
          <ProviderSlugField
            slug={form.slug}
            readOnly={Boolean(provider)}
            onChange={(slug) => setForm((value) => ({ ...value, slug, slugEdited: true }))}
          />
          <ProviderBackendField
            value={form.apiBackend}
            onChange={(apiBackend) => setForm((value) => ({ ...value, apiBackend }))}
          />
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
              {msg('llmModels.baseUrlHint', 'The server appends the API path to this URL.')}{' '}
              <code className="break-all">
                {form.baseUrl.replace(/\/+$/, '')}
                {form.apiBackend === 'messages' ? '/v1/messages' : '/v1/chat/completions'}
              </code>
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

function ProviderSlugField({
  slug,
  readOnly,
  onChange,
}: {
  slug: string;
  readOnly: boolean;
  onChange: (slug: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <Field>
      <FieldLabel htmlFor="llm-provider-slug">{msg('llmModels.slug', 'Slug')}</FieldLabel>
      <Input
        id="llm-provider-slug"
        value={slug}
        onChange={(event) => onChange(event.target.value)}
        placeholder="default"
        autoComplete="off"
        readOnly={readOnly}
        aria-readonly={readOnly}
        className="font-mono"
      />
      <FieldDescription>
        {readOnly
          ? msg('llmModels.slugReadOnlyHint', 'Models are referenced as {modelId}. The slug cannot be changed.', {
              modelId: `${slug}/<model-id>`,
            })
          : msg(
              'llmModels.slugHint',
              'Models are referenced as {modelId}. Leave blank to use default. The slug cannot be changed later.',
              { modelId: `${slug || 'default'}/<model-id>` },
            )}
      </FieldDescription>
    </Field>
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

import { useState, type FormEvent } from 'react';
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
import {
  ProviderModelsEditor,
  newModelDraft,
  parseContextWindow,
  type ProviderModelDraft,
} from './ProviderModelsEditor';
import { ProviderBackendField } from './ProviderBackendField';
import { isValidSlug, readableError } from './catalog';

type ProviderForm = {
  apiBackend: APIBackend;
  name: string;
  slug: string;
  baseUrl: string;
  apiKey: string;
  models: ProviderModelDraft[];
};

type MessageFormatter = ReturnType<typeof useI18n>['msg'];

function providerForm(provider: LLMProvider | null): ProviderForm {
  return provider
    ? {
        apiBackend: provider.api_backend,
        name: provider.name,
        slug: provider.slug,
        baseUrl: provider.base_url,
        apiKey: '',
        models:
          provider.model_ids.length > 0
            ? provider.model_ids.map((id) => ({
                ...newModelDraft(id),
                maxOutputTokens: String(provider.model_options?.[id]?.max_output_tokens ?? ''),
                inputModalities: provider.model_options?.[id]?.input_modalities ?? ['text'],
                contextWindow: String(provider.model_context_windows?.[id] ?? ''),
              }))
            : [newModelDraft()],
      }
    : {
        apiBackend: 'messages',
        name: '',
        slug: '',
        baseUrl: '',
        apiKey: '',
        models: [newModelDraft()],
      };
}

function providerFormInput(
  form: ProviderForm,
  provider: LLMProvider | null,
  organization: boolean,
  msg: MessageFormatter,
): { input: LLMProviderInput } | { error: string } {
  if (!form.name.trim() || !form.baseUrl.trim() || (!provider && !form.apiKey.trim())) {
    return { error: msg('llmModels.required', 'Display name, base URL, and API key are required.') };
  }
  const slug = form.slug.trim();
  if (!slug) return { error: msg('llmModels.providerIdRequired', 'Provider ID is required.') };
  if (!organization && slug === 'organization') {
    return {
      error: msg(
        'llmModels.errorSlugReserved',
        'Provider ID "organization" is reserved for the organization default provider.',
      ),
    };
  }
  if (!isValidSlug(slug)) {
    return {
      error: msg(
        'llmModels.errorSlugInvalid',
        'Provider ID must contain 1 to 32 lowercase letters, digits, ".", "_" or "-" and start with a letter or digit.',
      ),
    };
  }
  if (
    form.models.some(
      (model) =>
        model.id &&
        [model.contextWindow, model.maxOutputTokens].some(
          (value) => !Number.isSafeInteger(parseContextWindow(value) ?? 1),
        ),
    )
  ) {
    return {
      error: msg(
        'llmModels.contextWindowInvalid',
        'Token limits must be positive integers; K and M suffixes are supported.',
      ),
    };
  }
  const input: LLMProviderInput = {
    api_backend: form.apiBackend,
    name: form.name.trim(),
    slug,
    base_url: form.baseUrl.trim(),
    model_ids: form.models.map((model) => model.id).filter((id) => id !== ''),
    model_options: Object.fromEntries(
      form.models
        .filter((model) => model.id)
        .map((model) => [
          model.id,
          {
            input_modalities: model.inputModalities,
            ...(model.maxOutputTokens.trim() ? { max_output_tokens: parseContextWindow(model.maxOutputTokens)! } : {}),
          },
        ]),
    ),
    model_context_windows: Object.fromEntries(
      form.models
        .filter((model) => model.id && model.contextWindow.trim())
        .map((model) => [model.id, parseContextWindow(model.contextWindow)!]),
    ),
  };
  if (form.apiKey.trim()) {
    input.api_key = form.apiKey.trim();
  }
  return { input };
}

export function ProviderFormDialog({
  provider,
  organization = false,
  error,
  isPending,
  onClose,
  onSubmit,
  onDiscoverModels,
}: {
  provider: LLMProvider | null;
  organization?: boolean;
  error: string;
  isPending: boolean;
  onClose: () => void;
  onSubmit: (input: LLMProviderInput) => Promise<void>;
  onDiscoverModels: (
    baseUrl: string,
    apiKey: string,
    apiBackend: APIBackend,
    provider?: LLMProvider,
  ) => Promise<string[]>;
}) {
  const { msg } = useI18n();
  const [form, setForm] = useState<ProviderForm>(() => ({
    ...providerForm(provider),
    ...(organization ? { slug: 'organization' } : {}),
  }));
  const [formError, setFormError] = useState('');
  const [discovering, setDiscovering] = useState(false);

  const fetchModels = async () => {
    const baseUrl = form.baseUrl.trim();
    const apiKey = form.apiKey.trim();
    if (!baseUrl || (!apiKey && !provider)) {
      setFormError(msg('llmModels.fetchModelsNeedCredentials', 'Enter a base URL and API key first.'));
      return;
    }
    if (provider && !apiKey && (baseUrl !== provider.base_url || form.apiBackend !== provider.api_backend)) {
      setFormError(
        msg(
          'llmModels.fetchChangedEndpoint',
          'Enter an API key to fetch models after changing the endpoint or protocol.',
        ),
      );
      return;
    }
    setFormError('');
    setDiscovering(true);
    try {
      const modelIds = await onDiscoverModels(baseUrl, apiKey, form.apiBackend, provider ?? undefined);
      if (modelIds.length === 0) {
        toast.info(msg('llmModels.discoverEmpty', 'The provider returned no models. You can add model IDs later.'), {
          id: 'llm-models-discover',
        });
        return;
      }
      setForm((current) => ({ ...current, models: mergeModels(current.models, modelIds) }));
    } catch (error) {
      toast.error(readableError(error, msg), { id: 'llm-models-discover' });
    } finally {
      setDiscovering(false);
    }
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const result = providerFormInput(form, provider, organization, msg);
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
          {!provider ? (
            <ProviderIdField
              slug={form.slug}
              readOnly={organization}
              onChange={(slug) => setForm((value) => ({ ...value, slug }))}
            />
          ) : null}
          <Field>
            <FieldLabel htmlFor="llm-provider-name">{msg('llmModels.name', 'Display name')}</FieldLabel>
            <Input
              id="llm-provider-name"
              value={form.name}
              onChange={(event) => setForm((value) => ({ ...value, name: event.target.value }))}
              placeholder={msg('llmModels.name', 'Display name')}
              autoComplete="off"
            />
          </Field>
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
          <ProviderModelsEditor
            models={form.models}
            discovering={discovering}
            onChange={(models) => setForm((value) => ({ ...value, models }))}
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

function ProviderIdField({
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
      <FieldLabel htmlFor="llm-provider-slug">{msg('llmModels.slug', 'Provider ID')}</FieldLabel>
      <Input
        id="llm-provider-slug"
        value={slug}
        onChange={(event) => onChange(event.target.value)}
        placeholder="acme-gateway"
        autoComplete="off"
        readOnly={readOnly}
        aria-readonly={readOnly}
        className="font-mono read-only:bg-muted read-only:text-muted-foreground"
      />
      <FieldDescription>
        {readOnly
          ? msg('llmModels.slugReadOnlyHint', 'Used in {modelId}. Provider ID cannot be changed.', {
              modelId: `${slug}/<model-id>`,
            })
          : msg('llmModels.slugHint', 'Used in {modelId}. Provider ID cannot be changed after creation.', {
              modelId: `${slug || 'acme-gateway'}/<model-id>`,
            })}
      </FieldDescription>
    </Field>
  );
}

function mergeModels(existing: ProviderModelDraft[], discovered: string[]) {
  const models = existing.filter((model) => model.id !== '');
  const known = new Set(models.map((model) => model.id));
  return [...models, ...discovered.filter((id) => !known.has(id)).map((id) => newModelDraft(id))];
}

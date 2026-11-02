import type { useI18n } from '../../shared/i18n';
import type { APIBackend, LLMProvider, LLMProviderInput } from './api';
import { newModelDraft, parseContextWindow, type ProviderModelDraft } from './ProviderModelsEditor';
import { isValidSlug } from './catalog';

export type ProviderForm = {
  apiBackend: APIBackend;
  name: string;
  slug: string;
  baseUrl: string;
  apiKey: string;
  models: ProviderModelDraft[];
};

export type ProviderFieldErrors = Partial<Record<'name' | 'slug' | 'baseUrl' | 'apiKey' | 'supplier', string>>;

type MessageFormatter = ReturnType<typeof useI18n>['msg'];

export function providerForm(
  provider: Pick<LLMProvider, 'api_backend' | 'name' | 'slug' | 'base_url' | 'models'> | null,
): ProviderForm {
  return provider
    ? {
        apiBackend: provider.api_backend,
        name: provider.name,
        slug: provider.slug,
        baseUrl: provider.base_url,
        apiKey: '',
        models:
          provider.models.length > 0
            ? provider.models.map((model) => ({
                ...newModelDraft(model.id),
                imageInput: model.image_input,
                name: model.name ?? '',
                maxOutputTokens: String(model.max_output_tokens ?? ''),
                inputModalities: model.input_modalities ?? ['text'],
                contextWindow: String(model.context_window ?? ''),
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

function hasModelParameters(model: ProviderModelDraft): boolean {
  return Boolean(
    model.name.trim() ||
    model.contextWindow.trim() ||
    model.maxOutputTokens.trim() ||
    model.inputModalities.length !== 1 ||
    model.inputModalities[0] !== 'text',
  );
}

export function providerFormInput(
  form: ProviderForm,
  provider: LLMProvider | null,
  msg: MessageFormatter,
): { input: LLMProviderInput } | { error: string } | { errors: ProviderFieldErrors } {
  const errors = providerFieldErrors(form, provider, msg);
  if (Object.keys(errors).length) return { errors };
  const slug = form.slug.trim();
  if (form.models.some((model) => !model.id.trim() && hasModelParameters(model))) {
    return {
      error: msg('llmModels.modelIdRequired', 'Model ID is required when model parameters are configured.'),
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
    name: form.name.trim() || slug,
    slug,
    base_url: providerBaseURL(form.baseUrl),
    models: form.models
      .filter((model) => model.id.trim())
      .map((model) => ({
        id: model.id.trim(),
        ...(model.name.trim() ? { name: model.name.trim() } : {}),
        ...(model.contextWindow.trim() ? { context_window: parseContextWindow(model.contextWindow)! } : {}),
        ...(model.maxOutputTokens.trim() ? { max_output_tokens: parseContextWindow(model.maxOutputTokens)! } : {}),
        input_modalities: model.inputModalities,
        ...(model.imageInput ? { image_input: model.imageInput } : {}),
      })),
  };
  if (form.apiKey.trim()) {
    input.api_key = form.apiKey.trim();
  }
  return { input };
}

export function providerFieldErrors(
  form: ProviderForm,
  provider: LLMProvider | null,
  msg: MessageFormatter,
): ProviderFieldErrors {
  const errors: ProviderFieldErrors = {};
  if (!form.baseUrl.trim()) errors.baseUrl = msg('llmModels.baseUrlRequired', 'API URL is required.');
  if (!provider && !form.apiKey.trim()) errors.apiKey = msg('llmModels.apiKeyRequired', 'API key is required.');
  const slug = form.slug.trim();
  if (!slug) errors.slug = msg('llmModels.providerIdRequired', 'Provider ID is required.');
  else if (!provider && slug === 'organization')
    errors.slug = msg(
      'llmModels.errorSlugReserved',
      'Provider ID "organization" is reserved for the organization default provider.',
    );
  else if (!isValidSlug(slug))
    errors.slug = msg(
      'llmModels.errorSlugInvalid',
      'Provider ID must contain 1 to 32 lowercase letters, digits, ".", "_" or "-" and start with a letter or digit.',
    );
  return errors;
}

export function providerBaseURL(value: string): string {
  return value
    .trim()
    .replace(/\/+$/, '')
    .replace(/\/v1(?:\/messages|\/chat\/completions)?$/, '');
}

export function providerRequestURL(value: string, backend: APIBackend): string {
  const base = providerBaseURL(value);
  return base ? `${base}${backend === 'messages' ? '/v1/messages' : '/v1/chat/completions'}` : '';
}

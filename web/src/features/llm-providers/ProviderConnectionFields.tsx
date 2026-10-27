import type { Dispatch, SetStateAction } from 'react';
import { useI18n } from '../../shared/i18n';
import { Field, FieldDescription, FieldLabel, FieldError } from '../../shared/ui/field';
import { Input } from '../../shared/ui/input';
import type { LLMProvider } from './api';
import { providerRequestURL, type ProviderForm, type ProviderFieldErrors } from './provider-form';
import { ProviderBackendField } from './ProviderBackendField';

export function ProviderConnectionFields({
  form,
  setForm,
  provider,
  errors,
}: {
  form: ProviderForm;
  setForm: Dispatch<SetStateAction<ProviderForm>>;
  provider: LLMProvider | null;
  errors: ProviderFieldErrors;
}) {
  const { msg } = useI18n();
  return (
    <>
      {!provider ? (
        <ProviderIdField
          error={errors.slug}
          slug={form.slug}
          onChange={(slug) => setForm((value) => ({ ...value, slug }))}
        />
      ) : null}
      <Field>
        <FieldLabel htmlFor="llm-provider-name">{msg('llmModels.name', 'Display name')}</FieldLabel>
        <Input
          id="llm-provider-name"
          aria-invalid={Boolean(errors.name)}
          aria-describedby={errors.name ? 'llm-provider-name-error' : undefined}
          value={form.name || form.slug}
          onChange={(event) => setForm((value) => ({ ...value, name: event.target.value }))}
          placeholder={msg('llmModels.optionalName', 'Display name (optional)')}
          autoComplete="off"
        />
        {errors.name ? <FieldError id="llm-provider-name-error">{errors.name}</FieldError> : null}
      </Field>
      <Field>
        <FieldLabel htmlFor="llm-provider-url">{msg('llmModels.baseUrl', 'Base URL')}</FieldLabel>
        <Input
          id="llm-provider-url"
          aria-invalid={Boolean(errors.baseUrl)}
          aria-describedby={errors.baseUrl ? 'llm-provider-url-error' : undefined}
          type="url"
          value={form.baseUrl}
          onChange={(event) => {
            setForm((value) => ({ ...value, baseUrl: event.target.value }));
          }}
          placeholder="https://api.deepseek.com/anthropic"
          autoComplete="url"
        />
        <FieldDescription>
          {msg('llmModels.baseUrlHint', 'Request URL:')}{' '}
          <code className="break-all">{providerRequestURL(form.baseUrl, form.apiBackend) || '—'}</code>
        </FieldDescription>
        {errors.baseUrl ? <FieldError id="llm-provider-url-error">{errors.baseUrl}</FieldError> : null}
      </Field>
      <ProviderBackendField
        value={form.apiBackend}
        onChange={(apiBackend) => {
          setForm((value) => ({ ...value, apiBackend }));
        }}
      />
    </>
  );
}

export function ProviderAPIKeyField({
  form,
  setForm,
  provider,
  errors,
}: {
  form: ProviderForm;
  setForm: Dispatch<SetStateAction<ProviderForm>>;
  provider: LLMProvider | null;
  errors: ProviderFieldErrors;
}) {
  const { msg } = useI18n();
  return (
    <Field>
      <FieldLabel htmlFor="llm-provider-key">{msg('llmModels.apiKey', 'API key')}</FieldLabel>
      <Input
        id="llm-provider-key"
        aria-invalid={Boolean(errors.apiKey)}
        aria-describedby={errors.apiKey ? 'llm-provider-key-error' : undefined}
        type="password"
        value={form.apiKey}
        onChange={(event) => setForm((value) => ({ ...value, apiKey: event.target.value }))}
        placeholder={provider ? msg('llmModels.keepKey', 'Leave blank to keep the current key') : undefined}
        autoComplete="new-password"
      />
      {errors.apiKey ? <FieldError id="llm-provider-key-error">{errors.apiKey}</FieldError> : null}
    </Field>
  );
}

function ProviderIdField({
  slug,
  error,
  onChange,
}: {
  slug: string;
  error?: string;
  onChange: (slug: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <Field>
      <FieldLabel htmlFor="llm-provider-slug">{msg('llmModels.slug', 'Provider ID')}</FieldLabel>
      <Input
        id="llm-provider-slug"
        aria-invalid={Boolean(error)}
        aria-describedby={error ? 'llm-provider-slug-error' : undefined}
        value={slug}
        onChange={(event) => onChange(event.target.value)}
        placeholder="deepseek"
        autoComplete="off"
        className="font-mono"
      />
      <FieldDescription>
        {msg('llmModels.slugHint', 'Used in {modelId}. Provider ID cannot be changed after creation.', {
          modelId: `${slug || 'deepseek'}/<model-id>`,
        })}
      </FieldDescription>
      {error ? <FieldError id="llm-provider-slug-error">{error}</FieldError> : null}
    </Field>
  );
}

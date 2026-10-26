import type { Dispatch, SetStateAction } from 'react';
import { useI18n } from '../../shared/i18n';
import { Field, FieldDescription, FieldLabel, FieldError } from '../../shared/ui/field';
import { Input } from '../../shared/ui/input';
import type { LLMProvider } from './api';
import type { ProviderForm, ProviderFieldErrors } from './provider-form';
import { ProviderBackendField } from './ProviderBackendField';

export function ProviderConnectionFields({
  form,
  setForm,
  provider,
  organization,
  errors,
}: {
  form: ProviderForm;
  setForm: Dispatch<SetStateAction<ProviderForm>>;
  provider: LLMProvider | null;
  organization: boolean;
  errors: ProviderFieldErrors;
}) {
  const { msg } = useI18n();
  return (
    <>
      {!provider ? (
        <ProviderIdField
          error={errors.slug}
          slug={form.slug}
          readOnly={organization}
          onChange={(slug) => setForm((value) => ({ ...value, slug }))}
        />
      ) : null}
      <Field>
        <FieldLabel htmlFor="llm-provider-name">{msg('llmModels.name', 'Display name')}</FieldLabel>
        <Input
          id="llm-provider-name"
          aria-invalid={Boolean(errors.name)}
          aria-describedby={errors.name ? 'llm-provider-name-error' : undefined}
          value={form.name}
          onChange={(event) => setForm((value) => ({ ...value, name: event.target.value }))}
          placeholder={msg('llmModels.name', 'Display name')}
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
          placeholder="https://example.com/anthropic"
          autoComplete="url"
        />
        <FieldDescription>
          {msg('llmModels.baseUrlHint', 'Request URL:')}{' '}
          <code className="break-all">
            {form.baseUrl.replace(/\/+$/, '')}
            {form.apiBackend === 'messages' ? '/v1/messages' : '/v1/chat/completions'}
          </code>
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
  readOnly,
  error,
  onChange,
}: {
  slug: string;
  readOnly: boolean;
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
      {error ? <FieldError id="llm-provider-slug-error">{error}</FieldError> : null}
    </Field>
  );
}

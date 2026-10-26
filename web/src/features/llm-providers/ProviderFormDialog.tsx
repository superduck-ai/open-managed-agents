import { useEffect, useRef, useState, type FormEvent, type SetStateAction } from 'react';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../shared/ui/dialog';
import { FieldError } from '../../shared/ui/field';
import { toast } from '../../shared/ui/sonner';
import type { APIBackend, LLMProvider, LLMProviderInput, ProviderPresetCatalog, LLMProviderPreset } from './api';
import { ProviderModelsEditor, newModelDraft, type ProviderModelDraft } from './ProviderModelsEditor';
import { ProviderConnectionFields, ProviderAPIKeyField } from './ProviderConnectionFields';
import { readableError } from './catalog';

import { providerForm, providerFormInput, type ProviderForm, type ProviderFieldErrors } from './provider-form';
import { ProviderPresetPicker } from './ProviderPresetPicker';
import { ModelDiscoveryDialog } from './ModelDiscoveryDialog';
import { Tabs, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { ProviderAdvancedSettings } from './ProviderAdvancedSettings';

type ProviderDraft = {
  form: ProviderForm;
  presetId: string;
  advanced: boolean;
};

export function ProviderFormDialog({
  provider,
  organization = false,
  error,
  isPending,
  onClose,
  onSubmit,
  onDiscoverModels,
  presetCatalog,
  presetError,
  onReloadPresets,
}: {
  presetCatalog?: ProviderPresetCatalog;
  presetError?: string;
  onReloadPresets?: () => void;
  provider: LLMProvider | null;
  organization?: boolean;
  error: string;
  isPending: boolean;
  onClose: () => void;
  onSubmit: (input: LLMProviderInput) => Promise<boolean>;
  onDiscoverModels: (
    baseUrl: string,
    apiKey: string,
    apiBackend: APIBackend,
    provider?: LLMProvider,
  ) => Promise<string[]>;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(true);
  const [mode, setMode] = useState(onReloadPresets ? 'preset' : 'custom');
  const [presetId, setPresetId] = useState('');
  const [advanced, setAdvanced] = useState(false);
  const [candidates, setCandidates] = useState<string[] | null>(null);
  const discoveryVersion = useRef(0);
  const drafts = useRef<Record<string, ProviderDraft>>({});
  const busy = isPending;
  const presetMode = mode === 'preset' && !provider;

  const [form, setForm] = useState<ProviderForm>(() => ({
    ...providerForm(provider),
    ...(organization ? { slug: 'organization' } : {}),
  }));
  const [formError, setFormError] = useState('');
  const [fieldErrors, setFieldErrors] = useState<ProviderFieldErrors>({});
  const [validationAttempt, setValidationAttempt] = useState(0);
  const formRef = useRef<HTMLFormElement>(null);
  useEffect(() => {
    if (validationAttempt) formRef.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
  }, [validationAttempt]);
  const errorRef = useRef<HTMLDivElement>(null);
  const message = formError || error;
  useEffect(() => {
    if (message) errorRef.current?.focus();
  }, [message]);
  const [discovering, setDiscovering] = useState(false);

  const fetchModels = async () => {
    const version = ++discoveryVersion.current;
    const baseUrl = form.baseUrl.trim();
    const apiKey = form.apiKey.trim();
    setFormError('');
    if (!baseUrl || (!apiKey && !provider)) {
      setFieldErrors({
        ...(!baseUrl ? { baseUrl: msg('llmModels.baseUrlRequired', 'API URL is required.') } : {}),
        ...(!apiKey && !provider ? { apiKey: msg('llmModels.apiKeyRequired', 'API key is required.') } : {}),
      });
      if (!baseUrl) setAdvanced(true);
      setValidationAttempt((value) => value + 1);
      return;
    }
    if (provider && !apiKey && (baseUrl !== provider.base_url || form.apiBackend !== provider.api_backend)) {
      setFieldErrors({
        apiKey: msg(
          'llmModels.fetchChangedEndpoint',
          'Enter an API key to fetch models after changing the endpoint or protocol.',
        ),
      });
      setValidationAttempt((value) => value + 1);
      return;
    }
    setFormError('');
    setDiscovering(true);
    try {
      const modelIds = await onDiscoverModels(baseUrl, apiKey, form.apiBackend, provider ?? undefined);
      if (version !== discoveryVersion.current) return;
      if (modelIds.length === 0) {
        toast.info(msg('llmModels.discoverEmpty', 'The provider returned no models. You can add model IDs later.'), {
          id: 'llm-models-discover',
        });
        return;
      }
      setCandidates(modelIds);
    } catch (error) {
      if (version === discoveryVersion.current) toast.error(readableError(error, msg), { id: 'llm-models-discover' });
    } finally {
      if (version === discoveryVersion.current) setDiscovering(false);
    }
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!open || busy || discovering) return;
    if (presetMode && !presetId) {
      setFieldErrors({ supplier: msg('llmModels.supplierRequired', 'Select a model supplier.') });
      setValidationAttempt((value) => value + 1);
      return;
    }
    const result = providerFormInput(form, provider, organization, msg);
    if ('errors' in result) {
      setFieldErrors(result.errors);
      if (result.errors.name || result.errors.baseUrl || result.errors.slug) setAdvanced(true);
      setFormError('');
      setValidationAttempt((value) => value + 1);
      return;
    }
    setFieldErrors({});
    setFormError('');
    if ('error' in result) {
      setFormError(result.error);
      return;
    }
    if (await onSubmit(result.input)) setOpen(false);
  };

  const updateConnectionForm = (update: SetStateAction<ProviderForm>) => {
    const next = typeof update === 'function' ? update(form) : update;
    setFieldErrors((current) => {
      const remaining = { ...current };
      for (const key of ['name', 'slug', 'baseUrl', 'apiKey'] as const) {
        if (form[key] !== next[key]) delete remaining[key];
      }
      return remaining;
    });
    setForm(next);
  };
  const apiKeyField = (
    <ProviderAPIKeyField form={form} setForm={updateConnectionForm} provider={provider} errors={fieldErrors} />
  );

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen, details) => {
        if (!nextOpen && (details.reason === 'focus-out' || busy)) {
          details.cancel();
          return;
        }
        if (!nextOpen) discoveryVersion.current++;
        setOpen(nextOpen);
      }}
      onOpenChangeComplete={(nextOpen) => !nextOpen && onClose()}
    >
      <DialogContent
        closeButtonDisabled={busy}
        className="grid-rows-[auto_minmax(0,1fr)] max-h-[calc(100dvh-2rem)] gap-0 overflow-hidden p-0 sm:max-w-xl"
      >
        <DialogHeader className="px-4 py-4">
          <DialogTitle>
            {provider ? msg('llmModels.editProvider', 'Edit provider') : msg('llmModels.addProvider', 'Add provider')}
          </DialogTitle>
        </DialogHeader>
        <form ref={formRef} noValidate onSubmit={submit} className="grid min-h-0 grid-rows-[minmax(0,1fr)_auto]">
          <div className="subtle-scrollbar-auto min-h-0 space-y-5 overflow-y-auto px-4 pb-4">
            {message ? (
              <FieldError
                ref={errorRef}
                tabIndex={-1}
                className="min-w-0 rounded-md border border-destructive bg-destructive/10 p-3 outline-none [overflow-wrap:anywhere]"
              >
                {message}
              </FieldError>
            ) : null}
            <fieldset disabled={busy || discovering} className="grid min-w-0 gap-5">
              <ProviderCreationFields
                provider={provider}
                mode={mode}
                catalog={presetCatalog}
                error={fieldErrors.supplier || presetError}
                presetId={presetId}
                onReload={onReloadPresets}
                onModeChange={(value) => {
                  drafts.current[mode] = { form, presetId, advanced };
                  const next = drafts.current[value];
                  setMode(value);
                  setPresetId(next?.presetId ?? '');
                  setAdvanced(next?.advanced ?? false);
                  setForm(next?.form ?? { ...providerForm(null), ...(organization ? { slug: 'organization' } : {}) });
                  setFormError('');
                  setFieldErrors({});
                }}
                onPresetChange={(preset) => {
                  setPresetId(preset.id);
                  setFormError('');
                  setFieldErrors({});
                  setForm({
                    ...providerForm({ ...preset, slug: organization ? 'organization' : preset.id }),
                    apiKey: '',
                  });
                }}
              />
              {mode !== 'preset' || provider || presetId ? (
                <>
                  {presetMode ? apiKeyField : null}
                  <ProviderAdvancedSettings enabled={presetMode} open={advanced} onOpenChange={setAdvanced}>
                    <ProviderConnectionFields
                      form={form}
                      errors={fieldErrors}
                      setForm={updateConnectionForm}
                      provider={provider}
                      organization={organization}
                    />
                    {!presetMode ? apiKeyField : null}
                    <ProviderModelsEditor
                      models={form.models}
                      discovering={discovering}
                      onChange={(models) => setForm((value) => ({ ...value, models }))}
                      onFetch={() => void fetchModels()}
                    />
                  </ProviderAdvancedSettings>
                </>
              ) : null}
            </fieldset>
          </div>
          <div className="px-4 py-4">
            <DialogFooter>
              <Button type="button" variant="outline" disabled={busy} onClick={() => setOpen(false)}>
                {msg('common.cancel', 'Cancel')}
              </Button>
              <Button type="submit" disabled={busy || discovering}>
                {busy ? msg('common.saving', 'Saving...') : msg('common.save', 'Save')}
              </Button>
            </DialogFooter>
          </div>
        </form>
      </DialogContent>
      {candidates ? (
        <ModelDiscoveryDialog
          models={candidates}
          onClose={() => setCandidates(null)}
          onAccept={(ids) => {
            setForm((current) => ({ ...current, models: mergeModels(current.models, ids) }));
            setCandidates(null);
          }}
        />
      ) : null}
    </Dialog>
  );
}

function mergeModels(existing: ProviderModelDraft[], discovered: string[]) {
  const models = existing.filter((model) => model.id !== '');
  const known = new Set(models.map((model) => model.id));
  return [...models, ...discovered.filter((id) => !known.has(id)).map((id) => newModelDraft(id))];
}

function ProviderCreationFields({
  provider,
  mode,
  catalog,
  error,
  presetId,
  onReload,
  onModeChange,
  onPresetChange,
}: {
  provider: LLMProvider | null;
  mode: string;
  catalog?: ProviderPresetCatalog;
  error?: string;
  presetId: string;
  onReload?: () => void;
  onModeChange: (mode: string) => void;
  onPresetChange: (preset: LLMProviderPreset) => void;
}) {
  const { msg } = useI18n();
  if (provider) return null;
  return (
    <>
      <Tabs value={mode} onValueChange={(value) => onModeChange(String(value))}>
        <TabsList className="w-full">
          <TabsTrigger value="preset">{msg('llmModels.supplier', 'Model supplier')}</TabsTrigger>
          <TabsTrigger value="custom">{msg('llmModels.customApi', 'Custom model API')}</TabsTrigger>
        </TabsList>
      </Tabs>
      {mode === 'preset' ? (
        <ProviderPresetPicker
          catalog={catalog}
          error={error}
          value={presetId}
          onReload={onReload}
          onChange={onPresetChange}
        />
      ) : null}
    </>
  );
}

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Download, Loader2 } from 'lucide-react';
import { useState } from 'react';
import { useAuth } from '../../shared/auth/context';
import { toast } from '../../shared/ui/sonner';
import { useI18n } from '../../shared/i18n';
import { canManageLLMProviders } from '../../shared/permissions/llm-providers';
import { useWorkspace } from '../../shared/workspaces/context';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '../../shared/ui/alert-dialog';
import { Alert, AlertDescription } from '../../shared/ui/alert';
import { Button, ButtonLink } from '../../shared/ui/button';
import { ModelRows } from '../llm-providers/ModelCatalog';
import type { CatalogModel } from '../llm-providers/catalog';
import { Card, CardContent } from '../../shared/ui/card';
import { Field, FieldLabel } from '../../shared/ui/field';
import { Input } from '../../shared/ui/input';
import { Skeleton } from '../../shared/ui/skeleton';
import {
  deleteOrganizationLLMProvider,
  getOrganizationLLMConsole,
  getOrganizationLLMProvider,
  previewProviderModels,
  syncOrganizationLLMProvider,
  upsertOrganizationLLMProvider,
  type LLMProvider,
  type LLMProviderInput,
} from '../llm-providers/api';
import { readableError } from '../llm-providers/catalog';

const defaultOrganizationTenantName = 'AI网关';

export function OrganizationLLMProviderCard() {
  const { msg } = useI18n();
  const { account, csrfToken } = useAuth();
  const { orgUuid } = useWorkspace();
  const queryClient = useQueryClient();
  const canManage = canManageLLMProviders(account, orgUuid);
  const [replacing, setReplacing] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [tenantName, setTenantName] = useState(defaultOrganizationTenantName);
  const [gatewayDraft, setGatewayDraft] = useState<string | null>(null);
  const [apiKey, setApiKey] = useState('');
  const [modelIds, setModelIds] = useState<string[]>(['']);
  const [formError, setFormError] = useState<string | null>(null);

  const providerQuery = useQuery({
    queryKey: ['console', 'llm-providers', 'organization', orgUuid],
    queryFn: () => getOrganizationLLMProvider(orgUuid ?? ''),
    enabled: Boolean(orgUuid),
    retry: false,
  });
  const consoleQuery = useQuery({
    queryKey: ['console', 'llm-console', orgUuid],
    queryFn: () => getOrganizationLLMConsole(orgUuid ?? ''),
    enabled: Boolean(orgUuid) && canManage,
    retry: false,
  });
  const provider = providerQuery.data ?? null;
  const consoleURL = configuredConsoleURL(consoleQuery.data?.console_url);
  const gateway = gatewayDraft ?? gatewayURL(consoleQuery.data?.base_url);

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['console', 'llm-providers', 'organization', orgUuid] });
  };
  const saveMutation = useMutation({
    mutationFn: async () => {
      if (!orgUuid) {
        return;
      }
      const creating = provider == null;
      const input = organizationProviderInput(provider, tenantName, gateway, apiKey, modelIds);
      await upsertOrganizationLLMProvider(orgUuid, input, csrfToken);
      if (input.model_ids.length > 0) {
        return;
      }
      try {
        await syncOrganizationLLMProvider(orgUuid, csrfToken);
      } catch (error) {
        if (creating) {
          try {
            await deleteOrganizationLLMProvider(orgUuid, csrfToken);
          } catch {
            throw error;
          }
        }
        throw error;
      }
    },
    onSuccess: async () => {
      setTenantName(defaultOrganizationTenantName);
      setGatewayDraft(null);
      setApiKey('');
      setModelIds(['']);
      setReplacing(false);
      setFormError(null);
      await invalidate();
    },
    onError: async (error) => {
      setFormError(readableError(error, msg));
      await invalidate();
    },
  });
  const deleteMutation = useMutation({
    mutationFn: async () => {
      if (!orgUuid) {
        return;
      }
      await deleteOrganizationLLMProvider(orgUuid, csrfToken);
    },
    onSuccess: async () => {
      setConfirmRemove(false);
      setFormError(null);
      await invalidate();
    },
    onError: (error) => setFormError(readableError(error, msg)),
  });

  const title = msg('settings.organization.llmTitle', 'Organization models');
  if (!orgUuid) {
    return null;
  }
  if (providerQuery.isLoading) {
    return (
      <Card>
        <CardContent>
          <Skeleton className="h-10 w-[428px] max-w-full" />
        </CardContent>
      </Card>
    );
  }
  if (providerQuery.isError) {
    return (
      <Card>
        <CardContent className="space-y-2">
          <h2 className="text-xl font-semibold tracking-normal text-foreground">{title}</h2>
          <Alert variant="destructive">
            <AlertDescription>{readableError(providerQuery.error, msg)}</AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    );
  }

  return (
    <OrganizationLLMProviderSheet
      title={title}
      canManage={canManage}
      provider={provider}
      consoleURL={consoleURL}
      replacing={replacing}
      confirmRemove={confirmRemove}
      tenantName={tenantName}
      gateway={gateway}
      apiKey={apiKey}
      modelIds={modelIds}
      formError={formError}
      saving={saveMutation.isPending}
      removing={deleteMutation.isPending}
      onReplace={() => setReplacing(true)}
      onAskRemove={() => setConfirmRemove(true)}
      onConfirmRemoveChange={setConfirmRemove}
      onTenantName={setTenantName}
      onGateway={setGatewayDraft}
      onApiKey={setApiKey}
      onModelIds={setModelIds}
      onFormError={setFormError}
      onSave={() => saveMutation.mutate()}
      onCancelReplace={() => {
        setReplacing(false);
        setApiKey('');
        setFormError(null);
      }}
      onRemove={() => deleteMutation.mutate()}
    />
  );
}

function organizationProviderInput(
  provider: LLMProvider | null,
  providerName: string,
  gateway: string,
  apiKey: string,
  modelIds: string[],
): LLMProviderInput {
  const key = apiKey.trim();
  if (provider) {
    return {
      name: provider.name,
      base_url: provider.base_url,
      api_key: key,
      model_ids: provider.model_ids,
    };
  }
  return {
    name: providerName.trim(),
    base_url: gateway.trim(),
    api_key: key,
    model_ids: modelIds.filter((modelId) => modelId !== ''),
  };
}

function OrganizationLLMProviderSheet({
  title,
  canManage,
  provider,
  consoleURL,
  replacing,
  confirmRemove,
  tenantName,
  gateway,
  apiKey,
  modelIds,
  formError,
  saving,
  removing,
  onReplace,
  onAskRemove,
  onConfirmRemoveChange,
  onTenantName,
  onGateway,
  onApiKey,
  onModelIds,
  onFormError,
  onSave,
  onCancelReplace,
  onRemove,
}: {
  title: string;
  canManage: boolean;
  provider: LLMProvider | null;
  consoleURL: string;
  replacing: boolean;
  confirmRemove: boolean;
  tenantName: string;
  gateway: string;
  apiKey: string;
  modelIds: string[];
  formError: string | null;
  saving: boolean;
  removing: boolean;
  onReplace: () => void;
  onAskRemove: () => void;
  onConfirmRemoveChange: (open: boolean) => void;
  onTenantName: (value: string) => void;
  onGateway: (value: string) => void;
  onApiKey: (value: string) => void;
  onModelIds: (modelIds: string[]) => void;
  onFormError: (message: string | null) => void;
  onSave: () => void;
  onCancelReplace: () => void;
  onRemove: () => void;
}) {
  const { msg } = useI18n();
  const showForm = canManage && (!provider || replacing);
  const canSave = provider
    ? apiKey.trim() !== ''
    : tenantName.trim() !== '' && gateway.trim() !== '' && apiKey.trim() !== '';

  return (
    <Card>
      <CardContent className="space-y-4">
        <div>
          <h2 className="text-xl font-semibold tracking-normal text-foreground">{title}</h2>
          {canManage ? (
            <p className="mt-2 text-sm leading-6 text-muted-foreground">
              {msg(
                'settings.organization.llmDescription',
                'Members of this organization share this provider. A model ID already set in a workspace still wins there.',
              )}
            </p>
          ) : null}
        </div>
        {provider ? <ProviderStatus provider={provider} /> : null}
        {!provider && !canManage ? (
          <p className="text-sm leading-6 text-muted-foreground">
            {msg(
              'settings.organization.llmUnavailable',
              'This organization does not have a shared model yet. Ask an organization administrator.',
            )}
          </p>
        ) : null}
        {canManage && !provider ? <ConsoleSteps consoleURL={consoleURL} /> : null}
        {canManage && provider && !replacing ? (
          <ConnectedActions consoleURL={consoleURL} onReplace={onReplace} onAskRemove={onAskRemove} />
        ) : null}
        {showForm ? (
          <BindForm
            provider={provider}
            tenantName={tenantName}
            gateway={gateway}
            apiKey={apiKey}
            modelIds={modelIds}
            canSave={canSave && !saving}
            saving={saving}
            onTenantName={onTenantName}
            onGateway={onGateway}
            onApiKey={onApiKey}
            onModelIds={onModelIds}
            onFormError={onFormError}
            onSave={onSave}
            onCancel={provider ? onCancelReplace : undefined}
          />
        ) : null}
        {formError ? (
          <Alert variant="destructive">
            <AlertDescription>{formError}</AlertDescription>
          </Alert>
        ) : null}
        <RemoveOrganizationModelDialog
          open={confirmRemove}
          removing={removing}
          onOpenChange={onConfirmRemoveChange}
          onRemove={onRemove}
        />
      </CardContent>
    </Card>
  );
}

function ConnectedActions({
  consoleURL,
  onReplace,
  onAskRemove,
}: {
  consoleURL: string;
  onReplace: () => void;
  onAskRemove: () => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="flex flex-wrap items-center gap-2">
      <OpenConsole consoleURL={consoleURL} />
      <Button type="button" variant="outline" onClick={onReplace}>
        {msg('settings.organization.llmReplace', 'Replace key')}
      </Button>
      <Button type="button" variant="outline" onClick={onAskRemove}>
        {msg('settings.organization.llmRemove', 'Remove')}
      </Button>
    </div>
  );
}

function RemoveOrganizationModelDialog({
  open,
  removing,
  onOpenChange,
  onRemove,
}: {
  open: boolean;
  removing: boolean;
  onOpenChange: (open: boolean) => void;
  onRemove: () => void;
}) {
  const { msg } = useI18n();
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {msg('settings.organization.llmRemoveTitle', 'Remove the organization model?')}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {msg(
              'settings.organization.llmRemoveDescription',
              'Workspaces keep their own providers. Models that came only from the organization default stop resolving.',
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={removing}>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={removing} onClick={onRemove}>
            {msg('settings.organization.llmRemoveConfirm', 'Remove organization model')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function ProviderStatus({ provider }: { provider: LLMProvider }) {
  const { msg } = useI18n();
  return (
    <div className="space-y-1">
      <p className="text-sm font-medium text-foreground">{provider.name}</p>
      <p className="text-sm text-success">{msg('settings.organization.llmConnected', 'Connected')}</p>
      <p className="font-mono text-[13px] text-foreground">
        {msg('settings.organization.llmLast4', 'Key ending in {last4}', { last4: provider.api_key_last4 })}
      </p>
    </div>
  );
}

function ConsoleSteps({ consoleURL }: { consoleURL: string }) {
  const { msg } = useI18n();
  return (
    <div className="max-w-[428px] space-y-3">
      <p className="text-sm text-muted-foreground">
        {consoleURL
          ? consoleHost(consoleURL)
          : msg('settings.organization.llmConsoleMissing', 'The company console address is not configured.')}
      </p>
      <ol className="list-decimal space-y-2 pl-5 text-sm leading-6 text-muted-foreground">
        <li>
          <span className="font-medium text-foreground">
            {msg('settings.organization.llmStepTenant', 'Choose or create a tenant')}
          </span>{' '}
          {msg(
            'settings.organization.llmStepTenantBody',
            'In the company console, filter by product line, then pick a tenant or create one.',
          )}
        </li>
        <li>
          <span className="font-medium text-foreground">
            {msg('settings.organization.llmStepKey', 'Choose or create an API key')}
          </span>{' '}
          {msg(
            'settings.organization.llmStepKeyBody',
            'Open the tenant, then pick an available key or create one. The full secret is shown only once.',
          )}
        </li>
      </ol>
      <OpenConsole consoleURL={consoleURL} />
    </div>
  );
}

function OpenConsole({ consoleURL }: { consoleURL: string }) {
  const { msg } = useI18n();
  const label = msg('settings.organization.llmOpen', 'Open company console');
  if (!consoleURL) {
    return (
      <Button type="button" variant="outline" disabled>
        {label}
      </Button>
    );
  }
  return (
    <ButtonLink href={consoleURL} target="_blank" rel="noopener noreferrer" variant="outline">
      {label}
    </ButtonLink>
  );
}

function FetchedModelIds({
  catalog,
  selected,
  onSelected,
}: {
  catalog: string[];
  selected: string[];
  onSelected: (modelIds: string[]) => void;
}) {
  const { msg } = useI18n();
  if (catalog.length === 0) {
    return null;
  }
  const models: CatalogModel[] = catalog.map((modelId) => ({
    id: modelId,
    providerId: 'organization',
    providerName: '',
  }));
  const selectedCount = catalog.filter((modelId) => selected.includes(modelId)).length;
  return (
    <div className="space-y-2">
      <p className="text-sm text-muted-foreground">
        {msg('settings.organization.llmModelsSelected', '{selected} of {total} selected', {
          selected: selectedCount,
          total: catalog.length,
        })}
      </p>
      <div className="max-h-60 overflow-y-auto rounded-lg border border-border">
        <ModelRows
          models={models}
          emptyMessage=""
          selectedIds={selected}
          onToggle={(modelId) => onSelected(toggleModelId(selected, modelId, !selected.includes(modelId)))}
        />
      </div>
    </div>
  );
}

function toggleModelId(selected: string[], modelId: string, checked: boolean) {
  if (checked) {
    return selected.includes(modelId) ? selected : [...selected, modelId];
  }
  return selected.filter((item) => item !== modelId);
}

function BindForm({
  provider,
  tenantName,
  gateway,
  apiKey,
  modelIds,
  canSave,
  saving,
  onTenantName,
  onGateway,
  onApiKey,
  onModelIds,
  onFormError,
  onSave,
  onCancel,
}: {
  provider: LLMProvider | null;
  tenantName: string;
  gateway: string;
  apiKey: string;
  modelIds: string[];
  canSave: boolean;
  saving: boolean;
  onTenantName: (value: string) => void;
  onGateway: (value: string) => void;
  onApiKey: (value: string) => void;
  onModelIds: (modelIds: string[]) => void;
  onFormError: (message: string | null) => void;
  onSave: () => void;
  onCancel?: () => void;
}) {
  const { msg } = useI18n();
  const { csrfToken } = useAuth();
  const { orgUuid, activeWorkspaceId } = useWorkspace();
  const [discovering, setDiscovering] = useState(false);
  const [catalog, setCatalog] = useState<string[]>([]);

  const fetchModels = async () => {
    const baseUrl = gateway.trim();
    const key = apiKey.trim();
    if (!orgUuid || !baseUrl || !key) {
      onFormError(msg('llmModels.fetchModelsNeedCredentials', 'Enter a base URL and API key first.'));
      return;
    }
    onFormError(null);
    setDiscovering(true);
    try {
      const preview = await previewProviderModels(
        orgUuid,
        activeWorkspaceId,
        { base_url: baseUrl, api_key: key },
        csrfToken,
      );
      const discovered = (preview.model_ids ?? []).filter((modelId) => modelId !== '');
      if (discovered.length === 0) {
        toast.info(msg('llmModels.discoverEmpty', 'The provider returned no models. You can add model IDs later.'), {
          id: 'organization-llm-discover',
        });
        return;
      }
      setCatalog(discovered);
      onModelIds(discovered);
    } catch (error) {
      onFormError(readableError(error, msg));
    } finally {
      setDiscovering(false);
    }
  };

  return (
    <div className="space-y-4">
      {provider ? null : (
        <>
          <Field className="max-w-[428px] gap-2">
            <FieldLabel htmlFor="organization-llm-name">
              {msg('settings.organization.llmProviderName', 'Provider name')}
            </FieldLabel>
            <Input
              id="organization-llm-name"
              value={tenantName}
              className="h-10 px-4"
              onChange={(event) => onTenantName(event.target.value)}
            />
          </Field>
          <Field className="max-w-[428px] gap-2">
            <FieldLabel htmlFor="organization-llm-gateway">
              {msg('settings.organization.llmGateway', 'Model gateway address')}
            </FieldLabel>
            <Input
              id="organization-llm-gateway"
              value={gateway}
              className="h-10 px-4"
              onChange={(event) => onGateway(event.target.value)}
            />
          </Field>
        </>
      )}
      <Field className="max-w-[428px] gap-2">
        <div className="flex items-center justify-between gap-3">
          <FieldLabel htmlFor="organization-llm-key">{msg('settings.organization.llmKey', 'API key')}</FieldLabel>
          {provider ? null : (
            <Button
              type="button"
              variant="outline"
              size="sm"
              aria-busy={discovering}
              disabled={discovering || gateway.trim() === '' || apiKey.trim() === ''}
              onClick={() => void fetchModels()}
            >
              {discovering ? <Loader2 className="animate-spin" aria-hidden /> : <Download aria-hidden />}
              {msg('llmModels.fetchModels', 'Fetch model list')}
            </Button>
          )}
        </div>
        <Input
          id="organization-llm-key"
          type="password"
          autoComplete="off"
          value={apiKey}
          className="h-10 px-4"
          onChange={(event) => onApiKey(event.target.value)}
        />
        <FetchedModelIds catalog={catalog} selected={modelIds} onSelected={onModelIds} />
      </Field>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="button" size="lg" disabled={!canSave} onClick={onSave}>
          {saving
            ? msg('settings.organization.llmSaving', 'Saving')
            : msg('settings.organization.llmSave', 'Save organization model')}
        </Button>
        {onCancel ? (
          <Button type="button" variant="outline" size="lg" onClick={onCancel}>
            {msg('common.cancel', 'Cancel')}
          </Button>
        ) : null}
      </div>
    </div>
  );
}

function configuredConsoleURL(value: string | undefined) {
  if (!value) {
    return '';
  }
  try {
    const url = new URL(value);
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash) {
      return '';
    }
    return url.href;
  } catch {
    return '';
  }
}

function gatewayURL(value: string | undefined) {
  if (!value) {
    return '';
  }
  try {
    const url = new URL(value);
    if (
      (url.protocol !== 'https:' && url.protocol !== 'http:') ||
      url.username ||
      url.password ||
      url.search ||
      url.hash
    ) {
      return '';
    }
    return value;
  } catch {
    return '';
  }
}

function consoleHost(value: string) {
  return new URL(value).host;
}

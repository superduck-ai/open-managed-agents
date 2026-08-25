import { useI18n } from '../../../shared/i18n';
import { useAuth } from '../../../shared/auth/context';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Button } from '../../../shared/ui/button';
import { Checkbox } from '../../../shared/ui/checkbox';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../../shared/ui/dialog';
import { Label } from '../../../shared/ui/label';
import { ChevronDown } from 'lucide-react';
import { type FormEvent, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { compactAgentId } from '../agents/AgentsResourcePage';
import { loadMcpDirectoryServers } from '../agents/tools/api';
import { type McpDirectoryServer } from '../agents/tools/model';
import { listAgents, listManagedEntities, localTimezone, startMCPVaultAuth } from '../api';
import {
  DeploymentAddSelectField,
  DeploymentSelectField,
  DeploymentTextArea,
  DeploymentTextField,
  LockedAgentReferenceField,
  ManagedSelectField,
  ManagedTextArea,
  ManagedTextField,
  VaultMultiSelect,
} from '../components/common';
import { entityDialogSubtitle } from '../labels';
import {
  type AgentApiResponse,
  type AgentPageResponse,
  type CredentialFormValues,
  type EntityOption,
  type EnvironmentApiResponse,
  type ManagedEntityApiResponse,
  type ManagedEntityFormValues,
  type ManagedEntitySection,
  type MemoryApiResponse,
  type MemoryFormValues,
  type MemoryStoreApiResponse,
  type PageResponse,
  type VaultApiResponse,
  type VaultCredentialApiResponse,
} from '../types';
import { errorMessage } from '../utils';
import { areSessionFileResourcesValid, SessionFileResourcesField } from '../sessions/SessionFileResourcesField';
import {
  credentialAuthTypeLabel,
  credentialFormReady,
  credentialFormValues,
  initialFormValues,
  parseCredentialAuthType,
  patchCredentialFormValues,
  vaultOAuthErrorMessage,
} from './model';
import { CredentialMcpServerField } from './credential-mcp-server-field';
import { ManagedDialogCloseControl, ManagedDialogHeader, ManagedEntityDialogActions } from './dialog-components';
import { DeploymentDialogActions, DeploymentDialogHeader } from './deployment-dialog-components';
import { EnvironmentEntityDialog } from './environment-dialog';

type VaultOAuthCompleteMessage = {
  type: 'vault_oauth_complete';
  credential_id?: string;
  vault_id?: string;
  error_code?: string;
  flow_id?: string;
};

function isVaultOAuthCompleteMessage(value: unknown): value is VaultOAuthCompleteMessage {
  return Boolean(value && typeof value === 'object' && (value as { type?: unknown }).type === 'vault_oauth_complete');
}

function OptionalCredentialFields({
  title,
  defaultOpen = false,
  children,
}: {
  title: string;
  defaultOpen?: boolean;
  children: ReactNode;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(defaultOpen);
  return (
    <Collapsible open={open} onOpenChange={setOpen} className="overflow-hidden rounded-lg border border-border">
      <CollapsibleTrigger
        type="button"
        className="flex h-9 w-full items-center gap-2 px-3 text-left text-sm text-muted-foreground hover:text-foreground"
      >
        <ChevronDown className={`size-4 shrink-0 transition-transform ${open ? '' : '-rotate-90'}`} aria-hidden />
        <span className="font-medium text-foreground">{title}</span>
        <span className="text-xs text-muted-foreground">
          {msg('managedAgents.credentialVaults.credentialDialog.optional', 'Optional')}
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent className="space-y-3 border-t border-border px-3 py-3">{children}</CollapsibleContent>
    </Collapsible>
  );
}

export function CredentialDialog({
  credential,
  vaultId,
  workspaceId,
  orgUuid,
  firstCredential = false,
  onClose,
  onSubmit,
  onOAuthComplete,
}: {
  credential?: VaultCredentialApiResponse;
  vaultId: string;
  workspaceId: string;
  orgUuid?: string;
  firstCredential?: boolean;
  onClose: () => void;
  onSubmit: (values: CredentialFormValues) => Promise<void>;
  onOAuthComplete?: () => void;
}) {
  const { msg } = useI18n();
  const { csrfToken } = useAuth();
  const [values, setValues] = useState<CredentialFormValues>(() => credentialFormValues(credential));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [acknowledged, setAcknowledged] = useState(false);
  const [directoryServers, setDirectoryServers] = useState<McpDirectoryServer[]>([]);
  const pendingOAuthFlowIdRef = useRef<string | null>(null);
  const oauthPopupRef = useRef<Window | null>(null);
  const mode = credential ? 'edit' : 'create';
  const canSubmit = credentialFormReady(values, mode, acknowledged);
  const needsOAuthConnect = mode === 'create' && values.authType === 'mcp_oauth' && !values.token.trim();
  const waitingForOAuth = needsOAuthConnect && submitting;
  const showMcpUrl = values.authType === 'static_bearer' || values.authType === 'mcp_oauth';
  const showDirectoryPicker = values.authType === 'mcp_oauth' && mode === 'create';
  const patchValues = (patch: Partial<CredentialFormValues>) => {
    setValues((current) => patchCredentialFormValues(current, patch));
  };
  const abandonOAuthWait = (message?: string) => {
    pendingOAuthFlowIdRef.current = null;
    const popup = oauthPopupRef.current;
    oauthPopupRef.current = null;
    if (popup && !popup.closed) {
      popup.close();
    }
    if (message) {
      setError(message);
    }
    setSubmitting(false);
  };
  const dismissDialog = () => {
    if (waitingForOAuth) {
      abandonOAuthWait();
    } else if (submitting) {
      return;
    }
    onClose();
  };

  const authTypeOptions = useMemo(() => {
    if (credential) {
      return [{ id: values.authType, label: credentialAuthTypeLabel(values.authType, msg) }];
    }
    return [
      { id: 'static_bearer', label: credentialAuthTypeLabel('static_bearer', msg) },
      { id: 'mcp_oauth', label: credentialAuthTypeLabel('mcp_oauth', msg) },
      { id: 'environment_variable', label: credentialAuthTypeLabel('environment_variable', msg) },
    ];
  }, [credential, values.authType, msg]);

  const directoryOptions = useMemo<EntityOption[]>(
    () =>
      directoryServers
        .filter((server) => server.url)
        .map((server) => ({
          id: server.url as string,
          label: server.displayName,
          secondary: server.url as string,
        })),
    [directoryServers],
  );

  useEffect(() => {
    if (!showDirectoryPicker) {
      return;
    }
    let active = true;
    void loadMcpDirectoryServers()
      .then((servers) => {
        if (active) {
          setDirectoryServers(servers);
        }
      })
      .catch(() => {
        if (active) {
          setDirectoryServers([]);
        }
      });
    return () => {
      active = false;
    };
  }, [showDirectoryPicker]);

  useEffect(() => {
    if (!waitingForOAuth) {
      return;
    }
    const finish = (message: VaultOAuthCompleteMessage) => {
      const pendingFlowId = pendingOAuthFlowIdRef.current;
      if (!pendingFlowId || message.flow_id !== pendingFlowId) {
        return;
      }
      if (message.vault_id && message.vault_id !== vaultId) {
        return;
      }
      pendingOAuthFlowIdRef.current = null;
      oauthPopupRef.current = null;
      if (message.error_code) {
        setError(vaultOAuthErrorMessage(message.error_code, msg));
        setSubmitting(false);
        return;
      }
      onOAuthComplete?.();
      onClose();
    };
    const onMessage = (event: MessageEvent) => {
      if (event.origin !== window.location.origin || !isVaultOAuthCompleteMessage(event.data)) {
        return;
      }
      finish(event.data);
    };
    const channel = new BroadcastChannel('vault-oauth');
    channel.onmessage = (event) => {
      if (isVaultOAuthCompleteMessage(event.data)) {
        finish(event.data);
      }
    };
    let closedGraceTimer: number | undefined;
    const closedPoll = window.setInterval(() => {
      const popup = oauthPopupRef.current;
      if (!popup?.closed || !pendingOAuthFlowIdRef.current || closedGraceTimer !== undefined) {
        return;
      }
      // Success pages often close themselves just before/after postMessage; wait briefly.
      closedGraceTimer = window.setTimeout(() => {
        if (pendingOAuthFlowIdRef.current) {
          abandonOAuthWait(
            msg(
              'managedAgents.credentialVaults.credentialDialog.oauthWindowClosed',
              'OAuth window was closed before completing.',
            ),
          );
        }
      }, 800);
    }, 400);
    window.addEventListener('message', onMessage);
    return () => {
      window.clearInterval(closedPoll);
      if (closedGraceTimer !== undefined) {
        window.clearTimeout(closedGraceTimer);
      }
      window.removeEventListener('message', onMessage);
      channel.close();
    };
  }, [msg, onClose, onOAuthComplete, vaultId, waitingForOAuth]);

  const submitDirect = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit || needsOAuthConnect) {
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onSubmit(values);
    } catch (submitError) {
      setError(errorMessage(submitError));
      setSubmitting(false);
    }
  };

  const connectOAuth = async () => {
    if (!canSubmit || !needsOAuthConnect || !orgUuid) {
      setError(
        orgUuid
          ? msg('managedAgents.credentialVaults.credentialDialog.completeRequired', 'Complete the required fields.')
          : msg('managedAgents.credentialVaults.credentialDialog.noOrganization', 'No organization'),
      );
      return;
    }
    // Open synchronously while we still have the click gesture; await would lose activation.
    // Keep a real popup (not a new tab). Size matters: GitHub enables Authorize via JS and
    // often leaves it unresponsive in tiny popups.
    const popup = window.open(
      'about:blank',
      'vault-oauth',
      'popup=yes,width=960,height=800,scrollbars=yes,resizable=yes',
    );
    if (!popup) {
      setError(
        msg(
          'managedAgents.credentialVaults.credentialDialog.popupBlocked',
          'Popup blocked. Allow popups and try again.',
        ),
      );
      return;
    }
    oauthPopupRef.current = popup;
    setSubmitting(true);
    setError(null);
    pendingOAuthFlowIdRef.current = null;
    try {
      // GitHub Apps require an exact callback URL match with no extra query params.
      const redirectUrl = `${window.location.origin}/oauth/vault/success`;
      const started = await startMCPVaultAuth(
        orgUuid,
        {
          mcp_server_url: values.mcpServerUrl.trim(),
          vault_id: vaultId,
          workspace_id: workspaceId,
          redirect_url: redirectUrl,
          display_name: values.displayName.trim(),
          source: firstCredential ? 'vault_create' : 'vault_detail',
          ...(values.oauthClientId.trim() ? { client_id: values.oauthClientId.trim() } : {}),
          ...(values.oauthClientSecret.trim() ? { client_secret: values.oauthClientSecret.trim() } : {}),
        },
        csrfToken,
      );
      if (popup.closed) {
        abandonOAuthWait(
          msg(
            'managedAgents.credentialVaults.credentialDialog.oauthWindowClosed',
            'OAuth window was closed before completing.',
          ),
        );
        return;
      }
      pendingOAuthFlowIdRef.current = started.oauth_flow_id;
      popup.location.href = started.redirect_url;
    } catch (connectError) {
      abandonOAuthWait(errorMessage(connectError));
    }
  };

  let dialogTitle = msg('managedAgents.credentialVaults.credentialDialog.add', 'Add credential');
  if (credential) {
    dialogTitle = msg('managedAgents.credentialVaults.credentialDialog.edit', 'Edit credential');
  } else if (firstCredential) {
    dialogTitle = msg('managedAgents.credentialVaults.credentialDialog.addFirst', 'Add your first credential');
  }
  const dialogDescription =
    values.authType === 'mcp_oauth'
      ? msg(
          'managedAgents.credentialVaults.credentialDialog.oauthDescription',
          'Connect an MCP server with OAuth, or paste an access token.',
        )
      : msg(
          'managedAgents.credentialVaults.credentialDialog.description',
          'Store a credential for MCP servers or environment variables.',
        );

  return (
    <Dialog open onOpenChange={(open) => !open && dismissDialog()}>
      <DialogContent className="sm:max-w-[560px]">
        <form onSubmit={submitDirect}>
          <DialogHeader>
            <DialogTitle>{dialogTitle}</DialogTitle>
            <DialogDescription>{dialogDescription}</DialogDescription>
          </DialogHeader>
          <div className="mt-5 space-y-4">
            <ManagedTextField
              label={msg('managedAgents.credentialVaults.credentialDialog.displayName', 'Display name')}
              value={values.displayName}
              onChange={(displayName) => patchValues({ displayName })}
              autoFocus
            />
            <ManagedSelectField
              label={msg('managedAgents.credentialVaults.credentialDialog.authType', 'Authentication type')}
              value={values.authType}
              placeholder={msg('managedAgents.credentialVaults.credentialDialog.authType', 'Authentication type')}
              options={authTypeOptions}
              onChange={(authType) => patchValues({ authType: parseCredentialAuthType(authType) })}
            />
            {showMcpUrl ? (
              values.authType === 'mcp_oauth' ? (
                <CredentialMcpServerField
                  value={values.mcpServerUrl}
                  directoryOptions={directoryOptions}
                  readOnly={Boolean(credential)}
                  onChange={(mcpServerUrl) => patchValues({ mcpServerUrl })}
                />
              ) : (
                <ManagedTextField
                  label={msg('managedAgents.credentialVaults.credentialDialog.mcpServerUrl', 'MCP server URL')}
                  value={values.mcpServerUrl}
                  placeholder={msg(
                    'managedAgents.credentialVaults.credentialDialog.mcpServerUrlPlaceholder',
                    'https://example.com/mcp',
                  )}
                  disabled={Boolean(credential)}
                  onChange={(mcpServerUrl) => patchValues({ mcpServerUrl })}
                />
              )
            ) : null}
            {values.authType === 'static_bearer' ? (
              <ManagedTextField
                label={msg('managedAgents.credentialVaults.credentialDialog.token', 'Token')}
                type="password"
                value={values.token}
                placeholder={msg('managedAgents.credentialVaults.credentialDialog.token', 'Token')}
                onChange={(token) => patchValues({ token })}
              />
            ) : null}
            {values.authType === 'environment_variable' ? (
              <>
                <ManagedTextField
                  label={msg('managedAgents.credentialVaults.credentialDialog.secretName', 'Secret name')}
                  value={values.secretName}
                  placeholder={msg(
                    'managedAgents.credentialVaults.credentialDialog.secretNamePlaceholder',
                    'EXAMPLE_TOKEN',
                  )}
                  disabled={Boolean(credential)}
                  onChange={(secretName) => patchValues({ secretName })}
                />
                <ManagedTextField
                  label={msg('managedAgents.credentialVaults.credentialDialog.secretValue', 'Secret value')}
                  type="password"
                  value={values.secretValue}
                  placeholder={msg('managedAgents.credentialVaults.credentialDialog.secretValue', 'Secret value')}
                  onChange={(secretValue) => patchValues({ secretValue })}
                />
              </>
            ) : null}
            {values.authType === 'mcp_oauth' ? (
              <>
                <OptionalCredentialFields
                  title={msg('managedAgents.credentialVaults.credentialDialog.accessToken', 'Access token')}
                >
                  <ManagedTextField
                    label={msg('managedAgents.credentialVaults.credentialDialog.accessToken', 'Access token')}
                    type="password"
                    value={values.token}
                    placeholder={msg(
                      'managedAgents.credentialVaults.credentialDialog.accessTokenPlaceholder',
                      'Paste access token to skip OAuth popup',
                    )}
                    onChange={(token) => patchValues({ token })}
                  />
                </OptionalCredentialFields>
                {mode === 'create' ? (
                  <OptionalCredentialFields
                    title={msg('managedAgents.credentialVaults.credentialDialog.oauthClient', 'OAuth client')}
                  >
                    <ManagedTextField
                      label={msg('managedAgents.credentialVaults.credentialDialog.clientId', 'Client ID')}
                      value={values.oauthClientId}
                      placeholder={msg(
                        'managedAgents.credentialVaults.credentialDialog.clientIdPlaceholder',
                        'When dynamic registration is unavailable',
                      )}
                      onChange={(oauthClientId) => patchValues({ oauthClientId })}
                    />
                    <ManagedTextField
                      label={msg('managedAgents.credentialVaults.credentialDialog.clientSecret', 'Client secret')}
                      type="password"
                      value={values.oauthClientSecret}
                      placeholder={msg('managedAgents.credentialVaults.credentialDialog.optional', 'Optional')}
                      onChange={(oauthClientSecret) => patchValues({ oauthClientSecret })}
                    />
                  </OptionalCredentialFields>
                ) : null}
                {mode === 'create' && values.token.trim() ? (
                  <OptionalCredentialFields
                    title={msg('managedAgents.credentialVaults.credentialDialog.refresh', 'Refresh')}
                  >
                    <ManagedTextField
                      label={msg('managedAgents.credentialVaults.credentialDialog.refreshToken', 'Refresh token')}
                      type="password"
                      value={values.refreshToken}
                      onChange={(refreshToken) => patchValues({ refreshToken })}
                    />
                    <ManagedTextField
                      label={msg('managedAgents.credentialVaults.credentialDialog.tokenEndpoint', 'Token endpoint')}
                      value={values.refreshTokenEndpoint}
                      placeholder={msg(
                        'managedAgents.credentialVaults.credentialDialog.tokenEndpointPlaceholder',
                        'https://example.com/oauth/token',
                      )}
                      onChange={(refreshTokenEndpoint) => patchValues({ refreshTokenEndpoint })}
                    />
                    <ManagedTextField
                      label={msg('managedAgents.credentialVaults.credentialDialog.clientId', 'Client ID')}
                      value={values.refreshClientId}
                      onChange={(refreshClientId) => patchValues({ refreshClientId })}
                    />
                    <ManagedSelectField
                      label={msg(
                        'managedAgents.credentialVaults.credentialDialog.tokenEndpointAuth',
                        'Token endpoint auth',
                      )}
                      value={values.refreshAuthType}
                      placeholder={msg('managedAgents.credentialVaults.credentialDialog.authMethod', 'Auth method')}
                      options={[
                        { id: 'none', label: 'none' },
                        { id: 'client_secret_post', label: 'client_secret_post' },
                        { id: 'client_secret_basic', label: 'client_secret_basic' },
                      ]}
                      onChange={(refreshAuthType) =>
                        patchValues({
                          refreshAuthType:
                            refreshAuthType === 'client_secret_basic' || refreshAuthType === 'client_secret_post'
                              ? refreshAuthType
                              : 'none',
                        })
                      }
                    />
                    {values.refreshAuthType !== 'none' ? (
                      <ManagedTextField
                        label={msg('managedAgents.credentialVaults.credentialDialog.clientSecret', 'Client secret')}
                        type="password"
                        value={values.refreshClientSecret}
                        onChange={(refreshClientSecret) => patchValues({ refreshClientSecret })}
                      />
                    ) : null}
                  </OptionalCredentialFields>
                ) : null}
              </>
            ) : null}
            <Alert className="border-amber-300 bg-amber-50 text-amber-950 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-100">
              <AlertDescription>
                {msg(
                  'managedAgents.credentialVaults.credentialDialog.sharedWarning',
                  'Credentials in a vault are shared with anyone who can use this vault. You are responsible for storage and use.',
                )}
              </AlertDescription>
            </Alert>
            <div className="flex items-start gap-2">
              <Checkbox
                id="credential-ack"
                checked={acknowledged}
                onCheckedChange={(checked) => setAcknowledged(checked === true)}
              />
              <Label htmlFor="credential-ack" className="text-sm font-normal leading-5 text-foreground">
                {msg(
                  'managedAgents.credentialVaults.credentialDialog.acknowledge',
                  'I acknowledge this credential is shared and that I am responsible for its storage and use.',
                )}
              </Label>
            </div>
          </div>
          {error ? <p className="mt-4 text-sm text-destructive">{error}</p> : null}
          <DialogFooter className="mt-5">
            {firstCredential ? (
              <Button type="button" variant="ghost" disabled={submitting && !waitingForOAuth} onClick={dismissDialog}>
                {msg('managedAgents.quickstart.skip', 'Skip')}
              </Button>
            ) : (
              <Button type="button" variant="outline" disabled={submitting && !waitingForOAuth} onClick={dismissDialog}>
                {msg('common.cancel', 'Cancel')}
              </Button>
            )}
            {needsOAuthConnect ? (
              <Button type="button" disabled={!canSubmit || submitting} onClick={() => void connectOAuth()}>
                {submitting
                  ? msg('managedAgents.credentialVaults.credentialDialog.connecting', 'Connecting...')
                  : msg('managedAgents.credentialVaults.credentialDialog.connect', 'Connect')}
              </Button>
            ) : (
              <Button type="submit" disabled={!canSubmit || submitting}>
                {submitting
                  ? msg('common.saving', 'Saving...')
                  : credential
                    ? msg('common.saveChanges', 'Save changes')
                    : msg('managedAgents.credentialVaults.credentialDialog.add', 'Add credential')}
              </Button>
            )}
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function MemoryDialog({
  memory,
  onClose,
  onSubmit,
}: {
  memory?: MemoryApiResponse;
  onClose: () => void;
  onSubmit: (values: MemoryFormValues) => Promise<void>;
}) {
  const { msg } = useI18n();
  const [values, setValues] = useState<MemoryFormValues>(() => ({
    path: memory?.path || '',
    content: memory?.content || '',
  }));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const canSubmit = values.path.trim() && values.content.length > 0;
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onSubmit(values);
    } catch (submitError) {
      setError(errorMessage(submitError));
      setSubmitting(false);
    }
  };
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-[560px]">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>
              {memory
                ? msg('managedAgents.memoryStores.memoryDialog.edit', 'Edit memory')
                : msg('managedAgents.memoryStores.memoryDialog.add', 'Add memory')}
            </DialogTitle>
            <DialogDescription>
              {msg(
                'managedAgents.memoryStores.memoryDialog.description',
                'Save a path and its content in this memory store.',
              )}
            </DialogDescription>
          </DialogHeader>
          <div className="mt-5 space-y-4">
            <ManagedTextField
              label={msg('managedAgents.memoryStores.memoryDialog.path', 'Path')}
              value={values.path}
              placeholder="/notes/example.txt"
              onChange={(path) => setValues((current) => ({ ...current, path }))}
              autoFocus
            />
            <ManagedTextArea
              label={msg('managedAgents.memoryStores.memoryDialog.content', 'Content')}
              value={values.content}
              placeholder={msg('managedAgents.memoryStores.memoryDialog.placeholder', 'Memory content')}
              onChange={(content) => setValues((current) => ({ ...current, content }))}
            />
          </div>
          {error ? <p className="mt-4 text-sm text-destructive">{error}</p> : null}
          <DialogFooter className="mt-5">
            <Button type="button" variant="outline" onClick={onClose}>
              {msg('common.cancel', 'Cancel')}
            </Button>
            <Button type="submit" disabled={!canSubmit || submitting}>
              {submitting
                ? msg('common.saving', 'Saving...')
                : memory
                  ? msg('common.saveChanges', 'Save changes')
                  : msg('managedAgents.memoryStores.memoryDialog.add', 'Add memory')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

type ManagedEntityDialogProps = {
  section: ManagedEntitySection;
  title: string;
  entity?: ManagedEntityApiResponse;
  lockedAgent?: AgentApiResponse;
  workspaceId: string;
  onClose: () => void;
  onSubmit: (values: ManagedEntityFormValues) => Promise<void>;
};

export function ManagedEntityDialog(props: ManagedEntityDialogProps) {
  if (props.section === 'environments') {
    return (
      <EnvironmentEntityDialog
        title={props.title}
        entity={props.entity}
        onClose={props.onClose}
        onSubmit={props.onSubmit}
      />
    );
  }
  return <GenericManagedEntityDialog {...props} />;
}

function GenericManagedEntityDialog({
  section,
  title,
  entity,
  lockedAgent,
  workspaceId,
  onClose,
  onSubmit,
}: ManagedEntityDialogProps) {
  const { msg } = useI18n();
  const initialValues = useMemo<ManagedEntityFormValues>(
    () => ({
      ...initialFormValues(section, entity),
      ...(lockedAgent ? { agentId: lockedAgent.id } : {}),
    }),
    [entity, lockedAgent, section],
  );
  const [values, setValues] = useState<ManagedEntityFormValues>(initialValues);
  const [agents, setAgents] = useState<EntityOption[]>([]);
  const [environments, setEnvironments] = useState<EntityOption[]>([]);
  const [vaults, setVaults] = useState<EntityOption[]>([]);
  const [memoryStores, setMemoryStores] = useState<EntityOption[]>([]);
  const [loadingOptions, setLoadingOptions] = useState(section === 'sessions' || section === 'deployments');
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const submittingRef = useRef(false);
  const needsReferences = section === 'sessions' || section === 'deployments';
  useEffect(() => {
    if (!needsReferences) {
      return;
    }
    let active = true;

    void (async () => {
      await Promise.resolve();
      if (!active) {
        return;
      }

      setLoadingOptions(true);
      try {
        const [agentPage, environmentPage, vaultPage, memoryStorePage] = await Promise.all([
          lockedAgent ? Promise.resolve({ data: [], next_page: null } as AgentPageResponse) : listAgents(workspaceId),
          listManagedEntities('environments', workspaceId),
          listManagedEntities('credential-vaults', workspaceId),
          section === 'deployments'
            ? listManagedEntities('memory-stores', workspaceId)
            : Promise.resolve({ data: [], next_page: null } as PageResponse<ManagedEntityApiResponse>),
        ]);
        if (!active) {
          return;
        }
        const agentOptions = lockedAgent
          ? [
              {
                id: lockedAgent.id,
                label: lockedAgent.name || lockedAgent.id,
                secondary: `v${lockedAgent.version} · ${compactAgentId(lockedAgent.id)}`,
              },
            ]
          : (agentPage.data ?? []).map((agent) => ({
              id: agent.id,
              label: agent.name || agent.id,
              secondary: compactAgentId(agent.id),
            }));
        const environmentOptions = (environmentPage.data as EnvironmentApiResponse[]).map((environment) => ({
          id: environment.id,
          label: environment.name || environment.id,
          secondary: environment.id,
        }));
        const vaultOptions = (vaultPage.data as VaultApiResponse[]).map((vault) => ({
          id: vault.id,
          label: vault.display_name || vault.id,
          secondary: vault.id,
        }));
        const memoryStoreOptions = (memoryStorePage.data as MemoryStoreApiResponse[]).map((memoryStore) => ({
          id: memoryStore.id,
          label: memoryStore.name || memoryStore.id,
          secondary: memoryStore.id,
        }));
        setAgents(agentOptions);
        setEnvironments(environmentOptions);
        setVaults(vaultOptions);
        setMemoryStores(memoryStoreOptions);
        setValues((current) => ({
          ...current,
          agentId: lockedAgent?.id || current.agentId || (section === 'sessions' ? agentOptions[0]?.id || '' : ''),
          environmentId: current.environmentId || (section === 'sessions' ? environmentOptions[0]?.id || '' : ''),
        }));
        setLoadingOptions(false);
      } catch (error) {
        if (active) {
          setSubmitError(errorMessage(error));
          setLoadingOptions(false);
        }
      }
    })();

    return () => {
      active = false;
    };
  }, [lockedAgent, needsReferences, section, workspaceId]);

  const canSubmit =
    section === 'deployments'
      ? values.name.trim().length > 0 &&
        values.agentId.trim().length > 0 &&
        values.environmentId.trim().length > 0 &&
        values.initialMessage.trim().length > 0 &&
        (values.triggerType === 'manual' ||
          (values.triggerType === 'schedule' &&
            values.cronExpression.trim().length > 0 &&
            values.timezone.trim().length > 0)) &&
        !submitting &&
        !loadingOptions
      : section === 'sessions'
        ? (!needsReferences || (values.agentId.trim().length > 0 && values.environmentId.trim().length > 0)) &&
          areSessionFileResourcesValid(values.fileResources) &&
          !submitting &&
          !loadingOptions
        : values.name.trim().length > 0 &&
          (!needsReferences || (values.agentId.trim().length > 0 && values.environmentId.trim().length > 0)) &&
          !submitting &&
          !loadingOptions;

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit || submittingRef.current) {
      return;
    }
    submittingRef.current = true;
    setSubmitting(true);
    setSubmitError(null);
    try {
      await onSubmit(values);
    } catch (error) {
      setSubmitError(errorMessage(error));
      submittingRef.current = false;
      setSubmitting(false);
    }
  };
  const dialogSubtitleText = entityDialogSubtitle(section, msg);

  if (section === 'deployments') {
    return (
      <Dialog open onOpenChange={(open) => !open && onClose()}>
        <DialogContent
          className="flex max-h-[min(760px,calc(100dvh-2rem))] flex-col sm:max-w-[560px]"
          showCloseButton={false}
        >
          <form className="relative flex min-h-0 flex-col" onSubmit={handleSubmit}>
            <ManagedDialogCloseControl />

            <DeploymentDialogHeader title={title} />

            <div className="subtle-scrollbar mt-5 min-h-0 flex-1 space-y-[18px] overflow-y-auto pr-1">
              <DeploymentTextField
                label={msg('common.name', 'Name')}
                value={values.name}
                placeholder={msg('managedAgents.deployments.namePlaceholder', 'Nightly inbox triage')}
                onChange={(name) => setValues((current) => ({ ...current, name }))}
                autoFocus
              />
              {lockedAgent ? (
                <LockedAgentReferenceField agent={lockedAgent} variant="deployment" />
              ) : (
                <DeploymentSelectField
                  label={msg('managedAgents.common.agent', 'Agent')}
                  value={values.agentId}
                  placeholder={
                    loadingOptions
                      ? msg('managedAgents.agents.loading', 'Loading agents...')
                      : msg('managedAgents.deployments.selectAgent', 'Select an agent')
                  }
                  options={agents}
                  manageHref={`/workspaces/${workspaceId}/agents`}
                  manageLabel={msg('managedAgents.agents.manage', 'Manage agents')}
                  onChange={(agentId) => setValues((current) => ({ ...current, agentId }))}
                />
              )}
              <DeploymentTextArea
                label={msg('managedAgents.deployments.initialMessage', 'Initial message')}
                value={values.initialMessage}
                placeholder={msg(
                  'managedAgents.deployments.initialMessagePlaceholder',
                  "Summarize today's support tickets and post to #digest",
                )}
                helpText={msg(
                  'managedAgents.deployments.initialMessageHelp',
                  'Sent to the agent at the start of every run.',
                )}
                onChange={(initialMessage) => setValues((current) => ({ ...current, initialMessage }))}
              />
              <DeploymentSelectField
                label={msg('managedAgents.environments.kindTitle', 'Environment')}
                value={values.environmentId}
                placeholder={
                  loadingOptions
                    ? msg('managedAgents.environments.loading', 'Loading environments...')
                    : msg('managedAgents.quickstart.selectEnvironment', 'Select an environment')
                }
                options={environments}
                manageHref={`/workspaces/${workspaceId}/environments`}
                manageLabel={msg('managedAgents.environments.manage', 'Manage environments')}
                onChange={(environmentId) => setValues((current) => ({ ...current, environmentId }))}
              />
              <DeploymentAddSelectField
                label={msg('managedAgents.credentialVaults.title', 'Credential vaults')}
                optional
                valueLabel={msg('managedAgents.credentialVaults.kind', 'vault')}
                selectedIds={values.vaultIds}
                options={vaults}
                manageHref={`/workspaces/${workspaceId}/vaults`}
                manageLabel={msg('managedAgents.credentialVaults.manage', 'Manage credential vaults')}
                onChange={(vaultIds) => setValues((current) => ({ ...current, vaultIds }))}
              />
              <DeploymentAddSelectField
                label={msg('managedAgents.memoryStores.title', 'Memory stores')}
                optional
                valueLabel={msg('managedAgents.memoryStores.kind', 'memory store')}
                selectedIds={values.memoryStoreIds}
                options={memoryStores}
                manageHref={`/workspaces/${workspaceId}/memory-stores`}
                manageLabel={msg('managedAgents.memoryStores.manage', 'Manage memory stores')}
                onChange={(memoryStoreIds) => setValues((current) => ({ ...current, memoryStoreIds }))}
              />
              <DeploymentSelectField
                label={msg('managedAgents.common.trigger', 'Trigger')}
                value={values.triggerType}
                placeholder={msg('managedAgents.deployments.selectTrigger', 'Select a trigger')}
                options={[
                  { id: 'manual', label: msg('managedAgents.deployments.trigger.manual', 'Manual') },
                  { id: 'schedule', label: msg('managedAgents.deployments.trigger.scheduled', 'Scheduled') },
                ]}
                onChange={(triggerType) =>
                  setValues((current) => ({
                    ...current,
                    triggerType: triggerType === 'schedule' ? 'schedule' : triggerType === 'manual' ? 'manual' : '',
                  }))
                }
              />
              {values.triggerType === 'schedule' ? (
                <div className="grid gap-3 sm:grid-cols-2">
                  <DeploymentTextField
                    label={msg('managedAgents.deployments.cronExpression', 'Cron expression')}
                    value={values.cronExpression}
                    placeholder="0 9 * * 1"
                    onChange={(cronExpression) => setValues((current) => ({ ...current, cronExpression }))}
                  />
                  <DeploymentTextField
                    label={msg('managedAgents.deployments.timezone', 'Timezone')}
                    value={values.timezone}
                    placeholder={localTimezone()}
                    onChange={(timezone) => setValues((current) => ({ ...current, timezone }))}
                  />
                </div>
              ) : null}
            </div>

            {submitError ? <p className="mt-4 text-sm text-destructive">{submitError}</p> : null}

            <DeploymentDialogActions editing={Boolean(entity)} submitting={submitting} canSubmit={canSubmit} />
          </form>
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        className="flex max-h-[min(760px,calc(100dvh-2rem))] flex-col sm:max-w-[560px]"
        showCloseButton={false}
      >
        <form className="relative flex min-h-0 flex-col" onSubmit={handleSubmit}>
          <ManagedDialogCloseControl />

          <ManagedDialogHeader title={title} subtitle={dialogSubtitleText} />

          <div className="subtle-scrollbar mt-5 min-h-0 flex-1 space-y-4 overflow-y-auto pr-1">
            <ManagedTextField
              label={
                section === 'sessions' ? msg('managedAgents.sessions.fieldTitle', 'Title') : msg('common.name', 'Name')
              }
              value={values.name}
              placeholder={
                section === 'sessions'
                  ? msg('managedAgents.sessions.titlePlaceholder', 'Optional - name this run')
                  : msg('managedAgents.common.namePlaceholder', 'Enter a name')
              }
              onChange={(name) => setValues((current) => ({ ...current, name }))}
              autoFocus
            />

            {section === 'memory-stores' ? (
              <ManagedTextArea
                label={msg('common.description', 'Description')}
                value={values.description}
                placeholder={msg('managedAgents.common.descriptionPlaceholder', 'Add a description')}
                onChange={(description) => setValues((current) => ({ ...current, description }))}
              />
            ) : null}

            {needsReferences ? (
              <>
                {lockedAgent ? (
                  <LockedAgentReferenceField agent={lockedAgent} variant="managed" />
                ) : (
                  <ManagedSelectField
                    label={msg('managedAgents.common.agent', 'Agent')}
                    value={values.agentId}
                    placeholder={
                      loadingOptions
                        ? msg('managedAgents.agents.loading', 'Loading agents...')
                        : msg('managedAgents.deployments.selectAgent', 'Select an agent')
                    }
                    options={agents}
                    onChange={(agentId) => setValues((current) => ({ ...current, agentId }))}
                  />
                )}
                <ManagedSelectField
                  label={msg('managedAgents.environments.kindTitle', 'Environment')}
                  value={values.environmentId}
                  placeholder={
                    loadingOptions
                      ? msg('managedAgents.environments.loading', 'Loading environments...')
                      : msg('managedAgents.quickstart.selectEnvironment', 'Select an environment')
                  }
                  options={environments}
                  onChange={(environmentId) => setValues((current) => ({ ...current, environmentId }))}
                />
                <VaultMultiSelect
                  vaults={vaults}
                  selectedIds={values.vaultIds}
                  onChange={(vaultIds) => setValues((current) => ({ ...current, vaultIds }))}
                />
                {section === 'sessions' ? (
                  <SessionFileResourcesField
                    resources={values.fileResources}
                    workspaceId={workspaceId}
                    onChange={(fileResources) => setValues((current) => ({ ...current, fileResources }))}
                  />
                ) : null}
              </>
            ) : null}

            {section === 'credential-vaults' ? (
              <p className="text-sm leading-5 text-muted-foreground">
                {msg(
                  'managedAgents.credentialVaults.createHint',
                  'Continue after creating the vault to add credentials for tools and MCP servers.',
                )}
              </p>
            ) : null}
          </div>

          {submitError ? <p className="mt-4 text-sm text-destructive">{submitError}</p> : null}

          <ManagedEntityDialogActions
            section={section}
            editing={Boolean(entity)}
            submitting={submitting}
            canSubmit={canSubmit}
          />
        </form>
      </DialogContent>
    </Dialog>
  );
}

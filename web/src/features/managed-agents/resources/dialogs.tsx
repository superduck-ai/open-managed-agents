import { budgetErrorMessage } from './budget-errors';
import { useI18n } from '../../../shared/i18n';
import { useDialogDismissal } from '../../../shared/hooks/use-dialog-dismissal';
import { useAuth } from '../../../shared/auth/context';
import { Button } from '../../../shared/ui/button';
import { Checkbox } from '../../../shared/ui/checkbox';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import { Dialog, DialogClose, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../../../shared/ui/dialog';
import { Label } from '../../../shared/ui/label';
import { ChevronDown } from 'lucide-react';
import {
  type ComponentProps,
  type FormEvent,
  type RefObject,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { loadMcpDirectoryServers } from '../agents/tools/api';
import { type McpDirectoryServer } from '../agents/tools/model';
import { startMCPVaultAuth } from '../api';
import { ManagedSelectField, ManagedTextArea, ManagedTextField, ManagedWarningAlert } from '../components/common';
import { entityDialogSubtitle } from '../labels';
import {
  type AgentApiResponse,
  type CredentialFormValues,
  type EntityOption,
  type ManagedEntityApiResponse,
  type ManagedEntityFormValues,
  type ManagedEntitySection,
  type SessionApiResponse,
  type MemoryApiResponse,
  type MemoryFormValues,
  type VaultCredentialApiResponse,
} from '../types';
import { errorMessage } from '../utils';
import { EnvironmentVariableCredentialFields } from './credential-environment-fields';
import { managedEntityDialogCanSubmit } from './entity-dialog-ready';
import {
  credentialAuthTypeLabel,
  credentialFormReady,
  credentialFormValues,
  credentialDisplayName,
  initialFormValues,
  isPlatformMemoryMarkdownPath,
  parseCredentialAuthType,
  patchCredentialFormValues,
  vaultOAuthErrorMessage,
} from './model';
import { CredentialMcpServerField } from './credential-mcp-server-field';
import { ManagedDialogCloseControl, ManagedDialogHeader, ManagedEntityDialogActions } from './dialog-components';
import { DeploymentFormFields } from './deployment-form-fields';
import { DeploymentDialogActions, DeploymentDialogHeader } from './deployment-dialog-components';
import { EnvironmentEntityDialog } from './environment-dialog';
import { useManagedReferenceOptions } from './useManagedReferenceOptions';
import { SessionCreateDialog } from '../sessions/SessionCreateDialog';

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
    <Collapsible open={open} onOpenChange={setOpen} className="min-w-0 border-t-[0.5px] border-border pt-3">
      <CollapsibleTrigger
        type="button"
        className="flex min-h-9 w-full items-center gap-2 text-left text-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-[0.5px] focus-visible:ring-inset focus-visible:ring-foreground"
      >
        <ChevronDown
          className={`size-4 shrink-0 transition-transform duration-200 ease-out motion-reduce:transition-none ${open ? '' : '-rotate-90'}`}
          aria-hidden
        />
        <span className="min-w-0 flex-1 font-medium text-foreground">{title}</span>
        <span className="text-xs text-muted-foreground">
          {msg('managedAgents.credentialVaults.credentialDialog.optional', 'Optional')}
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent className="space-y-3 pt-3">{children}</CollapsibleContent>
    </Collapsible>
  );
}

function credentialDialogTypeSteps(values: CredentialFormValues, mode: 'create' | 'edit') {
  const mcpChosen = Boolean(values.mcpServerUrl.trim());
  const mcpAuth = values.authType === 'static_bearer' || values.authType === 'mcp_oauth';
  return {
    showMcpUrl: mcpAuth,
    showDirectoryPicker: mode === 'create' && mcpAuth,
    showBearerToken: values.authType === 'static_bearer' && (mode === 'edit' || mcpChosen),
    showOAuthFields: values.authType === 'mcp_oauth' && (mode === 'edit' || mcpChosen),
    // CMA: Bearer/OAuth show MCP first; token + shared-warning appear after a server is chosen.
    showCredentialCommitment: mode === 'edit' || values.authType === 'environment_variable' || (mcpAuth && mcpChosen),
  };
}

function CredentialSharedAck({
  acknowledged,
  onAcknowledgedChange,
}: {
  acknowledged: boolean;
  onAcknowledgedChange: (acknowledged: boolean) => void;
}) {
  const { msg } = useI18n();
  return (
    <>
      <ManagedWarningAlert>
        {msg(
          'managedAgents.credentialVaults.credentialDialog.sharedWarning',
          'This credential will be shared across this workspace. Anyone with API key access can use this credential in an agent session to access the service associated with the credential — including reading data and taking actions on behalf of the credential owner.',
        )}
      </ManagedWarningAlert>
      <div className="flex items-start gap-2">
        <Checkbox
          id="credential-ack"
          checked={acknowledged}
          onCheckedChange={(checked) => onAcknowledgedChange(checked === true)}
        />
        <Label htmlFor="credential-ack" className="text-sm font-normal leading-5 text-foreground">
          {msg(
            'managedAgents.credentialVaults.credentialDialog.acknowledge',
            'I acknowledge this credential is shared and that I am responsible for its storage and use.',
          )}
        </Label>
      </div>
    </>
  );
}

export function CredentialDialog({
  credential,
  vaultId,
  workspaceId,
  orgUuid,
  firstCredential = false,
  returnFocus,
  onClose,
  onSubmit,
  onOAuthComplete,
}: {
  credential?: VaultCredentialApiResponse;
  vaultId: string;
  workspaceId: string;
  orgUuid?: string;
  firstCredential?: boolean;
  returnFocus?: RefObject<HTMLElement | null>;
  onClose: () => void;
  onSubmit: (values: CredentialFormValues) => Promise<void>;
  onOAuthComplete?: () => void;
}) {
  const { msg } = useI18n();
  const { csrfToken } = useAuth();
  const [open, setOpen] = useState(true);
  const [values, setValues] = useState<CredentialFormValues>(() => credentialFormValues(credential));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [acknowledged, setAcknowledged] = useState(false);
  const [directoryServers, setDirectoryServers] = useState<McpDirectoryServer[]>([]);
  const pendingOAuthFlowIdRef = useRef<string | null>(null);
  const oauthPopupRef = useRef<Window | null>(null);
  const mode = credential ? 'edit' : 'create';
  const typeSelected = mode === 'edit' || Boolean(values.authType);
  const canSubmit = credentialFormReady(values, mode, acknowledged);
  const needsOAuthConnect = mode === 'create' && values.authType === 'mcp_oauth' && !values.token.trim();
  const waitingForOAuth = needsOAuthConnect && submitting;
  const { showMcpUrl, showDirectoryPicker, showBearerToken, showOAuthFields, showCredentialCommitment } =
    credentialDialogTypeSteps(values, mode);
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
    setOpen(false);
  };

  const authTypeOptions = useMemo(() => {
    if (credential) {
      return [{ id: values.authType, label: credentialAuthTypeLabel(values.authType, msg) }];
    }
    return [
      { id: 'mcp_oauth', label: credentialAuthTypeLabel('mcp_oauth', msg) },
      { id: 'static_bearer', label: credentialAuthTypeLabel('static_bearer', msg) },
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
      setOpen(false);
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
  }, [msg, onOAuthComplete, vaultId, waitingForOAuth]);

  const submitDirect = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit || submitting || needsOAuthConnect) {
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onSubmit(values);
      setOpen(false);
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
          display_name: credentialDisplayName(values) || undefined,
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

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => !nextOpen && dismissDialog()}
      onOpenChangeComplete={(nextOpen) => !nextOpen && onClose()}
    >
      <DialogContent
        finalFocus={returnFocus}
        closeButtonDisabled={submitting && !waitingForOAuth}
        className="flex max-h-[min(760px,calc(100dvh-2rem))] flex-col overflow-clip sm:max-w-[560px]"
      >
        <form className="relative flex min-h-0 flex-col" onSubmit={submitDirect}>
          <DialogHeader className="shrink-0">
            <DialogTitle>{dialogTitle}</DialogTitle>
          </DialogHeader>
          <div className="subtle-scrollbar mt-5 min-h-0 flex-1 overflow-y-auto pr-1">
            <fieldset disabled={submitting} className="min-w-0 space-y-4">
              <ManagedTextField
                label={msg('managedAgents.credentialVaults.credentialDialog.displayName', 'Name')}
                value={values.displayName}
                placeholder={msg(
                  'managedAgents.credentialVaults.credentialDialog.namePlaceholder',
                  'Example credential',
                )}
                optional={mode === 'create'}
                onChange={(displayName) => patchValues({ displayName })}
                autoFocus
              />
              <ManagedSelectField
                label={msg('managedAgents.credentialVaults.credentialDialog.type', 'Type')}
                value={values.authType}
                placeholder={msg('managedAgents.credentialVaults.credentialDialog.selectType', 'Select a type')}
                options={authTypeOptions}
                onChange={(authType) => patchValues({ authType: parseCredentialAuthType(authType) })}
              />
              {typeSelected ? (
                <>
                  {showMcpUrl ? (
                    mode === 'create' ? (
                      <CredentialMcpServerField
                        value={values.mcpServerUrl}
                        directoryOptions={directoryOptions}
                        onChange={(mcpServerUrl) => patchValues({ mcpServerUrl })}
                      />
                    ) : (
                      <ManagedTextField
                        label={msg('managedAgents.credentialVaults.credentialDialog.mcpServer', 'MCP server')}
                        value={values.mcpServerUrl}
                        placeholder={msg(
                          'managedAgents.credentialVaults.credentialDialog.mcpServerUrlPlaceholder',
                          'https://mcp.example.com',
                        )}
                        disabled
                        onChange={() => undefined}
                      />
                    )
                  ) : null}
                  {showBearerToken ? (
                    <ManagedTextField
                      label={msg('managedAgents.credentialVaults.credentialDialog.token', 'Token')}
                      type="password"
                      value={values.token}
                      placeholder={msg('managedAgents.credentialVaults.credentialDialog.token', 'Token')}
                      onChange={(token) => patchValues({ token })}
                    />
                  ) : null}
                  {values.authType === 'environment_variable' ? (
                    <EnvironmentVariableCredentialFields
                      values={values}
                      secretNameLocked={Boolean(credential)}
                      onChange={patchValues}
                    />
                  ) : null}
                  {showOAuthFields ? (
                    <>
                      <OptionalCredentialFields
                        title={msg('managedAgents.credentialVaults.credentialDialog.accessToken', 'Access token')}
                      >
                        <ManagedTextField
                          label={msg('managedAgents.credentialVaults.credentialDialog.accessToken', 'Access token')}
                          type="password"
                          value={values.token}
                          placeholder={
                            mode === 'edit'
                              ? msg(
                                  'managedAgents.credentialVaults.credentialDialog.keepToken',
                                  'Leave blank to keep the current token',
                                )
                              : msg(
                                  'managedAgents.credentialVaults.credentialDialog.accessTokenPlaceholder',
                                  'Paste access token to skip OAuth popup',
                                )
                          }
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
                            label={msg(
                              'managedAgents.credentialVaults.credentialDialog.tokenEndpoint',
                              'Token endpoint',
                            )}
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
                            placeholder={msg(
                              'managedAgents.credentialVaults.credentialDialog.authMethod',
                              'Auth method',
                            )}
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
                              label={msg(
                                'managedAgents.credentialVaults.credentialDialog.clientSecret',
                                'Client secret',
                              )}
                              type="password"
                              value={values.refreshClientSecret}
                              onChange={(refreshClientSecret) => patchValues({ refreshClientSecret })}
                            />
                          ) : null}
                        </OptionalCredentialFields>
                      ) : null}
                    </>
                  ) : null}
                  {showCredentialCommitment ? (
                    <CredentialSharedAck acknowledged={acknowledged} onAcknowledgedChange={setAcknowledged} />
                  ) : null}
                </>
              ) : null}
            </fieldset>
          </div>
          {error ? <p className="mt-4 shrink-0 text-sm text-destructive">{error}</p> : null}
          <DialogFooter className="mt-5 shrink-0">
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
  returnFocus,
  onClose,
  onSubmit,
}: {
  memory?: MemoryApiResponse;
  returnFocus?: ComponentProps<typeof DialogContent>['finalFocus'];
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
  const dialogProps = useDialogDismissal(onClose, submitting);
  const canSubmit = values.path.trim() && values.content.length > 0 && !isPlatformMemoryMarkdownPath(values.path);
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit || submitting) {
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
    <Dialog {...dialogProps}>
      <DialogContent
        className="grid-rows-[auto_minmax(0,1fr)] max-h-[min(720px,calc(100dvh-2rem))] gap-0 overflow-clip p-0 sm:max-w-[560px]"
        finalFocus={returnFocus}
        closeButtonDisabled={submitting}
      >
        <DialogHeader className="px-4 py-4 pr-12">
          <DialogTitle>
            {memory
              ? msg('managedAgents.memoryStores.memoryDialog.edit', 'Edit memory')
              : msg('managedAgents.memoryStores.memoryDialog.add', 'Add memory')}
          </DialogTitle>
        </DialogHeader>
        <form className="grid min-h-0 grid-rows-[minmax(0,1fr)_auto]" onSubmit={submit}>
          <fieldset
            disabled={submitting}
            className="subtle-scrollbar min-h-0 min-w-0 space-y-4 overflow-y-auto px-4 pb-4"
          >
            <ManagedTextField
              label={msg('managedAgents.memoryStores.memoryDialog.path', 'Path')}
              value={values.path}
              placeholder="/notes/example.txt"
              onChange={(path) => setValues((current) => ({ ...current, path }))}
              autoFocus
            />
            {isPlatformMemoryMarkdownPath(values.path) ? (
              <p className="text-sm text-destructive" role="alert">
                {msg(
                  'managedAgents.memoryStores.memoryDialog.platformMemoryMd',
                  'Platform MEMORY.md is created in the sandbox and cannot be added to a store.',
                )}
              </p>
            ) : null}
            <ManagedTextArea
              label={msg('managedAgents.memoryStores.memoryDialog.content', 'Content')}
              value={values.content}
              placeholder={msg('managedAgents.memoryStores.memoryDialog.placeholder', 'Memory content')}
              onChange={(content) => setValues((current) => ({ ...current, content }))}
            />
          </fieldset>
          <div className="px-4">
            {error ? (
              <p className="mt-4 text-sm text-destructive" role="alert">
                {error}
              </p>
            ) : null}
            <DialogFooter className="py-4">
              <DialogClose disabled={submitting} render={<Button type="button" variant="outline" />}>
                {msg('common.cancel', 'Cancel')}
              </DialogClose>
              <Button type="submit" disabled={!canSubmit || submitting}>
                {submitting
                  ? msg('common.saving', 'Saving...')
                  : memory
                    ? msg('common.saveChanges', 'Save changes')
                    : msg('managedAgents.memoryStores.memoryDialog.add', 'Add memory')}
              </Button>
            </DialogFooter>
          </div>
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
  returnFocus?: ComponentProps<typeof DialogContent>['finalFocus'];
  onClose: () => void;
  onSubmit: (values: ManagedEntityFormValues) => Promise<void>;
};

export function ManagedEntityDialog(props: ManagedEntityDialogProps) {
  if (props.section === 'sessions') {
    return (
      <SessionCreateDialog
        {...props}
        key={`${props.workspaceId}:${props.entity?.id || 'new'}`}
        entity={props.entity as SessionApiResponse | undefined}
      />
    );
  }
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
  returnFocus,
  onClose,
  onSubmit,
}: ManagedEntityDialogProps) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(true);
  const dialogProps = {
    open,
    onOpenChange: setOpen,
    onOpenChangeComplete: (nextOpen: boolean) => {
      if (!nextOpen) onClose();
    },
  };
  const initialValues = useMemo<ManagedEntityFormValues>(
    () => ({
      ...initialFormValues(section, entity),
      ...(lockedAgent ? { agentId: lockedAgent.id } : {}),
    }),
    [entity, lockedAgent, section],
  );
  const [values, setValues] = useState<ManagedEntityFormValues>(initialValues);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const submittingRef = useRef(false);
  const needsReferences = section === 'deployments';
  const {
    environments,
    vaults,
    memoryStores,
    loading: loadingOptions,
    error: referenceError,
  } = useManagedReferenceOptions(workspaceId, needsReferences);
  useEffect(() => {
    if (!needsReferences || loadingOptions || referenceError) return;
    setValues((current) => ({
      ...current,
      agentId: lockedAgent?.id || current.agentId,
    }));
  }, [environments, loadingOptions, lockedAgent, needsReferences, referenceError, section]);

  const canSubmit = managedEntityDialogCanSubmit(section, values, {
    submitting,
    loadingOptions: loadingOptions || Boolean(referenceError),
    needsReferences,
    editing: Boolean(entity),
  });

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
      setOpen(false);
    } catch (error) {
      setSubmitError(errorMessage(error));
      submittingRef.current = false;
      setSubmitting(false);
    }
  };
  const dialogSubtitleText = entityDialogSubtitle(section, msg);

  if (section === 'deployments') {
    return (
      <Dialog {...dialogProps}>
        <DialogContent
          finalFocus={returnFocus}
          className="flex max-h-[calc(100dvh-2rem)] flex-col p-5 sm:max-w-[960px] sm:p-7"
          showCloseButton={false}
        >
          <form className="relative flex min-h-0 flex-col" onSubmit={handleSubmit}>
            <ManagedDialogCloseControl />

            <DeploymentDialogHeader title={title} />

            <div className="subtle-scrollbar mt-7 min-h-0 flex-1 overflow-y-auto px-1">
              <DeploymentFormFields
                values={values}
                lockedAgent={lockedAgent}
                workspaceId={workspaceId}
                environments={environments}
                vaults={vaults}
                memoryStores={memoryStores}
                loadingOptions={loadingOptions}
                editing={Boolean(entity)}
                onChange={(patch) => setValues((current) => ({ ...current, ...patch }))}
              />
            </div>

            {submitError || referenceError ? (
              <p role="alert" className="mt-4 text-sm text-destructive">
                {budgetErrorMessage(submitError, msg) || referenceError}
              </p>
            ) : null}

            <DeploymentDialogActions
              editing={Boolean(entity)}
              submitting={submitting}
              canSubmit={canSubmit}
              onCancel={() => setOpen(false)}
            />
          </form>
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <Dialog {...dialogProps}>
      <DialogContent
        finalFocus={returnFocus}
        className="flex max-h-[min(760px,calc(100dvh-2rem))] flex-col sm:max-w-[560px]"
        showCloseButton={false}
      >
        <form className="relative flex min-h-0 flex-col" onSubmit={handleSubmit}>
          <ManagedDialogCloseControl />

          <ManagedDialogHeader title={title} subtitle={dialogSubtitleText} />

          <div className="subtle-scrollbar mt-5 min-h-0 flex-1 space-y-4 overflow-y-auto pr-1">
            <ManagedTextField
              label={msg('common.name', 'Name')}
              value={values.name}
              placeholder={msg('managedAgents.common.namePlaceholder', 'Enter a name')}
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
          </div>

          {submitError || referenceError ? (
            <p role="alert" className="mt-4 text-sm text-destructive">
              {budgetErrorMessage(submitError, msg) || referenceError}
            </p>
          ) : null}

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

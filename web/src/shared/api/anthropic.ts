import Anthropic, { APIError, type Uploadable } from '@anthropic-ai/sdk';
import { getConsoleRequestContext, type ApiError } from './client';

type AnthropicHeaderValue = string | null;
type AnthropicHeaders = Record<string, AnthropicHeaderValue>;

type AnthropicRequestContext = {
  organizationUuid?: string;
  workspaceId?: string;
};

type PageLike<T> = {
  data?: T[];
  has_more?: boolean;
  first_id?: string | null;
  last_id?: string | null;
  next_page?: string | null;
  prefixes?: unknown[];
};

export type AnthropicPageResponse<T> = {
  data: T[];
  has_more: boolean;
  first_id?: string | null;
  last_id?: string | null;
  next_page?: string | null;
  prefixes?: unknown[];
};

let cachedClient: Anthropic | null = null;
let cachedBaseURL = '';

export function anthropicBaseURL() {
  if (typeof window !== 'undefined' && window.location.origin) {
    return window.location.origin;
  }
  return 'http://127.0.0.1';
}

function anthropicFetch(input: RequestInfo | URL, init?: RequestInit) {
  if (typeof input === 'string' || input instanceof URL) {
    const value = String(input);
    if (value.startsWith('http://') || value.startsWith('https://')) {
      const url = new URL(value);
      if (url.origin === anthropicBaseURL()) {
        return fetch(`${url.pathname}${url.search}${url.hash}`, init);
      }
    }
  }
  if (typeof Request !== 'undefined' && input instanceof Request) {
    const url = new URL(input.url);
    if (url.origin === anthropicBaseURL()) {
      return fetch(new Request(`${url.pathname}${url.search}${url.hash}`, input), init);
    }
  }
  return fetch(input, init);
}

export function getAnthropicClient() {
  const baseURL = anthropicBaseURL();
  if (cachedClient && cachedBaseURL === baseURL) {
    return cachedClient;
  }

  cachedBaseURL = baseURL;
  cachedClient = new Anthropic({
    baseURL,
    apiKey: null,
    authToken: null,
    credentials: null,
    config: null,
    profile: null,
    maxRetries: 0,
    dangerouslyAllowBrowser: true,
    defaultHeaders: {
      'x-api-key': null,
      authorization: null
    },
    fetch: anthropicFetch,
    fetchOptions: {
      credentials: 'include'
    }
  });
  return cachedClient;
}

export function setAnthropicClientForTest(client: Anthropic | null) {
  cachedClient = client;
  cachedBaseURL = client ? cachedBaseURL : '';
}

export function anthropicRequestHeaders(context: AnthropicRequestContext = {}): AnthropicHeaders {
  const activeContext = { ...getConsoleRequestContext() };
  if (context.organizationUuid !== undefined) {
    activeContext.organizationUuid = context.organizationUuid;
  }
  if (context.workspaceId !== undefined) {
    activeContext.workspaceId = context.workspaceId;
  }
  const headers: AnthropicHeaders = {
    'x-api-key': null,
    authorization: null
  };
  if (activeContext.organizationUuid) {
    headers['x-organization-uuid'] = activeContext.organizationUuid;
  }
  if (activeContext.workspaceId) {
    headers['x-workspace-id'] = activeContext.workspaceId;
  }
  return headers;
}

function requestOptions(workspaceId?: string) {
  return {
    headers: anthropicRequestHeaders({ workspaceId })
  };
}

export function toPlainPage<T>(page: PageLike<T>): AnthropicPageResponse<T> {
  const response: AnthropicPageResponse<T> = {
    data: page.data ?? [],
    has_more: page.has_more ?? Boolean(page.next_page)
  };
  if ('first_id' in page) {
    response.first_id = page.first_id ?? null;
  }
  if ('last_id' in page) {
    response.last_id = page.last_id ?? null;
  }
  if ('next_page' in page) {
    response.next_page = page.next_page ?? null;
  }
  if ('prefixes' in page) {
    response.prefixes = page.prefixes ?? [];
  }
  return response;
}

async function sdkCall<T>(operation: () => Promise<T>): Promise<T> {
  try {
    return await operation();
  } catch (error) {
    throw normalizeSdkError(error);
  }
}

function normalizeSdkError(error: unknown) {
  if (!(error instanceof APIError)) {
    return error;
  }

  const payload = objectRecord(error.error);
  const nestedError = objectRecord(payload.error);
  const code =
    error.type ??
    stringValue(nestedError.type) ??
    stringValue(payload.type) ??
    stringValue(payload.code) ??
    'request_failed';
  const message =
    stringValue(nestedError.message) ??
    stringValue(payload.message) ??
    error.message ??
    'Request failed';

  return {
    status: error.status ?? 0,
    code,
    message
  } satisfies ApiError;
}

function objectRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

function stringValue(value: unknown) {
  return typeof value === 'string' && value ? value : null;
}

function sdkParams(params: Record<string, unknown>) {
  return params as never;
}

async function sdkPage<T>(operation: () => Promise<unknown>) {
  return toPlainPage<T>((await sdkCall(operation)) as PageLike<T>);
}

export const anthropicBetaApi = {
  files: {
    list<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkPage<T>(() => getAnthropicClient().beta.files.list(params, requestOptions(workspaceId)));
    },
    retrieveMetadata<T>(fileId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.files.retrieveMetadata(fileId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    upload<T>(file: Uploadable, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.files.upload({ file }, requestOptions(workspaceId))) as Promise<T>;
    },
    delete<T>(fileId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.files.delete(fileId, {}, requestOptions(workspaceId))) as Promise<T>;
    }
  },
  messageBatches: {
    create<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.messages.batches.create(sdkParams(params), requestOptions(workspaceId))) as Promise<T>;
    },
    list<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkPage<T>(() => getAnthropicClient().beta.messages.batches.list(params, requestOptions(workspaceId)));
    },
    retrieve<T>(batchId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.messages.batches.retrieve(batchId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    cancel<T>(batchId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.messages.batches.cancel(batchId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    delete<T>(batchId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.messages.batches.delete(batchId, {}, requestOptions(workspaceId))) as Promise<T>;
    }
  },
  skills: {
    list<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkPage<T>(() => getAnthropicClient().beta.skills.list(params, requestOptions(workspaceId)));
    },
    retrieve<T>(skillId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.skills.retrieve(skillId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    delete<T>(skillId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.skills.delete(skillId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    versions: {
      list<T>(skillId: string, params: Record<string, unknown>, workspaceId?: string) {
        return sdkPage<T>(() => getAnthropicClient().beta.skills.versions.list(skillId, params, requestOptions(workspaceId)));
      },
      retrieve<T>(skillId: string, version: string, workspaceId?: string) {
        return sdkCall(() =>
          getAnthropicClient().beta.skills.versions.retrieve(version, { skill_id: skillId }, requestOptions(workspaceId))
        ) as Promise<T>;
      },
      delete<T>(skillId: string, version: string, workspaceId?: string) {
        return sdkCall(() =>
          getAnthropicClient().beta.skills.versions.delete(version, { skill_id: skillId }, requestOptions(workspaceId))
        ) as Promise<T>;
      }
    }
  },
  agents: {
    create<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.agents.create(sdkParams(params), requestOptions(workspaceId))) as Promise<T>;
    },
    list<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkPage<T>(() => getAnthropicClient().beta.agents.list(params, requestOptions(workspaceId)));
    },
    retrieve<T>(agentId: string, params: Record<string, unknown> = {}, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.agents.retrieve(agentId, params, requestOptions(workspaceId))) as Promise<T>;
    },
    update<T>(agentId: string, params: Record<string, unknown>, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.agents.update(agentId, sdkParams(params), requestOptions(workspaceId))) as Promise<T>;
    },
    archive<T>(agentId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.agents.archive(agentId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    versions: {
      list<T>(agentId: string, params: Record<string, unknown>, workspaceId?: string) {
        return sdkPage<T>(() => getAnthropicClient().beta.agents.versions.list(agentId, params, requestOptions(workspaceId)));
      }
    }
  },
  sessions: {
    create<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.sessions.create(sdkParams(params), requestOptions(workspaceId))) as Promise<T>;
    },
    list<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkPage<T>(() => getAnthropicClient().beta.sessions.list(params, requestOptions(workspaceId)));
    },
    retrieve<T>(sessionId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.sessions.retrieve(sessionId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    update<T>(sessionId: string, params: Record<string, unknown>, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.sessions.update(sessionId, sdkParams(params), requestOptions(workspaceId))) as Promise<T>;
    },
    archive<T>(sessionId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.sessions.archive(sessionId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    delete<T>(sessionId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.sessions.delete(sessionId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    events: {
      list<T>(sessionId: string, params: Record<string, unknown>, workspaceId?: string) {
        return sdkPage<T>(() => getAnthropicClient().beta.sessions.events.list(sessionId, params, requestOptions(workspaceId)));
      },
      send<T>(sessionId: string, params: Record<string, unknown>, workspaceId?: string) {
        return sdkCall(() => getAnthropicClient().beta.sessions.events.send(sessionId, sdkParams(params), requestOptions(workspaceId))) as Promise<T>;
      }
    },
    resources: {
      list<T>(sessionId: string, params: Record<string, unknown>, workspaceId?: string) {
        return sdkPage<T>(() => getAnthropicClient().beta.sessions.resources.list(sessionId, params, requestOptions(workspaceId)));
      }
    },
    threads: {
      list<T>(sessionId: string, params: Record<string, unknown>, workspaceId?: string) {
        return sdkPage<T>(() => getAnthropicClient().beta.sessions.threads.list(sessionId, params, requestOptions(workspaceId)));
      }
    }
  },
  environments: {
    create<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.environments.create(sdkParams(params), requestOptions(workspaceId))) as Promise<T>;
    },
    list<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkPage<T>(() => getAnthropicClient().beta.environments.list(params, requestOptions(workspaceId)));
    },
    retrieve<T>(environmentId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.environments.retrieve(environmentId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    update<T>(environmentId: string, params: Record<string, unknown>, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.environments.update(environmentId, sdkParams(params), requestOptions(workspaceId))) as Promise<T>;
    },
    archive<T>(environmentId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.environments.archive(environmentId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    delete<T>(environmentId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.environments.delete(environmentId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    work: {
      list<T>(environmentId: string, params: Record<string, unknown>, workspaceId?: string) {
        return sdkPage<T>(() => getAnthropicClient().beta.environments.work.list(environmentId, params, requestOptions(workspaceId)));
      }
    }
  },
  deployments: {
    create<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.deployments.create(sdkParams(params), requestOptions(workspaceId))) as Promise<T>;
    },
    list<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkPage<T>(() => getAnthropicClient().beta.deployments.list(params, requestOptions(workspaceId)));
    },
    retrieve<T>(deploymentId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.deployments.retrieve(deploymentId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    update<T>(deploymentId: string, params: Record<string, unknown>, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.deployments.update(deploymentId, sdkParams(params), requestOptions(workspaceId))) as Promise<T>;
    },
    archive<T>(deploymentId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.deployments.archive(deploymentId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    run<T>(deploymentId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.deployments.run(deploymentId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    pause<T>(deploymentId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.deployments.pause(deploymentId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    unpause<T>(deploymentId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.deployments.unpause(deploymentId, {}, requestOptions(workspaceId))) as Promise<T>;
    }
  },
  deploymentRuns: {
    list<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkPage<T>(() => getAnthropicClient().beta.deploymentRuns.list(params, requestOptions(workspaceId)));
    }
  },
  vaults: {
    create<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.vaults.create(sdkParams(params), requestOptions(workspaceId))) as Promise<T>;
    },
    list<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkPage<T>(() => getAnthropicClient().beta.vaults.list(params, requestOptions(workspaceId)));
    },
    retrieve<T>(vaultId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.vaults.retrieve(vaultId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    update<T>(vaultId: string, params: Record<string, unknown>, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.vaults.update(vaultId, sdkParams(params), requestOptions(workspaceId))) as Promise<T>;
    },
    archive<T>(vaultId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.vaults.archive(vaultId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    delete<T>(vaultId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.vaults.delete(vaultId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    credentials: {
      create<T>(vaultId: string, params: Record<string, unknown>, workspaceId?: string) {
        return sdkCall(() =>
          getAnthropicClient().beta.vaults.credentials.create(vaultId, sdkParams(params), requestOptions(workspaceId))
        ) as Promise<T>;
      },
      list<T>(vaultId: string, params: Record<string, unknown>, workspaceId?: string) {
        return sdkPage<T>(() => getAnthropicClient().beta.vaults.credentials.list(vaultId, params, requestOptions(workspaceId)));
      },
      retrieve<T>(vaultId: string, credentialId: string, workspaceId?: string) {
        return sdkCall(() =>
          getAnthropicClient().beta.vaults.credentials.retrieve(credentialId, { vault_id: vaultId }, requestOptions(workspaceId))
        ) as Promise<T>;
      },
      update<T>(vaultId: string, credentialId: string, params: Record<string, unknown>, workspaceId?: string) {
        return sdkCall(() =>
          getAnthropicClient().beta.vaults.credentials.update(
            credentialId,
            sdkParams({ vault_id: vaultId, ...params }),
            requestOptions(workspaceId)
          )
        ) as Promise<T>;
      },
      archive<T>(vaultId: string, credentialId: string, workspaceId?: string) {
        return sdkCall(() =>
          getAnthropicClient().beta.vaults.credentials.archive(credentialId, { vault_id: vaultId }, requestOptions(workspaceId))
        ) as Promise<T>;
      },
      delete<T>(vaultId: string, credentialId: string, workspaceId?: string) {
        return sdkCall(() =>
          getAnthropicClient().beta.vaults.credentials.delete(credentialId, { vault_id: vaultId }, requestOptions(workspaceId))
        ) as Promise<T>;
      }
    }
  },
  memoryStores: {
    create<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.memoryStores.create(sdkParams(params), requestOptions(workspaceId))) as Promise<T>;
    },
    list<T>(params: Record<string, unknown>, workspaceId?: string) {
      return sdkPage<T>(() => getAnthropicClient().beta.memoryStores.list(params, requestOptions(workspaceId)));
    },
    retrieve<T>(memoryStoreId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.memoryStores.retrieve(memoryStoreId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    update<T>(memoryStoreId: string, params: Record<string, unknown>, workspaceId?: string) {
      return sdkCall(() =>
        getAnthropicClient().beta.memoryStores.update(memoryStoreId, sdkParams(params), requestOptions(workspaceId))
      ) as Promise<T>;
    },
    archive<T>(memoryStoreId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.memoryStores.archive(memoryStoreId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    delete<T>(memoryStoreId: string, workspaceId?: string) {
      return sdkCall(() => getAnthropicClient().beta.memoryStores.delete(memoryStoreId, {}, requestOptions(workspaceId))) as Promise<T>;
    },
    memories: {
      create<T>(memoryStoreId: string, params: Record<string, unknown>, workspaceId?: string) {
        return sdkCall(() =>
          getAnthropicClient().beta.memoryStores.memories.create(memoryStoreId, sdkParams(params), requestOptions(workspaceId))
        ) as Promise<T>;
      },
      list<T>(memoryStoreId: string, params: Record<string, unknown>, workspaceId?: string) {
        return sdkPage<T>(() => getAnthropicClient().beta.memoryStores.memories.list(memoryStoreId, params, requestOptions(workspaceId)));
      },
      retrieve<T>(memoryStoreId: string, memoryId: string, params: Record<string, unknown>, workspaceId?: string) {
        return sdkCall(() =>
          getAnthropicClient().beta.memoryStores.memories.retrieve(
            memoryId,
            sdkParams({ memory_store_id: memoryStoreId, ...params }),
            requestOptions(workspaceId)
          )
        ) as Promise<T>;
      },
      update<T>(memoryStoreId: string, memoryId: string, params: Record<string, unknown>, workspaceId?: string) {
        return sdkCall(() =>
          getAnthropicClient().beta.memoryStores.memories.update(
            memoryId,
            sdkParams({ memory_store_id: memoryStoreId, ...params }),
            requestOptions(workspaceId)
          )
        ) as Promise<T>;
      },
      delete<T>(memoryStoreId: string, memoryId: string, workspaceId?: string) {
        return sdkCall(() =>
          getAnthropicClient().beta.memoryStores.memories.delete(
            memoryId,
            { memory_store_id: memoryStoreId },
            requestOptions(workspaceId)
          )
        ) as Promise<T>;
      }
    }
  }
};

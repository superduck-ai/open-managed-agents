import type { GitRepositoryResourceFormValue, ManagedEntityFormValues, SessionResourceApiResponse } from '../types';
import { sessionFileAPIMountPath } from '../sessions/file-resource-path';
import { objectRecord } from '../utils';

export function emptyGitResource(): GitRepositoryResourceFormValue {
  return { url: '', authorizationToken: '', checkoutType: '', checkoutValue: '', mountPath: '' };
}

export function gitResourceURLValid(value: string) {
  try {
    const url = new URL(value.trim());
    return (
      url.protocol === 'https:' &&
      url.port === '' &&
      Boolean(url.hostname) &&
      url.pathname !== '/' &&
      !url.username &&
      !url.password &&
      !value.includes('?') &&
      !value.includes('#')
    );
  } catch {
    return false;
  }
}

export function gitResourceMountPathValid(value: string) {
  const path = value.trim();
  return (
    !path ||
    (path.startsWith('/workspace/') &&
      path
        .split('/')
        .slice(2)
        .every(
          (part) =>
            part.length > 0 && !['.', '..', '.git', '.claude', '.oma'].includes(part) && !/[\x00-\x1f\\]/.test(part),
        ))
  );
}

export function gitResourceValid(resource: GitRepositoryResourceFormValue) {
  return (
    gitResourceURLValid(resource.url) &&
    (!resource.checkoutType || Boolean(resource.checkoutValue.trim())) &&
    (resource.checkoutType !== 'commit' || /^[a-fA-F0-9]{7,64}$/.test(resource.checkoutValue.trim())) &&
    gitResourceMountPathValid(resource.mountPath)
  );
}

export function gitResourceBody(resource: GitRepositoryResourceFormValue) {
  if (!gitResourceValid(resource)) throw new Error('Complete the Git repository fields.');
  const checkout =
    resource.checkoutType === 'commit'
      ? { type: 'commit', sha: resource.checkoutValue.trim() }
      : { type: 'branch', name: resource.checkoutValue.trim() };
  return {
    type: 'github_repository',
    url: resource.url.trim(),
    ...(resource.authorizationToken.trim() ? { authorization_token: resource.authorizationToken.trim() } : {}),
    ...(resource.checkoutType ? { checkout } : {}),
    ...(resource.mountPath.trim() ? { mount_path: resource.mountPath.trim() } : {}),
  };
}

export function resourceFormValues(raw: unknown) {
  const resources: SessionResourceApiResponse[] = Array.isArray(raw) ? raw.map(objectRecord) : [];
  return {
    originalResources: resources,
    resourcesChanged: false,
    memoryStoreIds: resources
      .filter((resource) => resource.type === 'memory_store')
      .map((resource) => resource.memory_store_id)
      .filter((id): id is string => typeof id === 'string' && Boolean(id)),
    fileResources: resources
      .filter((resource) => resource.type === 'file' && resource.file_id)
      .map((resource) => ({
        fileId: resource.file_id!,
        mountPath: (resource.mount_path ?? '').replace(/^\/uploads\//, '').replace(/^\//, ''),
      })),
    gitResources: resources
      .filter((resource) => resource.type === 'github_repository')
      .map((resource): GitRepositoryResourceFormValue => {
        const checkout = objectRecord(resource.checkout);
        const checkoutType = checkout.type === 'branch' || checkout.type === 'commit' ? checkout.type : '';
        return {
          ...emptyGitResource(),
          url: typeof resource.url === 'string' ? resource.url : '',
          mountPath: resource.mount_path ?? '',
          checkoutType,
          checkoutValue: String(checkoutType === 'commit' ? (checkout.sha ?? '') : (checkout.name ?? '')),
        };
      }),
  };
}

export function managedResourcesBody(values: ManagedEntityFormValues, includeMemory: boolean) {
  const files = values.fileResources.map((resource) => {
    const mountPath = sessionFileAPIMountPath(resource.mountPath);
    return { type: 'file', file_id: resource.fileId.trim(), ...(mountPath ? { mount_path: mountPath } : {}) };
  });
  const memory = includeMemory
    ? values.memoryStoreIds.map((id) => {
        const existing = values.originalResources.find(
          (resource) => resource.type === 'memory_store' && resource.memory_store_id === id,
        );
        return {
          type: 'memory_store',
          memory_store_id: id,
          ...(existing?.access ? { access: existing.access } : {}),
          ...(typeof existing?.instructions === 'string' ? { instructions: existing.instructions } : {}),
        };
      })
    : [];
  return [...files, ...values.gitResources.map(gitResourceBody), ...memory];
}

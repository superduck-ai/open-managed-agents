import type { GitRepositoryResourceFormValue, ManagedEntityFormValues, SessionResourceApiResponse } from '../types';
import { sessionFileResourcePayload, areSessionFileResourcesValid } from '../sessions/file-resource-form';
import { areMemoryAttachesValid, entityMemoryAttaches, memoryAttachResources } from './memory-attach';
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
    (resource.checkoutType !== 'commit' ||
      /^(?:[a-fA-F0-9]{40}|[a-fA-F0-9]{64})$/.test(resource.checkoutValue.trim())) &&
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
    memoryAttaches: entityMemoryAttaches({ resources }),
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

export function managedResourceFieldsValid(values: ManagedEntityFormValues, editing: boolean) {
  return (
    (editing && !values.resourcesChanged) ||
    (areSessionFileResourcesValid(values.fileResources) &&
      values.gitResources.every(gitResourceValid) &&
      areMemoryAttachesValid(values.memoryAttaches))
  );
}

export function managedResourcesBody(values: ManagedEntityFormValues, includeMemory: boolean) {
  return [
    ...values.fileResources.map(sessionFileResourcePayload),
    ...values.gitResources.map(gitResourceBody),
    ...(includeMemory ? memoryAttachResources(values.memoryAttaches) : []),
  ];
}

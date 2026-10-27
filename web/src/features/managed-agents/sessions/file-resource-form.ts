import type { SessionFileResourceFormValue } from '../types';
import { hasSessionFileMountPath, isValidSessionFileMountPath, sessionFileAPIMountPath } from './file-resource-path';

export function areSessionFileResourcesValid(resources: SessionFileResourceFormValue[]) {
  return resources.every(
    (resource) =>
      resource.fileId.trim().length > 0 &&
      (!hasSessionFileMountPath(resource.mountPath) || isValidSessionFileMountPath(resource.mountPath)),
  );
}

export function sessionFileResourcePayload(resource: SessionFileResourceFormValue) {
  const mountPath = sessionFileAPIMountPath(resource.mountPath);
  return {
    type: 'file' as const,
    file_id: resource.fileId.trim(),
    ...(mountPath ? { mount_path: mountPath } : {}),
  };
}

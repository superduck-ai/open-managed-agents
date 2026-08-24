export const SESSION_FILE_UPLOADS_ROOT = '/mnt/session/uploads';

export function isValidSessionFileMountPath(value: string) {
  const path = value.trim();
  if (path.length === 0 || path.startsWith('/') || path.endsWith('/') || path.includes('//')) {
    return false;
  }
  return path.split('/').every((segment) => segment.length > 0 && segment !== '.' && segment !== '..');
}

export function sessionFileRuntimePath(value: string) {
  const path = value.trim();
  return isValidSessionFileMountPath(path) ? `${SESSION_FILE_UPLOADS_ROOT}/${path}` : '';
}

export function sessionFileAPIMountPath(value: string) {
  const path = value.trim();
  if (path.length === 0) {
    return undefined;
  }
  return isValidSessionFileMountPath(path) ? `/${path}` : path;
}

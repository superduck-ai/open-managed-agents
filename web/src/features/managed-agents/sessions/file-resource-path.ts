const sessionUploadsRoot = '/uploads';
const sandboxSessionRoot = '/mnt/session';

export function isValidSessionFileMountPath(value: string) {
  const path = value.trim();
  if (!path.startsWith(`${sessionUploadsRoot}/`) || path.endsWith('/') || path.includes('//')) {
    return false;
  }
  return path
    .slice(sessionUploadsRoot.length + 1)
    .split('/')
    .every((segment) => segment.length > 0 && segment !== '.' && segment !== '..');
}

export function sessionFileRuntimePath(value: string) {
  const path = value.trim();
  return isValidSessionFileMountPath(path) ? `${sandboxSessionRoot}${path}` : '';
}

export function sessionFileAPIMountPath(value: string) {
  const path = value.trim();
  return isValidSessionFileMountPath(path) ? path.slice(sessionUploadsRoot.length) : path;
}

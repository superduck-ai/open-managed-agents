export type DreamInternalSessionLike = {
  title?: string | null;
  metadata?: Record<string, string> | null;
};

const dreamInternalTitlePattern = /^Dream (?:preparation )?drm_/;

export function isDreamInternalSession(session: DreamInternalSessionLike) {
  if (session.metadata?.internal_kind === 'dream') {
    return true;
  }
  return dreamInternalTitlePattern.test(session.title ?? '');
}

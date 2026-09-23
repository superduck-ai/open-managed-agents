import { Server } from 'lucide-react';
import { useMemo, useState } from 'react';
import { cn } from '../../../../shared/lib/utils';

export function RemoteServerIcon({
  directoryIconUrl,
  className,
  iconClassName,
}: {
  directoryIconUrl?: string;
  className?: string;
  iconClassName?: string;
}) {
  const candidates = useMemo(() => remoteServerIconCandidates(directoryIconUrl), [directoryIconUrl]);
  const candidateKey = candidates.join('\u0000');
  const [failureState, setFailureState] = useState({ candidateKey, candidateIndex: 0 });
  const candidateIndex = failureState.candidateKey === candidateKey ? failureState.candidateIndex : 0;
  const candidate = candidates[candidateIndex];

  return (
    <span
      className={cn(
        'grid size-9 shrink-0 place-items-center overflow-hidden rounded-lg border border-border bg-secondary text-foreground',
        className,
      )}
    >
      {candidate ? (
        <img
          src={candidate}
          alt=""
          loading="lazy"
          decoding="async"
          className={cn('size-5 object-contain', iconClassName)}
          onError={() =>
            setFailureState((current) => ({
              candidateKey,
              candidateIndex: current.candidateKey === candidateKey ? current.candidateIndex + 1 : 1,
            }))
          }
        />
      ) : (
        <Server className={cn('size-5', iconClassName)} aria-hidden />
      )}
    </span>
  );
}

function remoteServerIconCandidates(iconUrl?: string) {
  const directoryUrl = parseHTTPURL(iconUrl);
  if (!directoryUrl) {
    return [];
  }
  return uniqueStrings([
    directoryIconCandidate(directoryUrl),
    new URL('/favicon.ico', directoryUrl.origin).toString(),
    publicFaviconCandidate(directoryUrl),
  ]);
}

function directoryIconCandidate(url?: URL) {
  if (!url || !isImageURL(url)) {
    return undefined;
  }
  return url.toString();
}

function isImageURL(url: URL) {
  return /\.(?:avif|gif|ico|jpe?g|png|svg|webp)$/i.test(url.pathname) || url.pathname.toLowerCase().includes('favicon');
}

function parseHTTPURL(value?: string) {
  if (!value) {
    return undefined;
  }
  try {
    const url = new URL(value);
    return url.protocol === 'http:' || url.protocol === 'https:' ? url : undefined;
  } catch {
    return undefined;
  }
}

function publicFaviconCandidate(directoryUrl: URL) {
  const faviconUrl = new URL('https://www.google.com/s2/favicons');
  faviconUrl.searchParams.set('domain', directoryUrl.hostname);
  faviconUrl.searchParams.set('sz', '64');
  return faviconUrl.toString();
}

function uniqueStrings(values: Array<string | undefined>) {
  return values.filter((value, index): value is string => Boolean(value) && values.indexOf(value) === index);
}

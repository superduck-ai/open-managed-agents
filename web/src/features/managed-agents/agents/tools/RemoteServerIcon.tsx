import { Server } from 'lucide-react';
import { useMemo, useState } from 'react';
import { cn } from '../../../../shared/lib/utils';

export function RemoteServerIcon({
  iconUrl,
  serverUrl,
  className,
  iconClassName,
}: {
  iconUrl?: string;
  serverUrl?: string;
  className?: string;
  iconClassName?: string;
}) {
  const candidates = useMemo(() => remoteServerIconCandidates(iconUrl, serverUrl), [iconUrl, serverUrl]);
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

function remoteServerIconCandidates(iconUrl?: string, serverUrl?: string) {
  const directoryUrl = parseHTTPURL(iconUrl);
  const server = parseHTTPURL(serverUrl);
  return uniqueStrings([
    directoryIconCandidate(directoryUrl),
    faviconCandidate(server),
    directoryUrl && !isImageURL(directoryUrl) ? publicFaviconCandidate(directoryUrl) : undefined,
    publicFaviconCandidate(server),
  ]);
}

function directoryIconCandidate(url?: URL) {
  if (!url) {
    return undefined;
  }
  if (isImageURL(url)) {
    return url.toString();
  }
  return faviconCandidate(url);
}

function faviconCandidate(url?: URL) {
  return url ? new URL('/favicon.ico', url.origin).toString() : undefined;
}

function publicFaviconCandidate(url?: URL) {
  if (!url) {
    return undefined;
  }
  const faviconUrl = new URL('https://www.google.com/s2/favicons');
  faviconUrl.searchParams.set('domain', url.hostname);
  faviconUrl.searchParams.set('sz', '64');
  return faviconUrl.toString();
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

function uniqueStrings(values: Array<string | undefined>) {
  return values.filter((value, index): value is string => Boolean(value) && values.indexOf(value) === index);
}

import { useState } from 'react';
import { productIcons } from './product-icons';

export function OAuthProviderAvatar({ logoUrl }: { logoUrl?: string }) {
  const [failedUrl, setFailedUrl] = useState<string>();
  return logoUrl && logoUrl !== failedUrl ? (
    <img
      src={logoUrl}
      onError={() => setFailedUrl(logoUrl)}
      alt=""
      referrerPolicy="no-referrer"
      className="size-6 shrink-0 rounded-md bg-white object-contain"
    />
  ) : (
    <productIcons.oauth
      aria-hidden
      className="size-6 shrink-0 rounded-md border border-border p-1 text-muted-foreground"
    />
  );
}

import { productIcons } from './product-icons';

export function OAuthProviderAvatar({ logoUrl }: { logoUrl?: string }) {
  return logoUrl ? (
    <img
      src={logoUrl}
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

import feishuLogo from './assets/feishu.ico';
import fridayLogo from './assets/friday.png';
import { productIcons } from './product-icons';
import { cn } from '../lib/utils';

const builtinLogos: Record<string, string> = {
  'builtin:feishu': feishuLogo,
  'builtin:friday': fridayLogo,
};

export function OAuthProviderAvatar({ providerKey, logoUrl }: { providerKey: string; logoUrl?: string }) {
  const src = logoUrl || builtinLogos[providerKey];
  return src ? (
    <img
      src={src}
      alt=""
      referrerPolicy="no-referrer"
      className={cn(
        'size-6 shrink-0 rounded-md object-contain',
        !logoUrl && providerKey === 'builtin:friday' && 'dark:invert',
      )}
    />
  ) : (
    <productIcons.oauth
      aria-hidden
      className="size-6 shrink-0 rounded-md border border-border p-1 text-muted-foreground"
    />
  );
}

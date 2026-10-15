import { cn } from '../lib/utils';

type BrandLogoProps = {
  compact?: boolean;
  decorative?: boolean;
  className?: string;
};

export function BrandLogo({ compact = false, decorative = false, className }: BrandLogoProps) {
  const asset = compact ? 'oma-mark' : 'oma-logo';

  return (
    <span
      role={decorative ? undefined : 'img'}
      aria-label={decorative ? undefined : 'Open Managed Agents'}
      aria-hidden={decorative || undefined}
      className={cn('inline-flex shrink-0 items-center', compact ? 'size-7' : 'h-8 w-[95px]', className)}
    >
      <img
        src={`/brand/${asset}.svg`}
        alt=""
        width={compact ? 538 : 1598}
        height="538"
        className="block h-full w-full object-contain dark:hidden"
      />
      <img
        src={`/brand/${asset}-white.svg`}
        alt=""
        width={compact ? 538 : 1598}
        height="538"
        className="hidden h-full w-full object-contain dark:block"
      />
    </span>
  );
}

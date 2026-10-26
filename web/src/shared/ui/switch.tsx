import { Switch as SwitchPrimitive } from '@base-ui/react/switch';

import { cn } from '@/shared/lib/utils';

function Switch({
  className,
  checked,
  disabled,
  readOnly,
  onCheckedChange,
  onClick,
  onClickCapture,
  size = 'default',
  ...props
}: SwitchPrimitive.Root.Props & {
  size?: 'sm' | 'default';
}) {
  const handleCheckedChange: SwitchPrimitive.Root.Props['onCheckedChange'] = (nextChecked, eventDetails) => {
    onCheckedChange?.(nextChecked, eventDetails);
  };

  const handleClick: SwitchPrimitive.Root.Props['onClick'] = (event) => {
    onClick?.(event);
  };

  const handleClickCapture: SwitchPrimitive.Root.Props['onClickCapture'] = (event) => {
    onClickCapture?.(event);

    if (event.defaultPrevented || disabled || readOnly || typeof checked !== 'boolean' || !onCheckedChange) {
      return;
    }

    event.preventDefault();
    event.stopPropagation();
    const applyFallbackChange = onCheckedChange as (nextChecked: boolean) => void;
    applyFallbackChange(!checked);
  };

  return (
    <SwitchPrimitive.Root
      data-slot="switch"
      data-size={size}
      className={cn(
        'peer group/switch relative inline-flex shrink-0 items-center rounded-full border-[0.5px] border-transparent p-[0.5px] transition-colors duration-250 ease-out motion-reduce:transition-none outline-none after:absolute after:-inset-x-3 after:-inset-y-2 focus-visible:outline-solid focus-visible:outline-[0.5px] focus-visible:outline-offset-2 focus-visible:outline-foreground aria-invalid:border-destructive aria-invalid:ring-[0.5px] aria-invalid:ring-destructive/20 data-[size=default]:h-[18px] data-[size=default]:w-[32px] data-[size=sm]:h-[14px] data-[size=sm]:w-[24px] dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40 data-checked:bg-primary data-unchecked:bg-muted-foreground/80 data-disabled:cursor-not-allowed data-disabled:opacity-50',
        className,
      )}
      checked={checked}
      disabled={disabled}
      readOnly={readOnly}
      onCheckedChange={handleCheckedChange}
      onClick={handleClick}
      onClickCapture={handleClickCapture}
      {...props}
    >
      <SwitchPrimitive.Thumb
        data-slot="switch-thumb"
        className="pointer-events-none block rounded-full bg-background ring-0 transition-transform duration-250 ease-out motion-reduce:transition-none group-data-[size=default]/switch:size-4 group-data-[size=sm]/switch:size-3 group-data-[size=default]/switch:data-checked:translate-x-[calc(100%-2px)] group-data-[size=sm]/switch:data-checked:translate-x-[calc(100%-2px)] dark:data-checked:bg-primary-foreground group-data-[size=default]/switch:data-unchecked:translate-x-0 group-data-[size=sm]/switch:data-unchecked:translate-x-0 dark:data-unchecked:bg-foreground"
      />
    </SwitchPrimitive.Root>
  );
}

export { Switch };

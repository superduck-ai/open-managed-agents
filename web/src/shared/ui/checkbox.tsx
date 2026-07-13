'use client';

import { Checkbox as CheckboxPrimitive } from '@base-ui/react/checkbox';
import * as React from 'react';

import { cn } from '@/shared/lib/utils';
import { CheckIcon, MinusIcon } from 'lucide-react';

function Checkbox({
  className,
  checked,
  disabled,
  readOnly,
  onCheckedChange,
  onClick,
  ...props
}: CheckboxPrimitive.Root.Props) {
  const changeHandledRef = React.useRef(false);

  const handleCheckedChange: CheckboxPrimitive.Root.Props['onCheckedChange'] = (nextChecked, eventDetails) => {
    changeHandledRef.current = true;
    onCheckedChange?.(nextChecked, eventDetails);
  };

  const handleClick: CheckboxPrimitive.Root.Props['onClick'] = (event) => {
    changeHandledRef.current = false;
    onClick?.(event);

    if (disabled || readOnly || typeof checked !== 'boolean' || !onCheckedChange) {
      return;
    }

    const target = event.currentTarget;
    globalThis.setTimeout(() => {
      if (changeHandledRef.current || !target.isConnected) {
        return;
      }
      if (target.getAttribute('aria-checked') !== String(checked)) {
        return;
      }
      changeHandledRef.current = true;
      const applyFallbackChange = onCheckedChange as (nextChecked: boolean) => void;
      applyFallbackChange(!checked);
    }, 0);
  };

  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(
        'peer relative flex size-4 shrink-0 items-center justify-center rounded-[4px] border border-input transition-colors outline-none group-has-disabled/field:opacity-50 after:absolute after:-inset-x-3 after:-inset-y-2 focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 aria-invalid:aria-checked:border-primary dark:bg-input/30 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40 data-checked:border-primary data-checked:bg-primary data-checked:text-primary-foreground dark:data-checked:bg-primary',
        className,
      )}
      checked={checked}
      disabled={disabled}
      readOnly={readOnly}
      onCheckedChange={handleCheckedChange}
      onClick={handleClick}
      {...props}
    >
      <CheckboxPrimitive.Indicator
        data-slot="checkbox-indicator"
        className="group grid place-content-center text-current transition-none [&>svg]:size-3.5"
      >
        <CheckIcon className="group-data-[indeterminate]:hidden" />
        <MinusIcon className="hidden group-data-[indeterminate]:block" />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
}

export { Checkbox };

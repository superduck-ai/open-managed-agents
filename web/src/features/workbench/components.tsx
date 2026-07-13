import { ReactNode } from 'react';
import clsx from 'clsx';
import { Button } from '@/shared/ui/button';
import { Label } from '@/shared/ui/label';
import { Switch } from '@/shared/ui/switch';

export function IconButton({
  label,
  children,
  onClick,
  disabled = false,
  compact = false,
}: {
  label: string;
  children: ReactNode;
  onClick?: () => void;
  disabled?: boolean;
  compact?: boolean;
}) {
  return (
    <Button
      type="button"
      aria-label={label}
      title={label}
      variant="ghost"
      className={clsx(
        'grid shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent disabled:hover:text-muted-foreground',
        compact ? 'size-8' : 'size-9',
      )}
      onClick={onClick}
      disabled={disabled}
    >
      {children}
    </Button>
  );
}

export function ToggleRow({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <Label className="workbench-tool-switch-row">
      <span>{label}</span>
      <Switch checked={checked} onCheckedChange={onChange} />
    </Label>
  );
}

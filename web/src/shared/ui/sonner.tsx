import { CircleCheck, Info, Loader2, OctagonX, TriangleAlert } from 'lucide-react';
import { type CSSProperties } from 'react';
import { createPortal } from 'react-dom';
import { Toaster as Sonner, toast, type ToasterProps } from 'sonner';
import { useTheme } from '../theme/context';

export { toast };

type SharedToasterProps = ToasterProps & {
  portal?: boolean;
};

export function Toaster({ portal = true, ...props }: SharedToasterProps) {
  const { resolvedTheme } = useTheme();
  const { style, ...rest } = props;

  const toaster = (
    <Sonner
      theme={resolvedTheme}
      position="bottom-right"
      className="toaster group"
      icons={{
        success: <CircleCheck className="size-4" />,
        info: <Info className="size-4" />,
        warning: <TriangleAlert className="size-4" />,
        error: <OctagonX className="size-4" />,
        loading: <Loader2 className="size-4 animate-spin" />,
      }}
      style={
        {
          '--normal-bg': 'var(--popover)',
          '--normal-text': 'var(--popover-foreground)',
          '--normal-border': 'var(--border)',
          '--border-radius': 'var(--radius)',
          ...style,
        } as CSSProperties
      }
      {...rest}
    />
  );

  return portal ? createPortal(toaster, document.body) : toaster;
}

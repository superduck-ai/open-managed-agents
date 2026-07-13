import { CircleCheck, Info, Loader2, OctagonX, TriangleAlert } from 'lucide-react';
import { type CSSProperties } from 'react';
import { Toaster as Sonner, toast, type ToasterProps } from 'sonner';
import { useTheme } from '../theme/context';

export { toast };

export function Toaster(props: ToasterProps) {
  const { resolvedTheme } = useTheme();
  const { style, ...rest } = props;

  return (
    <Sonner
      theme={resolvedTheme}
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
}

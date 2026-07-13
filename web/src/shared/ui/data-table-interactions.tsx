import { Check, Copy, EllipsisVertical } from "lucide-react";
import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type ComponentProps,
  type MouseEvent,
  type ReactNode,
} from "react";

import { copyText } from "@/shared/lib/clipboard";
import { cn } from "@/shared/lib/utils";
import { Button } from "@/shared/ui/button";
import { TableCell, TableRow } from "@/shared/ui/table";

export const dataTableClassName = "table-fixed border-separate border-spacing-y-px text-left";
export const dataTableHeaderRowClassName = "border-0 text-muted-foreground hover:bg-transparent";
export const dataTableHeaderCellClassName = "h-10 px-3 text-muted-foreground";

const DataTableRowHoverContext = createContext(false);

type DataTableRowProps = ComponentProps<"tr"> & {
  selected?: boolean;
  clickable?: boolean;
};

export function DataTableRow({
  selected = false,
  clickable = false,
  className,
  onMouseEnter,
  onMouseLeave,
  ...props
}: DataTableRowProps) {
  const [hovered, setHovered] = useState(false);

  return (
    <DataTableRowHoverContext.Provider value={hovered}>
      <TableRow
        data-state={selected ? "selected" : undefined}
        data-hovered={hovered ? "true" : undefined}
        className={cn(
          "oma-data-table-row border-0 bg-transparent text-foreground hover:bg-transparent data-[state=selected]:bg-transparent",
          clickable && "cursor-pointer",
          className,
        )}
        onMouseEnter={(event) => {
          setHovered(true);
          onMouseEnter?.(event);
        }}
        onMouseLeave={(event) => {
          setHovered(false);
          onMouseLeave?.(event);
        }}
        {...props}
      />
    </DataTableRowHoverContext.Provider>
  );
}

type DataTableCellProps = ComponentProps<"td"> & {
  edge?: "start" | "end";
};

export function DataTableCell({ edge, className, ...props }: DataTableCellProps) {
  return (
    <TableCell
      className={cn(
        "oma-data-table-cell h-10 px-3 align-middle transition-colors",
        edge === "start" && "rounded-l-lg",
        edge === "end" && "rounded-r-lg",
        className,
      )}
      {...props}
    />
  );
}

export function CopyIdCell({
  value,
  displayValue,
  children,
  ariaLabel,
  className,
  textClassName,
  buttonClassName,
  stopPropagation = false,
  alwaysVisible = false,
}: {
  value: string;
  displayValue?: ReactNode;
  children?: ReactNode;
  ariaLabel: string;
  className?: string;
  textClassName?: string;
  buttonClassName?: string;
  stopPropagation?: boolean;
  alwaysVisible?: boolean;
}) {
  const [copied, setCopied] = useState(false);
  const [keyboardFocused, setKeyboardFocused] = useState(false);
  const resetCopiedTimeoutRef = useRef<number | null>(null);
  const rowHovered = useContext(DataTableRowHoverContext);

  useEffect(() => {
    return () => {
      if (resetCopiedTimeoutRef.current !== null) {
        window.clearTimeout(resetCopiedTimeoutRef.current);
      }
    };
  }, []);

  const handleCopy = async (event: MouseEvent<HTMLButtonElement>) => {
    if (stopPropagation) {
      event.stopPropagation();
    }
    await copyText(value);
    setCopied(true);
    if (resetCopiedTimeoutRef.current !== null) {
      window.clearTimeout(resetCopiedTimeoutRef.current);
    }
    resetCopiedTimeoutRef.current = window.setTimeout(() => {
      setCopied(false);
      resetCopiedTimeoutRef.current = null;
    }, 900);
  };

  return (
    <span className={cn("inline-flex min-w-0 max-w-full items-center gap-2", className)}>
      <span className={cn("min-w-0 truncate font-mono text-xs font-medium text-foreground", textClassName)}>
        {children ?? displayValue ?? value}
      </span>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        aria-label={ariaLabel}
        data-copy-id-button
        data-copied={copied ? "true" : undefined}
        style={{ opacity: alwaysVisible || rowHovered || copied || keyboardFocused ? 1 : 0 }}
        className={cn("oma-copy-id-button shrink-0 text-muted-foreground/70 hover:text-foreground", buttonClassName)}
        onFocus={(event) => {
          setKeyboardFocused(event.currentTarget.matches(":focus-visible"));
        }}
        onBlur={() => setKeyboardFocused(false)}
        onClick={handleCopy}
      >
        {copied ? <Check className="size-3.5" aria-hidden /> : <Copy className="size-3.5" aria-hidden />}
      </Button>
    </span>
  );
}

export function RowIconButton({
  label,
  icon,
  className,
  iconClassName,
  ...props
}: Omit<ComponentProps<typeof Button>, "aria-label" | "children"> & {
  label: string;
  icon: ReactNode;
  iconClassName?: string;
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      aria-label={label}
      className={cn(
        "text-muted-foreground hover:bg-muted hover:text-foreground aria-expanded:bg-muted aria-expanded:text-foreground",
        className,
      )}
      {...props}
    >
      <span className={cn("[&>svg]:size-4", iconClassName)}>{icon}</span>
    </Button>
  );
}

export function MoreActionsButton({
  label,
  className,
  ...props
}: Omit<ComponentProps<typeof Button>, "aria-label" | "children"> & { label: string }) {
  return <RowIconButton label={label} icon={<EllipsisVertical aria-hidden />} className={className} {...props} />;
}

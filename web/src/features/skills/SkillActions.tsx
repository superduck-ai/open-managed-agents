import { useState, type ReactNode } from 'react';
import { MoreHorizontal, Upload, Download, Shapes, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
} from '@/shared/ui/dropdown-menu';
import { skillIconOptions } from './icons';
import { skillErrorMessage } from './errors';

export function SkillActions({
  name,
  iconKey = 'blocks',
  onUpdate,
  onDownload,
  onIcon,
  onDelete,
  children,
}: {
  name: string;
  iconKey?: string;
  onUpdate?: () => void;
  onDownload?: () => Promise<void>;
  onIcon?: (key: string) => Promise<void>;
  onDelete?: () => void;
  children?: ReactNode;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  async function runAction(action: () => Promise<void>) {
    setBusy(true);
    try {
      await action();
      setOpen(false);
    } catch (cause) {
      toast.error(skillErrorMessage(cause, msg));
    } finally {
      setBusy(false);
    }
  }
  return (
    <DropdownMenu
      open={open}
      onOpenChange={(next) => {
        if (!busy) setOpen(next);
      }}
    >
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={msg('skills.moreFor', 'More actions for {name}', { name })}
          />
        }
      >
        <MoreHorizontal size={17} aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" sideOffset={5} className="w-44">
        {onUpdate && (
          <DropdownMenuItem onClick={onUpdate}>
            <Upload size={15} aria-hidden />
            {msg('skills.update', 'Update skill')}
          </DropdownMenuItem>
        )}
        {onDownload && (
          <DropdownMenuItem disabled={busy} onClick={() => void runAction(onDownload)}>
            <Download size={15} aria-hidden />
            {msg('skills.download.action', 'Download skill')}
          </DropdownMenuItem>
        )}
        {onIcon && (
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <Shapes size={15} aria-hidden />
              {msg('skills.icons.change', 'Change icon')}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="skill-icon-popover">
              <div className="skill-icon-grid">
                {skillIconOptions.map(({ key, label, Icon }) => (
                  <DropdownMenuItem
                    key={key}
                    className={iconKey === key ? 'justify-center bg-accent' : 'justify-center'}
                    aria-label={msg(`skills.icons.${key}`, label)}
                    disabled={busy}
                    onClick={() => void runAction(() => onIcon(key))}
                  >
                    <Icon size={16} aria-hidden />
                  </DropdownMenuItem>
                ))}
              </div>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        )}
        {children}
        {onDelete && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onClick={onDelete}>
              <Trash2 size={15} aria-hidden />
              {msg('skills.delete.action', 'Delete skill')}
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

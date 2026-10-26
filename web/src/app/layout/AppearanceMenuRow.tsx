import { Monitor, Moon, Palette, Sun } from 'lucide-react';
import { useI18n } from '../../shared/i18n';
import { useTheme } from '../../shared/theme/context';
import {
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
} from '../../shared/ui/dropdown-menu';

export function AppearanceMenuRow() {
  const { msg } = useI18n();
  const { mode, setMode } = useTheme();
  const options = [
    { value: 'system', Icon: Monitor, label: msg('theme.system', 'System') },
    { value: 'light', Icon: Sun, label: msg('theme.light', 'Light') },
    { value: 'dark', Icon: Moon, label: msg('theme.dark', 'Dark') },
  ] as const;
  return (
    <DropdownMenuGroup className="flex min-h-9 items-center gap-3 px-3 py-1">
      <Palette className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      <DropdownMenuLabel className="min-w-0 flex-1 p-0 text-sm font-normal text-foreground">
        {msg('nav.appearance', 'Appearance')}
      </DropdownMenuLabel>
      <DropdownMenuRadioGroup
        value={mode}
        aria-label={msg('nav.appearance', 'Appearance')}
        className="flex shrink-0 gap-0.5 rounded-lg bg-muted p-0.5"
      >
        {options.map(({ value, Icon, label }) => (
          <DropdownMenuRadioItem
            key={value}
            value={value}
            aria-label={label}
            title={label}
            closeOnClick={false}
            onClick={() => setMode(value)}
            className="size-7 justify-center p-0 text-muted-foreground data-checked:bg-background data-checked:text-foreground data-checked:shadow-sm [&>[data-slot=dropdown-menu-radio-item-indicator]]:hidden"
          >
            <Icon className="size-4" aria-hidden />
          </DropdownMenuRadioItem>
        ))}
      </DropdownMenuRadioGroup>
    </DropdownMenuGroup>
  );
}

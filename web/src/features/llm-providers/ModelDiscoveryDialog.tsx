import { useState } from 'react';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Checkbox } from '../../shared/ui/checkbox';
import { DialogFooter } from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';

export function ModelDiscoveryDialog({
  models,
  onClose,
  onAccept,
}: {
  models: string[];
  onClose: () => void;
  onAccept: (ids: string[]) => void;
}) {
  const { msg } = useI18n();
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState(new Set(models));
  const visible = models.filter((id) => id.toLowerCase().includes(query.toLowerCase()));
  return (
    <div className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] gap-4 px-4 pb-4">
      <Input
        autoFocus
        aria-label={msg('llmModels.searchPlaceholder', 'Search models')}
        placeholder={msg('llmModels.searchPlaceholder', 'Search models')}
        value={query}
        onChange={(event) => setQuery(event.target.value)}
      />
      <div className="subtle-scrollbar-auto grid min-h-0 max-h-80 gap-3 overflow-y-auto py-2">
        {visible.map((id) => (
          <label key={id} className="flex min-w-0 items-center gap-3 text-sm">
            <Checkbox
              checked={selected.has(id)}
              onCheckedChange={(checked) =>
                setSelected((current) => {
                  const next = new Set(current);
                  if (checked) next.add(id);
                  else next.delete(id);
                  return next;
                })
              }
            />
            <span className="break-all font-mono">{id}</span>
          </label>
        ))}
        {!visible.length ? (
          <p className="text-sm text-muted-foreground">{msg('select.empty', 'No matching options.')}</p>
        ) : null}
      </div>
      <DialogFooter>
        <Button
          type="button"
          variant="outline"
          onClick={() => setSelected(selected.size ? new Set() : new Set(models))}
        >
          {msg('llmModels.toggleSelection', 'Select / clear all')}
        </Button>
        <Button type="button" variant="outline" onClick={onClose}>
          {msg('common.cancel', 'Cancel')}
        </Button>
        <Button
          type="button"
          disabled={!selected.size}
          onClick={() => onAccept(models.filter((id) => selected.has(id)))}
        >
          {msg('llmModels.addSelected', 'Add selected models')}
        </Button>
      </DialogFooter>
    </div>
  );
}

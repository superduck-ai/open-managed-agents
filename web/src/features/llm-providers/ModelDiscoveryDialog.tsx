import { useState } from 'react';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Checkbox } from '../../shared/ui/checkbox';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '../../shared/ui/dialog';
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
  const [open, setOpen] = useState(true);
  const visible = models.filter((id) => id.toLowerCase().includes(query.toLowerCase()));
  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      onOpenChangeComplete={(value) => {
        if (!value) onClose();
      }}
    >
      <DialogContent className="max-h-[calc(100dvh-4rem)] grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden">
        <DialogHeader>
          <DialogTitle>{msg('llmModels.chooseModels', 'Select models')}</DialogTitle>
        </DialogHeader>
        <Input
          aria-label={msg('llmModels.searchPlaceholder', 'Search models')}
          placeholder={msg('llmModels.searchPlaceholder', 'Search models')}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        <div className="subtle-scrollbar-auto grid min-h-0 gap-3 overflow-y-auto py-2">
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
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => setSelected(selected.size ? new Set() : new Set(models))}
          >
            {msg('llmModels.toggleSelection', 'Select / clear all')}
          </Button>
          <Button type="button" variant="outline" onClick={() => setOpen(false)}>
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
      </DialogContent>
    </Dialog>
  );
}

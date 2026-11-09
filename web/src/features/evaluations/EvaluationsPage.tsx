import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { DatasetCreateDialog } from './DatasetCreateDialog';
import { DatasetPanel } from './DatasetPanel';

export function EvaluationsPage() {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  return (
    <section className="relative min-h-[calc(100vh-48px)] bg-background text-foreground">
      <header className="mb-4 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 className="text-2xl leading-8 font-medium">{msg('evaluations.datasets', 'Datasets')}</h1>
          <p className="mt-2 max-w-[720px] text-sm leading-5 text-muted-foreground">
            {msg('evaluations.datasetDescription', 'Manage reusable cases for checking agent behavior.')}
          </p>
        </div>
        <Button type="button" onClick={() => setOpen(true)}>
          {msg('evaluations.createDataset', 'Create dataset')}
        </Button>
      </header>
      <DatasetPanel onCreate={() => setOpen(true)} />
      <DatasetCreateDialog open={open} onOpenChange={setOpen} />
    </section>
  );
}

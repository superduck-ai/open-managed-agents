import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { DatasetCreateDialog } from './DatasetCreateDialog';
import { DatasetPanel } from './DatasetPanel';
import { RubricCreateDialog } from './RubricCreateDialog';
import { RubricPanel } from './RubricPanel';

type EvaluationTab = 'datasets' | 'rubrics';

export function EvaluationsPage() {
  const { msg } = useI18n();
  const [tab, setTab] = useState<EvaluationTab>('datasets');
  const [createKind, setCreateKind] = useState<EvaluationTab | null>(null);
  return (
    <section className="relative min-h-[calc(100vh-48px)] bg-background text-foreground">
      <header className="mb-4 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 className="text-2xl leading-8 font-medium">{pageTitle(tab, msg)}</h1>
          <p className="mt-2 max-w-[720px] text-sm leading-5 text-muted-foreground">{pageDescription(tab, msg)}</p>
        </div>
        <Button type="button" onClick={() => setCreateKind(tab)}>
          {tab === 'rubrics'
            ? msg('evaluations.createRubric', 'Create rubric')
            : msg('evaluations.createDataset', 'Create dataset')}
        </Button>
      </header>
      <Tabs value={tab} onValueChange={(value) => setTab(value === 'rubrics' ? 'rubrics' : 'datasets')}>
        <TabsList variant="line" className="mb-4">
          <TabsTrigger value="datasets">{msg('evaluations.datasets', 'Datasets')}</TabsTrigger>
          <TabsTrigger value="rubrics">{msg('evaluations.rubrics', 'Rubrics')}</TabsTrigger>
        </TabsList>
        <TabsContent value="datasets">
          <DatasetPanel onCreate={() => setCreateKind('datasets')} />
        </TabsContent>
        <TabsContent value="rubrics">
          <RubricPanel onCreate={() => setCreateKind('rubrics')} />
        </TabsContent>
      </Tabs>
      <DatasetCreateDialog open={createKind === 'datasets'} onOpenChange={(open) => !open && setCreateKind(null)} />
      <RubricCreateDialog open={createKind === 'rubrics'} onOpenChange={(open) => !open && setCreateKind(null)} />
    </section>
  );
}

function pageTitle(tab: EvaluationTab, msg: (id: string, fallback: string) => string) {
  if (tab === 'rubrics') return msg('evaluations.rubrics', 'Rubrics');
  return msg('evaluations.datasets', 'Datasets');
}

function pageDescription(tab: EvaluationTab, msg: (id: string, fallback: string) => string) {
  if (tab === 'rubrics') {
    return msg('evaluations.rubricDescription', 'Define scoring rules so results stay comparable.');
  }
  return msg('evaluations.datasetDescription', 'Manage reusable cases for checking agent behavior.');
}

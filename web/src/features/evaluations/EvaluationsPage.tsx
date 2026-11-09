import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { DatasetPanel } from './DatasetPanel';
import { EvaluationCreateDialog, type EvaluationCreateKind } from './EvaluationDialogs';
import { RubricPanel } from './RubricPanel';
import { TaskPanel } from './TaskPanel';

type EvaluationTab = 'tasks' | 'datasets' | 'rubrics';

export function EvaluationsPage() {
  const { msg } = useI18n();
  const [tab, setTab] = useState<EvaluationTab>('tasks');
  const [createKind, setCreateKind] = useState<EvaluationCreateKind | null>(null);
  return (
    <section className="relative min-h-[calc(100vh-48px)] bg-background text-foreground">
      <header className="mb-4 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 className="text-2xl leading-8 font-medium">{pageTitle(tab, msg)}</h1>
          <p className="mt-2 max-w-[720px] text-sm leading-5 text-muted-foreground">{pageDescription(tab, msg)}</p>
        </div>
        <PageActions tab={tab} onCreate={setCreateKind} />
      </header>
      <Tabs
        value={tab}
        onValueChange={(value) => setTab(value === 'datasets' || value === 'rubrics' ? value : 'tasks')}
      >
        <TabsList variant="line" className="mb-4">
          <TabsTrigger value="tasks">{msg('evaluations.tasks', 'Tasks')}</TabsTrigger>
          <TabsTrigger value="datasets">{msg('evaluations.datasets', 'Datasets')}</TabsTrigger>
          <TabsTrigger value="rubrics">{msg('evaluations.rubrics', 'Rubrics')}</TabsTrigger>
        </TabsList>
        <TabsContent value="tasks">
          <TaskPanel onCreate={() => setCreateKind('evaluate')} />
        </TabsContent>
        <TabsContent value="datasets">
          <DatasetPanel onCreate={() => setCreateKind('dataset')} />
        </TabsContent>
        <TabsContent value="rubrics">
          <RubricPanel onCreate={() => setCreateKind('rubric')} />
        </TabsContent>
      </Tabs>
      <EvaluationCreateDialog
        kind={createKind}
        open={createKind !== null}
        onOpenChange={(open) => !open && setCreateKind(null)}
      />
    </section>
  );
}

function PageActions({ tab, onCreate }: { tab: EvaluationTab; onCreate: (kind: EvaluationCreateKind) => void }) {
  const { msg } = useI18n();
  if (tab === 'datasets') {
    return (
      <Button type="button" onClick={() => onCreate('dataset')}>
        {msg('evaluations.createDataset', 'Create dataset')}
      </Button>
    );
  }
  if (tab === 'rubrics') {
    return (
      <Button type="button" onClick={() => onCreate('rubric')}>
        {msg('evaluations.createRubric', 'Create rubric')}
      </Button>
    );
  }
  return (
    <Button type="button" onClick={() => onCreate('evaluate')}>
      {msg('evaluations.createTask', 'New task')}
    </Button>
  );
}

function pageTitle(tab: EvaluationTab, msg: (id: string, fallback: string) => string) {
  if (tab === 'datasets') return msg('evaluations.datasets', 'Datasets');
  if (tab === 'rubrics') return msg('evaluations.rubrics', 'Rubrics');
  return msg('evaluations.title', 'Evaluation');
}

function pageDescription(tab: EvaluationTab, msg: (id: string, fallback: string) => string) {
  if (tab === 'datasets') {
    return msg('evaluations.datasetDescription', 'Manage reusable cases for checking agent behavior.');
  }
  if (tab === 'rubrics') {
    return msg('evaluations.rubricDescription', 'Define scoring rules so results stay comparable.');
  }
  return msg('evaluations.description', 'Score an agent against a dataset and a rubric.');
}

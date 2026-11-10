import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { DatasetCreateDialog } from './DatasetCreateDialog';
import { DatasetPanel } from './DatasetPanel';
import { EvaluationCreateDialog } from './EvaluationDialogs';
import { RubricCreateDialog } from './RubricCreateDialog';
import { RubricPanel } from './RubricPanel';
import { TaskPanel } from './TaskPanel';

type EvaluationTab = 'tasks' | 'datasets' | 'rubrics';

export function EvaluationsPage() {
  const { msg } = useI18n();
  const [tab, setTab] = useState<EvaluationTab>('tasks');
  const [creatingDataset, setCreatingDataset] = useState(false);
  const [creatingRubric, setCreatingRubric] = useState(false);
  const [creatingTask, setCreatingTask] = useState(false);
  return (
    <section className="relative min-h-[calc(100vh-48px)] bg-background text-foreground">
      <header className="mb-4 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 className="text-2xl leading-8 font-medium">{pageTitle(tab, msg)}</h1>
          <p className="mt-2 max-w-[720px] text-sm leading-5 text-muted-foreground">{pageDescription(tab, msg)}</p>
        </div>
        <Button type="button" onClick={() => openCreate(tab, setCreatingDataset, setCreatingRubric, setCreatingTask)}>
          {createLabel(tab, msg)}
        </Button>
      </header>
      <Tabs value={tab} onValueChange={(value) => setTab(evaluationTab(value))}>
        <TabsList variant="line" className="mb-4">
          <TabsTrigger value="tasks">{msg('evaluations.tasks', 'Tasks')}</TabsTrigger>
          <TabsTrigger value="datasets">{msg('evaluations.datasets', 'Datasets')}</TabsTrigger>
          <TabsTrigger value="rubrics">{msg('evaluations.rubrics', 'Rubrics')}</TabsTrigger>
        </TabsList>
        <TabsContent value="tasks">
          <TaskPanel onCreate={() => setCreatingTask(true)} />
        </TabsContent>
        <TabsContent value="datasets">
          <DatasetPanel onCreate={() => setCreatingDataset(true)} />
        </TabsContent>
        <TabsContent value="rubrics">
          <RubricPanel onCreate={() => setCreatingRubric(true)} />
        </TabsContent>
      </Tabs>
      <DatasetCreateDialog open={creatingDataset} onOpenChange={setCreatingDataset} />
      <RubricCreateDialog open={creatingRubric} onOpenChange={setCreatingRubric} />
      <EvaluationCreateDialog kind="evaluate" open={creatingTask} onOpenChange={setCreatingTask} />
    </section>
  );
}

function evaluationTab(value: string): EvaluationTab {
  if (value === 'datasets' || value === 'rubrics') return value;
  return 'tasks';
}

function openCreate(
  tab: EvaluationTab,
  setDataset: (open: boolean) => void,
  setRubric: (open: boolean) => void,
  setTask: (open: boolean) => void,
) {
  if (tab === 'datasets') setDataset(true);
  else if (tab === 'rubrics') setRubric(true);
  else setTask(true);
}

function createLabel(tab: EvaluationTab, msg: (id: string, fallback: string) => string) {
  if (tab === 'datasets') return msg('evaluations.createDataset', 'Create dataset');
  if (tab === 'rubrics') return msg('evaluations.createRubric', 'Create rubric');
  return msg('evaluations.createTask', 'New task');
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

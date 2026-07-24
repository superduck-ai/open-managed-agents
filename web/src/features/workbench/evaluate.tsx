import { AlertCircle, ChevronDown, Code2, Loader2, Plus, Sparkles, Trash2, X } from 'lucide-react';
import { ChangeEvent, Dispatch, SetStateAction, useEffect, useRef, useState } from 'react';
import clsx from 'clsx';
import {
  listWorkbenchRevisions,
  WorkbenchEvaluation,
  WorkbenchMessage,
  WorkbenchRevision,
  WorkbenchStreamEvent,
} from './api';
import {
  buildRevisionPayload,
  EvaluateComparison,
  EvaluateComparisonOutput,
  EvaluateTestCase,
  formatDate,
  generatedValueString,
  hasOwn,
  normalizeRevision,
  titleMessageContent,
  WorkbenchExample,
  workbenchId,
} from './model';
import { Alert, AlertDescription, AlertTitle } from '@/shared/ui/alert';
import { Button } from '@/shared/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/shared/ui/dropdown-menu';
import { Switch } from '@/shared/ui/switch';
import { Textarea } from '@/shared/ui/textarea';

export function ResponsePreview({
  isRunning,
  error,
  responseText,
  showCreatePrompt,
  onCreatePrompt,
}: {
  isRunning: boolean;
  error: string | null;
  responseText: string;
  showCreatePrompt: boolean;
  onCreatePrompt: () => void | Promise<void>;
}) {
  if (error) {
    return (
      <Alert variant="destructive">
        <AlertCircle aria-hidden />
        <AlertTitle>Request failed</AlertTitle>
        <AlertDescription className="whitespace-pre-wrap break-words leading-6">{error}</AlertDescription>
      </Alert>
    );
  }
  if (responseText) {
    return <div className="whitespace-pre-wrap break-words text-sm leading-6 text-foreground">{responseText}</div>;
  }
  return (
    <div className="workbench-empty-response">
      {isRunning ? (
        <span className="flex items-center gap-2">
          <Loader2 className="size-4 animate-spin" aria-hidden />
          Running
        </span>
      ) : (
        <span className="workbench-empty-response-content">
          <span>Run prompt to see the selected model's response from the configured AI gateway</span>
          {showCreatePrompt ? (
            <>
              <span className="workbench-empty-response-or">or</span>
              <Button
                type="button"
                variant="outline"
                size="lg"
                className="bg-secondary"
                onClick={() => void onCreatePrompt()}
              >
                Create New Prompt
              </Button>
            </>
          ) : null}
        </span>
      )}
    </div>
  );
}

export function EvaluateView({
  variables,
  promptTitle,
  promptText,
  rows,
  setRows,
  comparisons,
  setComparisons,
  orgUuid,
  promptId,
  currentRevisionId,
  fallbackModelName,
  isReadOnly,
  saveRevisionLabel,
  canSaveRevision,
  onSaveRevision,
  onCreateRow,
  onUpdateVariables,
  onUpdateIdealOutput,
  onDeleteRow,
  onGenerateTestCases,
}: {
  variables: string[];
  promptTitle: string;
  promptText: string;
  rows: EvaluateTestCase[];
  setRows: Dispatch<SetStateAction<EvaluateTestCase[]>>;
  comparisons: EvaluateComparison[];
  setComparisons: Dispatch<SetStateAction<EvaluateComparison[]>>;
  orgUuid?: string;
  promptId?: string;
  currentRevisionId?: string;
  fallbackModelName: string;
  isReadOnly: boolean;
  saveRevisionLabel: string;
  canSaveRevision: boolean;
  onSaveRevision: () => void;
  onCreateRow: (row: EvaluateTestCase) => Promise<void>;
  onUpdateVariables: (row: EvaluateTestCase) => Promise<void>;
  onUpdateIdealOutput: (row: EvaluateTestCase) => Promise<void>;
  onDeleteRow: (row: EvaluateTestCase) => Promise<void>;
  onGenerateTestCases: (count: number) => Promise<void>;
}) {
  const [showPrompt, setShowPrompt] = useState(false);
  const [showIdealOutputs, setShowIdealOutputs] = useState(false);
  const [generateMenuOpen, setGenerateMenuOpen] = useState(false);
  const [comparisonMenuOpen, setComparisonMenuOpen] = useState(false);
  const [comparisonRevisions, setComparisonRevisions] = useState<WorkbenchRevision[]>([]);
  const [isLoadingComparisons, setIsLoadingComparisons] = useState(false);
  const [isGeneratingTestCases, setIsGeneratingTestCases] = useState(false);
  const importInputRef = useRef<HTMLInputElement | null>(null);
  const visibleVariables = variables.length ? variables : ['VARIABLE'];
  const variableLabel = visibleVariables.length === 1 ? `{{${visibleVariables[0]}}}` : 'Variables';
  const availableComparisonRevisions = comparisonRevisions.filter(
    (revision) =>
      revision.id !== currentRevisionId && !comparisons.some((comparison) => comparison.revisionId === revision.id),
  );
  useEffect(() => {
    if (!isReadOnly) {
      return;
    }
    setComparisonMenuOpen(false);
    setGenerateMenuOpen(false);
  }, [isReadOnly]);
  const columnTemplate = [
    '26px',
    showPrompt ? 'minmax(260px, 1fr)' : null,
    'minmax(220px, 0.9fr)',
    'minmax(320px, 1.2fr)',
    ...comparisons.map(() => 'minmax(320px, 1.2fr)'),
    showIdealOutputs ? 'minmax(260px, 1fr)' : null,
    '148px',
  ]
    .filter(Boolean)
    .join(' ');
  const handleComparisonMenuOpenChange = async (nextOpen: boolean) => {
    setComparisonMenuOpen(nextOpen);
    if (!nextOpen || isReadOnly) {
      return;
    }
    if (!orgUuid || !promptId || comparisonRevisions.length || isLoadingComparisons) {
      return;
    }
    setIsLoadingComparisons(true);
    try {
      setComparisonRevisions(
        (await listWorkbenchRevisions(orgUuid, promptId, false)).map((revision) =>
          normalizeRevision(revision, fallbackModelName),
        ),
      );
    } catch {
      setComparisonRevisions([]);
    } finally {
      setIsLoadingComparisons(false);
    }
  };
  const addComparison = (revision: WorkbenchRevision) => {
    if (isReadOnly) {
      return;
    }
    const revisionIndex = comparisonRevisions.findIndex((item) => item.id === revision.id);
    const comparison: EvaluateComparison = {
      id: revision.id,
      revisionId: revision.id,
      label: revisionIndex >= 0 ? `v${comparisonRevisions.length - revisionIndex}` : 'Version',
      revision,
    };
    setComparisons((current) =>
      current.some((item) => item.revisionId === revision.id) ? current : [...current, comparison],
    );
    setRows((current) =>
      current.map((row) => ({
        ...row,
        comparisonOutputs: {
          ...row.comparisonOutputs,
          [comparison.id]: row.comparisonOutputs[comparison.id] ?? emptyComparisonOutput(),
        },
      })),
    );
    setComparisonMenuOpen(false);
  };
  const removeComparison = (comparisonId: string) => {
    if (isReadOnly) {
      return;
    }
    setComparisons((current) => current.filter((comparison) => comparison.id !== comparisonId));
    setRows((current) =>
      current.map((row) => {
        const { [comparisonId]: _removed, ...comparisonOutputs } = row.comparisonOutputs;
        return { ...row, comparisonOutputs };
      }),
    );
  };
  const addRows = (count: number) => {
    if (isReadOnly) {
      return;
    }
    const createdRows = Array.from({ length: count }, () => createEvaluateRow(visibleVariables));
    setRows((current) => [...current, ...createdRows]);
    createdRows.forEach((row) => void onCreateRow(row));
    setGenerateMenuOpen(false);
  };
  const generateRows = async (count: number) => {
    if (isReadOnly) {
      return;
    }
    setIsGeneratingTestCases(true);
    setGenerateMenuOpen(false);
    try {
      await onGenerateTestCases(count);
    } finally {
      setIsGeneratingTestCases(false);
    }
  };
  const updateRowValue = (row: EvaluateTestCase, variable: string, value: string) => {
    if (isReadOnly) {
      return;
    }
    const nextRow = { ...row, values: { ...row.values, [variable]: value } };
    setRows((current) => current.map((item) => (item.id === row.id ? { ...item, values: nextRow.values } : item)));
    void onUpdateVariables(nextRow);
  };
  const updateIdealOutput = (row: EvaluateTestCase, value: string) => {
    if (isReadOnly) {
      return;
    }
    const nextRow = { ...row, idealOutput: value };
    setRows((current) => current.map((item) => (item.id === row.id ? { ...item, idealOutput: value } : item)));
    void onUpdateIdealOutput(nextRow);
  };
  const removeRow = (row: EvaluateTestCase) => {
    if (isReadOnly) {
      return;
    }
    setRows((current) => current.filter((item) => item.id !== row.id));
    void onDeleteRow(row);
  };
  const importRows = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.currentTarget.files?.[0];
    event.currentTarget.value = '';
    if (!file || isReadOnly) {
      return;
    }
    const importedRows = parseEvaluateCsv(await file.text(), visibleVariables);
    if (!importedRows.length) {
      return;
    }
    setRows(importedRows);
    importedRows.forEach((row) => void onCreateRow(row));
    if (importedRows.some((row) => row.idealOutput.trim())) {
      setShowIdealOutputs(true);
    }
  };
  const exportRows = () => {
    if (!rows.length) {
      return;
    }
    const blob = new Blob([evaluateRowsToCsv(rows, visibleVariables)], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `${slugifyFileName(promptTitle || 'workbench')}-test-cases.csv`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="workbench-evaluate-canvas">
      <div className="workbench-evaluate-controls">
        <div className="workbench-evaluate-toggle">
          <span>Show Prompt</span>
          <Switch
            aria-label="Show Prompt"
            checked={showPrompt}
            onCheckedChange={(nextChecked) => setShowPrompt(nextChecked)}
          />
        </div>
        <div className="workbench-evaluate-toggle">
          <span>Show Ideal Outputs</span>
          <Switch
            aria-label="Show Ideal Outputs"
            checked={showIdealOutputs}
            onCheckedChange={(nextChecked) => setShowIdealOutputs(nextChecked)}
          />
        </div>
      </div>

      {isReadOnly ? (
        <div className="workbench-evaluate-stale-card">
          <p>
            Prompt has been updated, and the evaluation table below has become stale and is read-only. Create a new
            revision with your changes to evaluate.
          </p>
          <Button type="button" className="mx-auto mt-4" disabled={!canSaveRevision} onClick={onSaveRevision}>
            Save Changes as {saveRevisionLabel}
          </Button>
        </div>
      ) : null}

      <div className="workbench-evaluate-table" aria-label="Evaluation results">
        <div
          className="workbench-evaluate-row workbench-evaluate-version-row"
          style={{ gridTemplateColumns: columnTemplate }}
        >
          <div className="workbench-evaluate-row-label">Row</div>
          {showPrompt ? <div /> : null}
          <div />
          <div>
            <span className="workbench-evaluate-version-chip">
              <span>{promptTitle}</span>
              <span>v1</span>
            </span>
          </div>
          {comparisons.map((comparison) => (
            <div key={comparison.id}>
              <span className="workbench-evaluate-version-chip">
                <span>{promptTitle}</span>
                <span>{comparison.label}</span>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="size-8 rounded-none border-l border-border text-muted-foreground hover:bg-accent hover:text-foreground"
                  aria-label={`Remove ${comparison.label} comparison`}
                  onClick={() => removeComparison(comparison.id)}
                >
                  <X className="size-3.5" aria-hidden />
                </Button>
              </span>
            </div>
          ))}
          {showIdealOutputs ? <div /> : null}
          <div>
            <div className="workbench-evaluate-comparison-anchor">
              <DropdownMenu
                open={comparisonMenuOpen}
                onOpenChange={(nextOpen) => void handleComparisonMenuOpenChange(nextOpen)}
              >
                <DropdownMenuTrigger
                  render={
                    <Button
                      type="button"
                      variant="ghost"
                      className="h-8 px-0 text-[13px] font-normal text-foreground hover:bg-transparent hover:text-foreground disabled:text-muted-foreground"
                      aria-label="Add comparison"
                      aria-expanded={comparisonMenuOpen}
                      disabled={isReadOnly || !orgUuid || !promptId}
                    />
                  }
                >
                  {isLoadingComparisons ? (
                    <Loader2 className="size-4 animate-spin" aria-hidden />
                  ) : (
                    <Plus className="size-4" aria-hidden />
                  )}
                  Add Comparison
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" side="bottom" sideOffset={8} className="w-auto min-w-[238px]">
                  {isLoadingComparisons ? (
                    <div className="workbench-evaluate-comparison-empty">Loading versions</div>
                  ) : availableComparisonRevisions.length ? (
                    availableComparisonRevisions.map((revision) => {
                      const index = comparisonRevisions.findIndex((item) => item.id === revision.id);
                      return (
                        <DropdownMenuItem
                          key={revision.id}
                          className="grid gap-0.5 px-2.5 py-[9px] text-left"
                          onClick={() => addComparison(revision)}
                        >
                          <span className="text-[13px] font-semibold leading-[18px]">
                            {index >= 0 ? `Version ${comparisonRevisions.length - index}` : 'Version'}
                          </span>
                          <span className="text-xs leading-4 text-muted-foreground">
                            {formatDate(revision.created_at)}
                          </span>
                        </DropdownMenuItem>
                      );
                    })
                  ) : (
                    <div className="workbench-evaluate-comparison-empty">No saved versions to compare</div>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </div>
        </div>
        <div className="workbench-evaluate-row workbench-evaluate-head" style={{ gridTemplateColumns: columnTemplate }}>
          <div />
          {showPrompt ? <div>Prompt</div> : null}
          <div>{variableLabel}</div>
          <div>Model output</div>
          {comparisons.map((comparison) => (
            <div key={comparison.id}>Model output</div>
          ))}
          {showIdealOutputs ? <div>Ideal output</div> : null}
          <div>Actions</div>
        </div>
        {rows.length ? (
          rows.map((row, rowIndex) => (
            <div
              key={row.id}
              className="workbench-evaluate-row workbench-evaluate-data-row"
              style={{ gridTemplateColumns: columnTemplate }}
            >
              <div className="workbench-evaluate-row-label">{rowIndex + 1}</div>
              {showPrompt ? <div className="workbench-evaluate-prompt-cell">{promptText || 'Prompt'}</div> : null}
              <div className="workbench-evaluate-variable-cell">
                {visibleVariables.map((name) => (
                  <label key={name} className="workbench-evaluate-variable-field">
                    <span>{visibleVariables.length === 1 ? variableLabel : `{{${name}}}`}</span>
                    <Textarea
                      aria-label={`${name} row ${rowIndex + 1}`}
                      value={row.values[name] ?? ''}
                      onChange={(event) => updateRowValue(row, name, event.currentTarget.value)}
                      placeholder="Enter an example value..."
                      disabled={isReadOnly}
                      className="min-h-[58px] resize-y rounded-md bg-secondary px-2.5 py-2 text-[13px] leading-[18px] text-foreground disabled:bg-secondary disabled:text-muted-foreground disabled:opacity-100"
                    />
                  </label>
                ))}
              </div>
              <EvaluateOutputCell output={row.modelOutput} error={row.runError} isRunning={row.isRunning} />
              {comparisons.map((comparison) => {
                const output = row.comparisonOutputs[comparison.id] ?? emptyComparisonOutput();
                return (
                  <EvaluateOutputCell
                    key={comparison.id}
                    output={output.modelOutput}
                    error={output.runError}
                    isRunning={output.isRunning}
                  />
                );
              })}
              {showIdealOutputs ? (
                <div>
                  <Textarea
                    aria-label={`Ideal output row ${rowIndex + 1}`}
                    className="min-h-[58px] resize-y rounded-md bg-secondary px-2.5 py-2 text-[13px] leading-[18px] text-foreground disabled:bg-secondary disabled:text-muted-foreground disabled:opacity-100"
                    value={row.idealOutput}
                    onChange={(event) => updateIdealOutput(row, event.currentTarget.value)}
                    placeholder="Enter ideal output..."
                    disabled={isReadOnly}
                  />
                </div>
              ) : null}
              <div>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="size-8 text-muted-foreground hover:bg-accent hover:text-foreground disabled:text-muted-foreground/70"
                  aria-label={`Delete row ${rowIndex + 1}`}
                  disabled={isReadOnly}
                  onClick={() => removeRow(row)}
                >
                  <Trash2 className="size-4" aria-hidden />
                </Button>
              </div>
            </div>
          ))
        ) : (
          <div className="workbench-evaluate-empty-state">
            <div className="workbench-evaluate-empty-icon" aria-hidden>
              <Code2 className="size-7" />
            </div>
            <h2>No test cases</h2>
            <p>Add, generate or import test cases using the buttons below to start evaluating prompts.</p>
          </div>
        )}
      </div>

      <div className="workbench-evaluate-actions">
        <Button type="button" variant="outline" disabled={isReadOnly} onClick={() => addRows(1)}>
          <Plus className="size-4" aria-hidden />
          Add Row
        </Button>
        <div className="workbench-evaluate-generate">
          <Button
            type="button"
            variant="outline"
            className="rounded-r-none bg-card"
            disabled={isReadOnly || isGeneratingTestCases}
            onClick={() => void generateRows(1)}
          >
            {isGeneratingTestCases ? (
              <Loader2 className="size-4 animate-spin" aria-hidden />
            ) : (
              <Sparkles className="size-4" aria-hidden />
            )}
            {isGeneratingTestCases ? 'Generating' : 'Generate Test Case'}
          </Button>
          <DropdownMenu open={generateMenuOpen} onOpenChange={setGenerateMenuOpen}>
            <DropdownMenuTrigger
              render={
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  className="size-8 rounded-l-none border-l-0 bg-card"
                  aria-label="Generate test case options"
                  aria-expanded={generateMenuOpen}
                  disabled={isReadOnly || isGeneratingTestCases}
                />
              }
            >
              <ChevronDown className="size-4" aria-hidden />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" side="top" sideOffset={6} className="w-auto min-w-[180px]">
              <DropdownMenuItem className="px-2 py-1.5 text-sm" onClick={() => void generateRows(1)}>
                Generate 1 test case
              </DropdownMenuItem>
              <DropdownMenuItem className="px-2 py-1.5 text-sm" onClick={() => void generateRows(5)}>
                Generate 5 test cases
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <Button type="button" variant="outline" disabled={isReadOnly} onClick={() => importInputRef.current?.click()}>
          Import Test Cases
        </Button>
        <input
          ref={importInputRef}
          type="file"
          accept=".csv,text/csv"
          aria-hidden="true"
          tabIndex={-1}
          data-testid="workbench-evaluate-import-input"
          className="workbench-hidden-file-input"
          onChange={(event) => void importRows(event)}
        />
        <Button type="button" variant="outline" disabled={!rows.length} onClick={exportRows}>
          Export to CSV
        </Button>
      </div>
    </div>
  );
}

export function EvaluateOutputCell({
  output,
  error,
  isRunning,
}: {
  output: string;
  error: string | null;
  isRunning: boolean;
}) {
  return (
    <div
      className={clsx(
        'workbench-evaluate-output-cell',
        output && 'has-output',
        error && 'has-error',
        isRunning && 'is-running',
      )}
    >
      {isRunning ? (
        <span>
          <Loader2 className="size-4 animate-spin" aria-hidden />
          Running
        </span>
      ) : error ? (
        error
      ) : output ? (
        output
      ) : (
        'Run All to generate model output.'
      )}
    </div>
  );
}

export function createEvaluateRow(variables: string[], sampleIndex?: number): EvaluateTestCase {
  const values = Object.fromEntries(
    variables.map((name) => [name, sampleIndex ? `${name} sample ${sampleIndex}` : '']),
  );
  return createEvaluateRowFromValues(variables, values);
}

export function createEvaluateRowFromValues(variables: string[], values: Record<string, string>): EvaluateTestCase {
  const id = workbenchId('eval-local');
  return {
    id,
    evaluationId: id,
    testCaseId: id,
    values: Object.fromEntries(variables.map((name) => [name, values[name] ?? ''])),
    idealOutput: '',
    modelOutput: '',
    rating: '',
    runError: null,
    isRunning: false,
    comparisonOutputs: {},
  };
}

export function emptyComparisonOutput(): EvaluateComparisonOutput {
  return {
    modelOutput: '',
    rating: '',
    runError: null,
    isRunning: false,
  };
}

export function evaluateRowsFromEvaluations(evaluations: WorkbenchEvaluation[], variables: string[]) {
  return evaluations
    .map((evaluation) => evaluateRowFromEvaluation(evaluation, variables))
    .filter((row): row is EvaluateTestCase => Boolean(row));
}

export function evaluateRowFromEvaluation(evaluation: WorkbenchEvaluation, variables: string[]) {
  const evaluationId = generatedValueString(evaluation.id);
  if (!evaluationId.trim()) {
    return null;
  }
  const rawValues =
    evaluation.variable_values && typeof evaluation.variable_values === 'object' ? evaluation.variable_values : {};
  const row = createEvaluateRowFromValues(
    variables,
    Object.fromEntries(variables.map((name) => [name, generatedValueString(rawValues[name])])),
  );
  return mergeEvaluationIntoRow(
    {
      ...row,
      id: evaluationId,
      evaluationId,
      testCaseId: generatedValueString(evaluation.test_case_id).trim() || evaluationId,
    },
    evaluation,
    variables,
  );
}

export function mergeEvaluationIntoRow(row: EvaluateTestCase, evaluation: WorkbenchEvaluation, variables: string[]) {
  const rawValues =
    evaluation.variable_values && typeof evaluation.variable_values === 'object'
      ? evaluation.variable_values
      : row.values;
  return {
    ...row,
    evaluationId: generatedValueString(evaluation.id).trim() || row.evaluationId,
    testCaseId: generatedValueString(evaluation.test_case_id).trim() || row.testCaseId,
    values: Object.fromEntries(
      variables.map((name) => [
        name,
        hasOwn(rawValues, name) ? generatedValueString(rawValues[name]) : row.values[name] || '',
      ]),
    ),
    idealOutput: hasOwn(evaluation, 'golden_answer') ? generatedValueString(evaluation.golden_answer) : row.idealOutput,
    modelOutput: hasOwn(evaluation, 'completion_text')
      ? generatedValueString(evaluation.completion_text)
      : hasOwn(evaluation, 'completion')
        ? generatedValueString(evaluation.completion)
        : row.modelOutput,
    rating: hasOwn(evaluation, 'rating') ? generatedValueString(evaluation.rating) : row.rating,
  };
}

export function mergeCreatedEvaluationIntoRow(row: EvaluateTestCase, evaluation: WorkbenchEvaluation) {
  return {
    ...row,
    evaluationId: generatedValueString(evaluation.id).trim() || row.evaluationId,
    testCaseId: generatedValueString(evaluation.test_case_id).trim() || row.testCaseId,
    rating: hasOwn(evaluation, 'rating') ? generatedValueString(evaluation.rating) : row.rating,
  };
}

export function mergeEvaluationVariablesIntoRow(
  row: EvaluateTestCase,
  evaluation: WorkbenchEvaluation,
  variables: string[],
) {
  const rawValues =
    evaluation.variable_values && typeof evaluation.variable_values === 'object'
      ? evaluation.variable_values
      : row.values;
  return {
    ...mergeEvaluationMetadataIntoRow(row, evaluation),
    values: Object.fromEntries(
      variables.map((name) => [
        name,
        hasOwn(rawValues, name) ? generatedValueString(rawValues[name]) : row.values[name] || '',
      ]),
    ),
  };
}

export function mergeEvaluationGoldenAnswerIntoRow(row: EvaluateTestCase, evaluation: WorkbenchEvaluation) {
  return {
    ...mergeEvaluationMetadataIntoRow(row, evaluation),
    idealOutput: hasOwn(evaluation, 'golden_answer') ? generatedValueString(evaluation.golden_answer) : row.idealOutput,
  };
}

export function mergeEvaluationMetadataIntoRow(row: EvaluateTestCase, evaluation: WorkbenchEvaluation) {
  return {
    ...row,
    evaluationId: generatedValueString(evaluation.id).trim() || row.evaluationId,
    testCaseId: generatedValueString(evaluation.test_case_id).trim() || row.testCaseId,
  };
}

export function evaluateRowRequestBody(row: EvaluateTestCase): Partial<WorkbenchEvaluation> {
  return {
    id: row.evaluationId || row.id,
    test_case_id: row.testCaseId || row.id,
    variable_values: row.values,
    golden_answer: row.idealOutput,
    completion_text: row.modelOutput,
    rating: row.rating,
  };
}

export function buildGenerateTestCasesPayload(draft: WorkbenchRevision, count: number, examples: WorkbenchExample[]) {
  return {
    ...buildRevisionPayload(draft, { includeEmptyMessages: false }),
    prompt: titleMessageContent(draft),
    num_testcases: count,
    existing_examples: examples.map((example) => ({
      variable_values: example.values,
      ideal_output: example.idealOutput,
      additional_context: example.additionalContext,
    })),
  };
}

export function buildGenerateVariablePayload(
  draft: WorkbenchRevision,
  examples: WorkbenchExample[],
  customChainOfThought: string,
) {
  return {
    ...buildRevisionPayload(draft, { includeEmptyMessages: false }),
    custom_chain_of_thought: customChainOfThought,
    existing_examples: examples.map((example) => ({
      variable_values: example.values,
      ideal_output: example.idealOutput,
      additional_context: example.additionalContext,
    })),
  };
}

export function buildGenerateExamplePayload(
  draft: WorkbenchRevision,
  examples: WorkbenchExample[],
  customChainOfThought: string,
) {
  return {
    system_prompt: draft.system_prompt || '',
    messages: buildGenerateExampleMessages(draft),
    custom_chain_of_thought: customChainOfThought,
    existing_examples: examples.map((example) => ({
      variable_values: example.values,
      ideal_output: example.idealOutput,
      additional_context: example.additionalContext,
    })),
  };
}

export function buildGenerateExampleMessages(draft: WorkbenchRevision): WorkbenchMessage[] {
  const messages = buildRevisionPayload(draft, { includeEmptyMessages: false }).messages;
  if (messages.some((message) => message.role === 'assistant')) {
    return messages;
  }
  return [
    ...messages,
    {
      role: 'assistant',
      content: [{ type: 'text', text: '' }],
    },
  ];
}

export function evaluateRowFromGeneratedEvent(event: WorkbenchStreamEvent, variables: string[]) {
  if (event.event && event.event !== 'test_case') {
    return null;
  }
  const rawValues = event.data.variable_values;
  if (!rawValues || typeof rawValues !== 'object' || Array.isArray(rawValues)) {
    return null;
  }
  const values = Object.fromEntries(
    variables.map((name) => [name, generatedValueString((rawValues as Record<string, unknown>)[name])]),
  );
  if (variables.every((name) => !values[name])) {
    return null;
  }
  return createEvaluateRowFromValues(variables, values);
}

export function evaluateRowsToCsv(rows: EvaluateTestCase[], variables: string[]) {
  const headers = [...variables.map((name) => `{{${name}}}`), 'ideal_output'];
  const lines = [
    headers.map(escapeCsvCell).join(','),
    ...rows.map((row) =>
      [...variables.map((name) => row.values[name] ?? ''), row.idealOutput].map(escapeCsvCell).join(','),
    ),
  ];
  return `${lines.join('\n')}\n`;
}

export function parseEvaluateCsv(text: string, variables: string[]) {
  const records = parseCsvRecords(text);
  if (records.length < 2) {
    return [];
  }
  const headers = records[0].map(normalizeEvaluateCsvHeader);
  const idealIndex = headers.findIndex((header) => header === 'ideal_output' || header === 'ideal output');
  const variableIndexes = variables.map((name) => {
    const normalizedName = normalizeEvaluateCsvHeader(name);
    const bracedName = normalizeEvaluateCsvHeader(`{{${name}}}`);
    return headers.findIndex((header) => header === normalizedName || header === bracedName);
  });
  return records
    .slice(1)
    .filter((record) => record.some((cell) => cell.trim()))
    .map((record) => ({
      ...createEvaluateRowFromValues(
        variables,
        Object.fromEntries(variables.map((name, index) => [name, record[variableIndexes[index]] ?? ''])),
      ),
      idealOutput: idealIndex >= 0 ? (record[idealIndex] ?? '') : '',
    }));
}

export function parseCsvRecords(text: string) {
  const records: string[][] = [];
  let record: string[] = [];
  let cell = '';
  let quoted = false;
  for (let index = 0; index < text.length; index += 1) {
    const char = text[index];
    const next = text[index + 1];
    if (quoted) {
      if (char === '"' && next === '"') {
        cell += '"';
        index += 1;
      } else if (char === '"') {
        quoted = false;
      } else {
        cell += char;
      }
    } else if (char === '"') {
      quoted = true;
    } else if (char === ',') {
      record.push(cell);
      cell = '';
    } else if (char === '\n') {
      record.push(cell);
      records.push(record);
      record = [];
      cell = '';
    } else if (char !== '\r') {
      cell += char;
    }
  }
  if (cell || record.length) {
    record.push(cell);
    records.push(record);
  }
  return records;
}

export function escapeCsvCell(value: string) {
  if (/[",\n\r]/.test(value)) {
    return `"${value.replaceAll('"', '""')}"`;
  }
  return value;
}

export function normalizeEvaluateCsvHeader(value: string) {
  return value
    .trim()
    .replace(/^{{\s*/, '')
    .replace(/\s*}}$/, '')
    .toLowerCase();
}

export function slugifyFileName(value: string) {
  const slug = value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
  return slug || 'workbench';
}

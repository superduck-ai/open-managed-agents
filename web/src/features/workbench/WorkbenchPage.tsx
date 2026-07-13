import {
  AlertCircle,
  Braces,
  ChevronDown,
  ChevronRight,
  Info,
  List,
  Loader2,
  Lock,
  MessageSquareDashed,
  MessageSquarePlus,
  Play,
  Plus,
  SlidersHorizontal,
  Sparkles,
  WandSparkles,
  Wrench,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import { useAuth } from "../../shared/auth/context";
import { useWorkspace } from "../../shared/workspaces/context";
import { Alert, AlertDescription, AlertTitle } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { Input } from "@/shared/ui/input";
import { Popover, PopoverTrigger } from "@/shared/ui/popover";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/shared/ui/tabs";
import {
  createWorkbenchEvaluation,
  createWorkbenchRevision,
  createWorkspacePrompt,
  deleteWorkbenchEvaluation,
  deleteWorkbenchPrompt,
  generateWorkbenchTitle,
  getPrepaidCredits,
  getWorkbenchKV,
  getWorkbenchModels,
  getWorkbenchPrompt,
  getWorkbenchRevision,
  listWorkbenchEvaluations,
  listWorkbenchPrompts,
  listWorkbenchRevisions,
  listWorkspacePrompts,
  saveWorkbenchEvaluationCompletion,
  setWorkbenchKV,
  shareWorkbenchPrompt,
  streamGeneratePrompt,
  streamGenerateTestCase,
  streamGenerateTestCases,
  streamWorkbenchCompletion,
  updateWorkbenchEvaluationGoldenAnswer,
  updateWorkbenchEvaluationVariables,
  updateWorkbenchPrompt,
  uploadWorkbenchFile,
  WorkbenchEvaluation,
  WorkbenchMessage,
  WorkbenchModel,
  WorkbenchPromptDetail,
  WorkbenchPromptSummary,
  WorkbenchRevision,
  WorkbenchStreamEvent,
} from "./api";
import {
  appendFileBlockToMessageContent,
  appendUrlBlockToMessageContent,
  buildRevisionPayload,
  buildRunRevisionPayload,
  canEvaluateRevision,
  CodeLanguage,
  createDefaultRevision,
  currentRouteIsWorkbenchIndex,
  currentRoutePromptId,
  currentRouteRequestsNewPrompt,
  currentRouteTab,
  defaultGeneratedPromptTitle,
  defaultSchema,
  defaultWebSearchToolForm,
  displayPromptTitle,
  DrawerName,
  drawerTitle,
  errorMessage,
  EvaluateComparison,
  EvaluateTestCase,
  extractGeneratedPromptInstructions,
  extractVariables,
  fallbackModels,
  generatePromptExamples,
  GeneratePromptStep,
  hasMultipleHumanMessages,
  hasRunnableMessage,
  isPromptCreator,
  isBlankWorkbenchDraft,
  mergePromptSummaries,
  messageText,
  modelDisplayName,
  mostRecentPromptForWorkbenchEntry,
  normalizeNewPromptRevision,
  normalizeRevision,
  numberValue,
  parseDraftRevision,
  parseTaggedValue,
  parseTaggedVariables,
  promptSummaryDisplayTitle,
  removeContentBlockFromMessageContent,
  replaceFileBlockInMessageContent,
  replaceMessageText,
  ResponseTab,
  revisionHasImageContent,
  savedPromptMeta,
  shortTime,
  streamSmoothedWorkbenchText,
  stringValue,
  stripTaggedVariables,
  syncWorkbenchIndexUrl,
  syncWorkbenchNewUrl,
  syncWorkbenchPromptUrl,
  syncWorkbenchTabUrl,
  textDeltaFromEvent,
  thinkingMode,
  titleMessageContent,
  ToolForm,
  trackWorkbenchEvent,
  truncateTitleMessageContent,
  WebSearchToolForm,
  webSearchToolFromForm,
  workbenchAccessState,
  WorkbenchAttachmentKind,
  workbenchDraftAutosaveKey,
  WorkbenchExample,
  workbenchExamplesFromPromptDetail,
  workbenchId,
  WorkbenchMode,
  workbenchPromptGeneratorWarning,
} from "./model";
import { PromptPicker, WorkbenchAccessUnavailable, WorkbenchShell } from "./shell";
import { MessageEditor, messageRemovalRange, SystemPromptEditableInput } from "./editor";
import {
  buildGenerateExamplePayload,
  buildGenerateTestCasesPayload,
  buildGenerateVariablePayload,
  createEvaluateRow,
  emptyComparisonOutput,
  evaluateRowFromGeneratedEvent,
  evaluateRowRequestBody,
  evaluateRowsFromEvaluations,
  EvaluateView,
  mergeCreatedEvaluationIntoRow,
  mergeEvaluationGoldenAnswerIntoRow,
  mergeEvaluationVariablesIntoRow,
  ResponsePreview,
} from "./evaluate";
import { ExamplesDrawer, HistoryDrawer, ModelDrawer, ToolsDrawer, VariablesDrawer } from "./drawers";
import {
  CodeModal,
  DeletePromptDialog,
  Dialog,
  GeneratePromptDialog,
  ImprovePromptDialog,
  PromptGeneratorConfirmDialog,
  SharePromptDialog,
  WorkbenchDrawer,
} from "./dialogs";

export function WorkbenchPage() {
  const { account } = useAuth();
  const { orgUuid, activeWorkspaceId } = useWorkspace();
  const workbenchAccess = useMemo(() => workbenchAccessState(account, orgUuid), [account, orgUuid]);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [models, setModels] = useState<WorkbenchModel[]>(fallbackModels);
  const [defaultModelName, setDefaultModelName] = useState(fallbackModels[0].model_name);
  const [promptList, setPromptList] = useState<WorkbenchPromptSummary[]>([]);
  const [prompt, setPrompt] = useState<WorkbenchPromptDetail | null>(null);
  const [promptName, setPromptName] = useState("Untitled");
  const [draft, setDraft] = useState<WorkbenchRevision>(() => createDefaultRevision());
  const [draftVersion, setDraftVersion] = useState<unknown>(undefined);
  const [promptRevisionCount, setPromptRevisionCount] = useState(1);
  const [saveStatus, setSaveStatus] = useState("Loading");
  const [activeMode, setActiveMode] = useState<WorkbenchMode>(() => currentRouteTab());
  const [systemOpen, setSystemOpen] = useState(false);
  const [promptMenuOpen, setPromptMenuOpen] = useState(false);
  const [promptPickerOpen, setPromptPickerOpen] = useState(false);
  const [promptSearch, setPromptSearch] = useState("");
  const [promptOnlyMine, setPromptOnlyMine] = useState(false);
  const [activeDrawer, setActiveDrawer] = useState<DrawerName | null>(null);
  const [toolForm, setToolForm] = useState<ToolForm>(null);
  const [responseTab, setResponseTab] = useState<ResponseTab>("preview");
  const [responseText, setResponseText] = useState("");
  const [runError, setRunError] = useState<string | null>(null);
  const [isRunning, setIsRunning] = useState(false);
  const [streamEvents, setStreamEvents] = useState<WorkbenchStreamEvent[]>([]);
  const [lastRunRequest, setLastRunRequest] = useState<Record<string, unknown> | null>(null);
  const [variableValues, setVariableValues] = useState<Record<string, string>>({});
  const [variableGenerationLogicOpen, setVariableGenerationLogicOpen] = useState(false);
  const [variableGenerationLogic, setVariableGenerationLogic] = useState("");
  const [isGeneratingVariableLogic, setIsGeneratingVariableLogic] = useState(false);
  const [codeOpen, setCodeOpen] = useState(false);
  const [codeLanguage, setCodeLanguage] = useState<CodeLanguage>("python");
  const [renameOpen, setRenameOpen] = useState(false);
  const [renameValue, setRenameValue] = useState("");
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [deletePromptTarget, setDeletePromptTarget] = useState<WorkbenchPromptSummary | null>(null);
  const [isDeletingPrompt, setIsDeletingPrompt] = useState(false);
  const [shareOpen, setShareOpen] = useState(false);
  const [isSharingPrompt, setIsSharingPrompt] = useState(false);
  const [shareError, setShareError] = useState<string | null>(null);
  const [customTool, setCustomTool] = useState({
    name: "",
    description: "",
    schema: defaultSchema,
  });
  const [webSearchTool, setWebSearchTool] = useState<WebSearchToolForm>(() => defaultWebSearchToolForm());
  const [examples, setExamples] = useState<WorkbenchExample[]>([]);
  const [evaluateRows, setEvaluateRows] = useState<EvaluateTestCase[]>([]);
  const [evaluateComparisons, setEvaluateComparisons] = useState<EvaluateComparison[]>([]);
  const [exampleFormOpen, setExampleFormOpen] = useState(false);
  const [exampleValues, setExampleValues] = useState<Record<string, string>>({});
  const [exampleIdealOutput, setExampleIdealOutput] = useState("");
  const [exampleAdditionalContext, setExampleAdditionalContext] = useState("");
  const [exampleContextOpen, setExampleContextOpen] = useState(false);
  const [editingExampleId, setEditingExampleId] = useState<string | null>(null);
  const [isGeneratingExample, setIsGeneratingExample] = useState(false);
  const [improveOpen, setImproveOpen] = useState(false);
  const [improveFeedback, setImproveFeedback] = useState("");
  const [improveThinkingEnabled, setImproveThinkingEnabled] = useState(false);
  const [isImproving, setIsImproving] = useState(false);
  const [promptGeneratorOpen, setPromptGeneratorOpen] = useState(false);
  const [promptGeneratorStep, setPromptGeneratorStep] = useState<GeneratePromptStep>("generate");
  const [promptGeneratorTask, setPromptGeneratorTask] = useState("");
  const [promptGeneratorOutput, setPromptGeneratorOutput] = useState("");
  const [promptGeneratorConfirmAction, setPromptGeneratorConfirmAction] = useState<"close" | "edit" | null>(null);
  const [promptGeneratorExamplesExpanded, setPromptGeneratorExamplesExpanded] = useState(false);
  const [promptGeneratorError, setPromptGeneratorError] = useState<string | null>(null);
  const [promptGeneratorThinkingEnabled, setPromptGeneratorThinkingEnabled] = useState(false);
  const [isGeneratingPrompt, setIsGeneratingPrompt] = useState(false);
  const [prepaidCreditAmount, setPrepaidCreditAmount] = useState<number | null>(null);
  const [isCreatingPrompt, setIsCreatingPrompt] = useState(false);
  const [uploadingMessageIndex, setUploadingMessageIndex] = useState<number | null>(null);
  const [uploadErrorByMessage, setUploadErrorByMessage] = useState<Record<number, string | null>>({});
  const saveTimerRef = useRef<number | undefined>(undefined);
  const runControllerRef = useRef<AbortController | null>(null);
  const promptGeneratorControllerRef = useRef<AbortController | null>(null);
  const promptGeneratorOutputFallbackRef = useRef<number | undefined>(undefined);
  const lastSavedDraftKeyRef = useRef<string | null>(null);
  const latestRevisionDraftKeyRef = useRef<string | null>(null);
  const creatingPromptRef = useRef(false);
  const activePromptIdRef = useRef<string | null>(null);
  const workbenchLoadSeqRef = useRef(0);

  const variables = useMemo(() => extractVariables(draft), [draft]);
  const selectedModel = models.find((model) => model.model_name === draft.model_name) ?? models[0] ?? fallbackModels[0];
  const promptTitle = useMemo(() => displayPromptTitle(promptName, draft), [draft, promptName]);
  const hasPromptText = hasRunnableMessage(draft);
  const hasVariables = variables.length > 0;
  const canEvaluate = hasVariables && draft.tools.length === 0;
  const evaluateUnavailableReason = "Run a prompt with at least one variable and no tools to use ‘Evaluate’.";
  const canRun = Boolean(orgUuid) && hasPromptText && !isLoading;
  const hasMissingVariableValues = variables.some((name) => !variableValues[name]?.trim());
  const canRunWithVariables = canRun && !hasMissingVariableValues;
  const currentDraftKey = prompt ? workbenchDraftAutosaveKey(prompt.id, draft) : null;
  const hasUnsavedChanges = Boolean(prompt && latestRevisionDraftKeyRef.current !== currentDraftKey);
  const canRunAllEvaluations =
    Boolean(orgUuid) && hasPromptText && evaluateRows.length > 0 && !isLoading && !hasUnsavedChanges;
  const canAddPrefillResponse =
    hasPromptText && variables.length === 0 && draft.messages[draft.messages.length - 1]?.role !== "assistant";
  const canSaveCurrentRevision = Boolean(orgUuid && prompt && hasUnsavedChanges && saveStatus !== "Saving");
  const isPromptReadOnly = draft.is_latest === false;
  const promptGeneratorWarning = useMemo(
    () => workbenchPromptGeneratorWarning(account, orgUuid, prepaidCreditAmount),
    [account, orgUuid, prepaidCreditAmount],
  );
  const nextRevisionLabel = `v${Math.max(2, promptRevisionCount + 1)}`;
  const visiblePromptList = useMemo(() => {
    const query = promptSearch.trim().toLowerCase();
    return promptList.filter((item) => {
      const title = promptSummaryDisplayTitle(item, prompt?.id, promptTitle);
      if (!item.name?.trim() && item.id !== prompt?.id) {
        return false;
      }
      if (promptOnlyMine && !isPromptCreator(item, account)) {
        return false;
      }
      if (!query) {
        return true;
      }
      return `${title} ${item.id}`.toLowerCase().includes(query);
    });
  }, [account, prompt?.id, promptList, promptOnlyMine, promptSearch, promptTitle]);
  const saveMeta = saveStatus.startsWith("Saved ")
    ? `Last saved ${saveStatus.slice("Saved ".length)}`
    : saveStatus === "Saved"
      ? savedPromptMeta(prompt, draft)
      : saveStatus;
  const showResponseCreatePrompt = useMemo(() => isBlankWorkbenchDraft(draft), [draft]);

  const applyPromptState = useCallback(
    (
      detail: WorkbenchPromptDetail,
      nextDraft: WorkbenchRevision,
      nextDraftVersion?: unknown,
      options: { markSaved?: boolean; latestSavedDraft?: WorkbenchRevision } = {},
    ) => {
      if (options.markSaved !== false) {
        lastSavedDraftKeyRef.current = workbenchDraftAutosaveKey(detail.id, nextDraft);
      }
      const latestSavedDraft =
        options.latestSavedDraft ?? normalizeRevision(detail.latest_revision, nextDraft.model_name);
      latestRevisionDraftKeyRef.current = workbenchDraftAutosaveKey(detail.id, latestSavedDraft);
      activePromptIdRef.current = detail.id;
      setPrompt(detail);
      setPromptName(detail.name?.trim() || "Untitled");
      setDraft(nextDraft);
      setDraftVersion(nextDraftVersion);
      const nextVariables = extractVariables(nextDraft);
      setPromptRevisionCount(1);
      const routeWantsEvaluate = currentRouteTab() === "evaluate";
      const nextMode = routeWantsEvaluate && canEvaluateRevision(nextDraft) ? "evaluate" : "prompt";
      setActiveMode(nextMode);
      if (routeWantsEvaluate && nextMode === "prompt") {
        syncWorkbenchTabUrl("prompt", "replace");
      }
      setSystemOpen(Boolean(nextDraft.system_prompt?.trim()));
      setActiveDrawer(null);
      setToolForm(null);
      setExampleFormOpen(false);
      setExamples(workbenchExamplesFromPromptDetail(detail, nextVariables));
      setEvaluateRows([]);
      setEvaluateComparisons([]);
      setExampleValues({});
      setExampleIdealOutput("");
      setExampleAdditionalContext("");
      setExampleContextOpen(false);
      setEditingExampleId(null);
      setIsGeneratingExample(false);
      setImproveOpen(false);
      setImproveFeedback("");
      setImproveThinkingEnabled(false);
      setPromptGeneratorOpen(false);
      setPromptGeneratorStep("generate");
      setPromptGeneratorTask("");
      setPromptGeneratorOutput("");
      setPromptGeneratorExamplesExpanded(false);
      setPromptGeneratorError(null);
      setIsGeneratingPrompt(false);
      setUploadingMessageIndex(null);
      setUploadErrorByMessage({});
      setPromptMenuOpen(false);
      setPromptPickerOpen(false);
      setPromptSearch("");
      setPromptOnlyMine(false);
      setCodeOpen(false);
      setDeleteConfirmOpen(false);
      setDeletePromptTarget(null);
      setShareOpen(false);
      setShareError(null);
      setResponseTab("preview");
      setResponseText("");
      setRunError(null);
      setStreamEvents([]);
      setLastRunRequest(null);
      setVariableValues({});
      setVariableGenerationLogic(stringValue(detail.kv_store?.test_case_generation_logic));
      setVariableGenerationLogicOpen(false);
      setIsGeneratingVariableLogic(false);
      setCustomTool({ name: "", description: "", schema: defaultSchema });
      setWebSearchTool(defaultWebSearchToolForm());
      setSaveStatus("Saved");
    },
    [],
  );

  const handlePromptPickerOpenChange = useCallback((open: boolean) => {
    setPromptPickerOpen(open);
    if (open) {
      setPromptMenuOpen(false);
      setActiveDrawer(null);
    }
  }, []);

  const resetCreatingPromptState = useCallback((nextDraft: WorkbenchRevision) => {
    lastSavedDraftKeyRef.current = null;
    latestRevisionDraftKeyRef.current = null;
    activePromptIdRef.current = null;
    setLoadError(null);
    setPrompt(null);
    setPromptName("Untitled");
    setDraft(nextDraft);
    setDraftVersion(undefined);
    setPromptRevisionCount(1);
    setActiveMode("prompt");
    setSystemOpen(false);
    setActiveDrawer(null);
    setToolForm(null);
    setExampleFormOpen(false);
    setExamples([]);
    setEvaluateRows([]);
    setEvaluateComparisons([]);
    setExampleValues({});
    setExampleIdealOutput("");
    setExampleAdditionalContext("");
    setExampleContextOpen(false);
    setEditingExampleId(null);
    setIsGeneratingExample(false);
    setImproveOpen(false);
    setImproveFeedback("");
    setImproveThinkingEnabled(false);
    setIsImproving(false);
    setPromptGeneratorOpen(false);
    setPromptGeneratorStep("generate");
    setPromptGeneratorTask("");
    setPromptGeneratorOutput("");
    setPromptGeneratorExamplesExpanded(false);
    setPromptGeneratorError(null);
    setIsGeneratingPrompt(false);
    setUploadingMessageIndex(null);
    setUploadErrorByMessage({});
    setPromptMenuOpen(false);
    setPromptPickerOpen(false);
    setPromptSearch("");
    setPromptOnlyMine(false);
    setCodeOpen(false);
    setRenameOpen(false);
    setDeleteConfirmOpen(false);
    setDeletePromptTarget(null);
    setShareOpen(false);
    setShareError(null);
    setResponseTab("preview");
    setResponseText("");
    setRunError(null);
    setStreamEvents([]);
    setLastRunRequest(null);
    setVariableValues({});
    setVariableGenerationLogic("");
    setVariableGenerationLogicOpen(false);
    setIsGeneratingVariableLogic(false);
    setCustomTool({ name: "", description: "", schema: defaultSchema });
    setWebSearchTool(defaultWebSearchToolForm());
    setSaveStatus("Creating");
  }, []);

  const openCodeModal = useCallback(() => {
    setActiveDrawer(null);
    setCodeOpen(true);
  }, []);

  const openImprovePrompt = useCallback(() => {
    if (isPromptReadOnly) {
      return;
    }
    setPromptPickerOpen(false);
    setActiveDrawer(null);
    setImproveFeedback("");
    setImproveThinkingEnabled(thinkingMode(draft.thinking) !== "disabled");
    setImproveOpen(true);
  }, [draft.thinking, isPromptReadOnly]);

  const openPromptGenerator = useCallback(() => {
    if (isPromptReadOnly || promptGeneratorWarning) {
      return;
    }
    setPromptPickerOpen(false);
    setActiveDrawer(null);
    setPromptGeneratorStep("generate");
    setPromptGeneratorTask("");
    setPromptGeneratorOutput("");
    setPromptGeneratorExamplesExpanded(false);
    setPromptGeneratorError(null);
    setPromptGeneratorThinkingEnabled(thinkingMode(draft.thinking) !== "disabled");
    setPromptGeneratorOpen(true);
  }, [draft.thinking, isPromptReadOnly, promptGeneratorWarning]);

  const loadWorkbench = useCallback(async () => {
    const loadSeq = ++workbenchLoadSeqRef.current;
    const isCurrentLoad = () => workbenchLoadSeqRef.current === loadSeq;
    if (!workbenchAccess.hasAccess) {
      setIsLoading(false);
      return;
    }
    if (!orgUuid) {
      setIsLoading(false);
      setLoadError("No organization is available for Workbench.");
      return;
    }

    setIsLoading(true);
    setLoadError(null);
    try {
      const routeIsWorkbenchIndex = currentRouteIsWorkbenchIndex();
      const [modelsResponse, availablePrompts] = await Promise.all([
        getWorkbenchModels(orgUuid),
        routeIsWorkbenchIndex ? listWorkbenchPrompts(orgUuid) : listWorkspacePrompts(orgUuid, activeWorkspaceId),
      ]);
      if (!isCurrentLoad()) {
        return;
      }
      const nextModels = modelsResponse.models?.length ? modelsResponse.models : fallbackModels;
      const nextDefaultModelName =
        modelsResponse.default_prompt_settings?.model_name ?? nextModels[0]?.model_name ?? fallbackModels[0].model_name;
      setModels(nextModels);
      setDefaultModelName(nextDefaultModelName);

      const routePromptId = currentRoutePromptId();
      let routeRequestsNewPrompt = currentRouteRequestsNewPrompt();
      let summaries = availablePrompts;
      let promptId = routePromptId;
      if (routeIsWorkbenchIndex) {
        const mostRecentPrompt = mostRecentPromptForWorkbenchEntry(summaries, activeWorkspaceId, account);
        if (mostRecentPrompt) {
          promptId = mostRecentPrompt.id;
          syncWorkbenchPromptUrl(promptId, "replace", { resetTab: true });
        } else {
          routeRequestsNewPrompt = true;
          syncWorkbenchNewUrl("replace");
        }
      } else if (!promptId && !routeRequestsNewPrompt) {
        promptId = summaries[0]?.id;
      }
      let detail: WorkbenchPromptDetail;
      let draftVersion: unknown;
      let normalizedDraft: WorkbenchRevision;
      let storedEvaluations: WorkbenchEvaluation[] = [];
      if (routeRequestsNewPrompt || !promptId) {
        detail = await createWorkspacePrompt(orgUuid, activeWorkspaceId);
        promptId = detail.id;
        if (!isCurrentLoad()) {
          return;
        }
        summaries = mergePromptSummaries(summaries, detail);
        syncWorkbenchPromptUrl(promptId, "replace", { resetTab: true });
        normalizedDraft = normalizeNewPromptRevision(detail.latest_revision, nextDefaultModelName);
      } else {
        detail = await getWorkbenchPrompt(orgUuid, promptId);
        if (!routePromptId && promptId) {
          syncWorkbenchPromptUrl(promptId, "replace");
        }

        const draftKV = await getWorkbenchKV(orgUuid, promptId, "draft_revision").catch(() => null);
        draftVersion = draftKV?.version;
        const parsedDraft = parseDraftRevision(draftKV?.value) ?? parseDraftRevision(detail.kv_store?.draft_revision);
        normalizedDraft = normalizeRevision(parsedDraft ?? detail.latest_revision, nextDefaultModelName);
        storedEvaluations = await listWorkbenchEvaluations(orgUuid, normalizedDraft.id).catch(() => []);
      }
      if (!isCurrentLoad()) {
        return;
      }

      applyPromptState(
        detail,
        normalizedDraft,
        draftVersion,
        routeRequestsNewPrompt ? { latestSavedDraft: normalizedDraft } : undefined,
      );
      setEvaluateRows(evaluateRowsFromEvaluations(storedEvaluations, extractVariables(normalizedDraft)));
      setPromptList(summaries);
      const revisions = await listWorkbenchRevisions(orgUuid, promptId, true).catch(() => []);
      setPromptRevisionCount(Math.max(1, revisions.length));
      setPromptList((current) => mergePromptSummaries(current, detail));
      if (revisions.length > 0) {
        setDraft((current) => ({ ...current, is_latest: true }));
      }
    } catch (error) {
      if (isCurrentLoad()) {
        setLoadError(errorMessage(error));
      }
    } finally {
      if (isCurrentLoad()) {
        setIsLoading(false);
      }
    }
  }, [account, activeWorkspaceId, applyPromptState, orgUuid, workbenchAccess.hasAccess]);

  useEffect(() => {
    void loadWorkbench();
  }, [loadWorkbench]);

  useEffect(() => {
    if (!workbenchAccess.hasAccess || !orgUuid) {
      setPrepaidCreditAmount(null);
      return;
    }
    let cancelled = false;
    void getPrepaidCredits(orgUuid)
      .then((credits) => {
        if (!cancelled) {
          setPrepaidCreditAmount(numberValue(credits.amount));
        }
      })
      .catch(() => {
        if (!cancelled) {
          setPrepaidCreditAmount(null);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [orgUuid, workbenchAccess.hasAccess]);

  useEffect(() => {
    if (!isGeneratingPrompt) {
      return;
    }
    const handleBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "Changes made will not be saved.";
      return event.returnValue;
    };
    window.addEventListener("beforeunload", handleBeforeUnload);
    return () => window.removeEventListener("beforeunload", handleBeforeUnload);
  }, [isGeneratingPrompt]);

  useEffect(() => {
    setVariableValues((current) => {
      const next: Record<string, string> = {};
      variables.forEach((name) => {
        next[name] = current[name] ?? "";
      });
      return next;
    });
    setEvaluateRows((current) =>
      current.map((row) => ({
        ...row,
        values: Object.fromEntries(variables.map((name) => [name, row.values[name] ?? ""])),
      })),
    );
  }, [variables]);

  useEffect(() => {
    setExampleValues((current) => {
      const next: Record<string, string> = {};
      variables.forEach((name) => {
        next[name] = current[name] ?? "";
      });
      return next;
    });
  }, [variables]);

  useEffect(() => {
    if (isLoading) {
      return;
    }
    if (activeMode === "evaluate" && !canEvaluate) {
      setActiveMode("prompt");
      syncWorkbenchTabUrl("prompt", "replace");
    }
  }, [activeMode, canEvaluate, isLoading]);

  const changeMode = useCallback(
    (nextMode: string) => {
      if (nextMode === "evaluate" && !canEvaluate) {
        return;
      }
      const normalizedMode: WorkbenchMode = nextMode === "evaluate" ? "evaluate" : "prompt";
      setActiveMode(normalizedMode);
      syncWorkbenchTabUrl(normalizedMode, "push");
    },
    [canEvaluate],
  );

  useEffect(() => {
    if (!prompt || !orgUuid || isLoading) {
      return;
    }
    const draftKey = workbenchDraftAutosaveKey(prompt.id, draft);
    if (lastSavedDraftKeyRef.current === draftKey) {
      return;
    }
    window.clearTimeout(saveTimerRef.current);
    setSaveStatus("Unsaved");
    saveTimerRef.current = window.setTimeout(() => {
      const payload = buildRevisionPayload(draft, { includeEmptyMessages: true });
      void setWorkbenchKV(orgUuid, prompt.id, "draft_revision", JSON.stringify(payload), draftVersion)
        .then((response) => {
          if (activePromptIdRef.current !== prompt.id) {
            return;
          }
          lastSavedDraftKeyRef.current = draftKey;
          setDraftVersion(response.version);
          setSaveStatus(`Saved ${shortTime()}`);
        })
        .catch(() => {
          if (activePromptIdRef.current === prompt.id) {
            setSaveStatus("Save failed");
          }
        });
    }, 900);

    return () => window.clearTimeout(saveTimerRef.current);
  }, [draft, draftVersion, isLoading, orgUuid, prompt]);

  const setDraftField = useCallback(<K extends keyof WorkbenchRevision>(field: K, value: WorkbenchRevision[K]) => {
    setDraft((current) => ({ ...current, [field]: value }));
  }, []);

  const setMessageText = useCallback((index: number, text: string) => {
    setDraft((current) => {
      const messages = current.messages.map((message, messageIndex) =>
        messageIndex === index ? { ...message, content: replaceMessageText(message.content, text) } : message,
      );
      return { ...current, messages };
    });
  }, []);

  const clearPromptGeneratorOutputFallback = useCallback(() => {
    if (promptGeneratorOutputFallbackRef.current !== undefined) {
      window.clearTimeout(promptGeneratorOutputFallbackRef.current);
      promptGeneratorOutputFallbackRef.current = undefined;
    }
  }, []);

  const attachFileToMessage = useCallback(async (messageIndex: number, file: File) => {
    if (!file) {
      return;
    }
    setUploadingMessageIndex(messageIndex);
    setUploadErrorByMessage((current) => ({ ...current, [messageIndex]: null }));
    try {
      const uploaded = await uploadWorkbenchFile(file);
      setDraft((current) => ({
        ...current,
        messages: current.messages.map((message, index) =>
          index === messageIndex
            ? { ...message, content: appendFileBlockToMessageContent(message.content, uploaded) }
            : message,
        ),
      }));
    } catch (error) {
      setUploadErrorByMessage((current) => ({ ...current, [messageIndex]: errorMessage(error) }));
    } finally {
      setUploadingMessageIndex((current) => (current === messageIndex ? null : current));
    }
  }, []);

  const addUrlAttachmentToMessage = useCallback((messageIndex: number, kind: WorkbenchAttachmentKind, url: string) => {
    setDraft((current) => ({
      ...current,
      messages: current.messages.map((message, index) =>
        index === messageIndex
          ? { ...message, content: appendUrlBlockToMessageContent(message.content, kind, url) }
          : message,
      ),
    }));
  }, []);

  const replaceFileInMessage = useCallback(async (messageIndex: number, blockIndex: number, file: File) => {
    if (!file) {
      return;
    }
    setUploadingMessageIndex(messageIndex);
    setUploadErrorByMessage((current) => ({ ...current, [messageIndex]: null }));
    try {
      const uploaded = await uploadWorkbenchFile(file);
      setDraft((current) => ({
        ...current,
        messages: current.messages.map((message, index) =>
          index === messageIndex
            ? { ...message, content: replaceFileBlockInMessageContent(message.content, blockIndex, uploaded) }
            : message,
        ),
      }));
    } catch (error) {
      setUploadErrorByMessage((current) => ({ ...current, [messageIndex]: errorMessage(error) }));
    } finally {
      setUploadingMessageIndex((current) => (current === messageIndex ? null : current));
    }
  }, []);

  const removeFileFromMessage = useCallback((messageIndex: number, blockIndex: number) => {
    setDraft((current) => ({
      ...current,
      messages: current.messages.map((message, index) =>
        index === messageIndex
          ? { ...message, content: removeContentBlockFromMessageContent(message.content, blockIndex) }
          : message,
      ),
    }));
  }, []);

  const addMessagePair = useCallback(() => {
    setDraft((current) => ({
      ...current,
      messages: [
        ...current.messages,
        { role: "assistant", content: [{ type: "text", text: "" }] },
        { role: "human", content: [{ type: "text", text: "" }] },
      ],
    }));
  }, []);

  const addPrefillResponse = useCallback(() => {
    setDraft((current) => ({
      ...current,
      messages: [...current.messages, { role: "assistant", content: [{ type: "text", text: "" }] }],
    }));
  }, []);

  const removeMessage = useCallback((index: number) => {
    setDraft((current) => {
      if (current.messages.length <= 1) {
        return current;
      }
      const [startIndex, endIndex] = messageRemovalRange(current.messages, index);
      return {
        ...current,
        messages: current.messages.filter((_, messageIndex) => messageIndex < startIndex || messageIndex > endIndex),
      };
    });
    setUploadingMessageIndex(null);
    setUploadErrorByMessage({});
  }, []);

  const saveCurrentRevision = useCallback(async () => {
    if (!orgUuid || !prompt || !hasUnsavedChanges || saveStatus === "Saving") {
      return;
    }
    setSaveStatus("Saving");
    const payload = buildRevisionPayload(draft, { includeEmptyMessages: true, newRevisionId: true });
    try {
      const saved = await createWorkbenchRevision(orgUuid, prompt.id, payload);
      const normalizedSaved = normalizeRevision(saved, draft.model_name);
      lastSavedDraftKeyRef.current = workbenchDraftAutosaveKey(prompt.id, normalizedSaved);
      latestRevisionDraftKeyRef.current = workbenchDraftAutosaveKey(prompt.id, normalizedSaved);
      setDraft(normalizedSaved);
      setPrompt((current) =>
        current
          ? { ...current, latest_revision: normalizedSaved, updated_at: saved.created_at ?? current.updated_at }
          : current,
      );
      setPromptRevisionCount((current) => Math.max(2, current + 1));
      setSaveStatus(`Saved ${shortTime()}`);
      setDraftVersion(undefined);
    } catch (error) {
      setSaveStatus(errorMessage(error));
    }
  }, [draft, hasUnsavedChanges, orgUuid, prompt, saveStatus]);

  const autoTitlePrompt = useCallback(
    async (targetPrompt: WorkbenchPromptDetail, revision: WorkbenchRevision) => {
      if (!orgUuid || targetPrompt.name?.trim()) {
        return;
      }

      let title = defaultGeneratedPromptTitle();
      const messageContent = truncateTitleMessageContent(titleMessageContent(revision));
      if (revision.messages.length > 0 && messageContent) {
        try {
          const result = await generateWorkbenchTitle({
            orgUuid,
            workspaceId: activeWorkspaceId,
            body: {
              message_content: messageContent,
              model: revision.model_name,
            },
            signal: new AbortController().signal,
          });
          const generatedTitle = result.completion?.trim();
          if (generatedTitle) {
            title = generatedTitle;
          }
        } catch {
          // Keep the timestamp fallback when title generation fails.
        }
      }

      try {
        const updated = await updateWorkbenchPrompt(orgUuid, targetPrompt.id, { name: title });
        const updatedName = updated.name?.trim() || title;
        const updatedPrompt = { ...targetPrompt, ...updated, name: updatedName };
        setPrompt((current) =>
          current?.id === targetPrompt.id
            ? {
                ...current,
                ...updated,
                name: updatedName,
                latest_revision: current.latest_revision ?? updated.latest_revision,
              }
            : current,
        );
        setPromptName(updatedName);
        setPromptList((current) => mergePromptSummaries(current, updatedPrompt));
      } catch {
        // Title generation should not block running or opening a generated prompt.
      }
    },
    [activeWorkspaceId, orgUuid],
  );

  const discardDraftChanges = useCallback(async () => {
    if (!orgUuid || !prompt) {
      return;
    }
    const latestDraft = normalizeRevision(prompt.latest_revision, draft.model_name);
    const latestVariables = extractVariables(latestDraft);
    const latestDraftKey = workbenchDraftAutosaveKey(prompt.id, latestDraft);
    window.clearTimeout(saveTimerRef.current);
    lastSavedDraftKeyRef.current = latestDraftKey;
    latestRevisionDraftKeyRef.current = latestDraftKey;
    setDraft(latestDraft);
    setDraftVersion(undefined);
    setVariableValues(Object.fromEntries(latestVariables.map((name) => [name, ""])));
    setEvaluateComparisons([]);
    setResponseTab("preview");
    setResponseText("");
    setRunError(null);
    setStreamEvents([]);
    setLastRunRequest(null);
    if (activeMode === "evaluate" && !canEvaluateRevision(latestDraft)) {
      setActiveMode("prompt");
      syncWorkbenchTabUrl("prompt", "replace");
    }
    setSaveStatus("Saved");
    const storedEvaluations = await listWorkbenchEvaluations(orgUuid, latestDraft.id).catch(() => []);
    setEvaluateRows(evaluateRowsFromEvaluations(storedEvaluations, latestVariables));
    try {
      const response = await setWorkbenchKV(
        orgUuid,
        prompt.id,
        "draft_revision",
        JSON.stringify(buildRevisionPayload(latestDraft, { includeEmptyMessages: true })),
        draftVersion,
      );
      setDraftVersion(response.version);
    } catch {
      setSaveStatus("Save failed");
    }
  }, [activeMode, draft.model_name, draftVersion, orgUuid, prompt]);

  const runPrompt = useCallback(async () => {
    if (isRunning) {
      runControllerRef.current?.abort();
      return;
    }
    if (!orgUuid || !prompt) {
      setRunError("No organization or prompt is available.");
      return;
    }
    if (hasMissingVariableValues) {
      setActiveDrawer("variables");
      setToolForm(null);
      return;
    }

    const controller = new AbortController();
    const submittedRevision = buildRunRevisionPayload(draft, variableValues, examples);
    const shouldGenerateTitle = !isPromptReadOnly && !prompt.name?.trim();
    runControllerRef.current = controller;
    setIsRunning(true);
    setRunError(null);
    setResponseText("");
    setStreamEvents([]);
    setLastRunRequest(submittedRevision);
    setResponseTab("preview");

    try {
      if (shouldGenerateTitle) {
        await autoTitlePrompt(prompt, draft);
      }
      await streamSmoothedWorkbenchText({
        signal: controller.signal,
        onDisplayText: setResponseText,
        onEvent: (event) => setStreamEvents((current) => [...current, event]),
        stream: (onEvent) =>
          streamWorkbenchCompletion({
            orgUuid,
            workspaceId: activeWorkspaceId,
            body: submittedRevision,
            signal: controller.signal,
            onEvent,
          }),
      });
    } catch (error) {
      if ((error as { name?: string }).name !== "AbortError") {
        setRunError(errorMessage(error));
      }
    } finally {
      setIsRunning(false);
      runControllerRef.current = null;
      const [revisionsResult] = await Promise.allSettled([
        listWorkbenchRevisions(orgUuid, prompt.id, true),
        listWorkbenchEvaluations(orgUuid, submittedRevision.id),
        getPrepaidCredits(orgUuid),
      ]);
      if (revisionsResult.status === "fulfilled" && revisionsResult.value.length > 0) {
        setPromptList((current) => mergePromptSummaries(current, prompt));
      }
    }
  }, [
    activeWorkspaceId,
    autoTitlePrompt,
    draft,
    examples,
    hasMissingVariableValues,
    isPromptReadOnly,
    isRunning,
    orgUuid,
    prompt,
    variableValues,
  ]);

  const runEvaluateRows = useCallback(async () => {
    if (isRunning) {
      runControllerRef.current?.abort();
      return;
    }
    if (!orgUuid || !prompt || !evaluateRows.length || hasUnsavedChanges) {
      return;
    }

    const rowsToRun = evaluateRows;
    const comparisonsToRun = evaluateComparisons;
    const controller = new AbortController();
    runControllerRef.current = controller;
    setIsRunning(true);
    setRunError(null);
    setResponseText("");
    setStreamEvents([]);
    setResponseTab("preview");
    setEvaluateRows((current) =>
      current.map((row) =>
        rowsToRun.some((candidate) => candidate.id === row.id)
          ? {
              ...row,
              modelOutput: "",
              runError: null,
              isRunning: true,
              comparisonOutputs: {
                ...row.comparisonOutputs,
                ...Object.fromEntries(
                  comparisonsToRun.map((comparison) => [
                    comparison.id,
                    {
                      ...(row.comparisonOutputs[comparison.id] ?? emptyComparisonOutput()),
                      modelOutput: "",
                      runError: null,
                      isRunning: true,
                    },
                  ]),
                ),
              },
            }
          : row,
      ),
    );

    try {
      for (const row of rowsToRun) {
        if (controller.signal.aborted) {
          break;
        }
        let rowAborted = false;
        const submittedRevision = buildRunRevisionPayload(draft, row.values, examples);
        let rowOutput = "";
        setLastRunRequest(submittedRevision);
        try {
          rowOutput = await streamSmoothedWorkbenchText({
            signal: controller.signal,
            onEvent: (event) => setStreamEvents((current) => [...current, event]),
            onDisplayText: (displayText) => {
              setEvaluateRows((current) =>
                current.map((item) => (item.id === row.id ? { ...item, modelOutput: displayText } : item)),
              );
            },
            stream: (onEvent) =>
              streamWorkbenchCompletion({
                orgUuid,
                workspaceId: activeWorkspaceId,
                body: submittedRevision,
                signal: controller.signal,
                onEvent,
              }),
          });
          if (rowOutput && row.evaluationId) {
            await saveWorkbenchEvaluationCompletion(orgUuid, row.evaluationId, rowOutput).catch(() => undefined);
          }
        } catch (error) {
          if ((error as { name?: string }).name === "AbortError") {
            rowAborted = true;
            break;
          }
          setEvaluateRows((current) =>
            current.map((item) => (item.id === row.id ? { ...item, runError: errorMessage(error) } : item)),
          );
        } finally {
          setEvaluateRows((current) =>
            current.map((item) => (item.id === row.id ? { ...item, isRunning: false } : item)),
          );
        }

        if (rowAborted || controller.signal.aborted) {
          break;
        }

        for (const comparison of comparisonsToRun) {
          if (controller.signal.aborted) {
            break;
          }
          const submittedComparison = buildRunRevisionPayload(comparison.revision, row.values, examples);
          let comparisonOutput = "";
          setLastRunRequest(submittedComparison);
          try {
            comparisonOutput = await streamSmoothedWorkbenchText({
              signal: controller.signal,
              onEvent: (event) => setStreamEvents((current) => [...current, event]),
              onDisplayText: (displayText) => {
                setEvaluateRows((current) =>
                  current.map((item) => {
                    if (item.id !== row.id) {
                      return item;
                    }
                    const existing = item.comparisonOutputs[comparison.id] ?? emptyComparisonOutput();
                    return {
                      ...item,
                      comparisonOutputs: {
                        ...item.comparisonOutputs,
                        [comparison.id]: {
                          ...existing,
                          modelOutput: displayText,
                          runError: null,
                          isRunning: true,
                        },
                      },
                    };
                  }),
                );
              },
              stream: (onEvent) =>
                streamWorkbenchCompletion({
                  orgUuid,
                  workspaceId: activeWorkspaceId,
                  body: submittedComparison,
                  signal: controller.signal,
                  onEvent,
                }),
            });
            if (!comparisonOutput) {
              setEvaluateRows((current) =>
                current.map((item) => {
                  if (item.id !== row.id) {
                    return item;
                  }
                  return {
                    ...item,
                    comparisonOutputs: {
                      ...item.comparisonOutputs,
                      [comparison.id]: {
                        ...(item.comparisonOutputs[comparison.id] ?? emptyComparisonOutput()),
                        isRunning: false,
                      },
                    },
                  };
                }),
              );
            }
          } catch (error) {
            if ((error as { name?: string }).name === "AbortError") {
              break;
            }
            setEvaluateRows((current) =>
              current.map((item) => {
                if (item.id !== row.id) {
                  return item;
                }
                return {
                  ...item,
                  comparisonOutputs: {
                    ...item.comparisonOutputs,
                    [comparison.id]: {
                      ...(item.comparisonOutputs[comparison.id] ?? emptyComparisonOutput()),
                      runError: errorMessage(error),
                    },
                  },
                };
              }),
            );
          } finally {
            setEvaluateRows((current) =>
              current.map((item) => {
                if (item.id !== row.id) {
                  return item;
                }
                return {
                  ...item,
                  comparisonOutputs: {
                    ...item.comparisonOutputs,
                    [comparison.id]: {
                      ...(item.comparisonOutputs[comparison.id] ?? emptyComparisonOutput()),
                      isRunning: false,
                    },
                  },
                };
              }),
            );
          }
        }
      }
    } finally {
      setIsRunning(false);
      runControllerRef.current = null;
      setEvaluateRows((current) =>
        current.map((row) => ({
          ...row,
          isRunning: false,
          comparisonOutputs: Object.fromEntries(
            Object.entries(row.comparisonOutputs).map(([comparisonId, output]) => [
              comparisonId,
              { ...output, isRunning: false },
            ]),
          ),
        })),
      );
      const [revisionsResult] = await Promise.allSettled([
        listWorkbenchRevisions(orgUuid, prompt.id, true),
        listWorkbenchEvaluations(orgUuid, draft.id),
        getPrepaidCredits(orgUuid),
      ]);
      if (revisionsResult.status === "fulfilled" && revisionsResult.value.length > 0) {
        setPromptList((current) => mergePromptSummaries(current, prompt));
      }
    }
  }, [
    activeWorkspaceId,
    draft,
    evaluateComparisons,
    evaluateRows,
    examples,
    hasUnsavedChanges,
    isRunning,
    orgUuid,
    prompt,
  ]);

  useEffect(() => {
    const handleWorkbenchShortcut = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        if (promptMenuOpen || promptPickerOpen || activeDrawer) {
          event.preventDefault();
          setPromptMenuOpen(false);
          setPromptPickerOpen(false);
          setActiveDrawer(null);
          setToolForm(null);
          setExampleFormOpen(false);
        }
        return;
      }

      if (event.key !== "Enter" || (!event.metaKey && !event.ctrlKey)) {
        return;
      }
      if (
        codeOpen ||
        renameOpen ||
        deleteConfirmOpen ||
        shareOpen ||
        improveOpen ||
        promptPickerOpen ||
        isCreatingPrompt
      ) {
        return;
      }
      event.preventDefault();
      if (activeMode === "evaluate") {
        void runEvaluateRows();
      } else {
        void runPrompt();
      }
    };

    window.addEventListener("keydown", handleWorkbenchShortcut);
    return () => window.removeEventListener("keydown", handleWorkbenchShortcut);
  }, [
    activeDrawer,
    activeMode,
    codeOpen,
    deleteConfirmOpen,
    improveOpen,
    isCreatingPrompt,
    promptMenuOpen,
    promptPickerOpen,
    renameOpen,
    shareOpen,
    runEvaluateRows,
    runPrompt,
  ]);

  const renamePrompt = useCallback(async () => {
    if (!orgUuid || !prompt) {
      return;
    }
    const nextName = renameValue.trim();
    if (!nextName) {
      return;
    }
    const updated = await updateWorkbenchPrompt(orgUuid, prompt.id, { name: nextName });
    const refreshed = await getWorkbenchPrompt(orgUuid, prompt.id).catch(() => updated);
    const refreshedName = refreshed.name?.trim() || updated.name?.trim() || nextName;
    setPrompt(refreshed);
    setPromptName(refreshedName);
    try {
      setPromptList(await listWorkbenchPrompts(orgUuid));
    } catch {
      setPromptList((current) => mergePromptSummaries(current, { ...refreshed, name: refreshedName }));
    }
    setRenameOpen(false);
    setPromptMenuOpen(false);
  }, [orgUuid, prompt, renameValue]);

  const deletePrompt = useCallback(async () => {
    const targetPrompt = deletePromptTarget ?? prompt;
    if (!orgUuid || !targetPrompt) {
      return;
    }
    setIsDeletingPrompt(true);
    try {
      await deleteWorkbenchPrompt(orgUuid, targetPrompt.id);
      setDeleteConfirmOpen(false);
      setDeletePromptTarget(null);
      setPromptList((current) => current.filter((item) => item.id !== targetPrompt.id));
      if (targetPrompt.id === prompt?.id) {
        syncWorkbenchIndexUrl("replace");
        await loadWorkbench();
      } else {
        try {
          const refreshed = await listWorkbenchPrompts(orgUuid);
          setPromptList(refreshed.filter((item) => item.id !== targetPrompt.id));
        } catch {
          // The optimistic removal above already matches the successful delete.
        }
      }
    } catch (error) {
      setSaveStatus(errorMessage(error));
    } finally {
      setIsDeletingPrompt(false);
    }
  }, [deletePromptTarget, loadWorkbench, orgUuid, prompt]);

  const requestPromptDelete = useCallback((targetPrompt: WorkbenchPromptSummary | null = null) => {
    setPromptMenuOpen(false);
    setDeletePromptTarget(targetPrompt);
    setDeleteConfirmOpen(true);
  }, []);

  const createNewPrompt = useCallback(async () => {
    if (!orgUuid || creatingPromptRef.current) {
      return;
    }
    creatingPromptRef.current = true;
    const creationSeq = ++workbenchLoadSeqRef.current;
    const isCurrentCreation = () => workbenchLoadSeqRef.current === creationSeq;
    setIsCreatingPrompt(true);
    window.clearTimeout(saveTimerRef.current);
    runControllerRef.current?.abort();
    runControllerRef.current = null;
    setIsRunning(false);
    resetCreatingPromptState(normalizeNewPromptRevision(undefined, defaultModelName));
    syncWorkbenchNewUrl("push");
    try {
      const created = await createWorkspacePrompt(orgUuid, activeWorkspaceId);
      if (!isCurrentCreation()) {
        return;
      }
      const normalizedDraft = normalizeNewPromptRevision(created.latest_revision, defaultModelName);
      syncWorkbenchPromptUrl(created.id, "replace", { resetTab: true });
      applyPromptState(created, normalizedDraft, undefined, { latestSavedDraft: normalizedDraft });
      try {
        const refreshedPrompts = await listWorkbenchPrompts(orgUuid);
        if (!isCurrentCreation()) {
          return;
        }
        setPromptList(mergePromptSummaries(refreshedPrompts, created));
      } catch {
        if (isCurrentCreation()) {
          setPromptList((current) => mergePromptSummaries(current, created));
        }
      }
    } catch (error) {
      if (isCurrentCreation()) {
        setSaveStatus(errorMessage(error));
      }
    } finally {
      creatingPromptRef.current = false;
      setIsCreatingPrompt(false);
    }
  }, [activeWorkspaceId, applyPromptState, defaultModelName, orgUuid, resetCreatingPromptState]);

  const selectPrompt = useCallback(
    async (item: WorkbenchPromptSummary) => {
      setPromptPickerOpen(false);
      if (!orgUuid) {
        return;
      }
      const loadSeq = ++workbenchLoadSeqRef.current;
      const isCurrentLoad = () => workbenchLoadSeqRef.current === loadSeq;
      const detail = await getWorkbenchPrompt(orgUuid, item.id);
      const draftKV = await getWorkbenchKV(orgUuid, item.id, "draft_revision").catch(() => null);
      const normalizedDraft = normalizeRevision(
        parseDraftRevision(draftKV?.value) ??
          parseDraftRevision(detail.kv_store?.draft_revision) ??
          detail.latest_revision,
        defaultModelName,
      );
      const [storedEvaluations, revisions] = await Promise.all([
        listWorkbenchEvaluations(orgUuid, normalizedDraft.id).catch(() => []),
        listWorkbenchRevisions(orgUuid, item.id, true).catch(() => []),
      ]);
      if (!isCurrentLoad()) {
        return;
      }
      syncWorkbenchPromptUrl(detail.id, "push");
      applyPromptState(detail, normalizedDraft, draftKV?.version);
      setPromptRevisionCount(Math.max(1, revisions.length));
      setEvaluateRows(evaluateRowsFromEvaluations(storedEvaluations, extractVariables(normalizedDraft)));
    },
    [applyPromptState, defaultModelName, orgUuid],
  );

  const copyPrompt = useCallback(async () => {
    if (!orgUuid || !prompt) {
      return;
    }
    setPromptMenuOpen(false);
    setSaveStatus("Creating");
    try {
      const savedSourceDraft = normalizeRevision(prompt.latest_revision, draft.model_name);
      const savedSourcePayload = buildRevisionPayload(savedSourceDraft, { includeEmptyMessages: true });
      const currentDraftPayload = buildRevisionPayload(draft, { includeEmptyMessages: true });
      const copyName = prompt.name?.trim() ? `${prompt.name.trim()} copy` : "";
      const created = await createWorkspacePrompt(orgUuid, activeWorkspaceId, {
        name: copyName,
        latest_revision: buildRevisionPayload(savedSourceDraft, { includeEmptyMessages: true, newRevisionId: true }),
      });
      const savedDraft = normalizeRevision(created.latest_revision ?? savedSourceDraft, draft.model_name);
      const copiedDraft = normalizeRevision(
        JSON.stringify(currentDraftPayload) === JSON.stringify(savedSourcePayload)
          ? savedDraft
          : {
              ...currentDraftPayload,
              id: workbenchId("workbench-revision"),
              created_at: new Date().toISOString(),
              is_latest: true,
            },
        draft.model_name,
      );
      syncWorkbenchPromptUrl(created.id, "push");
      applyPromptState({ ...created, latest_revision: savedDraft }, copiedDraft, undefined, {
        latestSavedDraft: savedDraft,
      });
      try {
        setPromptList(await listWorkbenchPrompts(orgUuid));
      } catch {
        setPromptList((current) => mergePromptSummaries(current, created));
      }
    } catch (error) {
      setSaveStatus(errorMessage(error));
    }
  }, [activeWorkspaceId, applyPromptState, draft, orgUuid, prompt]);

  const sharePrompt = useCallback(async () => {
    if (!orgUuid || !prompt) {
      return;
    }
    setIsSharingPrompt(true);
    setShareError(null);
    try {
      const shared = await shareWorkbenchPrompt(orgUuid, prompt.id);
      setPrompt(shared);
      setPromptList((current) => mergePromptSummaries(current, shared));
    } catch (error) {
      setShareError(errorMessage(error));
    } finally {
      setIsSharingPrompt(false);
    }
  }, [orgUuid, prompt]);

  const restoreRevision = useCallback(
    async (revisionId: string) => {
      if (!orgUuid || !prompt) {
        return;
      }
      setSaveStatus("Loading version");
      try {
        const restored = normalizeRevision(
          await getWorkbenchRevision(orgUuid, prompt.id, revisionId),
          draft.model_name,
        );
        const revisionVariables = extractVariables(restored);
        const storedEvaluations = await listWorkbenchEvaluations(orgUuid, restored.id).catch(() => []);
        lastSavedDraftKeyRef.current = null;
        setDraft(restored);
        setDraftVersion(undefined);
        setVariableValues(Object.fromEntries(revisionVariables.map((name) => [name, ""])));
        setEvaluateRows(evaluateRowsFromEvaluations(storedEvaluations, revisionVariables));
        setEvaluateComparisons([]);
        const nextMode = currentRouteTab() === "evaluate" && canEvaluateRevision(restored) ? "evaluate" : "prompt";
        setActiveMode(nextMode);
        if (nextMode === "prompt") {
          syncWorkbenchTabUrl("prompt", "replace");
        }
        setResponseTab("preview");
        setResponseText("");
        setRunError(null);
        setStreamEvents([]);
        setLastRunRequest(null);
        setActiveDrawer(null);
        setSaveStatus("Unsaved");
      } catch (error) {
        setSaveStatus(errorMessage(error));
      }
    },
    [draft.model_name, orgUuid, prompt],
  );

  const addCustomTool = useCallback(() => {
    let parsedSchema: Record<string, unknown>;
    try {
      parsedSchema = JSON.parse(customTool.schema) as Record<string, unknown>;
    } catch {
      parsedSchema = { type: "object", additionalProperties: true };
    }
    const name = customTool.name.trim();
    if (!name) {
      return;
    }
    setDraft((current) => ({
      ...current,
      tools: [
        ...current.tools,
        {
          id: workbenchId("tool"),
          name,
          description: customTool.description.trim(),
          input_schema: parsedSchema,
        },
      ],
    }));
    setCustomTool({ name: "", description: "", schema: defaultSchema });
    setToolForm(null);
    setActiveDrawer(null);
  }, [customTool]);

  const addWebSearchTool = useCallback(() => {
    setDraft((current) => ({
      ...current,
      tools: [...current.tools, webSearchToolFromForm(webSearchTool, workbenchId("tool"))],
    }));
    setWebSearchTool(defaultWebSearchToolForm());
    setToolForm(null);
    setActiveDrawer(null);
  }, [webSearchTool]);

  const updateWebSearchTool = useCallback(
    (index: number) => {
      setDraft((current) => ({
        ...current,
        tools: current.tools.map((tool, toolIndex) =>
          toolIndex === index ? webSearchToolFromForm(webSearchTool, tool.id ?? workbenchId("tool")) : tool,
        ),
      }));
      setWebSearchTool(defaultWebSearchToolForm());
      setToolForm(null);
      setActiveDrawer(null);
    },
    [webSearchTool],
  );

  const removeTool = useCallback((index: number) => {
    setDraft((current) => ({ ...current, tools: current.tools.filter((_, toolIndex) => toolIndex !== index) }));
  }, []);

  const generateTestCase = useCallback(async () => {
    if (!orgUuid || !variables.length) {
      return;
    }
    const controller = new AbortController();
    let streamedText = "";
    await streamGenerateTestCase({
      orgUuid,
      workspaceId: activeWorkspaceId,
      body: buildGenerateVariablePayload(draft, examples, variableGenerationLogic),
      signal: controller.signal,
      onEvent: (event) => {
        const delta = textDeltaFromEvent(event);
        if (delta) {
          streamedText += delta;
        }
      },
    }).catch(() => undefined);
    const tagged = parseTaggedVariables(streamedText, variables);
    setVariableValues((current) => {
      const next = { ...current };
      variables.forEach((name) => {
        next[name] = tagged[name] || next[name] || `${name} sample`;
      });
      return next;
    });
    const planning = parseTaggedValue(streamedText, "planning");
    if (planning && variableGenerationLogicOpen && !variableGenerationLogic.trim()) {
      setVariableGenerationLogic(planning);
    }
  }, [activeWorkspaceId, draft, examples, orgUuid, variableGenerationLogic, variableGenerationLogicOpen, variables]);

  const generateVariableLogic = useCallback(async () => {
    if (!orgUuid || !variables.length) {
      return;
    }
    setVariableGenerationLogicOpen(true);
    setIsGeneratingVariableLogic(true);
    const controller = new AbortController();
    let streamedText = "";
    try {
      await streamGenerateTestCase({
        orgUuid,
        workspaceId: activeWorkspaceId,
        body: buildGenerateVariablePayload(draft, examples, variableGenerationLogic),
        signal: controller.signal,
        onEvent: (event) => {
          const delta = textDeltaFromEvent(event);
          if (delta) {
            streamedText += delta;
          }
        },
      });
      const planning = parseTaggedValue(streamedText, "planning");
      const fallbackLogic = stripTaggedVariables(streamedText, ["planning", ...variables]).trim();
      setVariableGenerationLogic(
        planning ||
          fallbackLogic ||
          variableGenerationLogic ||
          "Describe how to create realistic values for this test case.",
      );
    } catch {
      setVariableGenerationLogic((current) => current || "Describe how to create realistic values for this test case.");
    } finally {
      setIsGeneratingVariableLogic(false);
    }
  }, [activeWorkspaceId, draft, examples, orgUuid, variableGenerationLogic, variables]);

  const generateExample = useCallback(async () => {
    if (!orgUuid || !variables.length) {
      return;
    }
    setIsGeneratingExample(true);
    const controller = new AbortController();
    let streamedText = "";
    await streamGenerateTestCase({
      orgUuid,
      workspaceId: activeWorkspaceId,
      body: buildGenerateExamplePayload(draft, examples, stringValue(prompt?.kv_store?.test_case_generation_logic)),
      signal: controller.signal,
      onEvent: (event) => {
        const delta = textDeltaFromEvent(event);
        if (delta) {
          streamedText += delta;
        }
      },
    }).catch(() => undefined);
    const tagged = parseTaggedVariables(streamedText, variables);
    setExampleValues((current) => {
      const next = { ...current };
      variables.forEach((name) => {
        next[name] = tagged[name] || next[name] || `${name} sample`;
      });
      return next;
    });
    const untagged = stripTaggedVariables(streamedText, ["planning", ...variables]).trim();
    if (untagged && !exampleIdealOutput.trim()) {
      setExampleIdealOutput(untagged);
    }
    setIsGeneratingExample(false);
  }, [activeWorkspaceId, draft, exampleIdealOutput, examples, orgUuid, prompt, variables]);

  const createEvaluateRowRecord = useCallback(
    async (row: EvaluateTestCase) => {
      if (!orgUuid || !draft.id) {
        return;
      }
      try {
        const created = await createWorkbenchEvaluation(orgUuid, draft.id, evaluateRowRequestBody(row));
        setEvaluateRows((current) =>
          current.map((item) => (item.id === row.id ? mergeCreatedEvaluationIntoRow(item, created) : item)),
        );
      } catch {
        // Keep the local row editable when persistence is unavailable.
      }
    },
    [draft.id, orgUuid, variables],
  );

  const updateEvaluateRowVariables = useCallback(
    async (row: EvaluateTestCase) => {
      if (!orgUuid || !row.evaluationId) {
        return;
      }
      try {
        const updated = await updateWorkbenchEvaluationVariables(orgUuid, row.evaluationId, row.values);
        setEvaluateRows((current) =>
          current.map((item) =>
            item.id === row.id ? mergeEvaluationVariablesIntoRow(item, updated, variables) : item,
          ),
        );
      } catch {
        // Local edits remain visible even if the backend rejects the patch.
      }
    },
    [orgUuid, variables],
  );

  const updateEvaluateRowIdealOutput = useCallback(
    async (row: EvaluateTestCase) => {
      if (!orgUuid || !row.evaluationId) {
        return;
      }
      try {
        const updated = await updateWorkbenchEvaluationGoldenAnswer(orgUuid, row.evaluationId, row.idealOutput);
        setEvaluateRows((current) =>
          current.map((item) => (item.id === row.id ? mergeEvaluationGoldenAnswerIntoRow(item, updated) : item)),
        );
      } catch {
        // Local edits remain visible even if the backend rejects the patch.
      }
    },
    [orgUuid, variables],
  );

  const deleteEvaluateRowRecord = useCallback(
    async (row: EvaluateTestCase) => {
      if (!orgUuid || !row.evaluationId) {
        return;
      }
      try {
        await deleteWorkbenchEvaluation(orgUuid, row.evaluationId);
      } catch {
        // Deletion is already reflected locally; the next list refresh can reconcile it.
      }
    },
    [orgUuid],
  );

  const generateEvaluateTestCases = useCallback(
    async (count: number) => {
      if (!orgUuid || !variables.length) {
        return;
      }
      const controller = new AbortController();
      const generatedRows: EvaluateTestCase[] = [];
      await streamGenerateTestCases({
        orgUuid,
        workspaceId: activeWorkspaceId,
        body: buildGenerateTestCasesPayload(draft, count, examples),
        signal: controller.signal,
        onEvent: (event) => {
          const row = evaluateRowFromGeneratedEvent(event, variables);
          if (row) {
            generatedRows.push(row);
          }
        },
      }).catch(() => undefined);
      const fallbackRows = Array.from({ length: Math.max(0, count - generatedRows.length) }, (_, index) =>
        createEvaluateRow(variables, generatedRows.length + index + 1),
      );
      const rowsToCreate = [...generatedRows, ...fallbackRows];
      setEvaluateRows((current) => [...current, ...rowsToCreate]);
      await Promise.all(rowsToCreate.map((row) => createEvaluateRowRecord(row)));
    },
    [activeWorkspaceId, createEvaluateRowRecord, draft, examples, orgUuid, variables],
  );

  const resetExampleForm = useCallback(() => {
    setExampleFormOpen(false);
    setEditingExampleId(null);
    setExampleValues(Object.fromEntries(variables.map((name) => [name, ""])));
    setExampleIdealOutput("");
    setExampleAdditionalContext("");
    setExampleContextOpen(false);
  }, [variables]);

  const openNewExampleForm = useCallback(() => {
    setEditingExampleId(null);
    setExampleValues(Object.fromEntries(variables.map((name) => [name, ""])));
    setExampleIdealOutput("");
    setExampleAdditionalContext("");
    setExampleContextOpen(false);
    setExampleFormOpen(true);
  }, [variables]);

  const editExample = useCallback(
    (example: WorkbenchExample) => {
      setEditingExampleId(example.id);
      setExampleValues(Object.fromEntries(variables.map((name) => [name, example.values[name] ?? ""])));
      setExampleIdealOutput(example.idealOutput);
      setExampleAdditionalContext(example.additionalContext);
      setExampleContextOpen(Boolean(example.additionalContext.trim()));
      setExampleFormOpen(true);
    },
    [variables],
  );

  const removeExample = useCallback(
    (exampleId: string) => {
      setExamples((current) => current.filter((example) => example.id !== exampleId));
      if (editingExampleId === exampleId) {
        resetExampleForm();
      }
    },
    [editingExampleId, resetExampleForm],
  );

  const addExample = useCallback(() => {
    if (!variables.every((name) => exampleValues[name]?.trim()) || !exampleIdealOutput.trim()) {
      return;
    }
    const nextExample = {
      id: editingExampleId ?? workbenchId("example"),
      values: Object.fromEntries(variables.map((name) => [name, exampleValues[name] ?? ""])),
      idealOutput: exampleIdealOutput.trim(),
      additionalContext: exampleAdditionalContext.trim(),
    };
    setExamples((current) => {
      if (!editingExampleId) {
        return [...current, nextExample];
      }
      return current.map((example) => (example.id === editingExampleId ? nextExample : example));
    });
    resetExampleForm();
  }, [editingExampleId, exampleAdditionalContext, exampleIdealOutput, exampleValues, resetExampleForm, variables]);

  const improvePrompt = useCallback(async () => {
    if (!orgUuid || !hasPromptText || isPromptReadOnly) {
      return;
    }
    setIsImproving(true);
    const controller = new AbortController();
    const originalPromptText = messageText(draft.messages[0]);
    let generated = "";
    try {
      generated = await streamSmoothedWorkbenchText({
        signal: controller.signal,
        onDisplayText: (displayText) => {
          if (displayText.trim()) {
            setMessageText(0, displayText);
          }
        },
        stream: (onEvent) =>
          streamGeneratePrompt({
            orgUuid,
            workspaceId: activeWorkspaceId,
            body: {
              ...buildRevisionPayload(draft, { includeEmptyMessages: false }),
              feedback: improveFeedback,
              thinking_enabled: improveThinkingEnabled,
            },
            signal: controller.signal,
            onEvent,
          }),
      });
      if (generated.trim()) {
        setMessageText(0, generated.trim());
      }
    } catch {
      setMessageText(0, originalPromptText);
    }
    setIsImproving(false);
    setImproveOpen(false);
    setImproveFeedback("");
  }, [
    activeWorkspaceId,
    draft,
    hasPromptText,
    improveFeedback,
    improveThinkingEnabled,
    isPromptReadOnly,
    orgUuid,
    setMessageText,
  ]);

  const generatePromptFromTask = useCallback(async () => {
    const task = promptGeneratorTask.trim();
    if (!orgUuid || !task || isPromptReadOnly || promptGeneratorWarning) {
      return;
    }
    clearPromptGeneratorOutputFallback();
    setIsGeneratingPrompt(true);
    setPromptGeneratorStep("output");
    setPromptGeneratorOutput("");
    setPromptGeneratorError(null);
    const controller = new AbortController();
    promptGeneratorControllerRef.current = controller;
    promptGeneratorOutputFallbackRef.current = window.setTimeout(() => {
      setPromptGeneratorStep("output");
      promptGeneratorOutputFallbackRef.current = undefined;
    }, 4000);
    let rawOutput = "";
    let generatedInstructions = "";
    try {
      rawOutput = await streamSmoothedWorkbenchText({
        signal: controller.signal,
        onRawText: (nextRawOutput) => {
          rawOutput = nextRawOutput;
        },
        onDisplayText: (displayText) => {
          if (displayText) {
            clearPromptGeneratorOutputFallback();
            setPromptGeneratorStep("output");
            setPromptGeneratorOutput(displayText);
          }
        },
        displayTextFromRaw: extractGeneratedPromptInstructions,
        stream: (onEvent) =>
          streamGeneratePrompt({
            orgUuid,
            workspaceId: activeWorkspaceId,
            body: {
              task,
              target_thinking_mode: promptGeneratorThinkingEnabled,
              isPromptConversion: false,
            },
            signal: controller.signal,
            onEvent,
          }),
      });
      generatedInstructions = extractGeneratedPromptInstructions(rawOutput);
      clearPromptGeneratorOutputFallback();
      if (generatedInstructions.trim()) {
        setPromptGeneratorStep("output");
        setPromptGeneratorOutput(generatedInstructions.trim());
      } else if (rawOutput.trim()) {
        setPromptGeneratorStep("output");
        setPromptGeneratorOutput(rawOutput.trim());
        setPromptGeneratorError("Generated prompt is malformed, displaying raw output");
      } else {
        setPromptGeneratorStep("generate");
        setPromptGeneratorError("Claude did not return a prompt. Try adding more detail.");
      }
    } catch (error) {
      clearPromptGeneratorOutputFallback();
      generatedInstructions = extractGeneratedPromptInstructions(rawOutput);
      if (!controller.signal.aborted) {
        setPromptGeneratorError(errorMessage(error));
        if (!generatedInstructions.trim() && !rawOutput.trim()) {
          setPromptGeneratorStep("generate");
        }
      } else if (!generatedInstructions.trim() && !rawOutput.trim()) {
        setPromptGeneratorStep("generate");
      }
    } finally {
      clearPromptGeneratorOutputFallback();
      if (promptGeneratorControllerRef.current === controller) {
        promptGeneratorControllerRef.current = null;
      }
      setIsGeneratingPrompt(false);
    }
  }, [
    activeWorkspaceId,
    clearPromptGeneratorOutputFallback,
    isPromptReadOnly,
    orgUuid,
    promptGeneratorTask,
    promptGeneratorThinkingEnabled,
    promptGeneratorWarning,
  ]);

  const resetPromptGenerator = useCallback(() => {
    promptGeneratorControllerRef.current?.abort();
    promptGeneratorControllerRef.current = null;
    clearPromptGeneratorOutputFallback();
    setIsGeneratingPrompt(false);
    setPromptGeneratorConfirmAction(null);
    setPromptGeneratorOpen(false);
    setPromptGeneratorStep("generate");
    setPromptGeneratorTask("");
    setPromptGeneratorOutput("");
    setPromptGeneratorExamplesExpanded(false);
    setPromptGeneratorError(null);
    setPromptGeneratorThinkingEnabled(false);
  }, [clearPromptGeneratorOutputFallback]);

  const resetPromptGeneratorOutput = useCallback(() => {
    promptGeneratorControllerRef.current?.abort();
    promptGeneratorControllerRef.current = null;
    clearPromptGeneratorOutputFallback();
    setIsGeneratingPrompt(false);
    setPromptGeneratorConfirmAction(null);
    setPromptGeneratorOutput("");
    setPromptGeneratorError(null);
    setPromptGeneratorStep("generate");
  }, [clearPromptGeneratorOutputFallback]);

  const closePromptGenerator = useCallback(() => {
    if (isGeneratingPrompt) {
      setPromptGeneratorConfirmAction("close");
      return;
    }
    resetPromptGenerator();
  }, [isGeneratingPrompt, resetPromptGenerator]);

  const stopPromptGenerator = useCallback(() => {
    promptGeneratorControllerRef.current?.abort();
    promptGeneratorControllerRef.current = null;
    clearPromptGeneratorOutputFallback();
    setIsGeneratingPrompt(false);
    if (!promptGeneratorOutput.trim()) {
      setPromptGeneratorStep("generate");
    }
  }, [clearPromptGeneratorOutputFallback, promptGeneratorOutput]);

  const editPromptGeneratorInstructions = useCallback(() => {
    if (promptGeneratorOutput.trim()) {
      setPromptGeneratorConfirmAction("edit");
      return;
    }
    resetPromptGeneratorOutput();
  }, [promptGeneratorOutput, resetPromptGeneratorOutput]);

  const confirmPromptGeneratorAction = useCallback(() => {
    if (promptGeneratorConfirmAction === "close") {
      resetPromptGenerator();
      return;
    }
    if (promptGeneratorConfirmAction === "edit") {
      resetPromptGeneratorOutput();
    }
  }, [promptGeneratorConfirmAction, resetPromptGenerator, resetPromptGeneratorOutput]);

  const openGeneratedPromptInWorkbench = useCallback(async () => {
    const generatedPrompt = promptGeneratorOutput.trim();
    if (!generatedPrompt) {
      return;
    }
    const nextMessages: WorkbenchMessage[] = draft.messages.length
      ? draft.messages.map((message, index) =>
          index === 0 ? { ...message, content: replaceMessageText(message.content, generatedPrompt) } : message,
        )
      : [{ role: "human", content: [{ type: "text", text: generatedPrompt }] }];
    const nextDraft: WorkbenchRevision = {
      ...draft,
      messages: nextMessages,
      variables: extractVariables({ ...draft, messages: nextMessages }),
    };
    setDraft(nextDraft);
    resetPromptGenerator();

    if (!orgUuid || !prompt) {
      return;
    }
    try {
      const saved = await createWorkbenchRevision(
        orgUuid,
        prompt.id,
        buildRevisionPayload(nextDraft, { includeEmptyMessages: true, newRevisionId: true }),
      );
      const normalizedSaved = normalizeRevision(saved, nextDraft.model_name);
      lastSavedDraftKeyRef.current = workbenchDraftAutosaveKey(prompt.id, normalizedSaved);
      latestRevisionDraftKeyRef.current = workbenchDraftAutosaveKey(prompt.id, normalizedSaved);
      setDraft(normalizedSaved);
      setPrompt((current) =>
        current
          ? { ...current, latest_revision: normalizedSaved, updated_at: saved.created_at ?? current.updated_at }
          : current,
      );
      setPromptRevisionCount((current) => Math.max(2, current + 1));
      setSaveStatus(`Saved ${shortTime()}`);
      setDraftVersion(undefined);
      await autoTitlePrompt(prompt, normalizedSaved);
    } catch (error) {
      setSaveStatus(errorMessage(error));
    }
  }, [autoTitlePrompt, draft, orgUuid, prompt, promptGeneratorOutput, resetPromptGenerator]);

  const selectPromptGeneratorExample = useCallback((example: (typeof generatePromptExamples)[number]) => {
    trackWorkbenchEvent("metaprompter.example.selected", { example: example.id });
    setPromptGeneratorTask(example.task);
    setPromptGeneratorOutput("");
    setPromptGeneratorError(null);
    setPromptGeneratorStep("generate");
  }, []);

  if (!workbenchAccess.hasAccess) {
    return <WorkbenchAccessUnavailable productName={workbenchAccess.productName} />;
  }

  if (isLoading) {
    return (
      <WorkbenchShell>
        <div
          className="flex h-full items-center justify-center text-sm text-muted-foreground"
          data-testid="workbench_main_loader"
        >
          <Loader2 className="mr-2 size-4 animate-spin" aria-hidden />
          Loading Workbench
        </div>
      </WorkbenchShell>
    );
  }

  if (loadError) {
    return (
      <WorkbenchShell>
        <div className="flex h-full items-center justify-center px-6">
          <Alert variant="destructive" className="max-w-[520px]">
            <AlertCircle aria-hidden />
            <AlertTitle>Workbench could not load</AlertTitle>
            <AlertDescription className="gap-3">
              <p className="leading-6">{loadError}</p>
              <Button type="button" variant="secondary" onClick={loadWorkbench}>
                Retry
              </Button>
            </AlertDescription>
          </Alert>
        </div>
      </WorkbenchShell>
    );
  }

  return (
    <WorkbenchShell>
      <div className="relative h-full min-h-0 overflow-hidden">
        <header className="workbench-topbar">
          <div className="workbench-title-stack">
            <div className="workbench-top-actions">
              <Popover open={promptPickerOpen} onOpenChange={handlePromptPickerOpenChange}>
                <PopoverTrigger
                  render={
                    <Button
                      type="button"
                      variant="ghost"
                      data-testid="prompts-list-button"
                      aria-label="Open prompt list"
                      title="Open prompt list"
                      aria-expanded={promptPickerOpen}
                      className={clsx("workbench-mini-button", promptPickerOpen && "bg-accent")}
                    />
                  }
                >
                  <List className="size-4" aria-hidden />
                </PopoverTrigger>
                {promptPickerOpen ? (
                  <PromptPicker
                    prompts={visiblePromptList}
                    selectedPromptId={prompt?.id}
                    search={promptSearch}
                    setSearch={setPromptSearch}
                    onlyMine={promptOnlyMine}
                    setOnlyMine={setPromptOnlyMine}
                    selectedPromptTitle={promptTitle}
                    account={account}
                    onClose={() => setPromptPickerOpen(false)}
                    onCreate={createNewPrompt}
                    isCreating={isCreatingPrompt}
                    onSelect={selectPrompt}
                    onRequestDelete={requestPromptDelete}
                  />
                ) : null}
              </Popover>
              <Button
                type="button"
                variant="ghost"
                data-testid="new-prompt-button"
                aria-label="New prompt"
                title="New prompt"
                className="workbench-mini-button workbench-new-prompt-button"
                disabled={isCreatingPrompt}
                onClick={createNewPrompt}
              >
                {isCreatingPrompt ? (
                  <Loader2 className="size-4 animate-spin" aria-hidden />
                ) : (
                  <Plus className="size-4" aria-hidden />
                )}
              </Button>
            </div>

            <div className="relative min-w-0">
              <DropdownMenu
                open={promptMenuOpen}
                onOpenChange={(open) => {
                  setPromptMenuOpen(open);
                  if (open) {
                    setPromptPickerOpen(false);
                    setActiveDrawer(null);
                  }
                }}
              >
                <DropdownMenuTrigger
                  render={
                    <Button
                      type="button"
                      variant="ghost"
                      data-testid="prompt-settings-dropdown"
                      aria-label="Prompt settings"
                      aria-expanded={promptMenuOpen}
                      className="workbench-title-button"
                    />
                  }
                >
                  <span className="min-w-0 truncate">{promptTitle}</span>
                  <ChevronDown className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" sideOffset={6} className="w-[220px]">
                  <DropdownMenuItem
                    onClick={() => {
                      setRenameValue(promptTitle);
                      setRenameOpen(true);
                    }}
                  >
                    Rename prompt
                  </DropdownMenuItem>
                  <DropdownMenuItem disabled={!canSaveCurrentRevision} onClick={saveCurrentRevision}>
                    Save
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setActiveDrawer("history")}>Version history</DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem onClick={copyPrompt}>Make a copy</DropdownMenuItem>
                  <DropdownMenuItem
                    onClick={() => {
                      setShareError(null);
                      setShareOpen(true);
                    }}
                  >
                    Share
                  </DropdownMenuItem>
                  <DropdownMenuItem variant="destructive" onClick={() => requestPromptDelete(null)}>
                    Delete
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
              <div className="workbench-save-meta">
                <Lock className="size-3.5" aria-hidden />
                <span>{saveMeta}</span>
                {hasUnsavedChanges || saveStatus === "Saving" ? (
                  <>
                    <span className="workbench-save-separator" aria-hidden>
                      &middot;
                    </span>
                    <Button
                      type="button"
                      variant="link"
                      className="workbench-save-link"
                      disabled={!canSaveCurrentRevision}
                      onClick={saveCurrentRevision}
                    >
                      Save changes
                    </Button>
                  </>
                ) : null}
              </div>
            </div>
          </div>
        </header>

        <Tabs value={activeMode} onValueChange={changeMode} className="h-full min-h-0 gap-0">
          <div className="workbench-view-switcher">
            <TabsList aria-label="Workbench mode" className="rounded-full p-1 shadow-sm">
              <TabsTrigger value="prompt" className="min-w-24 rounded-full px-4 text-sm">
                Prompt
              </TabsTrigger>
              <TabsTrigger
                value="evaluate"
                disabled={!canEvaluate}
                title={!canEvaluate ? evaluateUnavailableReason : undefined}
                className="min-w-24 rounded-full px-4 text-sm"
              >
                Evaluate
              </TabsTrigger>
            </TabsList>
          </div>

          <TabsContent value="prompt" className="mt-0 min-h-0">
            <div className="workbench-header-actions">
              <Button type="button" variant="outline" size="lg" className="bg-transparent px-4" onClick={openCodeModal}>
                Get Code
              </Button>
              <Button
                type="button"
                aria-label={isRunning ? "Stop" : "Run ⌘ + ⏎"}
                size="lg"
                className="px-4"
                disabled={!canRun}
                onClick={runPrompt}
              >
                {isRunning ? (
                  <Loader2 className="size-4 animate-spin" aria-hidden />
                ) : (
                  <Play className="size-4" fill="currentColor" strokeWidth={0} aria-hidden />
                )}
                <span>{isRunning ? "Stop" : "Run"}</span>
                {isRunning ? null : <span className="text-xs font-medium text-primary-foreground/70">⌘ + ⏎</span>}
              </Button>
            </div>

            <div className="workbench-grid">
              <main className="workbench-main subtle-scrollbar">
                <div className="workbench-toolbar">
                  <Button
                    type="button"
                    variant="ghost"
                    aria-label="Model settings"
                    title="Model settings"
                    className={clsx(
                      "workbench-toolbar-button workbench-toolbar-model-button",
                      activeDrawer === "model" && "is-active",
                    )}
                    onClick={() => setActiveDrawer("model")}
                  >
                    <SlidersHorizontal className="size-4" aria-hidden />
                    {modelDisplayName(selectedModel)}
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    aria-label="Variables"
                    title="Variables"
                    className={clsx("workbench-toolbar-button", activeDrawer === "variables" && "is-active")}
                    onClick={() => setActiveDrawer("variables")}
                  >
                    <Braces className="size-4" aria-hidden />
                    <span className="sr-only">Variables</span>
                    {variables.length ? <span className="workbench-toolbar-count">{variables.length}</span> : null}
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    aria-label="Tools"
                    title="Tools"
                    className={clsx(
                      "workbench-toolbar-button workbench-toolbar-icon-button",
                      activeDrawer === "tools" && "is-active",
                    )}
                    onClick={() => setActiveDrawer("tools")}
                  >
                    <Wrench className="size-4" aria-hidden />
                    <span className="sr-only">Tools</span>
                    {draft.tools.length ? <span className="workbench-toolbar-count">{draft.tools.length}</span> : null}
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    className={clsx("workbench-toolbar-button", activeDrawer === "examples" && "is-active")}
                    disabled={!hasVariables}
                    aria-label={
                      hasVariables ? "Help Claude understand the task better" : "Requires at least one variable"
                    }
                    title={hasVariables ? "Help Claude understand the task better" : "Requires at least one variable"}
                    onClick={() => {
                      setActiveDrawer("examples");
                      setExampleFormOpen(false);
                    }}
                  >
                    Examples
                    {examples.length ? <span className="workbench-toolbar-count">{examples.length}</span> : null}
                  </Button>
                  <span className="workbench-toolbar-spacer" aria-hidden />
                  <Button
                    type="button"
                    variant="ghost"
                    className="workbench-toolbar-button"
                    disabled={!hasPromptText || isPromptReadOnly}
                    aria-label={
                      hasPromptText
                        ? "Use Claude to optimize your prompt"
                        : "Add some text to the prompt to use this feature"
                    }
                    title={
                      hasPromptText
                        ? "Use Claude to optimize your prompt"
                        : "Add some text to the prompt to use this feature"
                    }
                    onClick={openImprovePrompt}
                  >
                    <WandSparkles className="size-4" aria-hidden />
                    Improve prompt
                  </Button>
                </div>

                <div className="workbench-editor-stack">
                  <div
                    className={clsx("workbench-system-card", systemOpen && "is-open")}
                    aria-label={systemOpen ? undefined : "Click to open system prompt"}
                    tabIndex={systemOpen ? undefined : 0}
                    onClick={systemOpen ? undefined : () => setSystemOpen(true)}
                    onKeyDown={
                      systemOpen
                        ? undefined
                        : (event) => {
                            if (event.key === "Enter" || event.key === " ") {
                              event.preventDefault();
                              setSystemOpen(true);
                            }
                          }
                    }
                  >
                    {systemOpen ? (
                      <>
                        <div className="workbench-system-expanded">
                          <div className="workbench-system-expanded-title">System Prompt</div>
                          <SystemPromptEditableInput
                            text={draft.system_prompt}
                            onChange={(nextText) => setDraftField("system_prompt", nextText)}
                            isReadOnly={isPromptReadOnly}
                          />
                        </div>
                        <span className="workbench-system-expanded-icons">
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            aria-label="System prompt info"
                            className="workbench-system-icon-button"
                          >
                            <Info className="size-4" aria-hidden />
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            aria-label="Close system prompt"
                            className="workbench-system-icon-button"
                            onClick={() => setSystemOpen(false)}
                          >
                            <ChevronDown className="size-4" aria-hidden />
                          </Button>
                        </span>
                      </>
                    ) : (
                      <div className="workbench-system-button">
                        <span className="workbench-system-copy">
                          <strong>System Prompt</strong>
                          <span className="workbench-system-meta">Define a role, tone or context (optional)</span>
                        </span>
                        <span className="workbench-system-icons">
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="workbench-system-icon-button"
                            onClick={(event) => event.stopPropagation()}
                          >
                            <Info className="size-4 text-muted-foreground" aria-hidden />
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="workbench-system-icon-button"
                            onClick={(event) => {
                              event.stopPropagation();
                              setSystemOpen(true);
                            }}
                          >
                            <ChevronRight className="size-4 text-muted-foreground" aria-hidden />
                          </Button>
                        </span>
                      </div>
                    )}
                  </div>

                  {draft.messages.map((message, index) => (
                    <MessageEditor
                      key={`${message.role}-${index}`}
                      message={message}
                      index={index}
                      messages={draft.messages}
                      onChange={setMessageText}
                      onRemove={removeMessage}
                      onUpload={attachFileToMessage}
                      onAddUrl={addUrlAttachmentToMessage}
                      onReplaceFile={replaceFileInMessage}
                      onRemoveFile={removeFileFromMessage}
                      onVariableClick={(name) => {
                        setVariableValues((current) => ({ ...current, [name]: current[name] ?? "" }));
                        setToolForm(null);
                        setActiveDrawer("variables");
                      }}
                      isReadOnly={isPromptReadOnly}
                      onShowPromptGenerator={openPromptGenerator}
                      promptGeneratorWarning={promptGeneratorWarning}
                      isGeneratingPrompt={isGeneratingPrompt}
                      isUploading={uploadingMessageIndex === index}
                      uploadError={uploadErrorByMessage[index]}
                    />
                  ))}

                  <div className="workbench-bottom-actions">
                    <Button
                      type="button"
                      variant="ghost"
                      className="gap-2 px-0 text-[13px] font-normal text-muted-foreground hover:bg-transparent hover:text-foreground disabled:text-muted-foreground/70 disabled:opacity-100"
                      disabled={!canAddPrefillResponse || isPromptReadOnly}
                      onClick={addPrefillResponse}
                    >
                      <MessageSquareDashed className="size-4" aria-hidden />
                      Pre-fill response
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      className="gap-2 px-0 text-[13px] font-normal text-muted-foreground hover:bg-transparent hover:text-foreground disabled:text-muted-foreground/70 disabled:opacity-100"
                      disabled={isPromptReadOnly}
                      onClick={addMessagePair}
                    >
                      <MessageSquarePlus className="size-4" aria-hidden />
                      Add message pair
                    </Button>
                  </div>
                </div>
              </main>

              <aside className="workbench-response-panel">
                <h2 className="sr-only">Response</h2>
                <div className="workbench-response-body subtle-scrollbar">
                  {responseTab === "preview" ? (
                    <ResponsePreview
                      isRunning={isRunning}
                      error={runError}
                      responseText={responseText}
                      showCreatePrompt={showResponseCreatePrompt}
                      onCreatePrompt={createNewPrompt}
                    />
                  ) : (
                    <pre className="subtle-scrollbar max-h-full overflow-auto whitespace-pre-wrap break-words rounded-lg border border-border bg-muted p-3 font-mono text-xs leading-5 text-foreground">
                      {JSON.stringify({ request: lastRunRequest, events: streamEvents }, null, 2)}
                    </pre>
                  )}
                </div>
              </aside>
            </div>
          </TabsContent>

          <TabsContent value="evaluate" className="mt-0 min-h-0">
            <div className="workbench-header-actions">
              <Button
                type="button"
                aria-label={isRunning ? "Stop" : "Run All ⌘ + ⏎"}
                size="lg"
                className="px-4"
                disabled={!canRunAllEvaluations && !isRunning}
                onClick={runEvaluateRows}
              >
                {isRunning ? (
                  <Loader2 className="size-4 animate-spin" aria-hidden />
                ) : (
                  <Play className="size-4" fill="currentColor" strokeWidth={0} aria-hidden />
                )}
                <span>{isRunning ? "Stop" : "Run All"}</span>
                {isRunning ? null : <span className="text-xs font-medium text-primary-foreground/70">⌘ + ⏎</span>}
              </Button>
            </div>

            <main className="workbench-evaluate-surface">
              <EvaluateView
                variables={variables}
                promptTitle={promptTitle}
                promptText={titleMessageContent(draft)}
                rows={evaluateRows}
                setRows={setEvaluateRows}
                comparisons={evaluateComparisons}
                setComparisons={setEvaluateComparisons}
                orgUuid={orgUuid}
                promptId={prompt?.id}
                currentRevisionId={draft.id}
                fallbackModelName={draft.model_name}
                isReadOnly={hasUnsavedChanges}
                saveRevisionLabel={nextRevisionLabel}
                canSaveRevision={canSaveCurrentRevision}
                onSaveRevision={() => void saveCurrentRevision()}
                onCreateRow={createEvaluateRowRecord}
                onUpdateVariables={updateEvaluateRowVariables}
                onUpdateIdealOutput={updateEvaluateRowIdealOutput}
                onDeleteRow={deleteEvaluateRowRecord}
                onGenerateTestCases={generateEvaluateTestCases}
              />
            </main>
          </TabsContent>
        </Tabs>

        {activeDrawer ? (
          <WorkbenchDrawer
            title={drawerTitle(activeDrawer)}
            kind={activeDrawer}
            headerAction={
              activeDrawer === "variables" ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="px-1.5"
                  disabled={!variables.length}
                  onClick={generateTestCase}
                >
                  <Sparkles className="size-4" aria-hidden />
                  Generate
                </Button>
              ) : null
            }
            onClose={() => setActiveDrawer(null)}
          >
            {activeDrawer === "model" ? (
              <ModelDrawer draft={draft} models={models} setDraft={setDraft} onRun={runPrompt} isRunning={isRunning} />
            ) : null}
            {activeDrawer === "variables" ? (
              <VariablesDrawer
                variables={variables}
                values={variableValues}
                setValues={setVariableValues}
                generationLogicOpen={variableGenerationLogicOpen}
                setGenerationLogicOpen={setVariableGenerationLogicOpen}
                generationLogic={variableGenerationLogic}
                setGenerationLogic={setVariableGenerationLogic}
                onGenerateGenerationLogic={generateVariableLogic}
                onClearGenerationLogic={() => {
                  setVariableGenerationLogic("");
                  setVariableGenerationLogicOpen(false);
                }}
                isGeneratingGenerationLogic={isGeneratingVariableLogic}
                onRun={runPrompt}
                isRunning={isRunning}
                canRun={canRunWithVariables}
              />
            ) : null}
            {activeDrawer === "tools" ? (
              <ToolsDrawer
                tools={draft.tools}
                toolForm={toolForm}
                setToolForm={setToolForm}
                customTool={customTool}
                setCustomTool={setCustomTool}
                webSearchTool={webSearchTool}
                setWebSearchTool={setWebSearchTool}
                onAddCustom={addCustomTool}
                onAddWebSearch={addWebSearchTool}
                onUpdateWebSearch={updateWebSearchTool}
                onRemove={removeTool}
                onRun={runPrompt}
                isRunning={isRunning}
                canRun={canRun}
              />
            ) : null}
            {activeDrawer === "examples" ? (
              <ExamplesDrawer
                variables={variables}
                examples={examples}
                formOpen={exampleFormOpen}
                editingExampleId={editingExampleId}
                values={exampleValues}
                setValues={setExampleValues}
                idealOutput={exampleIdealOutput}
                setIdealOutput={setExampleIdealOutput}
                additionalContext={exampleAdditionalContext}
                setAdditionalContext={setExampleAdditionalContext}
                contextOpen={exampleContextOpen}
                setContextOpen={setExampleContextOpen}
                isGenerating={isGeneratingExample}
                onGenerate={generateExample}
                onOpenNew={openNewExampleForm}
                onCancel={resetExampleForm}
                onEdit={editExample}
                onRemove={removeExample}
                onAdd={addExample}
                onRun={runPrompt}
                isRunning={isRunning}
                canRun={canRun}
              />
            ) : null}
            {activeDrawer === "history" ? (
              <HistoryDrawer
                promptId={prompt?.id}
                orgUuid={orgUuid}
                currentDraft={draft}
                currentRevisionId={draft.id}
                hasUnsavedChanges={hasUnsavedChanges}
                canSave={canSaveCurrentRevision}
                isSaving={saveStatus === "Saving"}
                onSave={saveCurrentRevision}
                onDiscard={discardDraftChanges}
                onRestore={restoreRevision}
              />
            ) : null}
          </WorkbenchDrawer>
        ) : null}
      </div>

      {codeOpen ? (
        <CodeModal
          language={codeLanguage}
          setLanguage={setCodeLanguage}
          revision={buildRevisionPayload(draft, { includeEmptyMessages: false })}
          onClose={() => setCodeOpen(false)}
        />
      ) : null}

      {renameOpen ? (
        <Dialog title="Rename your prompt" size="rename" onClose={() => setRenameOpen(false)}>
          <form
            className="workbench-rename-form"
            onSubmit={(event) => {
              event.preventDefault();
              void renamePrompt();
            }}
          >
            <Input
              aria-label="Name"
              value={renameValue}
              onChange={(event) => setRenameValue(event.currentTarget.value)}
              className="bg-secondary"
              autoFocus
            />
            <div className="workbench-rename-actions">
              <Button type="button" variant="outline" onClick={() => setRenameOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={!renameValue.trim()}>
                Save
              </Button>
            </div>
          </form>
        </Dialog>
      ) : null}

      {deleteConfirmOpen ? (
        <DeletePromptDialog
          isDeleting={isDeletingPrompt}
          onCancel={() => {
            setDeleteConfirmOpen(false);
            setDeletePromptTarget(null);
          }}
          onDelete={deletePrompt}
        />
      ) : null}

      {shareOpen && prompt ? (
        <SharePromptDialog
          prompt={prompt}
          promptTitle={promptTitle}
          account={account}
          isSharing={isSharingPrompt}
          error={shareError}
          onShare={sharePrompt}
          onClose={() => setShareOpen(false)}
        />
      ) : null}

      {improveOpen ? (
        <ImprovePromptDialog
          feedback={improveFeedback}
          setFeedback={setImproveFeedback}
          thinkingEnabled={improveThinkingEnabled}
          setThinkingEnabled={setImproveThinkingEnabled}
          showImageWarning={revisionHasImageContent(draft)}
          showMultiTurnWarning={hasMultipleHumanMessages(draft)}
          isImproving={isImproving}
          onImprove={improvePrompt}
          onClose={() => setImproveOpen(false)}
        />
      ) : null}

      {promptGeneratorOpen ? (
        <GeneratePromptDialog
          step={promptGeneratorStep}
          task={promptGeneratorTask}
          setTask={setPromptGeneratorTask}
          output={promptGeneratorOutput}
          setOutput={setPromptGeneratorOutput}
          examplesExpanded={promptGeneratorExamplesExpanded}
          setExamplesExpanded={setPromptGeneratorExamplesExpanded}
          thinkingEnabled={promptGeneratorThinkingEnabled}
          setThinkingEnabled={setPromptGeneratorThinkingEnabled}
          warning={promptGeneratorWarning}
          error={promptGeneratorError}
          isGenerating={isGeneratingPrompt}
          onGenerate={generatePromptFromTask}
          onStop={stopPromptGenerator}
          onEdit={editPromptGeneratorInstructions}
          onOpen={openGeneratedPromptInWorkbench}
          onSelectExample={selectPromptGeneratorExample}
          onBuyCredits={() => {
            window.location.href = "/settings/billing";
          }}
          onClose={closePromptGenerator}
        />
      ) : null}

      {promptGeneratorConfirmAction ? (
        <PromptGeneratorConfirmDialog
          kind={promptGeneratorConfirmAction}
          onCancel={() => setPromptGeneratorConfirmAction(null)}
          onConfirm={confirmPromptGeneratorAction}
        />
      ) : null}
    </WorkbenchShell>
  );
}

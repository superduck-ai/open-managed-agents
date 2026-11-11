package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Store interface {
	CreateEvaluationDataset(context.Context, db.EvaluationDataset, []db.EvaluationCaseWrite) (db.EvaluationDataset, error)
	ListEvaluationDatasets(context.Context, string, string, int, int) ([]db.EvaluationDataset, error)
	GetEvaluationDataset(context.Context, string, string, string) (db.EvaluationDataset, error)
	DeleteEvaluationDataset(context.Context, string, string, string, time.Time) error
	ListEvaluationCases(context.Context, string, string, string) ([]db.EvaluationCase, error)
	GetEvaluationCaseTrace(context.Context, string, string, string, string) (json.RawMessage, error)
	CreateEvaluationCases(context.Context, string, string, string, []db.EvaluationCaseWrite, time.Time) ([]db.EvaluationCase, error)
	UpdateEvaluationCase(context.Context, string, string, string, db.EvaluationCaseWrite, time.Time) (db.EvaluationCase, error)
	DeleteEvaluationCase(context.Context, string, string, string, string, time.Time) error
	CreateEvaluationRubric(context.Context, db.EvaluationRubric) (db.EvaluationRubric, error)
	AppendEvaluationRubricVersion(context.Context, string, string, string, db.EvaluationRubric, time.Time) (db.EvaluationRubric, error)
	ListEvaluationRubrics(context.Context, string, string, int, int) ([]db.EvaluationRubric, error)
	GetEvaluationRubric(context.Context, string, string, string) (db.EvaluationRubric, error)
	ListEvaluationRubricVersions(context.Context, string, string, string) ([]db.EvaluationRubric, error)
	DeleteEvaluationRubric(context.Context, string, string, string, time.Time) error
	CreateEvaluationTask(context.Context, db.EvaluationTask, []db.EvaluationTaskItem) (db.EvaluationTaskDetail, error)
	ListEvaluationTasks(context.Context, string, string, int, int) ([]db.EvaluationTask, error)
	ListEvaluationTaskChildren(context.Context, string, string, []string) ([]db.EvaluationTaskItem, []db.EvaluationTaskScore, error)
	GetEvaluationTask(context.Context, string, string, string) (db.EvaluationTaskDetail, error)
	RetryEvaluationTask(context.Context, string, string, string, int, time.Time) (db.EvaluationTaskRetry, error)
	SumEvaluationTaskUsage(context.Context, string, string, string) (db.EvaluationTaskUsage, error)
	GetAgent(context.Context, string, string) (db.Agent, error)
	GetEnvironment(context.Context, string, string) (db.Environment, error)
	GetAgentVersion(context.Context, string, string, int) (db.Agent, error)
	GetSession(context.Context, string, string) (db.Session, bool, error)
	ListSessionEventsPage(context.Context, db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error)
	GetFile(context.Context, string, string) (db.FileRecord, error)
	GetSessionAttachmentFile(context.Context, string, string, string) (db.FileRecord, error)
}

type SessionInput struct {
	Principal             auth.Principal
	AgentExternalID       string
	AgentVersion          int
	EnvironmentExternalID string
	Resources             json.RawMessage
	VaultIDs              []string
	Query                 string
	Content               json.RawMessage
	Title                 string
	Metadata              map[string]string
}

type Handler struct {
	store   Store
	logger  *slog.Logger
	runner  *TaskRunner
	model   RubricModel
	adapter *httpapi.ErrorAdapter
}

func NewHandler(store Store, logger *slog.Logger) *Handler {
	logger = logging.LoggerOrDefault(logger)
	return &Handler{store: store, logger: logger, adapter: httpapi.NewErrorAdapter(logger)}
}

func (h *Handler) WithTaskRunner(runner *TaskRunner) *Handler {
	h.runner = runner
	return h
}

func (h *Handler) WithRubricModel(model RubricModel) *Handler {
	h.model = model
	return h
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	base := "/workspaces/{workspaceId}/evaluations"
	router.Post(base+"/datasets", h.adapter.Wrap(h.createDataset))
	router.Get(base+"/datasets", h.adapter.Wrap(h.listDatasets))
	router.Get(base+"/datasets/{datasetId}", h.adapter.Wrap(h.getDataset))
	router.Delete(base+"/datasets/{datasetId}", h.adapter.Wrap(h.deleteDataset))
	router.Get(base+"/datasets/{datasetId}/cases", h.adapter.Wrap(h.listCases))
	router.Post(base+"/datasets/{datasetId}/cases", h.adapter.Wrap(h.createCases))
	router.Post(base+"/datasets/{datasetId}/cases:import", h.adapter.Wrap(h.importCases))
	router.Post(base+"/datasets/{datasetId}/cases:generate", h.adapter.Wrap(h.generateCases))
	router.Get(base+"/datasets/{datasetId}/session-turns", h.adapter.Wrap(h.listSessionTurns))
	router.Post(base+"/datasets/{datasetId}/cases:from-session", h.adapter.Wrap(h.caseFromSession))
	router.Post(base+"/datasets/{datasetId}/cases/{caseId}", h.adapter.Wrap(h.updateCase))
	router.Delete(base+"/datasets/{datasetId}/cases/{caseId}", h.adapter.Wrap(h.deleteCase))
	router.Post(base+"/rubrics/generate", h.adapter.Wrap(h.generateRubric))
	router.Post(base+"/rubrics", h.adapter.Wrap(h.createRubric))
	router.Get(base+"/rubrics", h.adapter.Wrap(h.listRubrics))
	router.Get(base+"/rubrics/{rubricId}", h.adapter.Wrap(h.getRubric))
	router.Get(base+"/rubrics/{rubricId}/versions", h.adapter.Wrap(h.listRubricVersions))
	router.Delete(base+"/rubrics/{rubricId}", h.adapter.Wrap(h.deleteRubric))
	router.Post(base+"/rubrics/{rubricId}/versions", h.adapter.Wrap(h.appendRubricVersion))
	router.Post(base+"/tasks", h.adapter.Wrap(h.createTask))
	router.Get(base+"/tasks", h.adapter.Wrap(h.listTasks))
	router.Get(base+"/tasks/{taskId}", h.adapter.Wrap(h.getTask))
	router.Get(base+"/tasks/{taskId}/items/{itemId}/trace", h.adapter.Wrap(h.getTaskItemTrace))
	router.Post(base+"/tasks/{taskId}/cancel", h.adapter.Wrap(h.cancelTask))
	router.Post(base+"/tasks/{taskId}/retry", h.adapter.Wrap(h.retryTask))
}

func (h *Handler) createDataset(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[datasetWriteRequest](w, r, 2<<20)
	if err != nil {
		return invalidArgument(invalidJSONMessage, err)
	}
	name, description, err := normalizeDataset(*body)
	if err != nil {
		return err
	}
	drafts, err := datasetDrafts(*body)
	if err != nil {
		return err
	}
	if err := h.resolveDraftFiles(r.Context(), principal.WorkspaceUUID, &attachmentSession{}, drafts); err != nil {
		return err
	}
	dataset, writes, err := h.newDataset(principal, name, description, drafts)
	if err != nil {
		return err
	}
	created, err := h.store.CreateEvaluationDataset(r.Context(), dataset, writes)
	if err != nil {
		return mapStoreError(err, datasetNotFoundMessage, createFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, datasetJSON(created))
	return nil
}

func (h *Handler) listDatasets(w http.ResponseWriter, r *http.Request) error {
	principal, offset, limit, err := h.page(r)
	if err != nil {
		return err
	}
	rows, err := h.store.ListEvaluationDatasets(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, offset, limit+1)
	if err != nil {
		return internalFailure(loadFailedMessage, err)
	}
	next := nextPage(offset, limit, len(rows))
	if len(rows) > limit {
		rows = rows[:limit]
	}
	data := make([]datasetResponse, 0, len(rows))
	for _, row := range rows {
		data = append(data, datasetJSON(row))
	}
	writePage(w, data, next)
	return nil
}

func (h *Handler) getDataset(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	dataset, err := h.store.GetEvaluationDataset(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, chi.URLParam(r, "datasetId"))
	if err != nil {
		return mapStoreError(err, datasetNotFoundMessage, loadFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, datasetJSON(dataset))
	return nil
}

func (h *Handler) deleteDataset(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	datasetID := chi.URLParam(r, "datasetId")
	if err := h.store.DeleteEvaluationDataset(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, datasetID, time.Now().UTC()); err != nil {
		return mapStoreError(err, datasetNotFoundMessage, deleteFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"id": datasetID, "type": "evaluation_dataset_deleted", "deleted": true})
	return nil
}

func (h *Handler) listCases(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	cases, err := h.store.ListEvaluationCases(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, chi.URLParam(r, "datasetId"))
	if err != nil {
		return mapStoreError(err, datasetNotFoundMessage, loadFailedMessage)
	}
	data := make([]caseResponse, 0, len(cases))
	for _, item := range cases {
		data = append(data, caseJSON(item))
	}
	writePage(w, data, nil)
	return nil
}

func (h *Handler) createCases(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[casesWriteRequest](w, r, 2<<20)
	if err != nil {
		return invalidArgument(invalidJSONMessage, err)
	}
	drafts, err := normalizeCaseList(body.Cases)
	if err != nil {
		return err
	}
	created, err := h.appendCases(r.Context(), principal, chi.URLParam(r, "datasetId"), drafts)
	if err != nil {
		return err
	}
	data := make([]caseResponse, 0, len(created))
	for _, item := range created {
		data = append(data, caseJSON(item))
	}
	writePage(w, data, nil)
	return nil
}

func (h *Handler) importCases(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[importRequest](w, r, 2<<20)
	if err != nil {
		return invalidArgument(invalidJSONMessage, err)
	}
	drafts, err := parseCaseDocument(body.Content)
	if err != nil {
		return err
	}
	created, err := h.appendCases(r.Context(), principal, chi.URLParam(r, "datasetId"), drafts)
	if err != nil {
		return err
	}
	data := make([]caseResponse, 0, len(created))
	for _, item := range created {
		data = append(data, caseJSON(item))
	}
	writePage(w, data, nil)
	return nil
}

func (h *Handler) updateCase(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[caseUpdateRequest](w, r, 1<<20)
	if err != nil {
		return invalidArgument(invalidJSONMessage, err)
	}
	draft, err := body.draft()
	if err != nil {
		return err
	}
	if err := h.resolveDraftFiles(r.Context(), principal.WorkspaceUUID, &attachmentSession{id: draft.SourceSessionID}, []caseDraft{draft}); err != nil {
		return err
	}
	datasetID := chi.URLParam(r, "datasetId")
	caseID := chi.URLParam(r, "caseId")
	if body.keepsTrace() {
		trace, err := h.caseTrace(r.Context(), principal, datasetID, caseID)
		if err != nil {
			return err
		}
		draft.ExpectedToolTrace = trace
	}
	updated, err := h.store.UpdateEvaluationCase(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, datasetID, caseWrite(caseID, draft), time.Now().UTC())
	if err != nil {
		return mapStoreError(err, caseNotFoundMessage, createFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, caseJSON(updated))
	return nil
}

func (h *Handler) deleteCase(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	caseID := chi.URLParam(r, "caseId")
	if err := h.store.DeleteEvaluationCase(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, chi.URLParam(r, "datasetId"), caseID, time.Now().UTC()); err != nil {
		return mapStoreError(err, caseNotFoundMessage, deleteFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"id": caseID, "type": "evaluation_case_deleted", "deleted": true})
	return nil
}

type datasetWriteRequest struct {
	Name    string    `json:"name"`
	Desc    string    `json:"desc"`
	Content string    `json:"content"`
	Cases   []rawCase `json:"cases"`
}

type casesWriteRequest struct {
	Cases []rawCase `json:"cases"`
}

type importRequest struct {
	Content string `json:"content"`
}

func datasetDrafts(body datasetWriteRequest) ([]caseDraft, error) {
	drafts, err := optionalCases(body.Cases)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(body.Content) == "" {
		return drafts, nil
	}
	imported, err := parseCaseDocument(body.Content)
	if err != nil {
		return nil, err
	}
	combined := append(drafts, imported...)
	if len(combined) > maxImportCases {
		return nil, invalidArgument(importTooLargeMessage, nil)
	}
	return combined, nil
}

func (h *Handler) caseTrace(ctx context.Context, principal auth.Principal, datasetID, caseID string) (json.RawMessage, error) {
	dataset, err := h.store.GetEvaluationDataset(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, datasetID)
	if err != nil {
		return nil, mapStoreError(err, datasetNotFoundMessage, loadFailedMessage)
	}
	trace, err := h.store.GetEvaluationCaseTrace(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, dataset.VersionUUID, caseID)
	if err != nil {
		return nil, mapStoreError(err, caseNotFoundMessage, loadFailedMessage)
	}
	return trace, nil
}

func normalizeDataset(body datasetWriteRequest) (string, string, error) {
	name, err := normalizeName(body.Name)
	if err != nil {
		return "", "", err
	}
	description, err := normalizeDescription(body.Desc)
	if err != nil {
		return "", "", err
	}
	return name, description, nil
}

func (h *Handler) newDataset(principal auth.Principal, name, description string, drafts []caseDraft) (db.EvaluationDataset, []db.EvaluationCaseWrite, error) {
	datasetID, versionID, err := newPair("evalds_", "edv_")
	if err != nil {
		return db.EvaluationDataset{}, nil, err
	}
	now := time.Now().UTC()
	writes, err := caseWrites(drafts)
	if err != nil {
		return db.EvaluationDataset{}, nil, err
	}
	return db.EvaluationDataset{
		UUID: uuid.NewString(), ExternalID: datasetID, OrganizationUUID: principal.OrganizationUUID, WorkspaceUUID: principal.WorkspaceUUID,
		Name: name, Description: description, VersionUUID: uuid.NewString(), VersionExternalID: versionID, CreatedAt: now,
	}, writes, nil
}

func (h *Handler) appendCases(ctx context.Context, principal auth.Principal, datasetID string, drafts []caseDraft) ([]db.EvaluationCase, error) {
	return h.appendCasesInSession(ctx, principal, datasetID, drafts, "")
}

func (h *Handler) appendCasesInSession(ctx context.Context, principal auth.Principal, datasetID string, drafts []caseDraft, sessionUUID string) ([]db.EvaluationCase, error) {
	if err := h.resolveDraftFiles(ctx, principal.WorkspaceUUID, &attachmentSession{uuid: sessionUUID}, drafts); err != nil {
		return nil, err
	}
	writes, err := caseWrites(drafts)
	if err != nil {
		return nil, err
	}
	created, err := h.store.CreateEvaluationCases(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, datasetID, writes, time.Now().UTC())
	if err != nil {
		return nil, mapStoreError(err, datasetNotFoundMessage, createFailedMessage)
	}
	return created, nil
}

func caseWrites(drafts []caseDraft) ([]db.EvaluationCaseWrite, error) {
	writes := make([]db.EvaluationCaseWrite, 0, len(drafts))
	for _, draft := range drafts {
		externalID, err := ids.New("evalcase_")
		if err != nil {
			return nil, internalFailure(createFailedMessage, err)
		}
		writes = append(writes, caseWrite(externalID, draft))
	}
	return writes, nil
}

func caseWrite(externalID string, draft caseDraft) db.EvaluationCaseWrite {
	return db.EvaluationCaseWrite{
		UUID: uuid.NewString(), ExternalID: externalID, Query: draft.Query, ExpectedAnswer: draft.ExpectedAnswer,
		ExpectedToolTrace: draft.ExpectedToolTrace, SourceSessionID: draft.SourceSessionID, Polarity: draft.Polarity, Acceptance: draft.Acceptance,
		Inputs: draft.Inputs,
	}
}

type attachmentSession struct {
	id   string
	uuid string
}

func (h *Handler) resolveDraftFiles(ctx context.Context, workspaceUUID string, session *attachmentSession, drafts []caseDraft) error {
	seen := map[string]string{}
	for _, draft := range drafts {
		if err := resolveInputs(draft.Inputs, func(fileID string) (string, error) {
			if mime, ok := seen[fileID]; ok {
				return mime, nil
			}
			file, err := h.inputFile(ctx, workspaceUUID, session, fileID)
			if err != nil {
				return "", err
			}
			seen[fileID] = file.MimeType
			return file.MimeType, nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) inputFile(ctx context.Context, workspaceUUID string, session *attachmentSession, fileID string) (db.FileRecord, error) {
	file, err := h.store.GetFile(ctx, workspaceUUID, fileID)
	if err == nil {
		return file, nil
	}
	if !errors.Is(err, db.ErrNotFound) {
		return db.FileRecord{}, mapStoreError(err, fileNotFoundMessage, loadFailedMessage)
	}
	sessionUUID, err := h.attachmentSessionUUID(ctx, workspaceUUID, session)
	if err != nil {
		return db.FileRecord{}, err
	}
	if sessionUUID == "" {
		return db.FileRecord{}, invalidArgument(fileNotFoundMessage, err)
	}
	file, err = h.store.GetSessionAttachmentFile(ctx, workspaceUUID, sessionUUID, fileID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return db.FileRecord{}, invalidArgument(fileNotFoundMessage, err)
		}
		return db.FileRecord{}, mapStoreError(err, fileNotFoundMessage, loadFailedMessage)
	}
	return file, nil
}

func (h *Handler) attachmentSessionUUID(ctx context.Context, workspaceUUID string, session *attachmentSession) (string, error) {
	if session == nil || (session.id == "" && session.uuid == "") {
		return "", nil
	}
	if session.uuid != "" {
		return session.uuid, nil
	}
	loaded, ok, err := h.store.GetSession(ctx, workspaceUUID, session.id)
	if err != nil {
		return "", internalFailure(loadFailedMessage, err)
	}
	if !ok {
		return "", notFound(sessionNotFoundMessage, nil)
	}
	session.uuid = loaded.UUID
	return session.uuid, nil
}

func newPair(leftPrefix, rightPrefix string) (string, string, error) {
	left, err := ids.New(leftPrefix)
	if err != nil {
		return "", "", internalFailure(createFailedMessage, err)
	}
	right, err := ids.New(rightPrefix)
	if err != nil {
		return "", "", internalFailure(createFailedMessage, err)
	}
	return left, right, nil
}

func (h *Handler) page(r *http.Request) (auth.Principal, int, int, error) {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return auth.Principal{}, 0, 0, err
	}
	limit, err := httpapi.ParseLimit(r, 100)
	if err != nil {
		return auth.Principal{}, 0, 0, invalidArgument(limitInvalidMessage, err)
	}
	offset, err := decodeOffset(r.URL.Query().Get("page"))
	if err != nil {
		return auth.Principal{}, 0, 0, err
	}
	return principal, offset, limit, nil
}

func scopedPrincipal(r *http.Request) (auth.Principal, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.OrganizationUUID == "" || principal.WorkspaceUUID == "" {
		return auth.Principal{}, notFound(datasetNotFoundMessage, nil)
	}
	if chi.URLParam(r, "orgUuid") != principal.OrganizationUUID {
		return auth.Principal{}, notFound(datasetNotFoundMessage, nil)
	}
	workspaceID := chi.URLParam(r, "workspaceId")
	if workspaceID == "" || (workspaceID != "default" && workspaceID != principal.WorkspaceUUID && workspaceID != principal.WorkspaceExternalID) {
		return auth.Principal{}, notFound(datasetNotFoundMessage, nil)
	}
	return principal, nil
}

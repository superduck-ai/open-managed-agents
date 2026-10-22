package evaluations

import (
	"context"
	"encoding/json"
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
	CreateEvaluationDataset(context.Context, db.EvaluationDataset) (db.EvaluationDataset, error)
	CreateEvaluationDatasetWithCases(context.Context, db.EvaluationDataset, []db.EvaluationCaseWrite) (db.EvaluationDataset, error)
	ListEvaluationDatasetsPage(context.Context, db.ListEvaluationDatasetsPageParams) ([]db.EvaluationDataset, bool, error)
	GetEvaluationDataset(context.Context, string, string, string) (db.EvaluationDataset, error)
	ListEvaluationCasesPage(context.Context, db.ListEvaluationCasesPageParams) ([]db.EvaluationCase, bool, error)
	CreateEvaluationCases(context.Context, string, string, string, []db.EvaluationCaseWrite, time.Time) ([]db.EvaluationCase, error)
	CreateEvaluationEvaluator(context.Context, db.EvaluationEvaluator) (db.EvaluationEvaluator, error)
	ListEvaluationEvaluatorsPage(context.Context, db.ListEvaluationEvaluatorsPageParams) ([]db.EvaluationEvaluator, bool, error)
	GetEvaluationEvaluator(context.Context, string, string, string) (db.EvaluationEvaluator, error)
	GetEvaluationDatasetVersion(context.Context, string, string, string) (db.EvaluationDatasetVersion, error)
	ListEvaluationCasesByVersion(context.Context, string, string, string) ([]db.EvaluationCase, error)
	GetAgent(context.Context, string, string) (db.Agent, error)
	GetAgentVersion(context.Context, string, string, int) (db.Agent, error)
	GetEnvironment(context.Context, string, string) (db.Environment, error)
	CreateEvaluationExperiment(context.Context, db.EvaluationExperimentCreate) (db.EvaluationExperiment, error)
	GetEvaluationExperiment(context.Context, string, string, string) (db.EvaluationExperiment, error)
	ListEvaluationExperimentsPage(context.Context, db.ListEvaluationExperimentsPageParams) ([]db.EvaluationExperiment, bool, error)
	ListCompletedEvaluationExperiments(context.Context, string, string, string, string) ([]db.EvaluationExperiment, error)
	UpdateEvaluationExperimentStatus(context.Context, string, string, string, string, time.Time) error
	SetEvaluationExperimentItemSession(context.Context, db.EvaluationExperimentItemSession) error
	ListRunningEvaluationExperimentItemsBySession(context.Context, string, string, string) ([]db.EvaluationExperimentSessionMatch, error)
	FinishRunningEvaluationExperimentItem(context.Context, string, string, string, string, string, time.Time) (bool, error)
	GetEvaluationEvaluatorVersion(context.Context, string, string, string) (db.EvaluationEvaluatorVersion, error)
	CreateEvaluationExperimentScore(context.Context, db.EvaluationExperimentScore) (db.EvaluationExperimentScore, error)
	ListSessionEventsPage(context.Context, db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error)
}

type CaseSessionInput struct {
	AgentExternalID       string
	AgentVersion          int
	EnvironmentExternalID string
	Resources             json.RawMessage
	Turns                 []string
	Metadata              map[string]string
}

type CaseSessionStarter interface {
	StartCaseSession(context.Context, auth.Principal, CaseSessionInput) (string, error)
}

type Handler struct {
	store   Store
	logger  *slog.Logger
	starter CaseSessionStarter
	model   EvaluatorModel
}

func NewHandler(store Store, logger *slog.Logger) *Handler {
	return &Handler{store: store, logger: logging.LoggerOrDefault(logger)}
}

func (h *Handler) WithSessionStarter(starter CaseSessionStarter) *Handler {
	h.starter = starter
	return h
}

func (h *Handler) WithEvaluatorModel(model EvaluatorModel) *Handler {
	h.model = model
	return h
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/workspaces/{workspaceId}/evaluation-datasets", h.wrap(h.createDataset))
	r.Get("/workspaces/{workspaceId}/evaluation-datasets", h.wrap(h.listDatasets))
	r.Get("/workspaces/{workspaceId}/evaluation-dataset-presets", h.wrap(h.listDatasetPresets))
	r.Get("/workspaces/{workspaceId}/evaluation-dataset-presets/{presetId}", h.wrap(h.getDatasetPreset))
	r.Get("/workspaces/{workspaceId}/evaluation-dataset-presets/{presetId}/cases", h.wrap(h.listDatasetPresetCases))
	r.Get("/workspaces/{workspaceId}/evaluation-evaluator-presets", h.wrap(h.listEvaluatorPresets))
	r.Get("/workspaces/{workspaceId}/evaluation-evaluator-presets/{presetId}", h.wrap(h.getEvaluatorPreset))
	r.Get("/workspaces/{workspaceId}/evaluation-datasets/{datasetId}", h.wrap(h.getDataset))
	r.Get("/workspaces/{workspaceId}/evaluation-datasets/{datasetId}/cases", h.wrap(h.listCases))
	r.Post("/workspaces/{workspaceId}/evaluation-datasets/{datasetId}/cases", h.wrap(h.createCase))
	r.Post("/workspaces/{workspaceId}/evaluation-datasets/{datasetId}/cases:import", h.wrap(h.importCases))
	r.Post("/workspaces/{workspaceId}/evaluation-evaluators", h.wrap(h.createEvaluator))
	r.Get("/workspaces/{workspaceId}/evaluation-evaluators", h.wrap(h.listEvaluators))
	r.Get("/workspaces/{workspaceId}/evaluation-evaluators/{evaluatorId}", h.wrap(h.getEvaluator))
	r.Post("/workspaces/{workspaceId}/evaluation-experiments", h.wrap(h.createExperiment))
	r.Get("/workspaces/{workspaceId}/evaluation-experiments", h.wrap(h.listExperiments))
	r.Post("/workspaces/{workspaceId}/evaluation-experiments:compare", h.wrap(h.compareExperiments))
	r.Get("/workspaces/{workspaceId}/evaluation-experiments/{experimentId}", h.wrap(h.getExperiment))
	r.Post("/workspaces/{workspaceId}/evaluation-experiments/{experimentId}:start", h.wrap(h.startExperiment))
}

func (h *Handler) createDataset(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r, datasetNotFound())
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[createDatasetRequest](w, r, maxDatasetBodyBytes)
	if err != nil {
		return invalidJSONRequest(err)
	}
	dataset, err := newDataset(principal, *body)
	if err != nil {
		return err
	}
	sourceVersionID := strings.TrimSpace(body.SourceVersionID)
	var created db.EvaluationDataset
	if sourceVersionID == "" {
		created, err = h.store.CreateEvaluationDataset(r.Context(), dataset)
		if err != nil {
			return mapStoreError(err, datasetNotFoundMessage, createDatasetFailedMessage)
		}
	} else {
		created, err = h.createDatasetFromPreset(r.Context(), dataset, sourceVersionID)
		if err != nil {
			return err
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, datasetResponseFrom(created))
	return nil
}

func (h *Handler) listDatasets(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r, datasetNotFound())
	if err != nil {
		return err
	}
	limit, cursor, err := pageQuery(r, decodeDatasetCursor)
	if err != nil {
		return err
	}
	datasets, hasMore, err := h.store.ListEvaluationDatasetsPage(r.Context(), db.ListEvaluationDatasetsPageParams{
		OrganizationUUID: principal.OrganizationUUID,
		WorkspaceUUID:    principal.WorkspaceUUID,
		Limit:            limit,
		Cursor:           cursor,
	})
	if err != nil {
		return mapStoreError(err, datasetNotFoundMessage, listDatasetsFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, datasetPage(datasets, hasMore))
	return nil
}

func (h *Handler) getDataset(w http.ResponseWriter, r *http.Request) error {
	principal, datasetID, err := scopedDatasetID(r)
	if err != nil {
		return err
	}
	dataset, err := h.store.GetEvaluationDataset(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, datasetID)
	if err != nil {
		return mapStoreError(err, datasetNotFoundMessage, loadDatasetFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, datasetResponseFrom(dataset))
	return nil
}

func (h *Handler) listCases(w http.ResponseWriter, r *http.Request) error {
	principal, datasetID, err := scopedDatasetID(r)
	if err != nil {
		return err
	}
	limit, cursor, err := pageQuery(r, decodeCaseCursor)
	if err != nil {
		return err
	}
	cases, hasMore, err := h.store.ListEvaluationCasesPage(r.Context(), db.ListEvaluationCasesPageParams{
		OrganizationUUID:  principal.OrganizationUUID,
		WorkspaceUUID:     principal.WorkspaceUUID,
		DatasetExternalID: datasetID,
		Limit:             limit,
		Cursor:            cursor,
	})
	if err != nil {
		return mapStoreError(err, datasetNotFoundMessage, listCasesFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, casePage(cases, hasMore))
	return nil
}

func (h *Handler) createCase(w http.ResponseWriter, r *http.Request) error {
	principal, datasetID, err := scopedDatasetID(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[caseInput](w, r, maxDatasetBodyBytes)
	if err != nil {
		return invalidJSONRequest(err)
	}
	item, err := normalizeCase(*body)
	if err != nil {
		return err
	}
	created, err := h.insertCases(r, principal, datasetID, []normalizedCase{item})
	if err != nil {
		return err
	}
	if len(created) == 0 {
		return mapStoreError(errMissingCreatedCase, datasetNotFoundMessage, createCasesFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, caseResponseFrom(created[0]))
	return nil
}

func (h *Handler) importCases(w http.ResponseWriter, r *http.Request) error {
	principal, datasetID, err := scopedDatasetID(r)
	if err != nil {
		return err
	}
	cases, err := readImportedCases(w, r)
	if err != nil {
		return err
	}
	created, err := h.insertCases(r, principal, datasetID, cases)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, casePage(created, false))
	return nil
}

func (h *Handler) insertCases(r *http.Request, principal auth.Principal, datasetID string, cases []normalizedCase) ([]db.EvaluationCase, error) {
	writes, err := newCaseWrites(cases)
	if err != nil {
		return nil, mapStoreError(err, datasetNotFoundMessage, createCasesFailedMessage)
	}
	created, err := h.store.CreateEvaluationCases(
		r.Context(),
		principal.OrganizationUUID,
		principal.WorkspaceUUID,
		datasetID,
		writes,
		time.Now().UTC(),
	)
	if err != nil {
		return nil, mapStoreError(err, datasetNotFoundMessage, createCasesFailedMessage)
	}
	return created, nil
}

func newDataset(principal auth.Principal, body createDatasetRequest) (db.EvaluationDataset, error) {
	name, description, err := normalizeDataset(body)
	if err != nil {
		return db.EvaluationDataset{}, err
	}
	externalID, err := ids.New("evalds_")
	if err != nil {
		return db.EvaluationDataset{}, mapStoreError(err, datasetNotFoundMessage, createDatasetFailedMessage)
	}
	versionID, err := ids.New("edv_")
	if err != nil {
		return db.EvaluationDataset{}, mapStoreError(err, datasetNotFoundMessage, createDatasetFailedMessage)
	}
	now := time.Now().UTC()
	return db.EvaluationDataset{
		UUID:              uuid.NewString(),
		ExternalID:        externalID,
		OrganizationUUID:  principal.OrganizationUUID,
		WorkspaceUUID:     principal.WorkspaceUUID,
		Name:              name,
		Description:       description,
		CreatedByUserUUID: principal.UserUUID,
		CreatedAt:         now,
		UpdatedAt:         now,
		Version: db.EvaluationDatasetVersion{
			UUID:       uuid.NewString(),
			ExternalID: versionID,
			Version:    1,
			CreatedAt:  now,
		},
	}, nil
}

func newCaseWrites(cases []normalizedCase) ([]db.EvaluationCaseWrite, error) {
	writes := make([]db.EvaluationCaseWrite, 0, len(cases))
	for _, item := range cases {
		externalID, err := ids.New("evalcase_")
		if err != nil {
			return nil, err
		}
		writes = append(writes, db.EvaluationCaseWrite{
			UUID:           uuid.NewString(),
			ExternalID:     externalID,
			Name:           item.Name,
			Description:    item.Description,
			Turns:          item.Turns,
			Checkpoints:    item.Checkpoints,
			InputFiles:     item.InputFiles,
			ReferenceFiles: item.ReferenceFiles,
		})
	}
	return writes, nil
}

func scopedDatasetID(r *http.Request) (auth.Principal, string, error) {
	return scopedPathID(r, "datasetId", datasetNotFound())
}

func scopedEvaluatorID(r *http.Request) (auth.Principal, string, error) {
	return scopedPathID(r, "evaluatorId", evaluatorNotFound())
}

func scopedPathID(r *http.Request, param string, missing error) (auth.Principal, string, error) {
	principal, err := scopedPrincipal(r, missing)
	if err != nil {
		return auth.Principal{}, "", err
	}
	value := strings.TrimSpace(chi.URLParam(r, param))
	if value == "" {
		return auth.Principal{}, "", missing
	}
	return principal, value, nil
}

func scopedPrincipal(r *http.Request, missing error) (auth.Principal, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.OrganizationUUID) == "" || strings.TrimSpace(principal.WorkspaceUUID) == "" {
		return auth.Principal{}, missing
	}
	if strings.TrimSpace(chi.URLParam(r, "orgUuid")) != strings.TrimSpace(principal.OrganizationUUID) {
		return auth.Principal{}, missing
	}
	if !workspacePathMatches(principal, chi.URLParam(r, "workspaceId")) {
		return auth.Principal{}, missing
	}
	return principal, nil
}

func workspacePathMatches(principal auth.Principal, value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && (value == "default" || value == principal.WorkspaceUUID || value == principal.WorkspaceExternalID)
}

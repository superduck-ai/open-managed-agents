package evaluations

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type presetDatasetResponse struct {
	ID          string                   `json:"id"`
	Type        string                   `json:"type"`
	Name        string                   `json:"name"`
	Desc        string                   `json:"desc"`
	Status      string                   `json:"status"`
	Versions    []datasetVersionResponse `json:"versions"`
	PublishedAt string                   `json:"published_at"`
}

type presetDatasetPageResponse struct {
	Data     []presetDatasetResponse `json:"data"`
	NextPage *string                 `json:"next_page"`
}

func (h *Handler) listDatasetPresets(w http.ResponseWriter, r *http.Request) error {
	if _, err := scopedPrincipal(r, datasetNotFound()); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, presetDatasetPage(presetDatasets()))
	return nil
}

func (h *Handler) getDatasetPreset(w http.ResponseWriter, r *http.Request) error {
	if _, err := scopedPrincipal(r, datasetNotFound()); err != nil {
		return err
	}
	dataset, err := presetDatasetByID(strings.TrimSpace(chi.URLParam(r, "presetId")))
	if err != nil {
		return presetLookupError(err, presetDatasetNotFoundMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, presetDatasetResponseFrom(dataset))
	return nil
}

func (h *Handler) listDatasetPresetCases(w http.ResponseWriter, r *http.Request) error {
	if _, err := scopedPrincipal(r, datasetNotFound()); err != nil {
		return err
	}
	dataset, err := presetDatasetByID(strings.TrimSpace(chi.URLParam(r, "presetId")))
	if err != nil {
		return presetLookupError(err, presetDatasetNotFoundMessage)
	}
	version, err := presetDatasetVersionOwned(dataset, strings.TrimSpace(r.URL.Query().Get("version_id")))
	if err != nil {
		return presetLookupError(err, presetDatasetNotFoundMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, casePage(presetCaseRecords(dataset, version), false))
	return nil
}

func (h *Handler) listEvaluatorPresets(w http.ResponseWriter, r *http.Request) error {
	if _, err := scopedPrincipal(r, evaluatorNotFound()); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, presetEvaluatorPage(presetEvaluators()))
	return nil
}

func (h *Handler) getEvaluatorPreset(w http.ResponseWriter, r *http.Request) error {
	if _, err := scopedPrincipal(r, evaluatorNotFound()); err != nil {
		return err
	}
	evaluator, err := presetEvaluatorByID(strings.TrimSpace(chi.URLParam(r, "presetId")))
	if err != nil {
		return presetLookupError(err, presetEvaluatorNotFoundMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, presetEvaluatorResponseFrom(evaluator))
	return nil
}

func (h *Handler) createDatasetFromPreset(ctx context.Context, dataset db.EvaluationDataset, sourceVersionID string, caseIDs []string) (db.EvaluationDataset, error) {
	_, version, err := lookupPresetDatasetVersion(sourceVersionID)
	if err != nil {
		return db.EvaluationDataset{}, presetLookupError(err, presetDatasetNotFoundMessage)
	}
	cases, err := presetCasesByID(version, caseIDs)
	if err != nil {
		return db.EvaluationDataset{}, err
	}
	writes, err := newCaseWrites(cases)
	if err != nil {
		return db.EvaluationDataset{}, mapStoreError(err, datasetNotFoundMessage, createDatasetFailedMessage)
	}
	created, err := h.store.CreateEvaluationDatasetWithCases(ctx, dataset, writes)
	if err != nil {
		return db.EvaluationDataset{}, mapStoreError(err, datasetNotFoundMessage, createDatasetFailedMessage)
	}
	return created, nil
}

func (h *Handler) createEvaluatorFromPreset(w http.ResponseWriter, r *http.Request, principal auth.Principal, body createEvaluatorRequest) error {
	if evaluatorPresetConflict(body) {
		return presetSourceConflict()
	}
	name, description, err := normalizeNameAndDescription(body.Name, body.Desc)
	if err != nil {
		return err
	}
	preset, err := presetEvaluatorVersion(strings.TrimSpace(body.SourceVersionID))
	if err != nil {
		return presetLookupError(err, presetEvaluatorNotFoundMessage)
	}
	agentID, err := normalizePresetAgentID(body.AgentID)
	if err != nil {
		return err
	}
	evaluator, err := newCustomEvaluator(principal, name, description, db.EvaluationEvaluatorVersion{
		EvaluatorType:  preset.EvaluatorType,
		Model:          preset.Model,
		AgentID:        agentID,
		PromptTemplate: preset.PromptTemplate,
		Variables:      append([]string(nil), preset.Variables...),
	})
	if err != nil {
		return err
	}
	created, err := h.store.CreateEvaluationEvaluator(r.Context(), evaluator)
	if err != nil {
		return mapStoreError(err, evaluatorNotFoundMessage, createEvaluatorFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, evaluatorResponseFrom(created))
	return nil
}

func evaluatorPresetConflict(body createEvaluatorRequest) bool {
	if strings.TrimSpace(body.EvaluatorType) != "" || strings.TrimSpace(body.Model) != "" || strings.TrimSpace(body.PromptTemplate) != "" {
		return true
	}
	return len(body.Variables) > 0
}

func presetEvaluatorByID(id string) (presetEvaluator, error) {
	for _, evaluator := range omaPresetEvaluators {
		if evaluator.ID == id {
			return evaluator, nil
		}
	}
	return presetEvaluator{}, errPresetNotFound
}

func presetCasesNormalized(version presetDatasetVersion) []normalizedCase {
	cases := make([]normalizedCase, 0, len(version.Cases))
	for _, item := range version.Cases {
		cases = append(cases, normalizedCase{
			Name:           item.Name,
			Description:    item.Description,
			Turns:          append([]string(nil), item.Turns...),
			Checkpoints:    append([]byte(nil), item.Checkpoints...),
			InputFiles:     append([]byte(nil), item.InputFiles...),
			ReferenceFiles: append([]byte(nil), item.ReferenceFiles...),
		})
	}
	return cases
}

func presetCaseRecords(dataset presetDataset, version presetDatasetVersion) []db.EvaluationCase {
	records := make([]db.EvaluationCase, 0, len(version.Cases))
	for position, item := range version.Cases {
		records = append(records, db.EvaluationCase{
			ExternalID:        item.ID,
			DatasetExternalID: dataset.ID,
			VersionExternalID: version.ID,
			Position:          position + 1,
			Name:              item.Name,
			Description:       item.Description,
			Turns:             item.Turns,
			Checkpoints:       item.Checkpoints,
			InputFiles:        item.InputFiles,
			ReferenceFiles:    item.ReferenceFiles,
			CreatedAt:         presetPublishedAt,
			UpdatedAt:         presetPublishedAt,
		})
	}
	return records
}

func presetDatasetResponseFrom(dataset presetDataset) presetDatasetResponse {
	versions := make([]datasetVersionResponse, 0, len(dataset.Versions))
	for _, version := range dataset.Versions {
		versions = append(versions, datasetVersionResponse{
			ID:        version.ID,
			Version:   version.Version,
			CaseCount: len(version.Cases),
		})
	}
	return presetDatasetResponse{
		ID:          dataset.ID,
		Type:        "evaluation_dataset_preset",
		Name:        dataset.Name,
		Desc:        dataset.Description,
		Status:      presetStatus(dataset.Archived),
		Versions:    versions,
		PublishedAt: presetPublishedAt.Format(time.RFC3339),
	}
}

func presetDatasetPage(datasets []presetDataset) presetDatasetPageResponse {
	data := make([]presetDatasetResponse, 0, len(datasets))
	for _, dataset := range datasets {
		data = append(data, presetDatasetResponseFrom(dataset))
	}
	return presetDatasetPageResponse{Data: data}
}

func presetEvaluatorResponseFrom(evaluator presetEvaluator) evaluatorResponse {
	response := evaluatorResponseFrom(evaluator.asRecord())
	response.Type = "evaluation_evaluator_preset"
	return response
}

func presetEvaluatorPage(evaluators []presetEvaluator) evaluatorPageResponse {
	data := make([]evaluatorResponse, 0, len(evaluators))
	for _, evaluator := range evaluators {
		data = append(data, presetEvaluatorResponseFrom(evaluator))
	}
	return evaluatorPageResponse{Data: data}
}

func (evaluator presetEvaluator) asRecord() db.EvaluationEvaluator {
	var archivedAt *time.Time
	if evaluator.Archived {
		archivedAt = &presetPublishedAt
	}
	return db.EvaluationEvaluator{
		ExternalID:  evaluator.ID,
		Name:        evaluator.Name,
		Description: evaluator.Description,
		CreatedAt:   presetPublishedAt,
		UpdatedAt:   presetPublishedAt,
		ArchivedAt:  archivedAt,
		Version: db.EvaluationEvaluatorVersion{
			ExternalID:     evaluator.VersionID,
			Version:        1,
			EvaluatorType:  evaluator.EvaluatorType,
			Model:          evaluator.Model,
			AgentID:        evaluator.AgentID,
			PromptTemplate: evaluator.PromptTemplate,
			Variables:      evaluator.Variables,
			CreatedAt:      presetPublishedAt,
		},
	}
}

func presetStatus(archived bool) string {
	if archived {
		return "archived"
	}
	return "available"
}

func newCustomEvaluator(principal auth.Principal, name, description string, version db.EvaluationEvaluatorVersion) (db.EvaluationEvaluator, error) {
	externalID, err := ids.New("evalev_")
	if err != nil {
		return db.EvaluationEvaluator{}, mapStoreError(err, evaluatorNotFoundMessage, createEvaluatorFailedMessage)
	}
	versionID, err := ids.New("eev_")
	if err != nil {
		return db.EvaluationEvaluator{}, mapStoreError(err, evaluatorNotFoundMessage, createEvaluatorFailedMessage)
	}
	now := time.Now().UTC()
	version.UUID = uuid.NewString()
	version.ExternalID = versionID
	version.Version = 1
	version.CreatedAt = now
	return db.EvaluationEvaluator{
		UUID:              uuid.NewString(),
		ExternalID:        externalID,
		OrganizationUUID:  principal.OrganizationUUID,
		WorkspaceUUID:     principal.WorkspaceUUID,
		Name:              name,
		Description:       description,
		CreatedByUserUUID: principal.UserUUID,
		CreatedAt:         now,
		UpdatedAt:         now,
		Version:           version,
	}, nil
}

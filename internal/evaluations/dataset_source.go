package evaluations

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (h *Handler) createDatasetFromSource(ctx context.Context, principal auth.Principal, dataset db.EvaluationDataset, body createDatasetRequest) (db.EvaluationDataset, error) {
	caseIDs, err := normalizeCaseIDs(body.CaseIDs)
	if err != nil {
		return db.EvaluationDataset{}, err
	}
	sourceVersionID := strings.TrimSpace(body.SourceVersionID)
	sourceDatasetID := strings.TrimSpace(body.SourceDatasetID)
	if sourceVersionID != "" && sourceDatasetID != "" {
		return db.EvaluationDataset{}, datasetSourceConflict()
	}
	if len(caseIDs) > 0 && sourceVersionID == "" && sourceDatasetID == "" {
		return db.EvaluationDataset{}, caseIDsRequireSource()
	}
	if sourceDatasetID != "" {
		return h.createDatasetFromDataset(ctx, principal, dataset, sourceDatasetID, caseIDs)
	}
	if sourceVersionID != "" {
		return h.createDatasetFromPreset(ctx, dataset, sourceVersionID, caseIDs)
	}
	created, err := h.store.CreateEvaluationDataset(ctx, dataset)
	if err != nil {
		return db.EvaluationDataset{}, mapStoreError(err, datasetNotFoundMessage, createDatasetFailedMessage)
	}
	return created, nil
}

func (h *Handler) createDatasetFromDataset(ctx context.Context, principal auth.Principal, dataset db.EvaluationDataset, sourceDatasetID string, caseIDs []string) (db.EvaluationDataset, error) {
	if len(caseIDs) == 0 {
		return db.EvaluationDataset{}, caseIDsRequired()
	}
	source, err := h.store.GetEvaluationDataset(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, sourceDatasetID)
	if err != nil {
		return db.EvaluationDataset{}, mapStoreError(err, datasetNotFoundMessage, createDatasetFailedMessage)
	}
	if source.ArchivedAt != nil {
		return db.EvaluationDataset{}, datasetArchived()
	}
	cases, err := h.store.ListEvaluationCasesByVersion(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, source.Version.UUID)
	if err != nil {
		return db.EvaluationDataset{}, mapStoreError(err, datasetNotFoundMessage, createDatasetFailedMessage)
	}
	selected, err := selectStoredCases(cases, caseIDs)
	if err != nil {
		return db.EvaluationDataset{}, err
	}
	writes, err := newCaseWrites(selected)
	if err != nil {
		return db.EvaluationDataset{}, mapStoreError(err, datasetNotFoundMessage, createDatasetFailedMessage)
	}
	created, err := h.store.CreateEvaluationDatasetWithCases(ctx, dataset, writes)
	if err != nil {
		return db.EvaluationDataset{}, mapStoreError(err, datasetNotFoundMessage, createDatasetFailedMessage)
	}
	return created, nil
}

func normalizeCaseIDs(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > maxImportedCases {
		return nil, caseIDsTooMany()
	}
	seen := make(map[string]struct{}, len(ids))
	cleaned := make([]string, 0, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			return nil, caseIDsInvalid()
		}
		if _, ok := seen[trimmed]; ok {
			return nil, caseIDsInvalid()
		}
		seen[trimmed] = struct{}{}
		cleaned = append(cleaned, trimmed)
	}
	return cleaned, nil
}

func selectStoredCases(cases []db.EvaluationCase, caseIDs []string) ([]normalizedCase, error) {
	byID := make(map[string]db.EvaluationCase, len(cases))
	for _, item := range cases {
		byID[item.ExternalID] = item
	}
	selected := make([]normalizedCase, 0, len(caseIDs))
	for _, id := range caseIDs {
		item, ok := byID[id]
		if !ok {
			return nil, caseIDsUnknown()
		}
		selected = append(selected, normalizedCase{
			Name:           item.Name,
			Description:    item.Description,
			Turns:          append([]string(nil), item.Turns...),
			Checkpoints:    append(json.RawMessage(nil), item.Checkpoints...),
			InputFiles:     append(json.RawMessage(nil), item.InputFiles...),
			ReferenceFiles: append(json.RawMessage(nil), item.ReferenceFiles...),
		})
	}
	return selected, nil
}

func presetCasesByID(version presetDatasetVersion, caseIDs []string) ([]normalizedCase, error) {
	if len(caseIDs) == 0 {
		return presetCasesNormalized(version), nil
	}
	byID := make(map[string]presetCase, len(version.Cases))
	for _, item := range version.Cases {
		byID[item.ID] = item
	}
	selected := make([]presetCase, 0, len(caseIDs))
	for _, id := range caseIDs {
		item, ok := byID[id]
		if !ok {
			return nil, caseIDsUnknown()
		}
		selected = append(selected, item)
	}
	return presetCasesNormalized(presetDatasetVersion{Cases: selected}), nil
}

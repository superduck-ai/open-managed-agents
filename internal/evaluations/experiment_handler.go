package evaluations

import (
	"context"
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/google/uuid"
)

type resolvedExperiment struct {
	Version     db.EvaluationDatasetVersion
	Cases       []db.EvaluationCase
	Agent       db.Agent
	Environment db.Environment
	Evaluators  []db.EvaluationEvaluator
}

func (h *Handler) createExperiment(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r, experimentNotFound())
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[createExperimentRequest](w, r, maxDatasetBodyBytes)
	if err != nil {
		return invalidJSONRequest(err)
	}
	draft, err := normalizeExperiment(*body)
	if err != nil {
		return err
	}
	created, err := h.persistExperiment(r.Context(), principal, draft)
	if err != nil {
		return err
	}
	if h.starter != nil {
		created, err = h.startPendingItems(r.Context(), principal, created)
		if err != nil {
			return err
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, experimentResponseFrom(created, true))
	return nil
}

func (h *Handler) listExperiments(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r, experimentNotFound())
	if err != nil {
		return err
	}
	limit, cursor, err := pageQuery(r, decodeExperimentCursor)
	if err != nil {
		return err
	}
	experiments, hasMore, err := h.store.ListEvaluationExperimentsPage(r.Context(), db.ListEvaluationExperimentsPageParams{
		OrganizationUUID: principal.OrganizationUUID,
		WorkspaceUUID:    principal.WorkspaceUUID,
		Limit:            limit,
		Cursor:           cursor,
	})
	if err != nil {
		return mapStoreError(err, experimentNotFoundMessage, listExperimentsFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, experimentPage(experiments, hasMore, false))
	return nil
}

func (h *Handler) getExperiment(w http.ResponseWriter, r *http.Request) error {
	principal, experimentID, err := scopedExperimentID(r)
	if err != nil {
		return err
	}
	experiment, err := h.store.GetEvaluationExperiment(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, experimentID)
	if err != nil {
		return mapStoreError(err, experimentNotFoundMessage, loadExperimentFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, experimentResponseFrom(experiment, true))
	return nil
}

func (h *Handler) startExperiment(w http.ResponseWriter, r *http.Request) error {
	principal, experimentID, err := scopedExperimentID(r)
	if err != nil {
		return err
	}
	experiment, err := h.store.GetEvaluationExperiment(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID, experimentID)
	if err != nil {
		return mapStoreError(err, experimentNotFoundMessage, loadExperimentFailedMessage)
	}
	if h.starter != nil {
		experiment, err = h.startPendingItems(r.Context(), principal, experiment)
		if err != nil {
			return err
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, experimentResponseFrom(experiment, true))
	return nil
}

func (h *Handler) compareExperiments(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r, experimentNotFound())
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[compareExperimentsRequest](w, r, maxDatasetBodyBytes)
	if err != nil {
		return invalidJSONRequest(err)
	}
	versionID, evaluatorIDs, err := normalizeCompare(*body)
	if err != nil {
		return err
	}
	version, err := h.loadDatasetVersion(r.Context(), principal, versionID, compareExperimentsFailedMessage)
	if err != nil {
		return err
	}
	evaluators, err := h.loadEvaluators(r.Context(), principal, evaluatorIDs, compareExperimentsFailedMessage)
	if err != nil {
		return err
	}
	matches, err := h.store.ListCompletedEvaluationExperiments(
		r.Context(),
		principal.OrganizationUUID,
		principal.WorkspaceUUID,
		version.ExternalID,
		db.EvaluationEvaluatorSetKey(evaluatorVersionIDs(evaluators)),
	)
	if err != nil {
		return mapStoreError(err, experimentNotFoundMessage, compareExperimentsFailedMessage)
	}
	httpapi.WriteJSON(w, http.StatusOK, experimentPage(matches, false, true))
	return nil
}

func (h *Handler) persistExperiment(ctx context.Context, principal auth.Principal, draft experimentDraft) (db.EvaluationExperiment, error) {
	resolved, err := h.resolveExperiment(ctx, principal, draft)
	if err != nil {
		return db.EvaluationExperiment{}, err
	}
	input, err := newExperiment(principal, draft, resolved)
	if err != nil {
		return db.EvaluationExperiment{}, err
	}
	created, err := h.store.CreateEvaluationExperiment(ctx, input)
	if err != nil {
		return db.EvaluationExperiment{}, mapStoreError(err, experimentNotFoundMessage, createExperimentFailedMessage)
	}
	return created, nil
}

func (h *Handler) resolveExperiment(ctx context.Context, principal auth.Principal, draft experimentDraft) (resolvedExperiment, error) {
	version, err := h.loadDatasetVersion(ctx, principal, draft.DatasetVersionID, createExperimentFailedMessage)
	if err != nil {
		return resolvedExperiment{}, err
	}
	cases, err := h.store.ListEvaluationCasesByVersion(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, version.UUID)
	if err != nil {
		return resolvedExperiment{}, mapStoreError(err, datasetVersionNotFoundMessage, createExperimentFailedMessage)
	}
	if len(cases) == 0 {
		return resolvedExperiment{}, casesRequired()
	}
	agent, err := h.loadAgent(ctx, principal.WorkspaceUUID, draft.AgentID, draft.AgentVersion)
	if err != nil {
		return resolvedExperiment{}, err
	}
	environment, err := h.loadEnvironment(ctx, principal.WorkspaceUUID, draft.EnvironmentID)
	if err != nil {
		return resolvedExperiment{}, err
	}
	evaluators, err := h.loadEvaluators(ctx, principal, draft.EvaluatorIDs, createExperimentFailedMessage)
	if err != nil {
		return resolvedExperiment{}, err
	}
	return resolvedExperiment{Version: version, Cases: cases, Agent: agent, Environment: environment, Evaluators: evaluators}, nil
}

func (h *Handler) loadDatasetVersion(ctx context.Context, principal auth.Principal, externalID, failed string) (db.EvaluationDatasetVersion, error) {
	version, err := h.store.GetEvaluationDatasetVersion(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, externalID)
	if err != nil {
		return db.EvaluationDatasetVersion{}, mapStoreError(err, datasetVersionNotFoundMessage, failed)
	}
	return version, nil
}

func (h *Handler) loadAgent(ctx context.Context, workspaceUUID, agentID string, version int) (db.Agent, error) {
	var agent db.Agent
	var err error
	if version > 0 {
		agent, err = h.store.GetAgentVersion(ctx, workspaceUUID, agentID, version)
	} else {
		agent, err = h.store.GetAgent(ctx, workspaceUUID, agentID)
	}
	if err != nil {
		return db.Agent{}, mapStoreError(err, agentNotFoundMessage, createExperimentFailedMessage)
	}
	if agent.ArchivedAt != nil {
		return db.Agent{}, agentArchived()
	}
	if agent.CurrentVersion < 1 {
		return db.Agent{}, agentVersionInvalid()
	}
	return agent, nil
}

func (h *Handler) loadEnvironment(ctx context.Context, workspaceUUID, environmentID string) (db.Environment, error) {
	environment, err := h.store.GetEnvironment(ctx, workspaceUUID, environmentID)
	if err != nil {
		return db.Environment{}, mapStoreError(err, environmentNotFoundMessage, createExperimentFailedMessage)
	}
	if environment.ArchivedAt != nil {
		return db.Environment{}, environmentArchived()
	}
	return environment, nil
}

func (h *Handler) loadEvaluators(ctx context.Context, principal auth.Principal, evaluatorIDs []string, failed string) ([]db.EvaluationEvaluator, error) {
	evaluators := make([]db.EvaluationEvaluator, 0, len(evaluatorIDs))
	seen := make(map[string]struct{}, len(evaluatorIDs))
	for _, evaluatorID := range evaluatorIDs {
		evaluator, err := h.store.GetEvaluationEvaluator(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, evaluatorID)
		if err != nil {
			return nil, mapStoreError(err, evaluatorNotFoundMessage, failed)
		}
		if _, ok := seen[evaluator.UUID]; ok {
			return nil, evaluatorIDsDuplicate()
		}
		seen[evaluator.UUID] = struct{}{}
		evaluators = append(evaluators, evaluator)
	}
	return evaluators, nil
}

func newExperiment(principal auth.Principal, draft experimentDraft, resolved resolvedExperiment) (db.EvaluationExperimentCreate, error) {
	externalID, err := ids.New("evalexp_")
	if err != nil {
		return db.EvaluationExperimentCreate{}, mapStoreError(err, experimentNotFoundMessage, createExperimentFailedMessage)
	}
	now := time.Now().UTC()
	experimentUUID := uuid.NewString()
	bindings, versionIDs, err := newExperimentBindings(principal, experimentUUID, resolved.Evaluators, now)
	if err != nil {
		return db.EvaluationExperimentCreate{}, err
	}
	items, err := newExperimentItems(principal, experimentUUID, resolved.Cases, now)
	if err != nil {
		return db.EvaluationExperimentCreate{}, err
	}
	return db.EvaluationExperimentCreate{
		Experiment: db.EvaluationExperiment{
			UUID:                  experimentUUID,
			ExternalID:            externalID,
			OrganizationUUID:      principal.OrganizationUUID,
			WorkspaceUUID:         principal.WorkspaceUUID,
			Name:                  draft.Name,
			Description:           draft.Description,
			DatasetUUID:           resolved.Version.DatasetUUID,
			DatasetExternalID:     resolved.Version.DatasetExternalID,
			VersionUUID:           resolved.Version.UUID,
			VersionExternalID:     resolved.Version.ExternalID,
			AgentExternalID:       resolved.Agent.ExternalID,
			AgentVersion:          resolved.Agent.CurrentVersion,
			EnvironmentExternalID: resolved.Environment.ExternalID,
			TimeoutSeconds:        draft.TimeoutSeconds,
			Env:                   draft.Env,
			Resources:             draft.Resources,
			EvaluatorSetKey:       db.EvaluationEvaluatorSetKey(versionIDs),
			Status:                db.EvaluationExperimentStatusPending,
			CreatedByUserUUID:     principal.UserUUID,
			CreatedAt:             now,
			UpdatedAt:             now,
		},
		Bindings: bindings,
		Items:    items,
	}, nil
}

func newExperimentBindings(principal auth.Principal, experimentUUID string, evaluators []db.EvaluationEvaluator, now time.Time) ([]db.EvaluationExperimentBinding, []string, error) {
	bindings := make([]db.EvaluationExperimentBinding, 0, len(evaluators))
	versionIDs := make([]string, 0, len(evaluators))
	for index, evaluator := range evaluators {
		externalID, err := ids.New("evalbind_")
		if err != nil {
			return nil, nil, mapStoreError(err, experimentNotFoundMessage, createExperimentFailedMessage)
		}
		versionIDs = append(versionIDs, evaluator.Version.ExternalID)
		bindings = append(bindings, db.EvaluationExperimentBinding{
			UUID:                uuid.NewString(),
			ExternalID:          externalID,
			OrganizationUUID:    principal.OrganizationUUID,
			WorkspaceUUID:       principal.WorkspaceUUID,
			ExperimentUUID:      experimentUUID,
			Position:            index + 1,
			EvaluatorUUID:       evaluator.UUID,
			EvaluatorExternalID: evaluator.ExternalID,
			VersionUUID:         evaluator.Version.UUID,
			VersionExternalID:   evaluator.Version.ExternalID,
			CreatedAt:           now,
		})
	}
	return bindings, versionIDs, nil
}

func newExperimentItems(principal auth.Principal, experimentUUID string, cases []db.EvaluationCase, now time.Time) ([]db.EvaluationExperimentItem, error) {
	items := make([]db.EvaluationExperimentItem, 0, len(cases))
	for index, item := range cases {
		externalID, err := ids.New("evalitem_")
		if err != nil {
			return nil, mapStoreError(err, experimentNotFoundMessage, createExperimentFailedMessage)
		}
		position := item.Position
		if position < 1 {
			position = index + 1
		}
		items = append(items, db.EvaluationExperimentItem{
			UUID:             uuid.NewString(),
			ExternalID:       externalID,
			OrganizationUUID: principal.OrganizationUUID,
			WorkspaceUUID:    principal.WorkspaceUUID,
			ExperimentUUID:   experimentUUID,
			CaseUUID:         item.UUID,
			CaseExternalID:   item.ExternalID,
			Position:         position,
			Status:           db.EvaluationExperimentStatusPending,
			CreatedAt:        now,
			UpdatedAt:        now,
		})
	}
	return items, nil
}

func (h *Handler) startPendingItems(ctx context.Context, principal auth.Principal, experiment db.EvaluationExperiment) (db.EvaluationExperiment, error) {
	turns, err := h.caseTurns(ctx, principal, experiment)
	if err != nil {
		return db.EvaluationExperiment{}, err
	}
	changed := false
	for index := range experiment.Items {
		item := &experiment.Items[index]
		if item.Status != db.EvaluationExperimentStatusPending {
			continue
		}
		if err := h.startItem(ctx, principal, &experiment, item, turns[item.CaseExternalID]); err != nil {
			return db.EvaluationExperiment{}, err
		}
		changed = true
	}
	if !changed {
		return experiment, nil
	}
	return h.finishExperimentStart(ctx, principal, experiment)
}

func (h *Handler) caseTurns(ctx context.Context, principal auth.Principal, experiment db.EvaluationExperiment) (map[string][]string, error) {
	cases, err := h.store.ListEvaluationCasesByVersion(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, experiment.VersionUUID)
	if err != nil {
		return nil, mapStoreError(err, datasetVersionNotFoundMessage, startExperimentFailedMessage)
	}
	turns := make(map[string][]string, len(cases))
	for _, item := range cases {
		turns[item.ExternalID] = item.Turns
	}
	return turns, nil
}

func (h *Handler) startItem(ctx context.Context, principal auth.Principal, experiment *db.EvaluationExperiment, item *db.EvaluationExperimentItem, turns []string) error {
	update := db.EvaluationExperimentItemSession{
		OrganizationUUID: principal.OrganizationUUID,
		WorkspaceUUID:    principal.WorkspaceUUID,
		ExternalID:       item.ExternalID,
		UpdatedAt:        time.Now().UTC(),
	}
	sessionID, startErr := h.starter.StartCaseSession(ctx, principal, caseSessionInput(*experiment, *item, turns))
	if startErr != nil {
		h.logger.WarnContext(ctx, "evaluation case session failed",
			"organization_id", principal.OrganizationUUID,
			"workspace_id", principal.WorkspaceUUID,
			"experiment_id", experiment.ExternalID,
			"case_id", item.CaseExternalID,
			"error", startErr,
		)
		update.Status = db.EvaluationExperimentStatusFailed
		update.ErrorMessage = caseSessionErrorMessage(startErr)
	} else {
		update.Status = db.EvaluationExperimentStatusRunning
		update.SessionExternalID = sessionID
	}
	if err := h.store.SetEvaluationExperimentItemSession(ctx, update); err != nil {
		return mapStoreError(err, experimentNotFoundMessage, startExperimentFailedMessage)
	}
	item.Status = update.Status
	item.SessionExternalID = update.SessionExternalID
	item.ErrorMessage = update.ErrorMessage
	return nil
}

func (h *Handler) finishExperimentStart(ctx context.Context, principal auth.Principal, experiment db.EvaluationExperiment) (db.EvaluationExperiment, error) {
	status := experimentStatus(experiment.Items)
	if status != experiment.Status {
		updatedAt := time.Now().UTC()
		if err := h.store.UpdateEvaluationExperimentStatus(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, experiment.ExternalID, status, updatedAt); err != nil {
			return db.EvaluationExperiment{}, mapStoreError(err, experimentNotFoundMessage, startExperimentFailedMessage)
		}
	}
	reloaded, err := h.store.GetEvaluationExperiment(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, experiment.ExternalID)
	if err != nil {
		return db.EvaluationExperiment{}, mapStoreError(err, experimentNotFoundMessage, loadExperimentFailedMessage)
	}
	return reloaded, nil
}

func caseSessionInput(experiment db.EvaluationExperiment, item db.EvaluationExperimentItem, turns []string) CaseSessionInput {
	return CaseSessionInput{
		AgentExternalID:       experiment.AgentExternalID,
		AgentVersion:          experiment.AgentVersion,
		EnvironmentExternalID: experiment.EnvironmentExternalID,
		Resources:             experiment.Resources,
		Turns:                 turns,
		Metadata: map[string]string{
			"experiment_id": experiment.ExternalID,
			"case_id":       item.CaseExternalID,
		},
	}
}

func experimentStatus(items []db.EvaluationExperimentItem) string {
	if len(items) == 0 {
		return db.EvaluationExperimentStatusPending
	}
	var running, pending, failed, cancelled, completed int
	for _, item := range items {
		switch item.Status {
		case db.EvaluationExperimentStatusRunning:
			running++
		case db.EvaluationExperimentStatusPending:
			pending++
		case db.EvaluationExperimentStatusCancelled:
			cancelled++
		case db.EvaluationExperimentStatusFailed:
			failed++
		case db.EvaluationExperimentStatusCompleted:
			completed++
		}
	}
	switch {
	case running > 0:
		return db.EvaluationExperimentStatusRunning
	case pending > 0:
		return db.EvaluationExperimentStatusPending
	case failed > 0:
		return db.EvaluationExperimentStatusFailed
	case cancelled > 0:
		return db.EvaluationExperimentStatusCancelled
	case completed == len(items):
		return db.EvaluationExperimentStatusCompleted
	default:
		return db.EvaluationExperimentStatusPending
	}
}

func caseSessionErrorMessage(err error) string {
	var appErr *apperr.Error
	if errors.As(err, &appErr) && appErr.PublicMessage != "" {
		return truncateRunes(appErr.PublicMessage, maxExperimentItemErrorRunes)
	}
	return caseSessionFailedMessage
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

func evaluatorVersionIDs(evaluators []db.EvaluationEvaluator) []string {
	ids := make([]string, 0, len(evaluators))
	for _, evaluator := range evaluators {
		ids = append(ids, evaluator.Version.ExternalID)
	}
	return ids
}

func scopedExperimentID(r *http.Request) (auth.Principal, string, error) {
	return scopedPathID(r, "experimentId", experimentNotFound())
}

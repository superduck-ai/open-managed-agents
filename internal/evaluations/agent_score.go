package evaluations

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	agentScoreRoleKey            = "evaluation_role"
	agentScoreRole               = "agent_score"
	agentScoreExperimentKey      = "experiment_id"
	agentScoreItemKey            = "item_id"
	agentScoreVersionKey         = "evaluator_version_id"
	agentScoreFileMissingMessage = "Evaluation score file is missing"
	agentScoreSandboxResultPath  = "/mnt/session/uploads/data/session/result.json"
	agentScoreSandboxOutputPath  = "/mnt/session/outputs/score.json"

	AgentScoreResultDirectory = "/uploads/data/session"
	AgentScoreResultPath      = "/uploads/data/session/result.json"
	AgentScoreOutputPath      = "/outputs/score.json"
)

var ErrAgentScoreFileMissing = errors.New("evaluation score file missing")

type AgentScoreInput struct {
	OrganizationUUID      string
	WorkspaceUUID         string
	UserUUID              string
	AgentExternalID       string
	AgentVersion          int
	EnvironmentExternalID string
	ResultJSON            []byte
	Turns                 []string
	Metadata              map[string]string
	TimeoutSeconds        int
	Env                   json.RawMessage
}

type AgentScoreClaim struct {
	OrganizationUUID string
	WorkspaceUUID    string
	ExperimentID     string
	ItemID           string
	VersionID        string
}

type AgentScoreSandbox interface {
	Start(context.Context, AgentScoreInput) (string, error)
	Claim(context.Context, string, string, string) (AgentScoreClaim, bool, error)
	ReadScore(context.Context, string, string, string) ([]byte, error)
}

type agentScoreTarget struct {
	experiment db.EvaluationExperiment
	item       db.EvaluationExperimentItem
	binding    db.EvaluationExperimentBinding
	version    db.EvaluationEvaluatorVersion
	versionErr error
}

type agentScoreResult struct {
	SessionID        string              `json:"session_id"`
	Input            string              `json:"input"`
	Output           string              `json:"output"`
	Checkpoints      json.RawMessage     `json:"checkpoints"`
	AgentTrajectory  string              `json:"agent_trajectory"`
	InvocationResult string              `json:"invocation_result"`
	Artifacts        agentScoreArtifacts `json:"artifacts"`
}

type agentScoreArtifacts struct {
	InputFiles     json.RawMessage `json:"input_files"`
	ReferenceFiles json.RawMessage `json:"reference_files"`
}

func (h *Handler) scoreAgent(ctx context.Context, outcome sessionOutcome, experiment db.EvaluationExperiment, item db.EvaluationExperimentItem, binding db.EvaluationExperimentBinding, version db.EvaluationEvaluatorVersion, values scoreValues) {
	if h.sandbox == nil {
		h.recordScore(ctx, outcome, experiment, item, binding, version, nil, "", nil, agentScoringUnavailableMessage)
		return
	}
	agent, err := h.scoringAgent(ctx, outcome.WorkspaceUUID, version.AgentID, binding.AgentVersion)
	if err != nil {
		message := evaluatorScoreFailedMessage
		if errors.Is(err, db.ErrNotFound) {
			message = agentNotFoundMessage
		}
		h.recordScore(ctx, outcome, experiment, item, binding, version, nil, "", nil, message)
		return
	}
	if agent.ArchivedAt != nil || agent.CurrentVersion < 1 {
		message := evaluatorScoreFailedMessage
		if agent.ArchivedAt != nil {
			message = agentArchivedMessage
		}
		h.recordScore(ctx, outcome, experiment, item, binding, version, nil, "", nil, message)
		return
	}
	result, err := agentScoreResultJSON(outcome.SessionExternalID, values)
	if err != nil {
		h.recordScore(ctx, outcome, experiment, item, binding, version, nil, "", nil, evaluatorScoreFailedMessage)
		return
	}
	_, err = h.sandbox.Start(ctx, AgentScoreInput{
		OrganizationUUID:      outcome.OrganizationUUID,
		WorkspaceUUID:         outcome.WorkspaceUUID,
		UserUUID:              experiment.CreatedByUserUUID,
		AgentExternalID:       version.AgentID,
		AgentVersion:          agent.CurrentVersion,
		EnvironmentExternalID: experiment.EnvironmentExternalID,
		ResultJSON:            result,
		Turns:                 agentScoreTurns(result),
		Metadata:              agentScoreMetadata(experiment.ExternalID, item.ExternalID, binding.VersionExternalID),
		TimeoutSeconds:        experiment.TimeoutSeconds,
		Env:                   experiment.Env,
	})
	if err != nil {
		h.logger.WarnContext(ctx, "start evaluation agent score",
			"organization_id", outcome.OrganizationUUID,
			"workspace_id", outcome.WorkspaceUUID,
			"experiment_id", experiment.ExternalID,
			"evaluator_version_id", version.ExternalID,
			"error", err,
		)
		h.recordScore(ctx, outcome, experiment, item, binding, version, nil, "", nil, evaluatorScoreFailedMessage)
	}
}

func (h *Handler) scoringAgent(ctx context.Context, workspaceUUID, agentID string, frozenVersion int) (db.Agent, error) {
	if frozenVersion > 0 {
		return h.store.GetAgentVersion(ctx, workspaceUUID, agentID, frozenVersion)
	}
	return h.store.GetAgent(ctx, workspaceUUID, agentID)
}

func (h *Handler) settleAgentScore(ctx context.Context, outcome sessionOutcome) {
	if h.sandbox == nil {
		return
	}
	claim, ok, err := h.sandbox.Claim(ctx, outcome.OrganizationUUID, outcome.WorkspaceUUID, outcome.SessionExternalID)
	if err != nil {
		h.logger.WarnContext(ctx, "claim evaluation agent score",
			"organization_id", outcome.OrganizationUUID,
			"workspace_id", outcome.WorkspaceUUID,
			"session_id", outcome.SessionExternalID,
			"error", err,
		)
		return
	}
	if !ok || claim.OrganizationUUID != outcome.OrganizationUUID || claim.WorkspaceUUID != outcome.WorkspaceUUID {
		return
	}
	target, ok := h.agentScoreTarget(ctx, outcome, claim)
	if !ok {
		return
	}
	h.applyAgentScore(ctx, outcome, target)
}

func (h *Handler) agentScoreTarget(ctx context.Context, outcome sessionOutcome, claim AgentScoreClaim) (agentScoreTarget, bool) {
	experiment, err := h.store.GetEvaluationExperiment(ctx, outcome.OrganizationUUID, outcome.WorkspaceUUID, claim.ExperimentID)
	if err != nil {
		h.logger.WarnContext(ctx, "load evaluation experiment for agent score",
			"organization_id", outcome.OrganizationUUID,
			"workspace_id", outcome.WorkspaceUUID,
			"experiment_id", claim.ExperimentID,
			"error", err,
		)
		return agentScoreTarget{}, false
	}
	item, ok := experimentItemByID(experiment, claim.ItemID)
	if !ok {
		h.logger.WarnContext(ctx, "evaluation agent score item missing",
			"organization_id", outcome.OrganizationUUID,
			"workspace_id", outcome.WorkspaceUUID,
			"experiment_id", claim.ExperimentID,
			"session_id", outcome.SessionExternalID,
		)
		return agentScoreTarget{}, false
	}
	binding, ok := experimentBindingByVersion(experiment, claim.VersionID)
	if !ok {
		h.logger.WarnContext(ctx, "evaluation agent score binding missing",
			"organization_id", outcome.OrganizationUUID,
			"workspace_id", outcome.WorkspaceUUID,
			"experiment_id", claim.ExperimentID,
			"evaluator_version_id", claim.VersionID,
		)
		return agentScoreTarget{}, false
	}
	version, err := h.store.GetEvaluationEvaluatorVersion(ctx, outcome.OrganizationUUID, outcome.WorkspaceUUID, claim.VersionID)
	return agentScoreTarget{experiment: experiment, item: item, binding: binding, version: version, versionErr: err}, true
}

func (h *Handler) applyAgentScore(ctx context.Context, outcome sessionOutcome, target agentScoreTarget) {
	if target.versionErr != nil {
		h.recordScore(ctx, outcome, target.experiment, target.item, target.binding, target.version, nil, "", nil, evaluatorVersionNotFoundMessage)
		return
	}
	if outcome.Status != db.EvaluationExperimentStatusCompleted {
		message := outcome.ErrorMessage
		if message == "" {
			message = evaluatorScoreFailedMessage
		}
		h.recordScore(ctx, outcome, target.experiment, target.item, target.binding, target.version, nil, "", nil, message)
		return
	}
	raw, err := h.sandbox.ReadScore(ctx, outcome.OrganizationUUID, outcome.WorkspaceUUID, outcome.SessionExternalID)
	if err != nil {
		h.logger.WarnContext(ctx, "read evaluation agent score",
			"organization_id", outcome.OrganizationUUID,
			"workspace_id", outcome.WorkspaceUUID,
			"experiment_id", target.experiment.ExternalID,
			"session_id", outcome.SessionExternalID,
			"error", err,
		)
		h.recordScore(ctx, outcome, target.experiment, target.item, target.binding, target.version, nil, "", nil, scoreFailureMessage(err))
		return
	}
	scored, err := parseEvaluatorScoreText(string(raw))
	if err != nil {
		h.recordScore(ctx, outcome, target.experiment, target.item, target.binding, target.version, nil, "", nil, evaluatorScoreParseMessage)
		return
	}
	h.recordScore(ctx, outcome, target.experiment, target.item, target.binding, target.version, &scored.Score, scored.Explanation, scored.Details, "")
}

func agentScoreResultJSON(sessionID string, values scoreValues) ([]byte, error) {
	return json.Marshal(agentScoreResult{
		SessionID:        sessionID,
		Input:            values.Input,
		Output:           values.Output,
		Checkpoints:      values.CheckpointsJSON,
		AgentTrajectory:  values.AgentTrajectory,
		InvocationResult: values.InvocationResult,
		Artifacts: agentScoreArtifacts{
			InputFiles:     values.InputFiles,
			ReferenceFiles: values.ReferenceFiles,
		},
	})
}

func agentScoreTurns(result []byte) []string {
	return []string{
		"Read " + evaluatorResultPath + " and write " + evaluatorScorePath + " as JSON with score, explanation, and details.\n" +
			"Sandbox input: " + agentScoreSandboxResultPath + "\n" +
			"Sandbox output: " + agentScoreSandboxOutputPath + "\n" +
			string(result),
	}
}

func agentScoreMetadata(experimentID, itemID, versionID string) map[string]string {
	return map[string]string{
		agentScoreRoleKey:       agentScoreRole,
		agentScoreExperimentKey: experimentID,
		agentScoreItemKey:       itemID,
		agentScoreVersionKey:    versionID,
	}
}

func AgentScoreClaimFromMetadata(raw json.RawMessage) (AgentScoreClaim, bool) {
	var metadata map[string]string
	if len(raw) == 0 || json.Unmarshal(raw, &metadata) != nil {
		return AgentScoreClaim{}, false
	}
	if metadata[agentScoreRoleKey] != agentScoreRole {
		return AgentScoreClaim{}, false
	}
	claim := AgentScoreClaim{
		ExperimentID: metadata[agentScoreExperimentKey],
		ItemID:       metadata[agentScoreItemKey],
		VersionID:    metadata[agentScoreVersionKey],
	}
	if claim.ExperimentID == "" || claim.ItemID == "" || claim.VersionID == "" {
		return AgentScoreClaim{}, false
	}
	return claim, true
}

func experimentItemByID(experiment db.EvaluationExperiment, itemID string) (db.EvaluationExperimentItem, bool) {
	for _, item := range experiment.Items {
		if item.ExternalID == itemID {
			return item, true
		}
	}
	return db.EvaluationExperimentItem{}, false
}

func experimentBindingByVersion(experiment db.EvaluationExperiment, versionID string) (db.EvaluationExperimentBinding, bool) {
	for _, binding := range experiment.Bindings {
		if binding.VersionExternalID == versionID {
			return binding, true
		}
	}
	return db.EvaluationExperimentBinding{}, false
}

package evaluations

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	invalidJSONMessage              = "Invalid JSON body"
	invalidLimitMessage             = "limit must be between 1 and 100"
	invalidPageMessage              = "page is invalid"
	nameRequiredMessage             = "name is required"
	nameTooLongMessage              = "name must be at most 255 characters"
	descriptionTooLongMessage       = "desc must be at most 1024 characters"
	turnsRequiredMessage            = "turns must include at least one entry"
	turnsInvalidMessage             = "turns must be an array of strings"
	turnEmptyMessage                = "turns cannot include an empty entry"
	checkpointsInvalidMessage       = "checkpoints must be a JSON array or object"
	inputFilesInvalidMessage        = "input_files must be a JSON array"
	referenceFilesInvalidMessage    = "reference_files must be a JSON array"
	attachmentTooLargeMessage       = "case attachment is too large"
	importRequiredMessage           = "import requires at least one case"
	importTooLargeMessage           = "import accepts at most 1000 cases"
	importBodyTooLargeMessage       = "import body is too large"
	importJSONLInvalidMessage       = "import body must be JSONL"
	importZipInvalidMessage         = "import ZIP must contain one JSONL file"
	datasetNotFoundMessage          = "Evaluation dataset not found"
	datasetArchivedMessage          = "Evaluation dataset is archived"
	sessionIDRequiredMessage        = "session_id is required"
	sessionIDTooLongMessage         = "session_id must be at most 255 characters"
	sessionIDsRequiredMessage       = "session_ids must include at least one entry"
	sessionIDsTooManyMessage        = "session_ids accepts at most 50 entries"
	sessionNotFoundMessage          = "Session not found"
	scoringSessionMessage           = "Evaluation scoring sessions cannot be added to a dataset"
	loadSessionFailedMessage        = "Could not load session"
	presetDatasetNotFoundMessage    = "Evaluation dataset preset not found"
	presetEvaluatorNotFoundMessage  = "Evaluation evaluator preset not found"
	presetArchivedMessage           = "Published preset is archived"
	presetSourceConflictMessage     = "source_version_id cannot be combined with evaluator fields"
	duplicateResourceMessage        = "Evaluation resource already exists"
	createDatasetFailedMessage      = "Could not create evaluation dataset"
	listDatasetsFailedMessage       = "Could not list evaluation datasets"
	loadDatasetFailedMessage        = "Could not load evaluation dataset"
	createCasesFailedMessage        = "Could not create evaluation cases"
	listCasesFailedMessage          = "Could not list evaluation cases"
	evaluatorTypeInvalidMessage     = "evaluator_type must be llm or agent"
	modelRequiredMessage            = "model is required"
	modelForbiddenMessage           = "model must be empty for an agent evaluator"
	modelTooLongMessage             = "model must be at most 255 characters"
	agentIDRequiredMessage          = "agent_id is required"
	agentIDForbiddenMessage         = "agent_id must be empty for an llm evaluator"
	agentIDTooLongMessage           = "agent_id must be at most 255 characters"
	promptRequiredMessage           = "prompt_template is required"
	promptForbiddenMessage          = "prompt_template must be empty for an agent evaluator"
	promptTooLongMessage            = "prompt_template must be at most 16000 characters"
	promptVariableMessage           = "prompt_template references a variable that is not selected"
	variablesInvalidMessage         = "variables must use input, output, checkpoints, agent_trajectory, or invocation_result"
	variablesDuplicateMessage       = "variables cannot contain duplicates"
	variablesRequiredMessage        = "variables must include at least one entry"
	evaluatorNotFoundMessage        = "Evaluation evaluator not found"
	createEvaluatorFailedMessage    = "Could not create evaluation evaluator"
	listEvaluatorsFailedMessage     = "Could not list evaluation evaluators"
	loadEvaluatorFailedMessage      = "Could not load evaluation evaluator"
	datasetVersionRequiredMessage   = "dataset_version_id is required"
	datasetVersionNotFoundMessage   = "Evaluation dataset version not found"
	casesRequiredMessage            = "dataset version has no cases"
	environmentIDRequiredMessage    = "environment_id is required"
	environmentNotFoundMessage      = "Environment not found"
	agentNotFoundMessage            = "Agent not found"
	agentArchivedMessage            = "agent must not be archived"
	environmentArchivedMessage      = "environment must not be archived"
	agentVersionInvalidMessage      = "agent_version must be at least 1"
	timeoutInvalidMessage           = "timeout_seconds must be between 1 and 86400"
	evaluatorIDsRequiredMessage     = "evaluator_ids must include at least one entry"
	evaluatorIDsDuplicateMessage    = "evaluator_ids cannot contain duplicates"
	evaluatorIDsTooManyMessage      = "evaluator_ids accepts at most 20 entries"
	envInvalidMessage               = "env must be a JSON object of strings"
	resourcesInvalidMessage         = "resources must be a JSON array"
	experimentNotFoundMessage       = "Evaluation experiment not found"
	createExperimentFailedMessage   = "Could not create evaluation experiment"
	listExperimentsFailedMessage    = "Could not list evaluation experiments"
	loadExperimentFailedMessage     = "Could not load evaluation experiment"
	startExperimentFailedMessage    = "Could not start evaluation experiment"
	compareExperimentsFailedMessage = "Could not compare evaluation experiments"
	caseSessionFailedMessage        = "Could not create session"
	internalServerErrorMessage      = "Internal server error"
)

var errInvalidPageCursor = errors.New("invalid evaluation page cursor")
var errTrailingJSON = errors.New("trailing json")
var errMissingCreatedCase = errors.New("created evaluation case missing")
var errZipMember = errors.New("zip member")

func invalidJSONRequest(cause error) error {
	return apperr.New(apperr.InvalidArgument, invalidJSONMessage, cause)
}

func invalidLimit(cause error) error {
	return apperr.New(apperr.InvalidArgument, invalidLimitMessage, cause)
}

func invalidPage(cause error) error {
	return apperr.New(apperr.InvalidArgument, invalidPageMessage, cause)
}

func nameRequired() error {
	return apperr.New(apperr.InvalidArgument, nameRequiredMessage, nil)
}

func nameTooLong() error {
	return apperr.New(apperr.InvalidArgument, nameTooLongMessage, nil)
}

func descriptionTooLong() error {
	return apperr.New(apperr.InvalidArgument, descriptionTooLongMessage, nil)
}

func turnsRequired() error {
	return apperr.New(apperr.InvalidArgument, turnsRequiredMessage, nil)
}

func turnsInvalid() error {
	return apperr.New(apperr.InvalidArgument, turnsInvalidMessage, nil)
}

func turnEmpty() error {
	return apperr.New(apperr.InvalidArgument, turnEmptyMessage, nil)
}

func checkpointsInvalid() error {
	return apperr.New(apperr.InvalidArgument, checkpointsInvalidMessage, nil)
}

func inputFilesInvalid() error {
	return apperr.New(apperr.InvalidArgument, inputFilesInvalidMessage, nil)
}

func referenceFilesInvalid() error {
	return apperr.New(apperr.InvalidArgument, referenceFilesInvalidMessage, nil)
}

func attachmentTooLarge() error {
	return apperr.New(apperr.InvalidArgument, attachmentTooLargeMessage, nil)
}

func importRequired() error {
	return apperr.New(apperr.InvalidArgument, importRequiredMessage, nil)
}

func importTooLarge() error {
	return apperr.New(apperr.InvalidArgument, importTooLargeMessage, nil)
}

func importBodyTooLarge() error {
	return apperr.New(apperr.InvalidArgument, importBodyTooLargeMessage, nil)
}

func invalidImportJSONL(cause error) error {
	return apperr.New(apperr.InvalidArgument, importJSONLInvalidMessage, cause)
}

func invalidImportZip(cause error) error {
	return apperr.New(apperr.InvalidArgument, importZipInvalidMessage, cause)
}

func datasetNotFound() error {
	return apperr.New(apperr.NotFound, datasetNotFoundMessage, nil)
}

func datasetArchived() error {
	return apperr.New(apperr.InvalidArgument, datasetArchivedMessage, nil)
}

func sessionIDRequired() error {
	return apperr.New(apperr.InvalidArgument, sessionIDRequiredMessage, nil)
}

func sessionIDTooLong() error {
	return apperr.New(apperr.InvalidArgument, sessionIDTooLongMessage, nil)
}

func sessionIDsRequired() error {
	return apperr.New(apperr.InvalidArgument, sessionIDsRequiredMessage, nil)
}

func sessionIDsTooMany() error {
	return apperr.New(apperr.InvalidArgument, sessionIDsTooManyMessage, nil)
}

func sessionNotFound() error {
	return apperr.New(apperr.NotFound, sessionNotFoundMessage, nil)
}

func scoringSessionRejected() error {
	return apperr.New(apperr.InvalidArgument, scoringSessionMessage, nil)
}

func presetSourceConflict() error {
	return apperr.New(apperr.InvalidArgument, presetSourceConflictMessage, nil)
}

func presetLookupError(err error, notFoundMessage string) error {
	if errors.Is(err, errPresetArchived) {
		return apperr.New(apperr.InvalidArgument, presetArchivedMessage, err)
	}
	return apperr.New(apperr.NotFound, notFoundMessage, err)
}

func evaluatorNotFound() error {
	return apperr.New(apperr.NotFound, evaluatorNotFoundMessage, nil)
}

func evaluatorTypeInvalid() error {
	return apperr.New(apperr.InvalidArgument, evaluatorTypeInvalidMessage, nil)
}

func modelRequired() error {
	return apperr.New(apperr.InvalidArgument, modelRequiredMessage, nil)
}

func modelForbidden() error {
	return apperr.New(apperr.InvalidArgument, modelForbiddenMessage, nil)
}

func modelTooLong() error {
	return apperr.New(apperr.InvalidArgument, modelTooLongMessage, nil)
}

func agentIDRequired() error {
	return apperr.New(apperr.InvalidArgument, agentIDRequiredMessage, nil)
}

func agentIDForbidden() error {
	return apperr.New(apperr.InvalidArgument, agentIDForbiddenMessage, nil)
}

func agentIDTooLong() error {
	return apperr.New(apperr.InvalidArgument, agentIDTooLongMessage, nil)
}

func promptRequired() error {
	return apperr.New(apperr.InvalidArgument, promptRequiredMessage, nil)
}

func promptForbidden() error {
	return apperr.New(apperr.InvalidArgument, promptForbiddenMessage, nil)
}

func promptTooLong() error {
	return apperr.New(apperr.InvalidArgument, promptTooLongMessage, nil)
}

func promptVariableInvalid() error {
	return apperr.New(apperr.InvalidArgument, promptVariableMessage, nil)
}

func variablesInvalid() error {
	return apperr.New(apperr.InvalidArgument, variablesInvalidMessage, nil)
}

func variablesDuplicate() error {
	return apperr.New(apperr.InvalidArgument, variablesDuplicateMessage, nil)
}

func variablesRequired() error {
	return apperr.New(apperr.InvalidArgument, variablesRequiredMessage, nil)
}

func datasetVersionRequired() error {
	return apperr.New(apperr.InvalidArgument, datasetVersionRequiredMessage, nil)
}

func casesRequired() error {
	return apperr.New(apperr.InvalidArgument, casesRequiredMessage, nil)
}

func environmentIDRequired() error {
	return apperr.New(apperr.InvalidArgument, environmentIDRequiredMessage, nil)
}

func agentArchived() error {
	return apperr.New(apperr.InvalidArgument, agentArchivedMessage, nil)
}

func environmentArchived() error {
	return apperr.New(apperr.InvalidArgument, environmentArchivedMessage, nil)
}

func agentVersionInvalid() error {
	return apperr.New(apperr.InvalidArgument, agentVersionInvalidMessage, nil)
}

func timeoutInvalid() error {
	return apperr.New(apperr.InvalidArgument, timeoutInvalidMessage, nil)
}

func evaluatorIDsRequired() error {
	return apperr.New(apperr.InvalidArgument, evaluatorIDsRequiredMessage, nil)
}

func evaluatorIDsDuplicate() error {
	return apperr.New(apperr.InvalidArgument, evaluatorIDsDuplicateMessage, nil)
}

func evaluatorIDsTooMany() error {
	return apperr.New(apperr.InvalidArgument, evaluatorIDsTooManyMessage, nil)
}

func envInvalid() error {
	return apperr.New(apperr.InvalidArgument, envInvalidMessage, nil)
}

func resourcesInvalid() error {
	return apperr.New(apperr.InvalidArgument, resourcesInvalidMessage, nil)
}

func experimentNotFound() error {
	return apperr.New(apperr.NotFound, experimentNotFoundMessage, nil)
}

func mapStoreError(err error, notFoundMessage, message string) error {
	switch {
	case errors.Is(err, db.ErrDuplicate):
		return apperr.New(apperr.Conflict, duplicateResourceMessage, err)
	case errors.Is(err, db.ErrNotFound):
		return apperr.New(apperr.NotFound, notFoundMessage, err)
	default:
		return apperr.New(apperr.Internal, message, fmt.Errorf("%s: %w", message, err))
	}
}

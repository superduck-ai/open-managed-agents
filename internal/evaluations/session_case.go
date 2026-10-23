package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

const (
	maxSessionIDLen        = 255
	maxSessionPromoteBatch = 50
)

type createCaseFromSessionRequest struct {
	SessionID          string          `json:"session_id"`
	Name               string          `json:"name"`
	Desc               string          `json:"desc"`
	Checkpoints        json.RawMessage `json:"checkpoints"`
	IncludeOutputFiles bool            `json:"include_output_files"`
	namePrefix         string
}

type createCasesFromSessionsRequest struct {
	SessionIDs         []string        `json:"session_ids"`
	NamePrefix         string          `json:"name_prefix"`
	Desc               string          `json:"desc"`
	Checkpoints        json.RawMessage `json:"checkpoints"`
	IncludeOutputFiles bool            `json:"include_output_files"`
}

type sessionCaseFailure struct {
	SessionID string `json:"session_id"`
	Error     string `json:"error"`
}

type sessionCaseBatchResponse struct {
	Created []caseResponse       `json:"created"`
	Failed  []sessionCaseFailure `json:"failed"`
}

type sessionCaseFileRef struct {
	Name   string `json:"name"`
	FileID string `json:"file_id,omitempty"`
}

func (h *Handler) createCaseFromSession(w http.ResponseWriter, r *http.Request) error {
	principal, datasetID, err := scopedDatasetID(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[createCaseFromSessionRequest](w, r, maxDatasetBodyBytes)
	if err != nil {
		return invalidJSONRequest(err)
	}
	item, err := h.caseFromSession(r.Context(), principal, datasetID, *body)
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

func (h *Handler) createCasesFromSessions(w http.ResponseWriter, r *http.Request) error {
	principal, datasetID, err := scopedDatasetID(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[createCasesFromSessionsRequest](w, r, maxDatasetBodyBytes)
	if err != nil {
		return invalidJSONRequest(err)
	}
	sessionIDs, err := normalizeSessionIDList(body.SessionIDs)
	if err != nil {
		return err
	}
	if err = h.usableCustomDataset(r.Context(), principal, datasetID); err != nil {
		return err
	}
	response := sessionCaseBatchResponse{Created: []caseResponse{}, Failed: []sessionCaseFailure{}}
	for _, sessionID := range sessionIDs {
		item, buildErr := h.caseFromSession(r.Context(), principal, datasetID, createCaseFromSessionRequest{
			SessionID:          sessionID,
			Desc:               body.Desc,
			Checkpoints:        body.Checkpoints,
			IncludeOutputFiles: body.IncludeOutputFiles,
			namePrefix:         body.NamePrefix,
		})
		if buildErr != nil {
			response.Failed = append(response.Failed, sessionCaseFailure{SessionID: strings.TrimSpace(sessionID), Error: publicSessionCaseError(buildErr)})
			continue
		}
		created, insertErr := h.insertCases(r, principal, datasetID, []normalizedCase{item})
		if insertErr != nil || len(created) == 0 {
			response.Failed = append(response.Failed, sessionCaseFailure{SessionID: strings.TrimSpace(sessionID), Error: publicSessionCaseError(insertErr)})
			continue
		}
		response.Created = append(response.Created, caseResponseFrom(created[0]))
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
	return nil
}

func (h *Handler) caseFromSession(ctx context.Context, principal auth.Principal, datasetID string, body createCaseFromSessionRequest) (normalizedCase, error) {
	if _, err := normalizeSessionID(body.SessionID); err != nil {
		return normalizedCase{}, err
	}
	if err := h.usableCustomDataset(ctx, principal, datasetID); err != nil {
		return normalizedCase{}, err
	}
	return h.buildCaseFromSession(ctx, principal, body)
}

func (h *Handler) buildCaseFromSession(ctx context.Context, principal auth.Principal, body createCaseFromSessionRequest) (normalizedCase, error) {
	sessionID, err := normalizeSessionID(body.SessionID)
	if err != nil {
		return normalizedCase{}, err
	}
	session, err := h.sessionForCase(ctx, principal.WorkspaceUUID, sessionID)
	if err != nil {
		return normalizedCase{}, err
	}
	events, err := h.sessionScoreEvents(ctx, principal.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return normalizedCase{}, mapStoreError(err, sessionNotFoundMessage, loadSessionFailedMessage)
	}
	turns := sessionUserTurns(events)
	if len(turns) == 0 {
		return normalizedCase{}, turnsRequired()
	}
	inputFiles, referenceFiles, err := h.sessionCaseFiles(ctx, principal.WorkspaceUUID, session.ExternalID, body.IncludeOutputFiles)
	if err != nil {
		return normalizedCase{}, err
	}
	return normalizeCase(caseInput{
		Name:           sessionCaseName(body.Name, body.namePrefix, sessionTitle(session), turns),
		Desc:           body.Desc,
		Turns:          turns,
		Checkpoints:    body.Checkpoints,
		InputFiles:     inputFiles,
		ReferenceFiles: referenceFiles,
	})
}

func (h *Handler) usableCustomDataset(ctx context.Context, principal auth.Principal, datasetID string) error {
	dataset, err := h.store.GetEvaluationDataset(ctx, principal.OrganizationUUID, principal.WorkspaceUUID, datasetID)
	if err != nil {
		return mapStoreError(err, datasetNotFoundMessage, loadDatasetFailedMessage)
	}
	if dataset.ArchivedAt != nil {
		return datasetArchived()
	}
	return nil
}

func (h *Handler) sessionForCase(ctx context.Context, workspaceUUID, sessionID string) (db.Session, error) {
	session, found, err := h.store.GetSession(ctx, workspaceUUID, sessionID)
	if err != nil {
		return db.Session{}, mapStoreError(err, sessionNotFoundMessage, loadSessionFailedMessage)
	}
	if !found {
		return db.Session{}, sessionNotFound()
	}
	if sessionRequestsScore(session.Metadata) {
		return db.Session{}, scoringSessionRejected()
	}
	return session, nil
}

func (h *Handler) sessionCaseFiles(ctx context.Context, workspaceUUID, sessionID string, includeOutputs bool) (json.RawMessage, json.RawMessage, error) {
	resources, err := h.store.ListSessionResources(ctx, workspaceUUID, sessionID)
	if err != nil {
		return nil, nil, mapStoreError(err, sessionNotFoundMessage, loadSessionFailedMessage)
	}
	inputs, references := sessionCaseFileCandidates(resources, includeOutputs)
	inputs = h.copySessionCaseFiles(ctx, workspaceUUID, sessionID, inputs)
	references = h.copySessionCaseFiles(ctx, workspaceUUID, sessionID, references)
	inputFiles, err := marshalSessionCaseFiles(inputs)
	if err != nil {
		return nil, nil, mapStoreError(err, sessionNotFoundMessage, loadSessionFailedMessage)
	}
	referenceFiles, err := marshalSessionCaseFiles(references)
	if err != nil {
		return nil, nil, mapStoreError(err, sessionNotFoundMessage, loadSessionFailedMessage)
	}
	return inputFiles, referenceFiles, nil
}

func (h *Handler) copySessionCaseFiles(ctx context.Context, workspaceUUID, sessionID string, refs []sessionCaseFileRef) []sessionCaseFileRef {
	if h.files == nil || len(refs) == 0 {
		return refs
	}
	copied := make([]sessionCaseFileRef, 0, len(refs))
	for _, ref := range refs {
		next, err := h.files.CopyCaseFile(ctx, workspaceUUID, ref)
		if err != nil || next.FileID == "" {
			if err == nil {
				err = errCaseFileCopy
			}
			h.logger.WarnContext(ctx, "evaluation case file skipped",
				"session_id", sessionID,
				"file_id", ref.FileID,
				"reason", caseFileSkipReason(err),
				"error", err,
			)
			continue
		}
		if next.Name == "" {
			next.Name = ref.Name
		}
		copied = append(copied, next)
	}
	return copied
}

func normalizeSessionID(value string) (string, error) {
	sessionID := strings.TrimSpace(value)
	if sessionID == "" {
		return "", sessionIDRequired()
	}
	if utf8.RuneCountInString(sessionID) > maxSessionIDLen {
		return "", sessionIDTooLong()
	}
	return sessionID, nil
}

func normalizeSessionIDList(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, sessionIDsRequired()
	}
	if len(values) > maxSessionPromoteBatch {
		return nil, sessionIDsTooMany()
	}
	return values, nil
}

func publicSessionCaseError(err error) string {
	appErr, ok := errors.AsType[*apperr.Error](err)
	if ok && appErr != nil && appErr.PublicMessage != "" {
		return appErr.PublicMessage
	}
	return createCasesFailedMessage
}

func sessionRequestsScore(metadata json.RawMessage) bool {
	if len(metadata) == 0 {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(metadata, &fields) != nil {
		return false
	}
	raw, ok := fields[agentScoreRoleKey]
	if !ok {
		return false
	}
	var role string
	if json.Unmarshal(raw, &role) != nil {
		return false
	}
	return role == agentScoreRole
}

func sessionUserTurns(events []db.SessionEvent) []string {
	turns := make([]string, 0)
	for _, event := range events {
		if event.EventType != "user.message" {
			continue
		}
		text := strings.TrimSpace(messageText(event.Payload))
		if text == "" {
			continue
		}
		turns = append(turns, text)
	}
	return turns
}

func sessionCaseName(requested, prefix, title string, turns []string) string {
	if name := strings.TrimSpace(requested); name != "" {
		return name
	}
	name := strings.TrimSpace(title)
	if name == "" && len(turns) > 0 {
		name = turns[0]
	}
	return truncateRunes(strings.TrimSpace(prefix)+name, maxDatasetNameLen)
}

func sessionTitle(session db.Session) string {
	if session.Title == nil {
		return ""
	}
	return *session.Title
}

func sessionCaseFileCandidates(resources []db.SessionResource, includeOutputs bool) ([]sessionCaseFileRef, []sessionCaseFileRef) {
	inputs := make([]sessionCaseFileRef, 0)
	references := make([]sessionCaseFileRef, 0)
	for _, resource := range resources {
		ref, output, ok := sessionCaseFileCandidate(resource)
		if !ok {
			continue
		}
		if output {
			if includeOutputs {
				references = append(references, ref)
			}
			continue
		}
		inputs = append(inputs, ref)
	}
	return inputs, references
}

func marshalSessionCaseFiles(refs []sessionCaseFileRef) (json.RawMessage, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	return json.Marshal(refs)
}

func sessionCaseFileCandidate(resource db.SessionResource) (sessionCaseFileRef, bool, bool) {
	if resource.DeletedAt != nil || resource.ResourceType != sessioncontract.FileResourceType {
		return sessionCaseFileRef{}, false, false
	}
	var payload struct {
		FileID    string `json:"file_id"`
		MountPath string `json:"mount_path"`
		Name      string `json:"name"`
	}
	if len(resource.Payload) > 0 && json.Unmarshal(resource.Payload, &payload) != nil {
		return sessionCaseFileRef{}, false, false
	}
	mountPath := strings.TrimSpace(payload.MountPath)
	output := sessionCaseOutputPath(resource.Path) || sessionCaseOutputPath(mountPath)
	fileID := strings.TrimSpace(payload.FileID)
	if fileID == "" {
		fileID = strings.TrimSpace(resource.FileExternalID)
	}
	name := firstSessionFileName(strings.TrimSpace(payload.Name), mountPath, strings.TrimSpace(resource.Path), fileID)
	if name == "" {
		return sessionCaseFileRef{}, false, false
	}
	return sessionCaseFileRef{Name: name, FileID: fileID}, output, true
}

func sessionCaseOutputPath(value string) bool {
	path := strings.TrimSpace(value)
	return path == sandboxmount.OutputsRoot || strings.HasPrefix(path, sandboxmount.OutputsRoot+"/")
}

func firstSessionFileName(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

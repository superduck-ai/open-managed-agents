package evaluations

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

const maxSessionIDLen = 255

type createCaseFromSessionRequest struct {
	SessionID   string          `json:"session_id"`
	Name        string          `json:"name"`
	Desc        string          `json:"desc"`
	Checkpoints json.RawMessage `json:"checkpoints"`
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

func (h *Handler) caseFromSession(ctx context.Context, principal auth.Principal, datasetID string, body createCaseFromSessionRequest) (normalizedCase, error) {
	sessionID, err := normalizeSessionID(body.SessionID)
	if err != nil {
		return normalizedCase{}, err
	}
	if err := h.usableCustomDataset(ctx, principal, datasetID); err != nil {
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
	inputFiles, err := h.sessionCaseInputFiles(ctx, principal.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return normalizedCase{}, err
	}
	return normalizeCase(caseInput{
		Name:        sessionCaseName(body.Name, sessionTitle(session), turns),
		Desc:        body.Desc,
		Turns:       turns,
		Checkpoints: body.Checkpoints,
		InputFiles:  inputFiles,
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

func (h *Handler) sessionCaseInputFiles(ctx context.Context, workspaceUUID, sessionID string) (json.RawMessage, error) {
	resources, err := h.store.ListSessionResources(ctx, workspaceUUID, sessionID)
	if err != nil {
		return nil, mapStoreError(err, sessionNotFoundMessage, loadSessionFailedMessage)
	}
	raw, err := sessionCaseInputFileJSON(resources)
	if err != nil {
		return nil, mapStoreError(err, sessionNotFoundMessage, loadSessionFailedMessage)
	}
	return raw, nil
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

func sessionCaseName(requested, title string, turns []string) string {
	if name := strings.TrimSpace(requested); name != "" {
		return name
	}
	if name := strings.TrimSpace(title); name != "" {
		return truncateRunes(name, maxDatasetNameLen)
	}
	if len(turns) == 0 {
		return ""
	}
	return truncateRunes(turns[0], maxDatasetNameLen)
}

func sessionTitle(session db.Session) string {
	if session.Title == nil {
		return ""
	}
	return *session.Title
}

func sessionCaseInputFileJSON(resources []db.SessionResource) (json.RawMessage, error) {
	refs := make([]sessionCaseFileRef, 0)
	for _, resource := range resources {
		ref, ok := sessionCaseFileRefFrom(resource)
		if !ok {
			continue
		}
		refs = append(refs, ref)
	}
	if len(refs) == 0 {
		return nil, nil
	}
	return json.Marshal(refs)
}

func sessionCaseFileRefFrom(resource db.SessionResource) (sessionCaseFileRef, bool) {
	if resource.DeletedAt != nil || resource.ResourceType != sessioncontract.FileResourceType {
		return sessionCaseFileRef{}, false
	}
	if strings.HasPrefix(resource.Path, sandboxmount.OutputsRoot+"/") {
		return sessionCaseFileRef{}, false
	}
	var payload struct {
		FileID    string `json:"file_id"`
		MountPath string `json:"mount_path"`
		Name      string `json:"name"`
	}
	if len(resource.Payload) > 0 && json.Unmarshal(resource.Payload, &payload) != nil {
		return sessionCaseFileRef{}, false
	}
	mountPath := strings.TrimSpace(payload.MountPath)
	if strings.HasPrefix(mountPath, sandboxmount.OutputsRoot+"/") {
		return sessionCaseFileRef{}, false
	}
	fileID := strings.TrimSpace(payload.FileID)
	if fileID == "" {
		fileID = strings.TrimSpace(resource.FileExternalID)
	}
	name := firstSessionFileName(strings.TrimSpace(payload.Name), mountPath, strings.TrimSpace(resource.Path), fileID)
	if name == "" {
		return sessionCaseFileRef{}, false
	}
	return sessionCaseFileRef{Name: name, FileID: fileID}, true
}

func firstSessionFileName(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

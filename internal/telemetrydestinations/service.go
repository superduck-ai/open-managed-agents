package telemetrydestinations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"uuid"
)

const testInterval = 10 * time.Second

type Store interface {
	CreateTelemetryDestination(context.Context, db.TelemetryDestination, int) (db.TelemetryDestination, error)
	UpdateTelemetryDestination(context.Context, db.TelemetryDestination, time.Time) (db.TelemetryDestination, error)
	GetTelemetryDestination(context.Context, string, string) (db.TelemetryDestination, error)
	ListTelemetryDestinations(context.Context, string, []string, int, int) ([]db.TelemetryDestination, error)
	DeleteTelemetryDestination(context.Context, string, string) error
	MarkTelemetryDestinationTested(context.Context, string, string, time.Time, time.Duration) error
	ResolveTelemetryWorkspaces(context.Context, string, []string) (map[string]string, error)
	TelemetryWorkspaceExternalIDs(context.Context, string, []string) (map[string]string, error)
}

type Sealer interface {
	Seal(context.Context, secrets.Binding, []byte) (secrets.Envelope, error)
}

type Tester interface {
	Test(context.Context, db.TelemetryDestination) TestResult
}

type Service struct {
	store         Store
	sealer        Sealer
	tester        Tester
	allowInsecure bool
}

func NewService(store Store, sealer Sealer, tester Tester, allowInsecure bool) *Service {
	return &Service{store: store, sealer: sealer, tester: tester, allowInsecure: allowInsecure}
}

func HeaderBinding(organizationUUID, destinationID string) secrets.Binding {
	return secrets.Binding{OrganizationUUID: organizationUUID, WorkspaceUUID: "organization", VaultExternalID: "telemetry_destination", CredentialExternalID: destinationID}
}

type ListQuery struct {
	Limit        int
	Page         string
	WorkspaceIDs []string
}

func (s *Service) Create(ctx context.Context, organizationUUID string, request destinationRequest) (destinationResponse, error) {
	if request.EndpointURL == nil {
		return destinationResponse{}, invalidRequest("endpoint_url is required")
	}
	id, err := ids.New("tdst_")
	if err != nil {
		return destinationResponse{}, internalError(err)
	}
	destination := db.TelemetryDestination{
		UUID: uuid.NewV4().String(), ExternalID: id, OrganizationUUID: organizationUUID,
		TracesEnabled: true, TracesContentMode: contentModeMetadata, TracesMaxContentBytes: DefaultMaxContentBytes,
		WorkspaceScope: "all", State: "enabled", HeaderNames: []string{},
	}
	if err := s.apply(ctx, &destination, request); err != nil {
		return destinationResponse{}, err
	}
	created, err := s.store.CreateTelemetryDestination(ctx, destination, MaxDestinations)
	if errors.Is(err, db.ErrLimitExceeded) {
		return destinationResponse{}, destinationLimit(MaxDestinations)
	}
	if err != nil {
		return destinationResponse{}, mapStoreError(id, err)
	}
	return s.response(ctx, created)
}

func (s *Service) Get(ctx context.Context, organizationUUID, id string) (destinationResponse, error) {
	destination, err := s.store.GetTelemetryDestination(ctx, organizationUUID, id)
	if err != nil {
		return destinationResponse{}, mapStoreError(id, err)
	}
	return s.response(ctx, destination)
}

func (s *Service) Update(ctx context.Context, organizationUUID, id string, request destinationRequest) (destinationResponse, error) {
	current, err := s.store.GetTelemetryDestination(ctx, organizationUUID, id)
	if err != nil {
		return destinationResponse{}, mapStoreError(id, err)
	}
	next := current
	next.WorkspaceUUIDs = slices.Clone(current.WorkspaceUUIDs)
	if err := s.apply(ctx, &next, request); err != nil {
		return destinationResponse{}, err
	}
	updated, err := s.store.UpdateTelemetryDestination(ctx, next, current.UpdatedAt)
	if err != nil {
		return destinationResponse{}, mapStoreError(id, err)
	}
	return s.response(ctx, updated)
}

func (s *Service) Delete(ctx context.Context, organizationUUID, id string) (deletedResponse, error) {
	if err := s.store.DeleteTelemetryDestination(ctx, organizationUUID, id); err != nil {
		return deletedResponse{}, mapStoreError(id, err)
	}
	return deletedResponse{ID: id, Type: "telemetry_destination_deleted"}, nil
}

func (s *Service) Test(ctx context.Context, organizationUUID, id string) (testResponse, error) {
	destination, err := s.store.GetTelemetryDestination(ctx, organizationUUID, id)
	if err != nil {
		return testResponse{}, mapStoreError(id, err)
	}
	if err := s.store.MarkTelemetryDestinationTested(ctx, organizationUUID, id, time.Now().UTC(), testInterval); err != nil {
		if errors.Is(err, db.ErrLimitExceeded) {
			return testResponse{}, testTooSoon()
		}
		return testResponse{}, mapStoreError(id, err)
	}
	return testResponse{Type: "telemetry_destination_test_result", TestResult: s.tester.Test(ctx, destination)}, nil
}

func (s *Service) List(ctx context.Context, organizationUUID string, query ListQuery) (listResponse, error) {
	offset, err := decodePage(query.Page)
	if err != nil {
		return listResponse{}, err
	}
	var workspaceUUIDs []string
	if len(query.WorkspaceIDs) > 0 {
		resolved, err := s.store.ResolveTelemetryWorkspaces(ctx, organizationUUID, query.WorkspaceIDs)
		if err != nil {
			return listResponse{}, internalError(err)
		}
		for _, id := range query.WorkspaceIDs {
			if value, found := resolved[id]; found {
				workspaceUUIDs = append(workspaceUUIDs, value)
			}
		}
		if len(workspaceUUIDs) == 0 {
			return listResponse{Data: []destinationResponse{}}, nil
		}
	}
	destinations, err := s.store.ListTelemetryDestinations(ctx, organizationUUID, workspaceUUIDs, query.Limit+1, offset)
	if err != nil {
		return listResponse{}, internalError(err)
	}
	result := listResponse{Data: make([]destinationResponse, 0, len(destinations))}
	if len(destinations) > query.Limit {
		destinations = destinations[:query.Limit]
		next := encodePage(offset + query.Limit)
		result.NextPage = &next
	}
	for _, destination := range destinations {
		response, err := s.response(ctx, destination)
		if err != nil {
			return listResponse{}, err
		}
		result.Data = append(result.Data, response)
	}
	return result, nil
}

func (s *Service) apply(ctx context.Context, destination *db.TelemetryDestination, request destinationRequest) error {
	if err := applyDisplayName(destination, request.DisplayName); err != nil {
		return err
	}
	if err := s.applyEndpoints(destination, request); err != nil {
		return err
	}
	if err := applyTraces(destination, request.Traces); err != nil {
		return err
	}
	if err := s.applyHeaders(ctx, destination, request.Headers); err != nil {
		return err
	}
	if err := s.applyWorkspaces(ctx, destination, request.Workspaces); err != nil {
		return err
	}
	return applyState(destination, request.State)
}

func applyDisplayName(destination *db.TelemetryDestination, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var name *string
	if err := json.Unmarshal(raw, &name); err != nil {
		return invalidRequest("display_name must be a string or null")
	}
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		name = &trimmed
		if trimmed == "" {
			name = nil
		}
	}
	if err := validateDisplayName(name); err != nil {
		return err
	}
	destination.DisplayName = name
	return nil
}

func (s *Service) applyEndpoints(destination *db.TelemetryDestination, request destinationRequest) error {
	previousDefault := defaultTracesURL(destination.EndpointURL)
	if request.EndpointURL != nil {
		endpoint := strings.TrimSpace(*request.EndpointURL)
		if _, err := validateEndpointURL("endpoint_url", endpoint, s.allowInsecure); err != nil {
			return err
		}
		destination.EndpointURL = endpoint
		if destination.TracesEndpointURL == "" || destination.TracesEndpointURL == previousDefault {
			destination.TracesEndpointURL = defaultTracesURL(endpoint)
		}
	}
	if request.Traces != nil && len(request.Traces.EndpointURL) > 0 {
		var tracesURL *string
		if err := json.Unmarshal(request.Traces.EndpointURL, &tracesURL); err != nil {
			return invalidRequest("traces.endpoint_url must be a string or null")
		}
		destination.TracesEndpointURL = defaultTracesURL(destination.EndpointURL)
		if tracesURL != nil {
			destination.TracesEndpointURL = strings.TrimSpace(*tracesURL)
		}
	}
	base, err := validateEndpointURL("endpoint_url", destination.EndpointURL, s.allowInsecure)
	if err != nil {
		return err
	}
	return validateTracesURL(destination.TracesEndpointURL, base, s.allowInsecure)
}

func applyTraces(destination *db.TelemetryDestination, request *tracesRequest) error {
	if request == nil {
		return nil
	}
	if request.Enabled != nil {
		destination.TracesEnabled = *request.Enabled
	}
	if request.ContentMode != nil {
		if *request.ContentMode != contentModeMetadata && *request.ContentMode != contentModeContent {
			return invalidRequest("traces.content_mode must be metadata_only or content")
		}
		destination.TracesContentMode = *request.ContentMode
	}
	if request.MaxContentBytes != nil {
		if *request.MaxContentBytes < 1 || *request.MaxContentBytes > DefaultMaxContentBytes {
			return invalidRequestf("traces.max_content_bytes must be between 1 and %d", DefaultMaxContentBytes)
		}
		destination.TracesMaxContentBytes = *request.MaxContentBytes
	}
	return nil
}

func (s *Service) applyHeaders(ctx context.Context, destination *db.TelemetryDestination, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var headers map[string]string
	if err := json.Unmarshal(raw, &headers); err != nil {
		return invalidRequest("headers must be an object of string values or null")
	}
	if len(headers) == 0 {
		destination.HeaderNames, destination.Headers = []string{}, nil
		return nil
	}
	names, err := validateHeaders(headers)
	if err != nil {
		return err
	}
	plaintext, err := json.Marshal(headers)
	if err != nil {
		return internalError(err)
	}
	envelope, err := s.sealer.Seal(ctx, HeaderBinding(destination.OrganizationUUID, destination.ExternalID), plaintext)
	if err != nil {
		return internalError(err)
	}
	destination.HeaderNames, destination.Headers = names, &envelope
	return nil
}

func (s *Service) applyWorkspaces(ctx context.Context, destination *db.TelemetryDestination, request *workspacesRequest) error {
	if request == nil {
		return nil
	}
	request.Type = strings.TrimSpace(request.Type)
	if err := validateWorkspaces(*request); err != nil {
		return err
	}
	ids := make([]string, 0, len(request.WorkspaceIDs))
	for _, id := range request.WorkspaceIDs {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	resolved, err := s.store.ResolveTelemetryWorkspaces(ctx, destination.OrganizationUUID, ids)
	if err != nil {
		return internalError(err)
	}
	workspaceUUIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		value, found := resolved[id]
		if !found {
			return invalidRequestf("workspace %s not found", id)
		}
		workspaceUUIDs = append(workspaceUUIDs, value)
	}
	destination.WorkspaceScope, destination.WorkspaceUUIDs = request.Type, workspaceUUIDs
	return nil
}

func applyState(destination *db.TelemetryDestination, request *stateRequest) error {
	if request == nil {
		return nil
	}
	switch request.Type {
	case "enabled":
		destination.State, destination.DisabledReason = "enabled", nil
	case "disabled":
		if destination.State != "disabled" {
			reason := "caller"
			destination.State, destination.DisabledReason = "disabled", &reason
		}
	default:
		return invalidRequest("state.type must be enabled or disabled")
	}
	return nil
}

func (s *Service) response(ctx context.Context, destination db.TelemetryDestination) (destinationResponse, error) {
	externalIDs, err := s.store.TelemetryWorkspaceExternalIDs(ctx, destination.OrganizationUUID, destination.WorkspaceUUIDs)
	if err != nil {
		return destinationResponse{}, internalError(err)
	}
	workspaces := workspacesObject{Type: destination.WorkspaceScope, WorkspaceIDs: []string{}}
	for _, workspaceUUID := range destination.WorkspaceUUIDs {
		if id, found := externalIDs[workspaceUUID]; found {
			workspaces.WorkspaceIDs = append(workspaces.WorkspaceIDs, id)
		}
	}
	return destinationResponse{
		ID: destination.ExternalID, Type: "telemetry_destination", DisplayName: destination.DisplayName,
		EndpointURL: destination.EndpointURL, HeaderNames: destination.HeaderNames,
		State: stateObject{Type: destination.State, Reason: destination.DisabledReason},
		Traces: tracesObject{
			Enabled: destination.TracesEnabled, ContentMode: destination.TracesContentMode,
			EndpointURL: destination.TracesEndpointURL, MaxContentBytes: destination.TracesMaxContentBytes,
		},
		Workspaces:     workspaces,
		DeliveryHealth: deliveryHealth{LastError: destination.LastError, LastErrorAt: destination.LastErrorAt, LastSuccessAt: destination.LastSuccessAt},
		CreatedAt:      destination.CreatedAt, UpdatedAt: destination.UpdatedAt,
	}, nil
}

func encodePage(offset int) string {
	payload, _ := json.Marshal(struct {
		Offset int `json:"offset"`
	}{offset})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodePage(page string) (int, error) {
	if page == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(page)
	var cursor struct {
		Offset int `json:"offset"`
	}
	if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Offset < 0 || cursor.Offset > 100000 {
		return 0, invalidRequest("page is invalid")
	}
	return cursor.Offset, nil
}

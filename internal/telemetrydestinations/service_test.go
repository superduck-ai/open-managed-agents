package telemetrydestinations

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type memoryStore struct {
	destinations map[string]db.TelemetryDestination
	workspaces   map[string]string
	createErr    error
	updateErr    error
	testErr      error
	lastExpected time.Time
}

func newMemoryStore() *memoryStore {
	return &memoryStore{destinations: map[string]db.TelemetryDestination{}, workspaces: map[string]string{"wrkspc_a": "ws-a", "wrkspc_b": "ws-b"}}
}

func (s *memoryStore) CreateTelemetryDestination(_ context.Context, destination db.TelemetryDestination, limit int) (db.TelemetryDestination, error) {
	if s.createErr != nil {
		return db.TelemetryDestination{}, s.createErr
	}
	if len(s.destinations) >= limit {
		return db.TelemetryDestination{}, db.ErrLimitExceeded
	}
	destination.CreatedAt, destination.UpdatedAt = time.Unix(100, 0), time.Unix(100, 0)
	s.destinations[destination.ExternalID] = destination
	return destination, nil
}

func (s *memoryStore) UpdateTelemetryDestination(_ context.Context, destination db.TelemetryDestination, expected time.Time) (db.TelemetryDestination, error) {
	s.lastExpected = expected
	if s.updateErr != nil {
		return db.TelemetryDestination{}, s.updateErr
	}
	destination.UpdatedAt = expected.Add(time.Second)
	s.destinations[destination.ExternalID] = destination
	return destination, nil
}

func (s *memoryStore) GetTelemetryDestination(_ context.Context, _ string, id string) (db.TelemetryDestination, error) {
	destination, found := s.destinations[id]
	if !found {
		return db.TelemetryDestination{}, db.ErrNotFound
	}
	return destination, nil
}

func (s *memoryStore) ListTelemetryDestinations(_ context.Context, _ string, workspaceUUIDs []string, limit, offset int) ([]db.TelemetryDestination, error) {
	var result []db.TelemetryDestination
	for _, destination := range s.destinations {
		result = append(result, destination)
	}
	slices.SortFunc(result, func(left, right db.TelemetryDestination) int {
		return strings.Compare(left.ExternalID, right.ExternalID)
	})
	result = result[min(offset, len(result)):]
	return result[:min(limit, len(result))], nil
}

func (s *memoryStore) DeleteTelemetryDestination(_ context.Context, _ string, id string) error {
	if _, found := s.destinations[id]; !found {
		return db.ErrNotFound
	}
	delete(s.destinations, id)
	return nil
}

func (s *memoryStore) MarkTelemetryDestinationTested(context.Context, string, string, time.Time, time.Duration) error {
	return s.testErr
}

func (s *memoryStore) ResolveTelemetryWorkspaces(_ context.Context, _ string, ids []string) (map[string]string, error) {
	result := map[string]string{}
	for _, id := range ids {
		if value, found := s.workspaces[id]; found {
			result[id] = value
		}
	}
	return result, nil
}

func (s *memoryStore) TelemetryWorkspaceExternalIDs(_ context.Context, _ string, uuids []string) (map[string]string, error) {
	result := map[string]string{}
	for id, value := range s.workspaces {
		if slices.Contains(uuids, value) {
			result[value] = id
		}
	}
	return result, nil
}

type recordingSealer struct{ plaintext []byte }

func (s *recordingSealer) Seal(_ context.Context, binding secrets.Binding, plaintext []byte) (secrets.Envelope, error) {
	if binding.VaultExternalID != "telemetry_destination" || !strings.HasPrefix(binding.CredentialExternalID, "tdst_") {
		return secrets.Envelope{}, errors.New("unexpected binding")
	}
	s.plaintext = plaintext
	return secrets.Envelope{Ciphertext: []byte("sealed"), FormatVersion: 1, KeyProvider: "local", KeyVersion: 1}, nil
}

type fixedTester struct{ called int }

func (t *fixedTester) Test(context.Context, db.TelemetryDestination) TestResult {
	t.called++
	status := 200
	return TestResult{Success: true, StatusCode: &status}
}

func request(t *testing.T, raw string) destinationRequest {
	t.Helper()
	var value destinationRequest
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func requireKind(t *testing.T, err error, kind apperr.Kind, message string) {
	t.Helper()
	var appError *apperr.Error
	if !errors.As(err, &appError) || appError.Kind != kind || !strings.Contains(appError.PublicMessage, message) {
		t.Fatalf("error = %v, want %v containing %q", err, kind, message)
	}
}

func TestCreateRejectsInvalidDestinations(t *testing.T) {
	service := NewService(newMemoryStore(), &recordingSealer{}, &fixedTester{}, false)
	for _, test := range []struct{ body, message string }{
		{`{}`, "endpoint_url is required"},
		{`{"endpoint_url":"http://collector.example.com"}`, "must use https"},
		{`{"endpoint_url":"https://user:pass@collector.example.com"}`, "credentials"},
		{`{"endpoint_url":"https://collector.example.com?token=x"}`, "query"},
		{`{"endpoint_url":"https://collector.example.com:4318"}`, "port 443"},
		{`{"endpoint_url":"https://10.0.0.1"}`, "public hostname"},
		{`{"endpoint_url":"https://localhost"}`, "public hostname"},
		{`{"endpoint_url":"https://` + strings.Repeat("a", 2050) + `.com"}`, "at most 2048"},
		{`{"endpoint_url":"https://collector.example.com","traces":{"endpoint_url":"https://other.example.com/v1/traces"}}`, "same scheme and host"},
		{`{"endpoint_url":"https://collector.example.com","traces":{"content_mode":"everything"}}`, "content_mode"},
		{`{"endpoint_url":"https://collector.example.com","traces":{"max_content_bytes":786433}}`, "max_content_bytes"},
		{`{"endpoint_url":"https://collector.example.com","headers":{"Content-Type":"x"}}`, "not allowed"},
		{`{"endpoint_url":"https://collector.example.com","headers":{"bad name":"x"}}`, "not allowed"},
		{`{"endpoint_url":"https://collector.example.com","headers":{"X-Token":"a\nb"}}`, "invalid value"},
		{`{"endpoint_url":"https://collector.example.com","headers":{"X-Token":"` + strings.Repeat("v", 4097) + `"}}`, "invalid value"},
		{`{"endpoint_url":"https://collector.example.com","headers":{"x-a":"1","X-A":"2"}}`, "duplicated"},
		{`{"endpoint_url":"https://collector.example.com","workspaces":{"type":"some"}}`, "workspaces.type"},
		{`{"endpoint_url":"https://collector.example.com","workspaces":{"type":"include"}}`, "must not be empty"},
		{`{"endpoint_url":"https://collector.example.com","workspaces":{"type":"all","workspace_ids":["wrkspc_a"]}}`, "must be empty"},
		{`{"endpoint_url":"https://collector.example.com","workspaces":{"type":"include","workspace_ids":["wrkspc_missing"]}}`, "wrkspc_missing not found"},
		{`{"endpoint_url":"https://collector.example.com","state":{"type":"paused"}}`, "state.type"},
		{`{"endpoint_url":"https://collector.example.com","display_name":7}`, "display_name"},
	} {
		t.Run(test.message, func(t *testing.T) {
			_, err := service.Create(context.Background(), "org", request(t, test.body))
			requireKind(t, err, apperr.InvalidArgument, test.message)
		})
	}
	for _, body := range []string{
		`{"endpoint_url":"https://collector.example.com:443","traces":{"endpoint_url":"https://Collector.example.com/v1/traces"}}`,
		`{"endpoint_url":"https://collector.example.com","traces":{"endpoint_url":"https://collector.example.com:443/v1/traces"}}`,
	} {
		if _, err := NewService(newMemoryStore(), &recordingSealer{}, &fixedTester{}, false).Create(context.Background(), "org", request(t, body)); err != nil {
			t.Fatalf("explicit default port rejected %s: %v", body, err)
		}
	}
	insecure := NewService(newMemoryStore(), &recordingSealer{}, &fixedTester{}, true)
	if _, err := insecure.Create(context.Background(), "org", request(t, `{"endpoint_url":"https://collector.example.com","traces":{"endpoint_url":"https://collector.example.com:4318/v1/traces"}}`)); err == nil {
		t.Fatal("traces endpoint on another port was accepted")
	}
	headers := map[string]string{}
	for index := range 21 {
		headers["X-H"+strings.Repeat("a", index)] = "v"
	}
	encoded, _ := json.Marshal(map[string]any{"endpoint_url": "https://collector.example.com", "headers": headers})
	_, err := service.Create(context.Background(), "org", request(t, string(encoded)))
	requireKind(t, err, apperr.InvalidArgument, "at most 20")
}

func TestCreateEncryptsHeadersAndAppliesDefaults(t *testing.T) {
	store, sealer := newMemoryStore(), &recordingSealer{}
	service := NewService(store, sealer, &fixedTester{}, false)
	created, err := service.Create(context.Background(), "org", request(t, `{"display_name":"  Honeycomb ","endpoint_url":"https://api.example.com/otlp/","headers":{"x-honeycomb-team":"secret","Authorization":"Bearer x"},"workspaces":{"type":"include","workspace_ids":["wrkspc_b","wrkspc_a","wrkspc_b"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.ID, "tdst_") || *created.DisplayName != "Honeycomb" || created.State.Type != "enabled" || created.Traces.EndpointURL != "https://api.example.com/otlp/v1/traces" {
		t.Fatalf("created = %+v", created)
	}
	if created.Traces.ContentMode != "metadata_only" || created.Traces.MaxContentBytes != DefaultMaxContentBytes || !created.Traces.Enabled {
		t.Fatalf("trace defaults = %+v", created.Traces)
	}
	if strings.Join(created.HeaderNames, ",") != "Authorization,x-honeycomb-team" || strings.Join(created.Workspaces.WorkspaceIDs, ",") != "wrkspc_b,wrkspc_a" {
		t.Fatalf("headers/workspaces = %+v %+v", created.HeaderNames, created.Workspaces)
	}
	encoded, _ := json.Marshal(created)
	if strings.Contains(string(encoded), "secret") || !strings.Contains(string(sealer.plaintext), "secret") {
		t.Fatal("header values were exposed or not sealed")
	}
	for range MaxDestinations - 1 {
		if _, err := service.Create(context.Background(), "org", request(t, `{"endpoint_url":"https://api.example.com"}`)); err != nil {
			t.Fatal(err)
		}
	}
	_, err = service.Create(context.Background(), "org", request(t, `{"endpoint_url":"https://api.example.com"}`))
	requireKind(t, err, apperr.InvalidArgument, "at most 10")
}

func TestUpdateTracksDefaultsStateAndConcurrency(t *testing.T) {
	store := newMemoryStore()
	service := NewService(store, &recordingSealer{}, &fixedTester{}, false)
	created, err := service.Create(context.Background(), "org", request(t, `{"endpoint_url":"https://a.example.com","headers":{"X-Key":"1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.Update(context.Background(), "org", created.ID, request(t, `{"endpoint_url":"https://b.example.com","state":{"type":"disabled"},"headers":null,"traces":{"content_mode":"content","max_content_bytes":1000}}`))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Traces.EndpointURL != "https://b.example.com/v1/traces" || updated.State.Type != "disabled" || *updated.State.Reason != "caller" || len(updated.HeaderNames) != 0 || updated.Traces.ContentMode != "content" {
		t.Fatalf("updated = %+v", updated)
	}
	if !store.lastExpected.Equal(time.Unix(100, 0)) {
		t.Fatalf("expected version = %v", store.lastExpected)
	}
	custom, err := service.Update(context.Background(), "org", created.ID, request(t, `{"traces":{"endpoint_url":"https://b.example.com/custom"},"state":{"type":"enabled"}}`))
	if err != nil || custom.Traces.EndpointURL != "https://b.example.com/custom" || custom.State.Reason != nil {
		t.Fatalf("custom = %+v %v", custom, err)
	}
	kept, err := service.Update(context.Background(), "org", created.ID, request(t, `{"endpoint_url":"https://b.example.com/base"}`))
	if err != nil || kept.Traces.EndpointURL != "https://b.example.com/custom" {
		t.Fatalf("custom traces URL was overwritten: %+v %v", kept, err)
	}
	store.updateErr = db.ErrVersionConflict
	_, err = service.Update(context.Background(), "org", created.ID, request(t, `{"display_name":"x"}`))
	requireKind(t, err, apperr.Conflict, "concurrently")
	_, err = service.Update(context.Background(), "org", "tdst_missing", request(t, `{}`))
	requireKind(t, err, apperr.NotFound, "tdst_missing")
}

func TestListDeleteAndTest(t *testing.T) {
	store, tester := newMemoryStore(), &fixedTester{}
	service := NewService(store, &recordingSealer{}, tester, false)
	for range 3 {
		if _, err := service.Create(context.Background(), "org", request(t, `{"endpoint_url":"https://a.example.com"}`)); err != nil {
			t.Fatal(err)
		}
	}
	first, err := service.List(context.Background(), "org", ListQuery{Limit: 2})
	if err != nil || len(first.Data) != 2 || first.NextPage == nil {
		t.Fatalf("first page = %+v %v", first, err)
	}
	second, err := service.List(context.Background(), "org", ListQuery{Limit: 2, Page: *first.NextPage})
	if err != nil || len(second.Data) != 1 || second.NextPage != nil {
		t.Fatalf("second page = %+v %v", second, err)
	}
	if _, err := service.List(context.Background(), "org", ListQuery{Limit: 2, Page: "!!"}); err == nil {
		t.Fatal("accepted invalid page")
	}
	if none, err := service.List(context.Background(), "org", ListQuery{Limit: 2, WorkspaceIDs: []string{"wrkspc_unknown"}}); err != nil || len(none.Data) != 0 {
		t.Fatalf("unknown workspace filter = %+v %v", none, err)
	}
	id := first.Data[0].ID
	store.testErr = db.ErrLimitExceeded
	_, err = service.Test(context.Background(), "org", id)
	requireKind(t, err, apperr.RateLimited, "10 seconds")
	store.testErr = nil
	result, err := service.Test(context.Background(), "org", id)
	if err != nil || !result.Success || tester.called != 1 || result.Type != "telemetry_destination_test_result" {
		t.Fatalf("test = %+v %v", result, err)
	}
	deleted, err := service.Delete(context.Background(), "org", id)
	if err != nil || deleted.Type != "telemetry_destination_deleted" {
		t.Fatalf("delete = %+v %v", deleted, err)
	}
	_, err = service.Delete(context.Background(), "org", id)
	requireKind(t, err, apperr.NotFound, id)
}

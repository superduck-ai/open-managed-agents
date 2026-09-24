package tests

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestWebhookSubscriptionPatchIsolation(t *testing.T) {
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	name := "changed"
	patch := db.WebhookEndpointUpdate{Name: &name, UpdatedAt: time.Now().UTC()}
	if _, err := f.app.db.UpdateWebhookEndpoint(t.Context(), "00000000-0000-4000-8000-000000000042", f.endpoint.ExternalID, patch); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("cross workspace = %v", err)
	}
	// Simulate the worker changing state after an editor has loaded the subscription.
	f.exec(t, `UPDATE webhook_endpoints SET status='disabled', disabled_reason='delivery failure', consecutive_failures=20 WHERE uuid=$1`, f.endpoint.UUID)
	updated, err := f.app.db.UpdateWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID, patch)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "disabled" || updated.ConsecutiveFailures != 20 || updated.DisabledReason == nil || *updated.DisabledReason != "delivery failure" || updated.URL != f.endpoint.URL {
		t.Fatalf("metadata patch lost worker state: %+v", updated)
	}
	enabled := "enabled"
	updated, err = f.app.db.UpdateWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID, db.WebhookEndpointUpdate{Status: &enabled, UpdatedAt: time.Now().UTC()})
	if err != nil || updated.Status != "enabled" || updated.ConsecutiveFailures != 0 || updated.DisabledReason != nil || updated.Name != name {
		t.Fatalf("enable: %+v, %v", updated, err)
	}
	disabled := "disabled"
	updated, err = f.app.db.UpdateWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID, db.WebhookEndpointUpdate{Status: &disabled, UpdatedAt: time.Now().UTC()})
	if err != nil || updated.DisabledReason == nil || *updated.DisabledReason != "manual" {
		t.Fatalf("disable: %+v, %v", updated, err)
	}
	empty := ""
	updated, err = f.app.db.UpdateWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID, db.WebhookEndpointUpdate{Name: &empty, Description: &empty, EnabledEvents: []string{}, UpdatedAt: time.Now().UTC()})
	if err != nil || updated.Name != "" || len(updated.EnabledEvents) != 0 {
		t.Fatalf("clear: %+v, %v", updated, err)
	}
}

func TestWebhookSubscriptionPatchConcurrentWorker(t *testing.T) {
	f := newDeliveryFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	f.enqueue(t, 1)
	job := f.lease(t)
	name, description := "new name", "new description"
	start := make(chan struct{})
	errs := make(chan error, 3)
	var wg sync.WaitGroup
	for _, patch := range []db.WebhookEndpointUpdate{{Name: &name}, {Description: &description}} {
		wg.Go(func() {
			<-start
			patch.UpdatedAt = time.Now().UTC()
			_, err := f.app.db.UpdateWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID, patch)
			errs <- err
		})
	}
	wg.Go(func() {
		<-start
		applied, err := f.app.db.FailWebhookDeliveryJob(t.Context(), job, db.WebhookDeliveryFailure{Reason: "delivery failure", RetryDelay: time.Minute, MaxAttempts: 10, Terminal: true, DisableAfter: 24 * time.Hour})
		if err == nil && !applied {
			err = errors.New("worker result not applied")
		}
		errs <- err
	})
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	updated, err := f.app.db.GetWebhookEndpoint(t.Context(), f.endpoint.WorkspaceUUID, f.endpoint.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != name || updated.Description != description || updated.Status != "disabled" || updated.ConsecutiveFailures != 1 || updated.DisabledReason == nil || *updated.DisabledReason != "delivery failure" {
		t.Fatalf("concurrent result: %+v", updated)
	}
}

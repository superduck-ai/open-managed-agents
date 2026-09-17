package tests

import (
	"context"
	"os"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

// Opt in to test against the configured real S3-compatible service as well as PostgreSQL.
func TestEventPayloadIntegrationRealS3(t *testing.T) {
	if os.Getenv("TEST_EVENT_PAYLOAD_S3") != "1" {
		t.Skip("set TEST_EVENT_PAYLOAD_S3=1 to test configured S3 storage")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	client, err := storage.New(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := client.ForBucket(cfg.Storage.S3.Bucket)
	if err != nil {
		t.Fatal(err)
	}
	if err := objects.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	app := newPayloadIntegrationApp(t, objects)
	// Clean only UUID keys registered by this isolated fixture, even when assertions fail.
	t.Cleanup(func() {
		rows, err := app.pool.Query(context.Background(), `select object_key from event_payload_blobs`)
		if err != nil {
			t.Error(err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				t.Error(err)
				continue
			}
			if err := objects.Delete(context.Background(), key, storage.DeleteOptions{AllVersions: true}); err != nil {
				t.Errorf("clean S3 fixture: %v", err)
			}
		}
		if err := rows.Err(); err != nil {
			t.Error(err)
		}
	})
	session, epoch := newPayloadIntegrationSession(t, app)
	payload := sizedPrivatePayload("real-s3", 40000)
	postCodeSessionWorkerInternalEvents(t, app, session.ExternalID, internalPayloadRequest(epoch, payload))
	postCodeSessionWorkerInternalEvents(t, app, session.ExternalID, internalPayloadRequest(epoch, payload))
	page := getCodeSessionWorkerInternalEvents(t, app, session.ExternalID, "internal-events")
	if len(page.Data) != 1 {
		t.Fatalf("real S3 private event count %d", len(page.Data))
	}
	assertRawJSONEqual(t, page.Data[0].Payload, payload)
	postCodeSessionIngressEvents(t, app, session.ExternalID, `{"events":[`+payload+`]}`)
	public := listSessionEvents(t, app, session.SessionExternalID, "", config.DefaultAPIKey)
	assertPublicPayloadText(t, public.Data, payload)
	assertPayloadSQLCount(t, app, `select count(*) from event_payload_blobs where state='attached'`, 2)
}

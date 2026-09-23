package tunnels

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/superduck-ai/open-managed-agents/internal/cleanup"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type trackedPayloadCleanup struct {
	database *db.DB
	mu       sync.Mutex
	ids      []string
	keys     []string
}

func (q *trackedPayloadCleanup) EnqueueScheduledObjectCleanupResourceJob(ctx context.Context, id, workspace, bucket, key, kind, resource string, at time.Time) error {
	if err := q.database.EnqueueScheduledObjectCleanupResourceJob(ctx, id, workspace, bucket, key, kind, resource, at); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ids = append(q.ids, id)
	q.keys = append(q.keys, key)
	return nil
}

type largeToolInput struct {
	Text string `json:"text"`
}

// This uses an isolated, explicitly configured database and versioned S3 bucket.
// Connector token lookup remains the existing fixture; cleanup uses real PostgreSQL.
func TestTunnelPayloadRealStorageAndClient(t *testing.T) {
	url, endpoint, binary := os.Getenv("TEST_TUNNEL_PAYLOAD_DATABASE_URL"), os.Getenv("TEST_TUNNEL_PAYLOAD_S3_ENDPOINT"), os.Getenv("TEST_TUNNEL_CLIENT_BINARY")
	if url == "" || endpoint == "" || binary == "" {
		t.Skip("set TEST_TUNNEL_PAYLOAD_DATABASE_URL, TEST_TUNNEL_PAYLOAD_S3_ENDPOINT and TEST_TUNNEL_CLIENT_BINARY")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	database, err := db.Open(t.Context(), config.Config{Database: config.DatabaseConfig{URL: url}}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	cfg := config.StorageConfig{Type: config.StorageTypeS3, S3: config.S3Config{Endpoint: endpoint, Bucket: "tunnel-payload-test", Region: "us-east-1", AccessKeyID: "payload-test", SecretAccessKey: "payload-test-secret", ForcePathStyle: true}}
	client, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := client.ForBucket(cfg.S3.Bucket)
	if err != nil {
		t.Fatal(err)
	}
	if err := objects.Ensure(t.Context()); err != nil {
		t.Fatal(err)
	}
	s3Client := s3.NewFromConfig(aws.Config{Region: cfg.S3.Region, Credentials: credentials.NewStaticCredentialsProvider(cfg.S3.AccessKeyID, cfg.S3.SecretAccessKey, "")}, func(o *s3.Options) { o.BaseEndpoint = aws.String(endpoint); o.UsePathStyle = true })
	if _, err := s3Client.PutBucketVersioning(t.Context(), &s3.PutBucketVersioningInput{Bucket: aws.String(cfg.S3.Bucket), VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled}}); err != nil {
		t.Fatal(err)
	}
	jobs := &trackedPayloadCleanup{database: database}
	store := NewPayloadStore(database, objects)
	store.cleanup = jobs
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, id := range jobs.ids {
			if err := database.ExpediteObjectCleanupJob(ctx, id); err != nil {
				t.Error(err)
			}
		}
		worker := cleanup.NewWorker(database, client, time.Second, logger)
		for i := 0; i <= len(jobs.ids)/10; i++ {
			if err := worker.RunOnce(ctx, "tunnel-payload-test"); err != nil {
				t.Error(err)
			}
		}
		for _, key := range jobs.keys {
			versions, err := s3Client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(cfg.S3.Bucket), Prefix: aws.String(key)})
			if err != nil {
				t.Error(err)
				continue
			}
			if len(versions.Versions) != 0 || len(versions.DeleteMarkers) != 0 {
				t.Error("cleanup retained object versions")
			}
		}
	})
	for _, stream := range []bool{false, true} {
		name := "json"
		if stream {
			name = "sse"
		}
		t.Run(name, func(t *testing.T) { runLargeOfficialClient(t, binary, store, stream) })
	}
	if len(jobs.ids) < 5 {
		t.Fatalf("expected request/response/notification offloads, got %d", len(jobs.ids))
	}
	// Prove scheduled jobs cannot be leased before their deadlines.
	pending, err := database.LeaseObjectCleanupJobs(t.Context(), "premature-cleanup", 100)
	if err != nil || len(pending) != 0 {
		t.Fatalf("cleanup ran early: %d, %v", len(pending), err)
	}
}

func runLargeOfficialClient(t *testing.T, binary string, store *PayloadStore, stream bool) {
	t.Helper()
	cfg := brokerTestConfig()
	cfg.MaxBodyBytes = 16 << 20
	cfg.RequestTimeout = time.Minute
	b := testNATSBroker(t, cfg)
	b.payloads = store
	endpoint, controlURL, tunnelID := tunnelHTTPFixture(t, b)
	text := strings.Repeat("x", 3<<20)
	private := mcp.NewServer(&mcp.Implementation{Name: "large-payload", Version: "1"}, nil)
	mcp.AddTool(private, &mcp.Tool{Name: "echo_large", Description: "Echo complete input."}, func(ctx context.Context, req *mcp.CallToolRequest, input largeToolInput) (*mcp.CallToolResult, any, error) {
		if input.Text != text {
			return nil, nil, errPayloadDigestMismatch
		}
		if stream {
			if err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{ProgressToken: "large", Progress: 1, Message: text}); err != nil {
				return nil, nil, err
			}
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: input.Text}}}, nil, nil
	})
	privateHTTP := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return private }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: !stream}))
	defer privateHTTP.Close()
	stop := startOfficialConnector(t, binary, []string{"run", "--profile-dir", t.TempDir(), "--control-plane.base-url", controlURL, "--control-plane.url-path", "/connector", "--control-plane.tunnel-id", tunnelID, "--control-plane.api-key", "env:OMA_TEST_CONNECTOR_TOKEN", "--control-plane.poll-timeout", "1s", "--health.listen-addr", "127.0.0.1:0", "--log.level", "warn", "--mcp.server-url", "url=" + privateHTTP.URL}, "")
	defer stop()
	notifications := make(chan int, 2)
	client := mcp.NewClient(&mcp.Implementation{Name: "large-test", Version: "1"}, &mcp.ClientOptions{ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
		notifications <- len(req.Params.Message)
	}})
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "echo_large", Arguments: largeToolInput{Text: text}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 {
		t.Fatal("unexpected result")
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok || content.Text != text {
		t.Fatal("large response corrupted")
	}
	if stream {
		select {
		case size := <-notifications:
			if size != len(text) {
				t.Fatal("large notification corrupted")
			}
		case <-ctx.Done():
			t.Fatal("notification lost")
		}
	}
	// HTTP boundary rejects >16 MiB without enqueueing or uploading.
	oversized, _ := json.Marshal(largeToolInput{Text: strings.Repeat("x", 16<<20)})
	response, err := http.Post(endpoint, "application/json", strings.NewReader(string(oversized)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize status=%d", response.StatusCode)
	}
}

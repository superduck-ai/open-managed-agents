package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(value []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(value)
}

func (b *lockedBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}

type panicStore struct {
	runtimeStore
	endpoint string
	panicked atomic.Bool
}

func (s *panicStore) FindWebhookDeliveryTarget(ctx context.Context, workspace, endpoint string) (db.WebhookDeliveryTarget, bool, error) {
	if endpoint == s.endpoint && s.panicked.CompareAndSwap(false, true) {
		panic("private-credential-must-not-be-logged")
	}
	return s.runtimeStore.FindWebhookDeliveryTarget(ctx, workspace, endpoint)
}

func TestWebhookConsumePanicRecoveryAndRedelivery(t *testing.T) {
	conn := queueConnection(t, startQueueServer(t, t.TempDir()))
	cfg := config.WebhookConfig{WorkerEnabled: true, Concurrency: 1, AllowInsecure: true, Timeout: time.Second}
	q := createTestQueue(t, conn, config.WebhookStreamConfig{MaxBytes: 256 << 20, MaxAge: time.Hour, Replicas: 1}, cfg)
	consumerConfig, err := q.consumer.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	consumerConfig.Config.AckWait = 3 * time.Second
	q.consumer, err = q.js.UpdateConsumer(t.Context(), StreamName, consumerConfig.Config)
	if err != nil {
		t.Fatal(err)
	}
	first := testEnvelope(t)
	store := &panicStore{endpoint: first.EndpointUUID}
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer receiver.Close()
	store.target = db.WebhookDeliveryTarget{Status: "enabled", URL: receiver.URL, SigningSecret: "whsec_MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}
	var output lockedBuffer
	worker := &Worker{database: store, queue: q, cfg: cfg, logger: slog.New(slog.NewJSONHandler(&output, nil))}
	stop, err := worker.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	if err := q.Publish(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	waitRuntime(t, func() bool { return store.panicked.Load() })
	if err := q.Publish(t.Context(), testEnvelope(t)); err != nil {
		t.Fatal(err)
	}
	waitRuntime(t, func() bool {
		info, err := queueStream(t, q).Info(t.Context())
		return err == nil && info.State.Msgs == 1 && store.successes.Load() == 1
	})
	if store.failures.Load() != 0 {
		t.Fatal("internal panic counted as endpoint failure")
	}
	waitRuntime(t, func() bool {
		info, err := queueStream(t, q).Info(t.Context())
		return err == nil && info.State.Msgs == 0 && store.successes.Load() == 2
	})
	stop()

	worker.logger.Info("附加运行日志")
	logs := output.String()
	decoder := json.NewDecoder(strings.NewReader(logs))
	found := false
	for {
		var record struct {
			Level   string `json:"level"`
			Message string `json:"msg"`
			Queue   string `json:"queue"`
			Slot    int    `json:"consume_slot"`
			Stack   string `json:"stack"`
		}
		if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if record.Message != "queued message processing panicked" {
			continue
		}
		if found {
			t.Fatal("不应重复记录同一次消费异常")
		}
		found = true
		if record.Level != "ERROR" || record.Queue != "webhook" || record.Slot != 0 || record.Stack == "" {
			t.Fatalf("异常记录字段错误：level=%s queue=%s slot=%d stack_bytes=%d", record.Level, record.Queue, record.Slot, len(record.Stack))
		}
	}
	if !found {
		t.Fatal("缺少消费异常的结构化日志")
	}

	if strings.Contains(logs, "private-credential-must-not-be-logged") {
		t.Fatal("panic log exposed its value")
	}
}

func TestWebhookConsumeConfiguredConcurrency(t *testing.T) {
	for _, concurrency := range []int{0, 1, 4, 12} {
		t.Run(strconv.Itoa(concurrency), func(t *testing.T) {
			conn := queueConnection(t, startQueueServer(t, t.TempDir()))
			cfg := config.WebhookConfig{WorkerEnabled: true, Concurrency: concurrency, Timeout: time.Second}
			q := createTestQueue(t, conn, config.WebhookStreamConfig{MaxBytes: 256 << 20, MaxAge: time.Hour, Replicas: 1}, cfg)
			limit := concurrency
			if limit == 0 {
				limit = 10
			}
			store := &runtimeStore{entered: make(chan time.Duration, limit+1), block: true}
			for range limit + 1 {
				if err := q.Publish(t.Context(), testEnvelope(t)); err != nil {
					t.Fatal(err)
				}
			}
			tracked := &trackingConsumer{Consumer: q.consumer}
			q.consumer = tracked
			worker := runtimeTestWorker(t, q, store, cfg)
			runtime := newWorkerRuntime(t.Context(), worker)
			if runtime.Slots() != limit || runtime.transport.MaxIdleConns != limit || runtime.transport.MaxIdleConnsPerHost != limit || runtime.transport.MaxConnsPerHost != limit {
				t.Fatal("HTTP limits do not match concurrency")
			}
			runtime.Stop()
			stop, err := worker.Start(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(stop)
			for range limit {
				select {
				case <-store.entered:
				case <-time.After(5 * time.Second):
					t.Fatal("configured processing positions did not start")
				}
			}
			select {
			case <-store.entered:
				t.Fatal("worker exceeded configured concurrency")
			case <-time.After(100 * time.Millisecond):
			}
			if tracked.calls.Load() != int32(limit) {
				t.Fatal("Consume session count did not follow config")
			}
			stop()
			if store.successes.Load()+store.failures.Load() != 0 {
				t.Fatal("canceled processing wrote statistics")
			}
			assertQueueMessages(t, q, uint64(limit+1))
		})
	}
}

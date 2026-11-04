package codesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type deliveryTestStore struct {
	items       []db.DSHDeliveryLease
	leaseErr    error
	completeErr error
	retryErr    error
	completed   []string
	retried     []string
	leased      bool
}

func (s *deliveryTestStore) LeaseDSHDeliveries(context.Context) ([]db.DSHDeliveryLease, error) {
	s.leased = true
	return s.items, s.leaseErr
}
func (s *deliveryTestStore) CompleteDSHDelivery(_ context.Context, item db.DSHDeliveryLease) error {
	s.completed = append(s.completed, item.UUID)
	return s.completeErr
}
func (s *deliveryTestStore) RetryDSHDelivery(_ context.Context, item db.DSHDeliveryLease) error {
	s.retried = append(s.retried, item.UUID)
	return s.retryErr
}

type deliveryTestBroker func(context.Context, string) error

func (b deliveryTestBroker) DoubleAck(ctx context.Context, subject string) error {
	return b(ctx, subject)
}

func TestDSHDeliveryWorkerFailuresRemainRecoverable(t *testing.T) {
	failed := errors.New("unavailable")
	for _, kind := range []string{"lease", "ack", "complete", "retry", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			store := &deliveryTestStore{items: []db.DSHDeliveryLease{{UUID: "delivery", InputID: "input", AckSubject: "private-subject"}}}
			if kind == "lease" {
				store.leaseErr = failed
			}
			if kind == "complete" {
				store.completeErr = failed
			}
			if kind == "retry" {
				store.retryErr = failed
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if kind == "cancelled" {
				cancel()
			}
			calls := 0
			broker := deliveryTestBroker(func(ctx context.Context, subject string) error {
				calls++
				if !store.leased {
					t.Fatal("ACK preceded durable lease")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("unbounded broker request")
				}
				if kind == "ack" || kind == "retry" {
					return errors.New(subject)
				}
				return nil
			})
			var logs bytes.Buffer
			worker := NewDSHDeliveryWorker(store, broker, slog.New(slog.NewJSONHandler(&logs, nil)))
			err := worker.RunOnce(ctx)
			if kind == "ack" {
				if err != nil || len(store.retried) != 1 || len(store.completed) != 0 {
					t.Fatalf("retry=%v completed=%v err=%v", store.retried, store.completed, err)
				}
			} else if err == nil {
				t.Fatal("missing durable state failure")
			}
			if (kind == "lease" || kind == "cancelled") && calls != 0 {
				t.Fatal("ACK despite unavailable lease or shutdown")
			}
			if kind == "complete" && len(store.retried) != 0 {
				t.Fatal("lost lease must recover after expiration")
			}
			if kind == "ack" || kind == "retry" {
				var record struct {
					Level        string `json:"level"`
					Message      string `json:"msg"`
					DeliveryUUID string `json:"delivery_uuid"`
					InputID      string `json:"input_id"`
				}
				if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
					t.Fatal(err)
				}
				if record.Level != "WARN" || record.Message != "retry DSR input acknowledgement" || record.DeliveryUUID != "delivery" || record.InputID != "input" {
					t.Fatalf("record=%+v", record)
				}
			}
			if strings.Contains(logs.String(), "private-subject") {
				t.Fatal("ACK subject leaked")
			}
		})
	}
}

func TestDSHDeliveryWorkerContinuesAfterOneBrokerFailure(t *testing.T) {
	store := &deliveryTestStore{items: []db.DSHDeliveryLease{{UUID: "first", AckSubject: "fail"}, {UUID: "second", AckSubject: "ok"}}}
	worker := NewDSHDeliveryWorker(store, deliveryTestBroker(func(_ context.Context, subject string) error {
		if subject == "fail" {
			return errors.New("offline")
		}
		return nil
	}), slog.New(slog.DiscardHandler))
	if err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(store.retried) != 1 || store.retried[0] != "first" || len(store.completed) != 1 || store.completed[0] != "second" {
		t.Fatalf("retry=%v complete=%v", store.retried, store.completed)
	}
}

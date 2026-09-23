package eventpayload

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type preparationContextKey struct{}

type preparedPayloadKey struct {
	workspaceUUID string
	identity      string
	eventType     string
	digest        [32]byte
}

type preparedPayload struct {
	payload  []byte
	blobUUID *string
}

type transactionPreparation struct {
	time     time.Time
	payloads map[preparedPayloadKey]preparedPayload
}

// WithEventTx releases the transaction before registering and uploading a new
// blob, then repeats the DB work with the prepared object. Each attempt must
// recheck its locks and fences, reset its results, and have no external effects.
// The durable pending blob survives a failed attempt for the existing GC path.
func (s *Store) WithEventTx(ctx context.Context, fn func(context.Context, db.ManagedAgentEventTx) error) error {
	preparation := &transactionPreparation{time: time.Now().UTC(), payloads: make(map[preparedPayloadKey]preparedPayload)}
	txCtx := context.WithValue(ctx, preparationContextKey{}, preparation)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := s.database.WithManagedAgentEventTx(txCtx, func(tx db.ManagedAgentEventTx) error {
			return fn(txCtx, tx)
		})
		required, ok := errors.AsType[*preparationRequired](err)
		if !ok {
			return err
		}
		payload, blobUUID, err := required.store.prepare(ctx, required.organizationUUID, required.workspaceUUID, required.identity, required.payload, required.summary)
		if err != nil {
			return err
		}
		key := preparedPayloadKey{workspaceUUID: required.workspaceUUID, identity: required.identity, eventType: required.summary.Type, digest: sha256.Sum256(required.payload)}
		preparation.payloads[key] = preparedPayload{payload: payload, blobUUID: blobUUID}
	}
}

// EventTime keeps generated payload timestamps stable while preparing objects.
func EventTime(ctx context.Context) time.Time {
	if preparation, ok := ctx.Value(preparationContextKey{}).(*transactionPreparation); ok {
		return preparation.time
	}
	return time.Now().UTC()
}

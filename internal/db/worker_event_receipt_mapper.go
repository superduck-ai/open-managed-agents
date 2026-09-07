package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper WorkerEventReceiptMapper -sql ./worker_event_receipt_mapper.xml -out ./worker_event_receipt_mapper.sqlmap.gen.go -dialect postgres

// WorkerEventReceipt stores only the first accepted request association and a
// digest. Preview content is never persisted in this receipt table.
type WorkerEventReceipt struct {
	EventID    string `db:"event_external_id"`
	Scope      string `db:"scope"`
	RequestID  string `db:"model_request_id"`
	Hash       string `db:"payload_hash"`
	BlockIndex *int   `db:"block_index"`
}

type WorkerRequestBlockIndex struct {
	RequestID string `db:"model_request_id"`
	Index     int    `db:"block_index"`
}

type WorkerEventReceiptMapper interface {
	Find(ctx context.Context, workspaceUUID, codeSessionUUID string, epoch int64, eventID string) (WorkerEventReceipt, bool, error)
	Insert(ctx context.Context, workspaceUUID, codeSessionUUID string, epoch int64, receipt WorkerEventReceipt) (int64, error)
	DeleteBySession(ctx context.Context, workspaceUUID, sessionUUID string) (int64, error)
	LatestBlockIndices(ctx context.Context, workspaceUUID, codeSessionUUID string, epoch int64, requestIDs []string) ([]WorkerRequestBlockIndex, error)
}

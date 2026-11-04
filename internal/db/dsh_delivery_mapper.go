package db

import (
	"context"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper DSHDeliveryMapper -sql ./dsh_delivery_mapper.xml -out ./dsh_delivery_mapper.sqlmap.gen.go -dialect postgres

type dshDeliveryRow struct {
	UUID             string  `db:"uuid"`
	OrganizationUUID string  `db:"organization_uuid"`
	WorkspaceUUID    string  `db:"workspace_uuid"`
	CodeSessionUUID  string  `db:"code_session_uuid"`
	InputID          string  `db:"input_id"`
	AckSubject       string  `db:"ack_subject"`
	CleanupJobUUID   *string `db:"cleanup_job_uuid"`
	Status           string  `db:"status"`
	Attempts         int     `db:"attempts"`
	LeaseToken       *string `db:"lease_token"`
}

type DSHDeliveryMapper interface {
	PruneDiscarded(ctx context.Context) (int64, error)
	DiscardRetired(ctx context.Context) (int64, error)
	Record(ctx context.Context, scope DSHScope, inputID, ackSubject, cleanupJobID string) (int64, error)
	Find(ctx context.Context, scope DSHScope, inputID string) (dshDeliveryRow, bool, error)
	Ready(ctx context.Context, scope DSHScope, inputID string) (int64, error)
	Lease(ctx context.Context) ([]dshDeliveryRow, error)
	DeleteAcknowledged(ctx context.Context, item DSHDeliveryLease) (int64, error)
	Retry(ctx context.Context, item DSHDeliveryLease) (int64, error)
	ExpediteCleanup(ctx context.Context, item DSHDeliveryLease) (int64, error)
}

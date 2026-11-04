package db

import (
	"context"

	"github.com/superduck-ai/yourbatis"
)

type DSHDeliveryLease struct {
	UUID             string
	OrganizationUUID string
	WorkspaceUUID    string
	CodeSessionUUID  string
	InputID          string
	AckSubject       string
	CleanupJobUUID   *string
	LeaseToken       string
}

func (d *DB) RecordDSHDelivery(ctx context.Context, scope DSHScope, inputID, ackSubject, cleanupJobID string) (bool, error) {
	var completed bool
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if err := lockDSHWorker(ctx, executor, scope); err != nil {
			return err
		}
		execution, found, err := NewDSHExecutionMapper(executor).Execution(ctx, scope, inputID)
		if err != nil {
			return err
		}
		if !found {
			return ErrDSHInputNotEligible
		}
		completed = execution.Status == "completed"
		_, err = NewDSHDeliveryMapper(executor).Record(ctx, scope, inputID, ackSubject, cleanupJobID)
		return err
	})
	return completed, err
}

func (d *DB) DSHDeliveryAckSubject(ctx context.Context, scope DSHScope, inputID string) (string, error) {
	row, found, err := NewDSHDeliveryMapper(d.mapperDB).Find(ctx, scope, inputID)
	if err != nil || !found {
		return "", err
	}
	return row.AckSubject, nil
}

func (d *DB) LeaseDSHDeliveries(ctx context.Context) ([]DSHDeliveryLease, error) {
	mapper := NewDSHDeliveryMapper(d.mapperDB)
	if _, err := mapper.DiscardRetired(ctx); err != nil {
		return nil, err
	}
	if _, err := mapper.PruneDiscarded(ctx); err != nil {
		return nil, err
	}
	rows, err := mapper.Lease(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]DSHDeliveryLease, 0, len(rows))
	for _, row := range rows {
		result = append(result, DSHDeliveryLease{UUID: row.UUID, OrganizationUUID: row.OrganizationUUID, WorkspaceUUID: row.WorkspaceUUID,
			CodeSessionUUID: row.CodeSessionUUID, InputID: row.InputID, AckSubject: row.AckSubject, CleanupJobUUID: row.CleanupJobUUID, LeaseToken: *row.LeaseToken})
	}
	return result, nil
}

func (d *DB) CompleteDSHDelivery(ctx context.Context, item DSHDeliveryLease) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewDSHDeliveryMapper(executor)
		rows, err := mapper.DeleteAcknowledged(ctx, item)
		if err != nil || rows == 0 || item.CleanupJobUUID == nil {
			return err
		}
		_, err = mapper.ExpediteCleanup(ctx, item)
		return err
	})
}

func (d *DB) RetryDSHDelivery(ctx context.Context, item DSHDeliveryLease) error {
	_, err := NewDSHDeliveryMapper(d.mapperDB).Retry(ctx, item)
	return err
}

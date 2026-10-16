package db

import (
	"context"
	"database/sql"
	"errors"
)

func (d *DB) LeasePlatformAdminDelivery(ctx context.Context) (PlatformAdminDelivery, bool, error) {
	mapper := NewPlatformAdminDeliveryMapper(d.mapperDB)
	if err := mapper.CancelInvalid(ctx); err != nil {
		return PlatformAdminDelivery{}, false, err
	}
	row, err := mapper.Lease(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	return row, err == nil, err
}

func (d *DB) PlatformAdminDeliveryReady(ctx context.Context, deliveryUUID string, attempts int) (bool, error) {
	return NewPlatformAdminDeliveryMapper(d.mapperDB).Ready(ctx, deliveryUUID, attempts)
}

func (d *DB) FinishPlatformAdminDelivery(ctx context.Context, deliveryUUID string, attempts int, status string) error {
	return NewPlatformAdminDeliveryMapper(d.mapperDB).Finish(ctx, deliveryUUID, attempts, status)
}

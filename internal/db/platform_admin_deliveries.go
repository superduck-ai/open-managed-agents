package db

import (
	"context"
	"database/sql"
	"errors"
)

func (d *DB) StartPlatformAdminDelivery(ctx context.Context, deliveryUUID string) (PlatformAdminDelivery, bool, error) {
	row, err := NewPlatformAdminDeliveryMapper(d.mapperDB).Start(ctx, deliveryUUID)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	return row, err == nil, err
}
func (d *DB) FinishPlatformAdminDelivery(ctx context.Context, deliveryUUID, status string) error {
	return NewPlatformAdminDeliveryMapper(d.mapperDB).Finish(ctx, deliveryUUID, status)
}

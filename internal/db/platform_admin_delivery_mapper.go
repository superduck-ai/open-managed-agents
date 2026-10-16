package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper PlatformAdminDeliveryMapper -sql ./platform_admin_delivery_mapper.xml -out ./platform_admin_delivery_mapper.sqlmap.gen.go -dialect postgres

type PlatformAdminDelivery struct {
	UUID             string    `db:"uuid"`
	OrganizationUUID string    `db:"organization_uuid"`
	Recipient        string    `db:"recipient"`
	ExpiresAt        time.Time `db:"expires_at"`
}

type PlatformAdminDeliveryMapper interface {
	Start(ctx context.Context, deliveryUUID string) (PlatformAdminDelivery, error)
	Finish(ctx context.Context, deliveryUUID, status string) error
}

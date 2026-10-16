package db

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestPlatformAdminDeliveryMapperContracts(t *testing.T) {
	for _, tc := range []struct {
		name      string
		call      func(PlatformAdminDeliveryMapper) error
		response  mapperTestResponse
		args      []any
		kind      yourbatis.StatementKind
		fragments []string
	}{
		{"CancelInvalid", func(m PlatformAdminDeliveryMapper) error { return m.CancelInvalid(t.Context()) }, mapperTestResponse{}, []any{}, yourbatis.StatementUpdate, []string{"status = 'cancelled'", "AND NOT EXISTS", "i.organization_uuid = d.organization_uuid", "i.expires_at = d.expires_at"}},
		{"Lease", func(m PlatformAdminDeliveryMapper) error { _, err := m.Lease(t.Context()); return err }, mapperTestResponse{columns: []string{"uuid", "organization_uuid", "recipient", "expires_at", "attempts"}, rows: [][]driver.Value{{"delivery", "org", "recipient@example.com", time.Now(), int64(2)}}}, []any{}, yourbatis.StatementUpdate, []string{"FOR UPDATE SKIP LOCKED", "d.attempts < 5", "interval '1 minute'", "interval '2 minutes'", "attempts = attempts + 1", "i.deleted_at IS NULL"}},
		{"Ready", func(m PlatformAdminDeliveryMapper) error { _, err := m.Ready(t.Context(), "delivery", 2); return err }, mapperTestResponse{columns: []string{"ready"}, rows: [][]driver.Value{{true}}}, []any{"delivery", 2}, yourbatis.StatementSelect, []string{"d.uuid = $1", "d.attempts = $2", "d.status = 'sending'", "i.expires_at > clock_timestamp()"}},
		{"Finish", func(m PlatformAdminDeliveryMapper) error { return m.Finish(t.Context(), "delivery", 2, "sent") }, mapperTestResponse{}, []any{"sent", "delivery", 2}, yourbatis.StatementUpdate, []string{"status = $1", "uuid = $2 AND attempts = $3 AND status = 'sending'"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := errors.New("执行失败")
			executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: failure, execErr: failure})
			if err := tc.call(NewPlatformAdminDeliveryMapper(executor)); !errors.Is(err, failure) {
				t.Fatalf("未保留执行错误：%v", err)
			}
			executor = newMapperTestExecutor(t, tc.response)
			if err := tc.call(NewPlatformAdminDeliveryMapper(executor)); err != nil {
				t.Fatal(err)
			}
			assertMapperTestExecution(t, executor, "PlatformAdminDeliveryMapper."+tc.name, tc.kind, tc.args, tc.fragments...)
		})
	}
}

func TestPlatformAdminDeliveryLeaseNoRows(t *testing.T) {
	executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid", "organization_uuid", "recipient", "expires_at", "attempts"}})
	if _, err := NewPlatformAdminDeliveryMapper(executor).Lease(t.Context()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("空队列应返回 sql.ErrNoRows：%v", err)
	}
}

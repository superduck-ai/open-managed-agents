package db

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"github.com/superduck-ai/yourbatis"
	"testing"
	"time"
)

func TestPlatformAdminDeliveryMapperContracts(t *testing.T) {
	for _, tc := range []struct {
		name      string
		call      func(PlatformAdminDeliveryMapper) error
		response  mapperTestResponse
		args      []any
		fragments []string
	}{
		{"Start", func(m PlatformAdminDeliveryMapper) error { _, err := m.Start(t.Context(), "delivery"); return err }, mapperTestResponse{columns: []string{"uuid", "organization_uuid", "recipient", "expires_at"}, rows: [][]driver.Value{{"delivery", "org", "recipient@example.com", time.Now()}}}, []any{"delivery"}, []string{"d.uuid = $1", "i.organization_uuid = d.organization_uuid", "i.expires_at = d.expires_at", "i.deleted_at IS NULL", "i.expires_at > clock_timestamp()"}},
		{"Finish", func(m PlatformAdminDeliveryMapper) error { return m.Finish(t.Context(), "delivery", "sent") }, mapperTestResponse{}, []any{"sent", "delivery"}, []string{"status = $1", "uuid = $2 AND status = 'sending'"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := errors.New("执行失败")
			executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: failure, execErr: failure})
			if err := tc.call(NewPlatformAdminDeliveryMapper(executor)); !errors.Is(err, failure) {
				t.Fatal(err)
			}
			executor = newMapperTestExecutor(t, tc.response)
			if err := tc.call(NewPlatformAdminDeliveryMapper(executor)); err != nil {
				t.Fatal(err)
			}
			assertMapperTestExecution(t, executor, "PlatformAdminDeliveryMapper."+tc.name, yourbatis.StatementUpdate, tc.args, tc.fragments...)
		})
	}
	executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid", "organization_uuid", "recipient", "expires_at"}})
	if _, err := NewPlatformAdminDeliveryMapper(executor).Start(t.Context(), "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("不存在的任务：%v", err)
	}
}

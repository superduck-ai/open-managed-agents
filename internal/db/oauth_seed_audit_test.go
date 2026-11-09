package db

import (
	"context"
	"log/slog"
	"testing"
)

type oauthSeedHandler struct {
	slog.Handler
	record *slog.Record
}

func (h oauthSeedHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h oauthSeedHandler) Handle(_ context.Context, record slog.Record) error {
	*h.record = record.Clone()
	return nil
}

func TestOAuthSeedAudit(t *testing.T) {
	var record slog.Record
	auditBuiltinOAuthSeed(t.Context(), slog.New(oauthSeedHandler{record: &record}), PlatformOAuthProvider{UUID: "provider-uuid", ExternalID: "poap_FeishuBuiltinOAuthClient"})
	if record.Message != "platform oauth provider audit" || record.Level != slog.LevelInfo || record.Time.IsZero() {
		t.Fatal("seed 审计事件错误")
	}
	attrs := make(map[string]slog.Value)
	record.Attrs(func(attr slog.Attr) bool { attrs[attr.Key] = attr.Value; return true })
	for key, want := range map[string]string{"actor_type": "system", "provider_uuid": "provider-uuid", "provider_key": "builtin:feishu", "action": "seed", "result": "success"} {
		if attrs[key].String() != want {
			t.Fatalf("审计字段错误：%s", key)
		}
	}
	if _, ok := attrs["actor_account_uuid"]; ok {
		t.Fatal("seed 伪造人类账号")
	}
}

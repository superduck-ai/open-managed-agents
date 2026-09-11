package platformauth

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

func TestVerifiedEmailWithPendingInviteCreatesHomePostgres(t *testing.T) {
	store := isolatedLoginDatabase(t)
	ctx := t.Context()
	var invitedOrg string
	err := store.WithPlatformAuthTx(ctx, func(tx db.PlatformAuthTxStore) error {
		org, err := tx.InsertOrganization(ctx, db.PlatformAuthOrganizationInput{Name: "邀请来源"})
		invitedOrg = org.UUID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	invite, err := store.CreateAdminInvite(ctx, db.AdminInvite{
		ExternalID: "invite_pending_login", OrganizationUUID: invitedOrg,
		Email: "first@example.com", Role: "user", Status: "pending",
		InvitedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	service, _, sender := newEmailLoginTestService(store)
	if err := service.RequestEmailLogin(ctx, invite.Email); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.VerifyEmailLogin(ctx, invite.Email, "invalid"); err == nil {
		t.Fatal("无效验证码不应创建身份")
	}
	userID, home, err := service.VerifyEmailLogin(ctx, invite.Email, sender.code)
	if err != nil || home == invitedOrg || home == "" {
		t.Fatalf("首次验证应自建组织: %q, %v", home, err)
	}
	session, err := store.ResolvePlatformSessionIdentity(ctx, platformsession.CreateInput{
		SessionKey: "verified-session", UserUUID: userID, OrgUUID: home,
	})
	if err != nil || session.WorkspaceUUID == "" || session.HomeOrganizationUUID != home || session.VerifiedEmail != invite.Email {
		t.Fatalf("首次登录会话: %+v, %v", session, err)
	}
	current, err := store.GetAdminInvite(ctx, invitedOrg, invite.ExternalID)
	if err != nil || current.Status != "pending" {
		t.Fatalf("登录不应消费邀请: %+v, %v", current, err)
	}
	orgs, err := store.ListBootstrapOrganizationsByEmail(ctx, invite.Email)
	if err != nil || len(orgs) != 1 || orgs[0].UUID != home {
		t.Fatalf("首次成员仅自建组织: %+v, %v", orgs, err)
	}
	if _, err := store.DeleteAdminUser(ctx, home, userID); err != nil {
		t.Fatal(err)
	}
	againID, againOrg, err := service.VerifyEmailLogin(ctx, invite.Email, sender.code)
	if err != nil || againID != userID || againOrg != home {
		t.Fatalf("移除后不得重新注册组织: %q, %q, %v", againID, againOrg, err)
	}
}

func isolatedLoginDatabase(t *testing.T) *db.DB {
	t.Helper()
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("需要隔离 PostgreSQL 测试地址")
	}
	ctx := t.Context()
	admin, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := "login_test_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = admin.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close(context.Background())
	})
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	store, err := db.Open(ctx, config.Config{Database: config.DatabaseConfig{URL: parsed.String()}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return store
}

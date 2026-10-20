//go:build integration

package db

import (
	"encoding/json/v2"
	"os"
	"testing"
	"time"
	"uuid"
)

func TestInvitationDeliveryMigration(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("需要隔离 PostgreSQL，跳过不能视为验收通过")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, address)
	if _, err := provider.UpTo(ctx, 91); err != nil {
		t.Fatal(err)
	}
	invitation, org, old, latest := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
	invited := time.Now().UTC().Truncate(time.Microsecond)
	expires := invited.Add(24 * time.Hour)
	if _, err := database.ExecContext(ctx, `INSERT INTO organization_invites (uuid, external_id, organization_uuid, email, role, status, invited_at, expires_at)
 VALUES ($1, 'invite_migration', $2, 'migration@example.com', 'user', 'pending', $3, $4)`, invitation, org, invited, expires); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO platform_admin_invite_deliveries (uuid, organization_uuid, invitation_uuid, recipient, expires_at, status, created_at)
 VALUES ($1,$2,$3,'migration@example.com',$4,'sent',now()-interval '1 minute'), ($5,$2,$3,'migration@example.com',$4,'failed',now())`, old, org, invitation, expires, latest); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE river_job (kind text NOT NULL, args jsonb NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO river_job (kind, args) VALUES ('platform_invitation_delivery', jsonb_build_object('delivery_uuid', $1::text)), ('unrelated', jsonb_build_object('delivery_uuid', $1::text))`, latest); err != nil {
		t.Fatal(err)
	}
	verify := func() {
		t.Helper()
		var status string
		var removed bool
		if err := database.QueryRowContext(ctx, `SELECT email_delivery_status, to_regclass(current_schema() || '.platform_admin_invite_deliveries') IS NULL FROM organization_invites WHERE uuid=$1`, invitation).Scan(&status, &removed); err != nil || status != "failed" || !removed {
			t.Fatalf("最新状态或删表：%s %v %v", status, removed, err)
		}
		var payload []byte
		if err := database.QueryRowContext(ctx, `SELECT args FROM river_job WHERE kind='platform_invitation_delivery'`).Scan(&payload); err != nil {
			t.Fatal(err)
		}
		var snapshot struct {
			InvitationUUID   string    `json:"invitation_uuid"`
			OrganizationUUID string    `json:"organization_uuid"`
			Recipient        string    `json:"recipient"`
			InvitedAt        time.Time `json:"invited_at"`
			ExpiresAt        time.Time `json:"expires_at"`
		}
		if err := json.Unmarshal(payload, &snapshot, json.RejectUnknownMembers(true)); err != nil || snapshot.InvitationUUID != invitation || snapshot.OrganizationUUID != org || snapshot.Recipient != "migration@example.com" || !snapshot.InvitedAt.Equal(invited) || !snapshot.ExpiresAt.Equal(expires) {
			t.Fatalf("任务快照未转换：%s %v", payload, err)
		}
		var untouched bool
		if err := database.QueryRowContext(ctx, `SELECT args ? 'delivery_uuid' FROM river_job WHERE kind='unrelated'`).Scan(&untouched); err != nil || !untouched {
			t.Fatalf("改写了其他任务：%v %v", untouched, err)
		}
	}
	if _, err := provider.UpTo(ctx, 92); err != nil {
		t.Fatal(err)
	}
	verify()
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 92); err != nil {
		t.Fatal(err)
	}
	verify()
}

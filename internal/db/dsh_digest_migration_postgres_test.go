//go:build integration

package db

import (
	"os"
	"strings"
	"testing"
)

func TestDSHDigestMigrationPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.UpTo(ctx, 122); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO dsh_public_receipts(organization_uuid,workspace_uuid,code_session_uuid,event_id,payload_digest) VALUES('53000000-0000-0000-0000-000000000001','53000000-0000-0000-0000-000000000002','53000000-0000-0000-0000-000000000003',$1,$2)`
	oldDigest := "jcs-sha256-v1:" + strings.Repeat("a", 64)
	newDigest := "exact-json-sha256-v1:" + strings.Repeat("b", 64)
	execClaimTest(t, ctx, database, insert, "old", oldDigest)
	if _, err := database.ExecContext(ctx, insert, "new", newDigest); err == nil {
		t.Fatal("old schema accepted new digest")
	}
	if _, err := migrations.UpTo(ctx, 123); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"unknown:" + strings.Repeat("a", 64), "exact-json-sha256-v1:short", "exact-json-sha256-v1:" + strings.Repeat("A", 64)} {
		if _, err := database.ExecContext(ctx, insert, "invalid", invalid); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
	execClaimTest(t, ctx, database, insert, "new", newDigest)
	if _, err := migrations.Down(ctx); err == nil {
		t.Fatal("downgraded with new digest rows")
	}
	var stored string
	if err := database.QueryRowContext(ctx, `SELECT payload_digest FROM dsh_public_receipts WHERE event_id='old'`).Scan(&stored); err != nil || stored != oldDigest {
		t.Fatalf("old=%s err=%v", stored, err)
	}
	if err := database.QueryRowContext(ctx, `SELECT payload_digest FROM dsh_public_receipts WHERE event_id='new'`).Scan(&stored); err != nil || stored != newDigest {
		t.Fatalf("new=%s err=%v", stored, err)
	}
	execClaimTest(t, ctx, database, `DELETE FROM dsh_public_receipts WHERE event_id='new'`)
	if _, err := migrations.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, insert, "new", newDigest); err == nil {
		t.Fatal("downgrade did not restore old constraint")
	}
	if _, err := migrations.UpTo(ctx, 123); err != nil {
		t.Fatal(err)
	}
	execClaimTest(t, ctx, database, insert, "new", newDigest)
}

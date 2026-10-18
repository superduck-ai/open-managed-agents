//go:build integration

package db

import (
	"os"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/yourbatis"
)

func TestSessionInputFileInstanceMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		databaseURL = cfg.Database.URL
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 82); err != nil {
		t.Fatal(err)
	}
	seedEnvironmentWorkMigrationSessions(t, ctx, database)
	_, err := database.ExecContext(ctx, `
 INSERT INTO files (uuid,external_id,workspace_uuid,filename,mime_type,size_bytes,sha256,s3_bucket,s3_key,created_by_api_key_uuid)
 VALUES ('52000000-0000-0000-0000-000000000099','file_migration_source','52000000-0000-0000-0000-000000000002',
 'source.txt','text/plain',7,repeat('a',64),'input-migration','source-key','52000000-0000-0000-0000-000000000004');
 INSERT INTO session_resources (external_id,organization_uuid,workspace_uuid,session_uuid,session_external_id,resource_type,payload,path,parent_path,file_uuid,deleted_at)
 SELECT 'sesrsc_input_migration_' || n, '52000000-0000-0000-0000-000000000001','52000000-0000-0000-0000-000000000002',
 '52000000-0000-0000-0000-000000000011','sesn_migration_work_active','file',
 jsonb_build_object('file_id','file_migration_source','mount_path','/input-' || n),'/uploads/input-' || n,'/uploads',
 '52000000-0000-0000-0000-000000000099', CASE WHEN n=3 THEN now() ELSE NULL END
 FROM generate_series(1,3) n;
 UPDATE session_resources SET workspace_uuid = '52000000-0000-0000-0000-000000000009'
 WHERE external_id='sesrsc_input_migration_1';`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 83); err == nil {
		t.Fatal("cross-workspace source migrated")
	}
	assertMigrationColumnExists(t, ctx, database, "session_resources", "source_file_uuid", false)
	if _, err := database.ExecContext(ctx, `UPDATE session_resources SET workspace_uuid='52000000-0000-0000-0000-000000000002' WHERE external_id='sesrsc_input_migration_1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 83); err != nil {
		t.Fatal(err)
	}
	var count int
	err = database.QueryRowContext(ctx, `SELECT count(*) FROM session_resources resource JOIN files instance ON instance.uuid=resource.file_uuid AND instance.workspace_uuid=resource.workspace_uuid
 JOIN files source ON source.uuid=resource.source_file_uuid AND source.workspace_uuid=resource.workspace_uuid
 WHERE resource.external_id LIKE 'sesrsc_input_migration_%' AND resource.deleted_at IS NULL
 AND instance.external_id <> source.external_id AND instance.scope_type='session' AND instance.scope_id=resource.session_external_id
 AND resource.payload->>'file_id'=instance.external_id AND instance.s3_key=source.s3_key AND instance.size_bytes=source.size_bytes
 AND instance.downloadable=source.downloadable`).Scan(&count)
	if err != nil || count != 2 {
		t.Fatalf("backfilled inputs = %d, error %v", count, err)
	}
	err = database.QueryRowContext(ctx, `SELECT count(DISTINCT file_uuid) FROM session_resources WHERE source_file_uuid IS NOT NULL`).Scan(&count)
	if err != nil || count != 2 {
		t.Fatalf("distinct instance count = %d, error %v", count, err)
	}
	err = database.QueryRowContext(ctx, `SELECT count(*) FROM files WHERE s3_key='source-key'`).Scan(&count)
	if err != nil || count != 3 {
		t.Fatalf("physical metadata row count = %d, error %v", count, err)
	}
	mapperDB := yourbatis.NewDB(database, yourbatis.DialectPostgres, yourbatis.WithDatabaseID("postgres"))
	if _, err := NewFileMapper(mapperDB).GetFile(ctx, "52000000-0000-0000-0000-000000000002", "file_migration_source"); err != nil {
		t.Fatalf("retired legacy reference hides source metadata: %v", err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	err = database.QueryRowContext(ctx, `SELECT count(*) FROM files WHERE s3_key='source-key'`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("rollback retained instance rows: %d, %v", count, err)
	}
	if _, err := provider.UpTo(ctx, 83); err != nil {
		t.Fatal(err)
	}
}

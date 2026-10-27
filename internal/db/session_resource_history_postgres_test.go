//go:build integration

package db

import (
	"os"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionResourceHistoricalOwnershipMigration(t *testing.T) {
	for _, scenario := range []string{"attachment", "deleted attachment", "deleted reference", "null path"} {
		t.Run(scenario, func(t *testing.T) {
			databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
			if databaseURL == "" {
				t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
			}
			ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
			if _, err := provider.UpTo(ctx, 33); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(ctx, migrationBackfillFixtureSQL); err != nil {
				t.Fatal(err)
			}
			if _, err := provider.UpTo(ctx, 46); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(ctx, `update session_resources set payload = jsonb_build_object('id', external_id, 'type', 'file', 'file_id', 'file_source_migration_184', 'source', '/uploads', 'mount_path', '/uploads/input.txt') where external_id = 'sesrsc_input_migration_184'`); err != nil {
				t.Fatal(err)
			}
			if scenario == "attachment" || scenario == "deleted attachment" {
				if _, err := database.ExecContext(ctx, `update filestore_entries set path = '/uploads/message_attachments/event/1.txt', parent_path = '/uploads/message_attachments/event' where external_id = 'fse_expired_output_migration_184'`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := provider.UpTo(ctx, 88); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "deleted attachment":
				if _, err := database.ExecContext(ctx, `update session_resources set deleted_at = now() where path = '/uploads/message_attachments/event/1.txt'`); err != nil {
					t.Fatal(err)
				}
			case "deleted reference":
				if _, err := database.ExecContext(ctx, `update session_resources set file_uuid = '80000000-0000-0000-0000-000000000002', payload = jsonb_set(payload, '{file_id}', to_jsonb((select external_id from files where uuid = '80000000-0000-0000-0000-000000000002'))), deleted_at = now() where external_id = 'sesrsc_input_migration_184'`); err != nil {
					t.Fatal(err)
				}
			case "null path":
				if _, err := database.ExecContext(ctx, `update session_resources set path = null, parent_path = null, file_uuid = null, deleted_at = now() where external_id = 'sesrsc_input_migration_184'`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := provider.UpTo(ctx, 104); err != nil {
				t.Fatal(err)
			}
			if scenario == "null path" {
				if _, err := provider.UpTo(ctx, 105); err == nil {
					t.Fatal("accepted a File without a namespace path")
				}
				return
			}
			if _, err := provider.UpTo(ctx, 106); err != nil {
				t.Fatalf("upgrade normal historical shape: %v", err)
			}
			if scenario == "attachment" || scenario == "deleted attachment" {
				var scope, ownership string
				if err := database.QueryRowContext(ctx, `select file.scope_id, resource.file_ownership from session_resources resource join files file on file.uuid = resource.file_uuid where resource.path = '/uploads/message_attachments/event/1.txt'`).Scan(&scope, &ownership); err != nil {
					t.Fatal(err)
				}
				if scope != "sesn_migration_184" || ownership != "owned" {
					t.Fatalf("scope=%q ownership=%q", scope, ownership)
				}
			}
			if _, err := provider.Down(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := provider.UpTo(ctx, 106); err != nil {
				t.Fatalf("up/down/up: %v", err)
			}
		})
	}
}

func TestSessionResourceDeletedMemoryMigration(t *testing.T) {
	for _, scenario := range []string{"cross workspace", "invalid payload", "deleted store"} {
		t.Run(scenario, func(t *testing.T) {
			databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
			if databaseURL == "" {
				t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
			}
			ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
			if _, err := provider.UpTo(ctx, 104); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(ctx, sessionFileOwnershipMigrationFixtureSQL); err != nil {
				t.Fatal(err)
			}
			if _, err := provider.UpTo(ctx, 105); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(ctx, sessionResourcePayloadMigrationFixtureSQL); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "cross workspace":
				if _, err := database.ExecContext(ctx, `update memory_stores set workspace_uuid = '20000000-0000-0000-0000-000000000002'`); err != nil {
					t.Fatal(err)
				}
			case "invalid payload":
				if _, err := database.ExecContext(ctx, `update session_resources set payload = '{"memory_store_id":42}' where resource_type = 'memory_store'`); err != nil {
					t.Fatal(err)
				}
			case "deleted store":
				api := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
				if _, err := api.DeleteMemoryStore(ctx, "20000000-0000-0000-0000-000000000001", "mem_payload"); err != nil {
					t.Fatal(err)
				}
			}
			_, err := provider.UpTo(ctx, 106)
			if scenario != "deleted store" {
				if err == nil {
					t.Fatal("accepted invalid Memory identity")
				}
				return
			}
			if err != nil {
				t.Fatalf("upgrade after supported hard delete: %v", err)
			}
			var retired bool
			if err := database.QueryRowContext(ctx, `select deleted_at is not null and memory_store_uuid is null from session_resources where external_id = 'sesrsc_memory_payload'`).Scan(&retired); err != nil {
				t.Fatal(err)
			}
			if !retired {
				t.Fatal("dangling Memory reference was not retired")
			}
			if _, err := provider.Down(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := provider.UpTo(ctx, 106); err != nil {
				t.Fatalf("retired Memory up/down/up: %v", err)
			}
		})
	}
}

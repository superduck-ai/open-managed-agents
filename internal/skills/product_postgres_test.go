package skills

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEnsureProductBuiltinSkillsRejectsVersionConflict(t *testing.T) {
	ctx := context.Background()
	database, store, cleanup := openProductSkillFixture(t)
	defer cleanup()

	_, err := seedProductBuiltinSkills(ctx, database, store, []productSkillSource{{
		ID:       "dream",
		Markdown: []byte("---\nname: dream\nversion: 1.0.0\n---\n\n# other\n"),
	}}, nil)
	if err != nil {
		t.Fatalf("seed conflicting dream: %v", err)
	}

	_, err = EnsureProductBuiltinSkills(ctx, database, store, nil)
	if err == nil || !strings.Contains(err.Error(), "already exists with different content") {
		t.Fatalf("ensure conflict err = %v, want version conflict", err)
	}
}

func TestEnsureProductBuiltinSkills(t *testing.T) {
	ctx := context.Background()
	database, store, cleanup := openProductSkillFixture(t)
	defer cleanup()

	result, err := EnsureProductBuiltinSkills(ctx, database, store, nil)
	if err != nil {
		t.Fatalf("ensure product builtin skills: %v", err)
	}
	if result.Imported != 1 || len(result.Skills) != 1 || result.Skills[0] != "dream" {
		t.Fatalf("ensure result = %+v, want dream", result)
	}

	skill, err := database.GetBuiltinSkill(ctx, "dream")
	if err != nil {
		t.Fatalf("get product dream skill: %v", err)
	}
	if skill.LatestVersion == nil || *skill.LatestVersion != "1.0.0" {
		t.Fatalf("latest version = %v, want 1.0.0", skill.LatestVersion)
	}
	version, err := database.GetBuiltinSkillVersion(ctx, "dream", "latest")
	if err != nil {
		t.Fatalf("get product dream version: %v", err)
	}
	data, ok := store.object(version.S3Key)
	if !ok {
		t.Fatalf("product dream object missing from store: %s", version.S3Key)
	}
	if !strings.Contains(version.S3Key, "builtin-skills/dream/versions/1.0.0/") {
		t.Fatalf("s3 key = %s, want builtin-skills/dream/versions/1.0.0/", version.S3Key)
	}
	names := zipEntryNames(t, data)
	if len(names) != 1 || names[0] != "dream/SKILL.md" {
		t.Fatalf("product dream zip entries = %v, want [dream/SKILL.md]", names)
	}

	if _, err := EnsureProductBuiltinSkills(ctx, database, store, nil); err != nil {
		t.Fatalf("ensure product builtin skills idempotent rerun: %v", err)
	}
	rerunVersion, err := database.GetBuiltinSkillVersion(ctx, "dream", "latest")
	if err != nil {
		t.Fatalf("get product dream version after rerun: %v", err)
	}
	if !rerunVersion.CreatedAt.Equal(version.CreatedAt) {
		t.Fatalf("version created_at changed on idempotent rerun: got %s, want %s", rerunVersion.CreatedAt, version.CreatedAt)
	}
}

func TestSeedBuiltinSkillsPruneKeepsProductSkills(t *testing.T) {
	ctx := context.Background()
	database, store, cleanup := openProductSkillFixture(t)
	defer cleanup()

	if _, err := EnsureProductBuiltinSkills(ctx, database, store, nil); err != nil {
		t.Fatalf("ensure product builtin skills: %v", err)
	}

	dir := t.TempDir()
	otherID := fmt.Sprintf("keep-%d", time.Now().UnixNano())
	pkg, err := packProductSkill(otherID, []byte("---\nname: "+otherID+"\n---\n\n# other\n"))
	if err != nil {
		t.Fatalf("pack other skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, otherID+".skill"), pkg.Zip, 0o600); err != nil {
		t.Fatalf("write other skill: %v", err)
	}
	if _, err := SeedBuiltinSkills(ctx, database, store, BuiltinSeedOptions{Dir: dir, Prune: true}, nil); err != nil {
		t.Fatalf("seed prune: %v", err)
	}
	if _, err := database.GetBuiltinSkill(ctx, "dream"); err != nil {
		t.Fatalf("product dream after prune: %v", err)
	}
}

func openProductSkillFixture(t *testing.T) (*db.DB, *memoryObjectStore, func()) {
	t.Helper()
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	database, err := db.Open(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.Migrate(ctx); err != nil {
		database.Close()
		t.Fatalf("migrate database: %v", err)
	}
	pool, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		database.Close()
		t.Fatalf("open cleanup pool: %v", err)
	}
	deleteDreamBuiltinRows(t, pool)
	store := newMemoryObjectStore("product-skill-bucket")
	return database, store, func() {
		deleteDreamBuiltinRows(t, pool)
		pool.Close()
		database.Close()
	}
}

func deleteDreamBuiltinRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `delete from builtin_skill_versions where skill_external_id = 'dream' or skill_external_id like 'keep-%'`); err != nil {
		t.Fatalf("cleanup dream skill versions: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `delete from builtin_skills where external_id = 'dream' or external_id like 'keep-%'`); err != nil {
		t.Fatalf("cleanup dream skill: %v", err)
	}
}

type memoryObjectStore struct {
	mu      sync.Mutex
	bucket  string
	objects map[string][]byte
}

func newMemoryObjectStore(bucket string) *memoryObjectStore {
	return &memoryObjectStore{bucket: bucket, objects: map[string][]byte{}}
}

func (s *memoryObjectStore) Ensure(context.Context) error { return nil }

func (s *memoryObjectStore) Name() string { return s.bucket }

func (s *memoryObjectStore) Upload(_ context.Context, key string, body io.Reader, _ storage.UploadOptions) (storage.UploadResult, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return storage.UploadResult{}, err
	}
	s.mu.Lock()
	s.objects[key] = data
	s.mu.Unlock()
	return storage.UploadResult{Size: int64(len(data))}, nil
}

func (s *memoryObjectStore) Open(_ context.Context, key string, _ *storage.ByteRange) (storage.Object, error) {
	s.mu.Lock()
	data, ok := s.objects[key]
	s.mu.Unlock()
	if !ok {
		return storage.Object{}, storage.ErrNotFound
	}
	return storage.Object{Body: io.NopCloser(bytes.NewReader(data)), Size: int64(len(data))}, nil
}

func (s *memoryObjectStore) Copy(_ context.Context, sourceKey, destinationKey string) (storage.CopyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[sourceKey]
	if !ok {
		return storage.CopyResult{}, storage.ErrNotFound
	}
	copied := append([]byte(nil), data...)
	s.objects[destinationKey] = copied
	return storage.CopyResult{}, nil
}

func (s *memoryObjectStore) Delete(_ context.Context, key string, _ storage.DeleteOptions) error {
	s.mu.Lock()
	delete(s.objects, key)
	s.mu.Unlock()
	return nil
}

func (s *memoryObjectStore) object(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	return data, ok
}

var _ storage.ObjectStore = (*memoryObjectStore)(nil)

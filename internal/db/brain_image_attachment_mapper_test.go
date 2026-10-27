package db

import (
	"github.com/superduck-ai/yourbatis"
	"reflect"
	"strings"
	"testing"
)

func TestBrainImageAttachmentMapperBindings(t *testing.T) {
	for _, lock := range []bool{false, true} {
		bound := buildBrainImageAttachmentMapperFind(yourbatis.DialectPostgres, "workspace", "session", "digest", lock)
		if strings.Contains(bound.SQL, "FOR UPDATE") != lock {
			t.Fatalf("lock=%v SQL=%s", lock, bound.SQL)
		}
		if !reflect.DeepEqual(argumentValues(bound.Args), []any{"workspace", "session", "digest"}) {
			t.Fatalf("args = %#v", bound.Args)
		}
	}
	input := BrainImageAttachment{OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionUUID: "session", SHA256: "digest", MediaType: "image/png", SizeBytes: 42, Bucket: "bucket", ObjectKey: "object"}
	insert := buildBrainImageAttachmentMapperInsert(yourbatis.DialectPostgres, input)
	if !reflect.DeepEqual(argumentValues(insert.Args), []any{"org", "workspace", "session", "digest", "image/png", int64(42), "bucket", "object", "session", "workspace", "org"}) {
		t.Fatalf("insert args = %#v", insert.Args)
	}
	attach := buildBrainImageAttachmentMapperAttach(yourbatis.DialectPostgres, "workspace", "session", "digest")
	if !strings.Contains(attach.SQL, "state = 'pending'") || !reflect.DeepEqual(argumentValues(attach.Args), []any{"workspace", "session", "digest"}) {
		t.Fatalf("attach = %+v", attach)
	}
	cleanup := buildBrainImageAttachmentMapperScheduleCleanup(yourbatis.DialectPostgres, 10)
	if !strings.Contains(cleanup.SQL, "SKIP LOCKED") || !reflect.DeepEqual(argumentValues(cleanup.Args), []any{10}) {
		t.Fatalf("cleanup = %+v", cleanup)
	}
	completed := buildBrainImageAttachmentMapperDeleteCompleted(yourbatis.DialectPostgres, "job")
	if !strings.Contains(completed.SQL, "image.workspace_uuid = job.workspace_uuid") || !reflect.DeepEqual(argumentValues(completed.Args), []any{"job"}) {
		t.Fatalf("completed = %+v", completed)
	}
}

func argumentValues(arguments []yourbatis.Argument) []any {
	values := make([]any, len(arguments))
	for index, argument := range arguments {
		values[index] = argument.Value
	}
	return values
}

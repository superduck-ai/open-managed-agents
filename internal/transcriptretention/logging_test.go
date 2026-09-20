package transcriptretention

import (
	"context"
	"log/slog"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type recordHandler struct{ record slog.Record }

func (*recordHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordHandler) Handle(_ context.Context, record slog.Record) error {
	h.record = record.Clone()
	return nil
}
func (h *recordHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordHandler) WithGroup(string) slog.Handler      { return h }

func TestArchiveLogMetadata(t *testing.T) {
	handler := &recordHandler{}
	service := &Service{logger: slog.New(handler), policy: Policy{DryRun: true}}
	service.logSegment(t.Context(), db.TranscriptScope{WorkspaceUUID: "workspace", CodeSessionExternalID: "session"}, 3, 100, 20)
	if handler.record.Level != slog.LevelInfo || handler.record.Message != "transcript archive segment" {
		t.Fatal("wrong event record")
	}
	attrs := map[string]slog.Value{}
	handler.record.Attrs(func(a slog.Attr) bool { attrs[a.Key] = a.Value; return true })
	if len(attrs) != 7 || attrs["event_count"].Int64() != 3 || attrs["raw_bytes"].Int64() != 100 || attrs["size_bytes"].Int64() != 20 || attrs["workspace_id"].String() != "workspace" || attrs["code_session_id"].String() != "session" || !attrs["dry_run"].Bool() {
		t.Fatalf("wrong safe metadata: %v", attrs)
	}
}

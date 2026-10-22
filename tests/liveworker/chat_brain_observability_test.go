//go:build integration

package liveworker

import (
	"context"
	"encoding/json"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/observability"
)

func TestChatBrainObservability(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	cfg, err := config.Load()
	requireOK(t, err)
	if !cfg.Observability.Enabled {
		t.Fatal("real OpenObserve configuration is required")
	}
	_, stop := startLazyBrainRunner(t, e, cfg)
	defer stop()
	preview := make(chan struct{}, 4)
	var calls atomic.Int32
	model := chatModelFixture(t, preview, &calls, true)
	configureChatModel(t, e, model.URL)
	node := startBrainNode(t, e, cfg, "observability")
	defer node.stop(t)
	f := createPublicChat(t, e)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	events, closeStream := connectChatStream(t, ctx, f)
	defer closeStream()
	input := submitChat(t, f, "请回复固定的聊天验证文本。")
	final := receiveChatAnswer(t, ctx, events, func() { preview <- struct{}{} })
	verifyChatHistory(t, f, input, final)
	if calls.Load() != 1 {
		t.Fatal("unexpected duplicate model calls")
	}
	f.code, err = e.database.GetCodeSessionBySessionExternalID(t.Context(), e.key.WorkspaceUUID.String(), f.session.ExternalID)
	requireOK(t, err)
	waitChatIdle(t, f, 1)
	chatProof(t, started, "brain_observability_roundtrip")

	backend := observability.NewOpenObserve(cfg.Observability.OpenObserve, nil)
	service := observability.New(backend, e.database)
	scope := observability.Scope{OrganizationUUID: e.key.OrganizationUUID.String(), WorkspaceUUID: e.key.WorkspaceUUID.String(), SessionID: f.session.ExternalID}
	window := observability.Window{Start: started.Add(-time.Minute), End: started.Add(time.Hour)}
	var page observability.Page
	waitRealWorker(t, "native turn/end readable from real OpenObserve", func() bool {
		list, queryErr := service.List(t.Context(), scope, window, 0)
		if queryErr != nil || len(list.Items) == 0 {
			return false
		}
		for _, item := range list.Items {
			if item.ID != f.code.ExternalID {
				continue
			}
			page, queryErr = service.Page(t.Context(), scope, window, item.ID, -1)
			if queryErr != nil {
				return false
			}
			for _, raw := range page.Events {
				var event struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(raw, &event) == nil && event.Type == "turn/end" {
					return true
				}
			}
		}
		return false
	}, 45*time.Second)
	verifyStoredNativeHistory(t, f, page)
	suffix, err := service.Page(t.Context(), scope, window, f.code.ExternalID, page.NextSeq)
	requireOK(t, err)
	if len(suffix.Events) != 0 || suffix.NextSeq != page.NextSeq {
		t.Fatal("native cursor replay duplicated history")
	}
	chatProof(t, started, "brain_observability_native_history")

	for _, foreign := range []observability.Scope{
		{OrganizationUUID: uuid.NewString(), WorkspaceUUID: scope.WorkspaceUUID},
		{OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: uuid.NewString()},
	} {
		list, err := backend.List(t.Context(), foreign, window, 0)
		requireOK(t, err)
		isolated, err := backend.Page(t.Context(), foreign, window, f.code.ExternalID, -1)
		requireOK(t, err)
		if len(list.Items) != 0 || len(isolated.Events) != 0 {
			t.Fatal("OpenObserve trajectory escaped its tenant scope")
		}
	}
	chatProof(t, started, "brain_observability_isolation")
}

func verifyStoredNativeHistory(t *testing.T, f *liveSession, page observability.Page) {
	t.Helper()
	var header struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	}
	requireOK(t, json.Unmarshal(page.Header, &header))
	if header.ID != f.code.ExternalID || header.Version != 4 {
		t.Fatal("native Session header was not preserved")
	}
	rows, _, err := f.env.service.EventPayloadStore().ListCodeSessionInternalEventsPage(t.Context(), db.ListCodeSessionInternalEventsPageParams{WorkspaceUUID: f.code.WorkspaceUUID, CodeSessionExternalID: f.code.ExternalID, Limit: 500})
	requireOK(t, err)
	originals := map[int]json.RawMessage{}
	for _, row := range rows {
		var payload struct {
			Subtype string `json:"subtype"`
			Data    struct {
				Header struct {
					ID string `json:"id"`
				} `json:"header"`
				Events []json.RawMessage `json:"events"`
			} `json:"data"`
		}
		requireOK(t, json.Unmarshal(row.Payload, &payload))
		if payload.Subtype != "dsh_log" || payload.Data.Header.ID != f.code.ExternalID {
			continue
		}
		for _, raw := range payload.Data.Events {
			var event struct {
				Seq int `json:"seq"`
			}
			requireOK(t, json.Unmarshal(raw, &event))
			originals[event.Seq] = raw
		}
	}
	if len(page.Events) == 0 || len(originals) == 0 {
		t.Fatal("native ledger is empty")
	}
	assistant := false
	for seq, raw := range page.Events {
		var envelope struct {
			Seq  int    `json:"seq"`
			Type string `json:"type"`
		}
		requireOK(t, json.Unmarshal(raw, &envelope))
		if envelope.Seq != seq {
			t.Fatal("native history lost event ordering")
		}
		assistant = assistant || envelope.Type == "assistant/message"
		original, found := originals[seq]
		if !found {
			t.Fatalf("OpenObserve event %d has no canonical source", seq)
		}
		var want, got any
		requireOK(t, json.Unmarshal(original, &want))
		requireOK(t, json.Unmarshal(raw, &got))
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("OpenObserve changed native event %d", seq)
		}
	}
	if !assistant {
		t.Fatal("native model output is missing")
	}
}

package channels

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestAttachmentPreparationResumesPerFileReceipts(t *testing.T) {
	for _, failure := range []string{"upload_rejected", "attach_rejected", "attach_unknown", "upload_unknown"} {
		t.Run(failure, func(t *testing.T) {
			uploads, attaches := 0, 0
			resources := []omaResource{}
			uploadedNames := map[string]int{}
			failed := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet:
					_ = json.MarshalWrite(w, map[string]any{"data": resources})
				case r.URL.Path == "/v1/files":
					file, header, err := r.FormFile("file")
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					_ = file.Close()
					if header.Filename == "second.csv" && !failed && failure == "upload_rejected" {
						failed = true
						w.WriteHeader(http.StatusTooManyRequests)
						return
					}
					uploads++
					uploadedNames[header.Filename]++
					if header.Filename == "second.csv" && !failed && failure == "upload_unknown" {
						failed = true
						_, _ = w.Write([]byte(`{`))
						return
					}
					_ = json.MarshalWrite(w, omaFile{ID: fmt.Sprintf("file_up_%d", uploads), Filename: header.Filename, MimeType: "text/csv"})
				default:
					var input struct {
						FileID    string `json:"file_id"`
						MountPath string `json:"mount_path"`
					}
					if err := json.UnmarshalRead(r.Body, &input); err != nil {
						t.Error(err)
					}
					if strings.HasSuffix(input.MountPath, "/second.csv") && !failed && failure == "attach_rejected" {
						failed = true
						w.WriteHeader(http.StatusTooManyRequests)
						return
					}
					attaches++
					resource := omaResource{ID: fmt.Sprintf("resource_%d", attaches), Type: "file", FileID: "clone_" + input.FileID, MountPath: "/mnt/session" + input.MountPath}
					resources = append(resources, resource)
					if strings.HasSuffix(input.MountPath, "/second.csv") && !failed && failure == "attach_unknown" {
						failed = true
						_, _ = w.Write([]byte(`{`))
						return
					}
					_ = json.MarshalWrite(w, resource)
				}
			}))
			defer server.Close()
			store := &fakeRuntimeStore{}
			worker, _ := testWorker(t, store, server.URL)
			row := db.ChannelInput{UUID: "input", State: "prepared", LeaseOwner: worker.deliveryOwner, LeaseUntil: time.Now().Add(time.Hour)}
			store.inputs = map[string]db.ChannelInput{row.UUID: row}
			worker.adapter = &attachmentBatchAdapter{channelAdapter: worker.adapter, data: []byte("name,value\nmarker,real")}
			msg := &channelMessage{MessageID: "message", Content: "完整读取本轮附件", input: &row, Resources: []channelAttachment{
				{Type: "file", FileKey: "first", FileName: "first.csv"}, {Type: "file", FileKey: "second", FileName: "second.csv"},
			}}
			content, _, err := worker.inputContent(t.Context(), "key", "sesn_1", msg)
			if err == nil || content != nil {
				t.Fatalf("第二个附件失败时不得提交部分内容: content=%v err=%v", content, err)
			}
			persisted := store.inputs[row.UUID]
			if persisted.PreparedContent == "" || persisted.SessionExternalID != "sesn_1" {
				t.Fatal("第一个附件成功后未保存逐项交接回执")
			}
			msg.input = &persisted
			content, mounted, err := worker.inputContent(t.Context(), "key", "sesn_1", msg)
			if failure == "upload_unknown" {
				if err == nil || content != nil || uploads != 2 || attaches != 1 {
					t.Fatalf("上传结果未知不得盲目重传或提交: uploads=%d attaches=%d err=%v", uploads, attaches, err)
				}
				return
			}
			if err != nil || len(content) != 2 || len(mounted) != 2 || uploads != 2 || attaches != 2 || uploadedNames["first.csv"] != 1 {
				t.Fatalf("恢复必须复用已成功上传和原生副本挂载: uploads=%d attaches=%d content=%v err=%v", uploads, attaches, content, err)
			}
			final := store.inputs[row.UUID]
			msg.input = &final
			if _, _, err := worker.inputContent(t.Context(), "key", "sesn_1", msg); err != nil || uploads != 2 || attaches != 2 {
				t.Fatalf("完整准备记录不得再上传挂载: %v", err)
			}
		})
	}
}

type failingPreparationStore struct {
	*fakeRuntimeStore
	failAt   int
	advances int
}

func (s *failingPreparationStore) AdvanceChannelInput(ctx context.Context, scope db.ChannelInputScope, inputUUID, owner string, change db.ChannelInputAdvance) (db.ChannelInput, error) {
	s.advances++
	if s.advances == s.failAt {
		return db.ChannelInput{}, errors.New("回执落库失败")
	}
	return s.fakeRuntimeStore.AdvanceChannelInput(ctx, scope, inputUUID, owner, change)
}

func TestAttachmentCheckpointFailureDoesNotRepeatSideEffects(t *testing.T) {
	for checkpoint := 1; checkpoint <= 5; checkpoint++ {
		t.Run(fmt.Sprintf("checkpoint_%d", checkpoint), func(t *testing.T) {
			files := &filesServer{}
			server := httptest.NewServer(files.handler(t))
			defer server.Close()
			base := &fakeRuntimeStore{}
			worker, _ := testWorker(t, base, server.URL)
			store := &failingPreparationStore{fakeRuntimeStore: base, failAt: checkpoint}
			worker.supervisor.store = store
			row := db.ChannelInput{UUID: "input", State: "prepared", LeaseOwner: worker.deliveryOwner, LeaseUntil: time.Now().Add(time.Hour)}
			base.inputs = map[string]db.ChannelInput{row.UUID: row}
			worker.adapter = &attachmentBatchAdapter{channelAdapter: worker.adapter, data: []byte("data")}
			msg := &channelMessage{MessageID: "message", input: &row, Resources: []channelAttachment{{Type: "file", FileKey: "first", FileName: "data.csv"}}}
			if _, _, err := worker.inputContent(t.Context(), "key", "sesn_1", msg); err == nil {
				t.Fatal("回执落库失败不得返回可提交的成功结果")
			}
			if checkpoint == 1 && files.uploads != 0 {
				t.Fatal("上传意图落库失败仍执行了上传")
			}
			persisted := base.inputs[row.UUID]
			msg.input = &persisted
			_, _, err := worker.inputContent(t.Context(), "key", "sesn_1", msg)
			if checkpoint == 2 {
				if err == nil || files.uploads != 1 || files.attached != 0 {
					t.Fatalf("上传回执未知不得重传: uploads=%d attached=%d err=%v", files.uploads, files.attached, err)
				}
				return
			}
			if err != nil || files.uploads != 1 || files.attached != 1 {
				t.Fatalf("恢复应复用已确认副作用: uploads=%d attached=%d err=%v", files.uploads, files.attached, err)
			}
		})
	}
}

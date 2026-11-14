package channels

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

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

func TestAttachmentPreparationUsesNativePathAfterLostReceipt(t *testing.T) {
	for _, failure := range []string{"rejected", "lost_reply", "checkpoint"} {
		t.Run(failure, func(t *testing.T) {
			files := &filesServer{}
			server := httptest.NewServer(files.handler(t))
			defer server.Close()
			base := &fakeRuntimeStore{}
			worker, _ := testWorker(t, base, server.URL)
			if failure == "checkpoint" {
				worker.supervisor.store = &failingPreparationStore{fakeRuntimeStore: base, failAt: 2}
			}
			calls := 0
			worker.supervisor.putAttachment = func(ctx context.Context, key, session, path, media string, body io.Reader) (db.FileRecord, error) {
				calls++
				if calls == 2 && failure == "rejected" {
					return db.FileRecord{}, errors.New("原生存储拒绝")
				}
				file, err := files.putAttachment(ctx, key, session, path, media, body)
				if calls == 2 && failure == "lost_reply" {
					return db.FileRecord{}, errors.New("写入成功但回执丢失")
				}
				return file, err
			}
			row := db.ChannelInput{UUID: "input", State: "prepared", LeaseOwner: worker.deliveryOwner, LeaseUntil: time.Now().Add(time.Hour)}
			base.inputs = map[string]db.ChannelInput{row.UUID: row}
			worker.adapter = &attachmentBatchAdapter{channelAdapter: worker.adapter, data: []byte("完整附件")}
			msg := &channelMessage{MessageID: "message", Content: "完整读取", input: &row, Resources: []channelAttachment{
				{Type: "file", FileKey: "first", FileName: "same.csv"}, {Type: "file", FileKey: "second", FileName: "same.csv"},
			}}
			content, _, err := worker.inputContent(t.Context(), "key", "sesn_1", msg)
			if err == nil || content != nil {
				t.Fatalf("失败不能提交部分内容: %v %v", content, err)
			}
			persisted := base.inputs[row.UUID]
			msg.input = &persisted
			content, mounted, err := worker.inputContent(t.Context(), "key", "sesn_1", msg)
			if err != nil || len(content) != 2 || len(mounted) != 2 || files.writes != 2 {
				t.Fatalf("恢复应复用原生稳定路径: writes=%d mounted=%v err=%v", files.writes, mounted, err)
			}
			if files.mountPaths[0] == files.mountPaths[1] {
				t.Fatal("同名附件覆盖")
			}
			persisted = base.inputs[row.UUID]
			msg.input = &persisted
			if _, _, err := worker.inputContent(t.Context(), "key", "sesn_1", msg); err != nil || files.writes != 2 {
				t.Fatal("完整准备内容被重复写入")
			}
		})
	}
}

func TestLegacyAttachmentReceiptDoesNotRepeatUnknownUpload(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused")
	for _, receipt := range []channelAttachmentReceipt{{Uploading: true}, {Mounting: true}} {
		prepared, _ := json.Marshal(channelPreparedInput{Preparing: true, Attachments: []channelAttachmentReceipt{receipt}})
		msg := &channelMessage{MessageID: "message", input: &db.ChannelInput{PreparedContent: string(prepared)}, Resources: []channelAttachment{{Type: "file", FileKey: "first"}}}
		worker.adapter = &attachmentBatchAdapter{channelAdapter: worker.adapter, data: []byte("data")}
		if content, _, err := worker.inputContent(t.Context(), "key", "sesn_1", msg); err == nil || content != nil {
			t.Fatal("旧未知回执不能盲目重放")
		}
	}
}

func TestLegacyConfirmedUploadContinuesThroughNativeAttachment(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused")
	worker.adapter = &attachmentBatchAdapter{channelAdapter: worker.adapter, data: []byte("data")}
	writes := 0
	worker.supervisor.putAttachment = func(_ context.Context, _, _, _, _ string, body io.Reader) (db.FileRecord, error) {
		writes++
		data, err := io.ReadAll(body)
		if string(data) != "data" {
			t.Fatal("旧附件内容改变")
		}
		return db.FileRecord{ExternalID: "native-file"}, err
	}
	prepared := channelPreparedInput{Preparing: true, Attachments: []channelAttachmentReceipt{{File: omaFile{ID: "confirmed-old-file"}}}}
	msg := &channelMessage{MessageID: "legacy-message", Resources: []channelAttachment{{Type: "file", FileKey: "first", FileName: "data.bin"}}}
	content, mounted, err := worker.messageContent(t.Context(), "key", "sesn_1", msg, "", &prepared)
	if err != nil || writes != 1 || len(content) != 1 || len(mounted) != 1 {
		t.Fatalf("已确认上传不应按未知结果阻断: writes=%d err=%v", writes, err)
	}
}

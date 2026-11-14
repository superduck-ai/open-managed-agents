package channels

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type attachmentBatchAdapter struct {
	channelAdapter
	data  []byte
	calls int
	fail  string
}

func (a *attachmentBatchAdapter) downloadAttachment(_ context.Context, _ string, resource channelAttachment, destination io.Writer) (string, error) {
	a.calls++
	if a.fail == "image" && resource.Type == "image" {
		return "", errors.New("适配器拒绝不支持的图片")
	}
	if resource.FileKey == "second" {
		switch a.fail {
		case "missing":
			return "", errors.New("未取得附件内容")
		case "download":
			return "", errors.New("下载中断")
		}
	}
	_, err := destination.Write(a.data)
	return "application/octet-stream", err
}

func TestAttachmentBatchRejectsBeforeAnyUpload(t *testing.T) {
	for _, failure := range []string{"missing", "download", "image", "kind"} {
		t.Run(failure, func(t *testing.T) {
			directory := t.TempDir()
			t.Setenv("TMPDIR", directory)
			files := &filesServer{}
			server := httptest.NewServer(files.handler(t))
			defer server.Close()
			worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
			worker.supervisor.putAttachment = files.putAttachment
			adapter := &attachmentBatchAdapter{channelAdapter: worker.adapter, data: []byte("附件内容"), fail: failure}
			worker.adapter = adapter
			resources := []channelAttachment{{Type: "file", FileKey: "first", FileName: "first.bin"}, {Type: "file", FileKey: "second", FileName: "second.bin"}}
			switch failure {
			case "image":
				resources[1].Type = "image"
			case "kind":
				resources[1].Type = "video"
			}
			content, _, err := worker.messageContent(t.Context(), "key", "sesn_1", &channelMessage{MessageID: "message", Resources: resources}, "必须完整读取两份附件", nil)
			if err == nil || len(content) != 0 || files.writes != 0 {
				t.Fatalf("整批校验失败不得部分上传、挂载或提交内容: err=%v content=%v writes=%d", err, content, files.writes)
			}
			entries, readErr := os.ReadDir(directory)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("失败附件临时文件未释放: count=%d err=%v", len(entries), readErr)
			}

		})
	}
}

func TestAttachmentBatchSizeBoundaries(t *testing.T) {
	buffer := make([]byte, (25<<20)+1)
	for _, test := range []struct {
		name        string
		size, count int
		wantError   bool
	}{
		{"空文件", 0, 1, false},
		{"单文件25MiB", 25 << 20, 1, false},
		{"大文件不受渠道25MiB限制", (25 << 20) + 1, 1, false},
		{"整批100MiB", 25 << 20, 4, false},
		{"整批超过100MiB不额外拒绝", 25 << 20, 5, false},
		{"超过十个附件不额外拒绝", 1, 11, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused")
			worker.adapter = &attachmentBatchAdapter{channelAdapter: worker.adapter, data: buffer[:test.size]}
			resources := make([]channelAttachment, test.count)
			for i := range resources {
				resources[i] = channelAttachment{Type: "file", FileKey: "file", FileName: "data.bin"}
			}
			attachments, err := worker.downloadMessageAttachments(t.Context(), &channelMessage{MessageID: "message", Resources: resources})
			for _, attachment := range attachments {
				attachment.close()
			}
			if (err != nil) != test.wantError || err == nil && len(attachments) != test.count {
				t.Fatalf("共享渠道不应额外限制OMA附件: count=%d err=%v", len(attachments), err)
			}
		})
	}
}

func TestInboundBinaryAndEmptyFilesMount(t *testing.T) {
	for _, payload := range [][]byte{{}, {0, 1, 2, 3}, {'P', 'K', 3, 4, 0, 0, 0, 0}} {
		files := &filesServer{}
		server := httptest.NewServer(files.handler(t))
		worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
		worker.supervisor.putAttachment = files.putAttachment
		stub := attachFeishuStub(t, worker)
		stub.attachment = payload
		content, mounted, err := worker.messageContent(t.Context(), "key", "sesn_1", &channelMessage{MessageID: "message", Resources: []channelAttachment{{Type: "file", FileKey: "data", FileName: "data.bin"}}}, "", nil)
		server.Close()
		if err != nil || len(content) != 1 || len(mounted) != 1 || files.writes != 1 {
			t.Fatalf("普通二进制和空文件应挂载，不得按嗅探类型丢弃: size=%d err=%v content=%v mounted=%v", len(payload), err, content, mounted)
		}
	}
}

func TestPreparedPartialAttachmentBatchCannotResume(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused")
	msg := &channelMessage{input: &db.ChannelInput{PreparedContent: `{"content":[{"type":"text","text":"此前只准备了部分内容"}],"skipped":["missing.pdf"],"mounted":{}}`}}
	content, _, err := worker.inputContent(t.Context(), "key", "sesn_1", msg)
	if err == nil || content != nil {
		t.Fatalf("旧版跳过附件的准备记录不能继续提交: content=%v err=%v", content, err)
	}
}

type streamingAttachmentAdapter struct{ channelAdapter }

func (a streamingAttachmentAdapter) downloadAttachment(_ context.Context, _ string, _ channelAttachment, destination io.Writer) (string, error) {
	var block [32 << 10]byte
	for range 2048 {
		if _, err := destination.Write(block[:]); err != nil {
			return "", err
		}
	}
	return "application/octet-stream", nil
}

func TestAttachmentBatchSpoolsInsteadOfRetainingBytes(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("TMPDIR", directory)
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused")
	worker.adapter = streamingAttachmentAdapter{worker.adapter}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	attachments, err := worker.downloadMessageAttachments(t.Context(), &channelMessage{MessageID: "message", Resources: []channelAttachment{{Type: "file", FileKey: "first"}, {Type: "file", FileKey: "second"}}})
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, attachment := range attachments {
			attachment.close()
		}
	}()
	for _, attachment := range attachments {
		if attachment.size != 64<<20 {
			t.Fatalf("临时文件字节数=%d", attachment.size)
		}
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
		t.Fatalf("128MiB附件批次不应整批驻留内存: allocated=%d", allocated)
	}
}

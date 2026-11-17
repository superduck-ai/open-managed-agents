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
)

type artifactPolicyAdapter struct {
	channelAdapter
	sent     []string
	failSend bool
}

func (a *artifactPolicyAdapter) SendMedia(_ context.Context, _ *channelMessage, resource omaResource, _ string, _ []byte) (string, error) {
	a.sent = append(a.sent, resource.FileID)
	if a.failSend {
		return "", errors.New("上传被明确拒绝")
	}
	return "artifact-receipt", nil
}

func TestArtifactSelectionOnlyIncludesNativeOutputs(t *testing.T) {
	resources := []omaResource{
		{ID: "input", Type: "file", FileID: "file_input", MountPath: "/mnt/session/uploads/changed.txt"},
		{ID: "lookalike", Type: "file", FileID: "file_lookalike", MountPath: "/mnt/user-data/outputs-other/private.txt"},
		{ID: "output", Type: "file", FileID: "file_output", MountPath: "/mnt/user-data/outputs/result.txt"},
	}
	downloaded := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/resources"):
			_ = json.MarshalWrite(w, map[string]any{"data": resources})
		case strings.HasSuffix(r.URL.Path, "/content"):
			downloaded = append(downloaded, r.URL.Path)
			_, _ = w.Write([]byte("data"))
		default:
			id := strings.TrimPrefix(r.URL.Path, "/v1/files/")
			_ = json.MarshalWrite(w, omaFile{ID: id, Filename: "result.txt", MimeType: "text/plain", Size: 4})
		}
	}))
	defer server.Close()
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	adapter := &artifactPolicyAdapter{channelAdapter: worker.adapter}
	worker.adapter = adapter
	sendTestArtifacts(t, worker, &channelReplyTask{sessionID: "sesn_1"}, &channelMessage{ChatID: "chat"}, artifactBaseline{})
	if len(downloaded) != 1 || downloaded[0] != "/v1/files/file_output/content" || len(adapter.sent) != 1 || adapter.sent[0] != "file_output" {
		t.Fatalf("只能回传原生outputs，不能把新上传或相似前缀当产物: downloaded=%v sent=%v", downloaded, adapter.sent)
	}
}

func TestArtifactNativeBudgetsAndDownloadIntegrity(t *testing.T) {
	buffer := make([]byte, (25<<20)+1)
	for _, tc := range []struct {
		name             string
		count            int
		metadataSize     int64
		actualSize       int
		wrongID          bool
		failSend         bool
		wantDownloads    int
		wantSendAttempts int
	}{
		{name: "大文件复用OMA元数据", count: 1, metadataSize: (25 << 20) + 1, actualSize: (25 << 20) + 1, wantDownloads: 1, wantSendAttempts: 1},
		{name: "错误元数据标识", count: 1, metadataSize: 4, actualSize: 4, wrongID: true},
		{name: "非法负大小", count: 1, metadataSize: -1},
		{name: "整轮超过100MiB不额外截断", count: 5, metadataSize: 25 << 20, actualSize: 25 << 20, wantDownloads: 5, wantSendAttempts: 5},
		{name: "下载长度与元数据不符", count: 1, metadataSize: 4, actualSize: 3, wantDownloads: 1},
		{name: "下载实际长度超过OMA元数据", count: 1, metadataSize: 4, actualSize: (25 << 20) + 1, wantDownloads: 1},
		{name: "发送失败不截断后续候选", count: 12, metadataSize: 4, actualSize: 4, failSend: true, wantDownloads: 12, wantSendAttempts: 12},
		{name: "十二个产物全部交付", count: 12, metadataSize: 4, actualSize: 4, wantDownloads: 12, wantSendAttempts: 12},
		{name: "单文件25MiB边界", count: 1, metadataSize: 25 << 20, actualSize: 25 << 20, wantDownloads: 1, wantSendAttempts: 1},
		{name: "普通小文件", count: 1, metadataSize: 4, actualSize: 4, wantDownloads: 1, wantSendAttempts: 1},
		{name: "共享渠道层允许空文件交给适配器判断", count: 1, wantDownloads: 1, wantSendAttempts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resources := make([]omaResource, tc.count)
			for i := range resources {
				resources[i] = omaResource{ID: fmt.Sprintf("resource_%d", i), Type: "file", FileID: fmt.Sprintf("file_%d", i), Filename: "result.bin", MimeType: "application/octet-stream", MountPath: fmt.Sprintf("/mnt/user-data/outputs/result_%d.bin", i)}
			}
			downloads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/resources"):
					_ = json.MarshalWrite(w, map[string]any{"data": resources})
				case strings.HasSuffix(r.URL.Path, "/content"):
					downloads++
					_, _ = w.Write(buffer[:tc.actualSize])
				default:
					id := strings.TrimPrefix(r.URL.Path, "/v1/files/")
					if tc.wrongID {
						id = "file_unrelated"
					}
					_ = json.MarshalWrite(w, omaFile{ID: id, Filename: "result.bin", MimeType: "application/octet-stream", Size: tc.metadataSize})
				}
			}))
			defer server.Close()
			worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
			adapter := &artifactPolicyAdapter{channelAdapter: worker.adapter, failSend: tc.failSend}
			worker.adapter = adapter
			sendTestArtifacts(t, worker, &channelReplyTask{sessionID: "sesn_1"}, &channelMessage{ChatID: "chat"}, artifactBaseline{})
			if downloads != tc.wantDownloads || len(adapter.sent) != tc.wantSendAttempts {
				t.Fatalf("原生产物数量或长度验证不符: downloads=%d/%d sends=%d/%d", downloads, tc.wantDownloads, len(adapter.sent), tc.wantSendAttempts)
			}
		})
	}
}

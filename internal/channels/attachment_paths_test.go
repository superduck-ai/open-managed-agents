package channels

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAttachmentPathsUseConfirmedResources(t *testing.T) {
	for _, state := range []string{"new", "existing", "output", "missing"} {
		t.Run(state, func(t *testing.T) {
			existing := state == "existing"
			uploads, attaches := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet:
					resources := []omaResource{}
					if existing || state == "output" {
						mountPath := "/mnt/session/uploads/existing/data.csv"
						if state == "output" {
							mountPath = "/mnt/user-data/outputs/data.csv"
						}
						resources = append(resources, omaResource{ID: "existing", Type: "file", FileID: "uploaded_1", MountPath: mountPath})
					}
					_ = json.MarshalWrite(w, map[string]any{"data": resources})
				case r.URL.Path == "/v1/files":
					uploads++
					_ = json.MarshalWrite(w, omaFile{ID: fmt.Sprintf("uploaded_%d", uploads), Filename: "data.csv", MimeType: "text/csv"})
				default:
					attaches++
					var input struct {
						FileID string `json:"file_id"`
					}
					if err := json.UnmarshalRead(r.Body, &input); err != nil {
						t.Error(err)
					}
					resource := omaResource{ID: "resource_" + input.FileID, Type: "file", FileID: "clone_" + input.FileID, MountPath: "/mnt/session/uploads/confirmed/" + input.FileID + "/data.csv"}
					if state == "missing" {
						resource.MountPath = ""
					}
					_ = json.MarshalWrite(w, resource)
				}
			}))
			defer server.Close()
			worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
			worker.adapter = &attachmentBatchAdapter{channelAdapter: worker.adapter, data: []byte("a,b\n1,2")}
			msg := &channelMessage{MessageID: "message", Resources: []channelAttachment{{Type: "file", FileKey: "first", FileName: "data.csv"}, {Type: "file", FileKey: "second", FileName: "data.csv"}}}
			content, mounted, err := worker.messageContent(t.Context(), "key", "sesn_1", msg, "请对比这两份同名文件", nil)
			if state == "missing" {
				if err == nil || content != nil {
					t.Fatalf("挂载回执不完整时不能猜路径提交: err=%v content=%v", err, content)
				}
				return
			}
			first := "/mnt/session/uploads/confirmed/uploaded_1/data.csv"
			if existing {
				first = "/mnt/session/uploads/existing/data.csv"
			}
			encoded, _ := json.Marshal(content)
			if err != nil || len(content) != 2 || content[0]["text"] != "请对比这两份同名文件" || !strings.Contains(string(encoded), first) || !strings.Contains(string(encoded), "/mnt/session/uploads/confirmed/uploaded_2/data.csv") || len(mounted) != 2 {
				t.Fatalf("本轮同名附件必须包含各自实际挂载路径，保留原文: err=%v content=%s mounted=%v", err, encoded, mounted)
			}
			wantAttaches := 2
			if existing {
				wantAttaches = 1
			}
			if attaches != wantAttaches {
				t.Fatalf("已有挂载不得重复挂载: %d", attaches)
			}
		})
	}
}

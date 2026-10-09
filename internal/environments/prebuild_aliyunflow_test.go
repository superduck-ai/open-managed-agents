package environments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestAliyunFlowImageBuilderContract(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/log" && r.Header.Get("x-yunxiao-token") != "token" {
			t.Error("missing Flow token")
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /runs":
			var request struct {
				Params string `json:"params"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				return
			}
			var params struct {
				Envs map[string]string `json:"envs"`
			}
			if err := json.Unmarshal([]byte(request.Params), &params); err != nil {
				t.Error(err)
				return
			}
			want := map[string]string{
				"DOCKERFILE_TEXT": "base64:RlJPTSBiYXNlCg==",
				"IMAGE_REPO":      "repo",
				"IMAGE_TAG":       "attempt",
			}
			if !reflect.DeepEqual(params.Envs, want) {
				t.Errorf("Flow envs = %v, want %v", params.Envs, want)
			}
			_, _ = io.WriteString(w, `123`)
		case "GET /runs/123":
			_, _ = io.WriteString(w, `{"status":"SUCCESS","stages":[{"stageInfo":{"jobs":[{"id":456}]}}]}`)
		case "GET /pipelineRuns/123/jobs/456/steps":
			_, _ = io.WriteString(w, `{"buildId":789,"buildProcessNodes":[{"stepIndex":0,"finish":true,"status":"SUCCESS"}]}`)
		case "GET /pipelineRuns/123/jobs/456/step/log/url":
			if r.URL.Query().Get("buildId") != "789" || r.URL.Query().Get("stepIndex") != "0" {
				t.Errorf("unexpected log query: %s", r.URL.RawQuery)
			}
			_, _ = fmt.Fprintf(w, `{"downloadUrl":%q}`, server.URL+"/log")
		case "GET /log":
			if r.Header.Get("x-yunxiao-token") != "" || r.Header.Get("Range") != "bytes=0-" {
				t.Errorf("unexpected log headers: %v", r.Header)
			}
			_, _ = io.WriteString(w, "built\n")
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	flow := aliyunFlowImageBuilder{http: newBuildHTTP("x-yunxiao-token", "token"), pipelineURL: server.URL}
	flow.http.client = server.Client()
	ctx := context.Background()
	ref, err := flow.Start(ctx, imageBuildInput{Dockerfile: "FROM base\n", Repository: "repo", Tag: "attempt"})
	if err != nil {
		t.Fatal(err)
	}
	status, err := flow.GetStatus(ctx, ref)
	if err != nil || status.State != "succeeded" || status.ImageRef != "repo:attempt" {
		t.Fatalf("Flow status = %+v, error = %v", status, err)
	}
	first, err := flow.ReadLogs(ctx, ref, "")
	if err != nil || first.Text != "--- 456/789/0 ---\nbuilt\n" || first.NextCursor == "" || first.Complete {
		t.Fatalf("first log chunk = %+v, error = %v", first, err)
	}
	last, err := flow.ReadLogs(ctx, ref, first.NextCursor)
	if err != nil || last.Text != "" || !last.Complete {
		t.Fatalf("final log chunk = %+v, error = %v", last, err)
	}
}

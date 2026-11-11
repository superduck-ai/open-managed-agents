//go:build integration

package codesessions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxToolOutputSafeStorage(t *testing.T) {
	for _, symlink := range []bool{true, false} {
		t.Run(map[bool]string{true: "reject symlink", false: "preserve large Unicode bytes"}[symlink], func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "outputs")
			foreign := t.TempDir()
			if symlink {
				if err := os.Symlink(foreign, root); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					ID     string `json:"id"`
					Method string `json:"method"`
					Params struct {
						Name      string `json:"name"`
						Arguments struct {
							Command string `json:"command"`
						} `json:"arguments"`
					} `json:"params"`
				}
				if json.NewDecoder(r.Body).Decode(&input) != nil || input.Method != "tools/call" || input.Params.Name != "bash" || r.Header.Get("Mcp-Protocol-Version") != "2026-07-28" || r.Header.Get("Mcp-Method") != "tools/call" || r.Header.Get("Mcp-Name") != "bash" {
					t.Error("unexpected stateless sandbox protocol")
					w.WriteHeader(400)
					return
				}
				calls++
				if len(input.Params.Arguments.Command) > 65536 {
					t.Error("unbounded command")
					w.WriteHeader(400)
					return
				}
				command := strings.Replace(input.Params.Arguments.Command, `root="/tmp/tool-outputs"`, `root="`+root+`"`, 1)
				output, err := exec.CommandContext(r.Context(), "sh", "-c", command).CombinedOutput()
				body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": input.ID, "result": map[string]any{"resultType": "complete", "isError": err != nil, "content": []map[string]string{{"type": "text", "text": string(output)}}}})
				if calls%2 == 0 {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write(append(append([]byte(": progress\n\ndata: "), body...), []byte("\n\n")...))
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(body)
				}
			}))
			defer httpServer.Close()
			text := strings.Repeat("中😀\x00'`$()", 12000)
			path, err := saveSandboxToolOutput(t.Context(), httpServer.URL, text)
			if symlink {
				entries, _ := os.ReadDir(foreign)
				if err == nil || len(entries) != 0 {
					t.Fatalf("followed symlink: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, filepath.Base(path)))
			if err != nil || string(data) != text || calls < 2 {
				t.Fatalf("storage mismatch: %v calls=%d bytes=%d", err, calls, len(data))
			}
		})
	}
}

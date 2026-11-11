package codesessions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"uuid"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type sandboxOutputInput struct {
	Content string `json:"content"`
}

func (h *Handler) handleSandboxToolOutput(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "code_session_id")
	claims, ok := h.authorizeSessionIngressClaims(w, r, id)
	if !ok {
		return
	}
	session, found, err := h.db.GetCodeSession(r.Context(), id)
	if err == nil && (!found || session.OrganizationUUID != claims.OrganizationUUID || session.WorkspaceUUID != claims.WorkspaceUUID || session.CurrentWorkerEpoch != claims.WorkerEpoch) {
		err = db.ErrWorkerEpochMismatch
	}
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSandboxMCPRequestBytes))
	decoder.DisallowUnknownFields()
	var input sandboxOutputInput
	if err = decoder.Decode(&input); err == nil {
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			err = fmt.Errorf("multiple output payloads")
		}
	}
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxRequestReadError(err))
		return
	}
	endpoint, err := h.ensureSessionSandbox(r.Context(), session, true)
	if err == nil {
		err = h.db.ValidateCodeSessionIngressWorkerEpoch(r.Context(), claims.OrganizationUUID, claims.WorkspaceUUID, id, claims.WorkerEpoch)
	}
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return
	}
	path, err := saveSandboxToolOutput(r.Context(), endpoint, input.Content)
	if err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"path": path})
}

func sandboxOutputCommand(content, name, inode string) string {
	const script = `import base64,os,sys
root="/tmp/tool-outputs"
try: os.mkdir(root,0o700)
except FileExistsError: pass
directory=os.open(root,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
try:
 flags=os.O_WRONLY|os.O_NOFOLLOW
 flags |= (os.O_CREAT|os.O_EXCL) if sys.argv[3]=="0" else os.O_APPEND
 fd=os.open(sys.argv[2],flags,0o600,dir_fd=directory)
 with os.fdopen(fd,"wb") as output:
  info=os.fstat(output.fileno())
  if info.st_nlink!=1 or (sys.argv[3]!="0" and info.st_ino!=int(sys.argv[3])): raise RuntimeError("output identity changed")
  output.write(base64.b64decode(sys.argv[1],validate=True))
finally: os.close(directory)
print("OMA_OUTPUT_INODE="+str(info.st_ino))`
	return "python3 -c '" + script + "' '" + base64.StdEncoding.EncodeToString([]byte(content)) + "' '" + name + "' '" + inode + "'"
}

func saveSandboxToolOutput(ctx context.Context, endpoint, content string) (string, error) {
	name := "mcp_" + uuid.NewV4().String() + ".txt"
	path := "/tmp/tool-outputs/" + name

	inode := "0"
	data := []byte(content)
	for offset := 0; offset < len(data) || offset == 0; offset += 32768 {
		end := min(offset+32768, len(data))
		result, err := callSandboxOutput(ctx, endpoint, sandboxOutputCommand(string(data[offset:end]), name, inode))
		if err != nil || result == nil || result.IsError {
			return "", fmt.Errorf("output sandbox storage failed")
		}
		next := sandboxOutputInode(result)
		if next == "" || (inode != "0" && inode != next) {
			return "", fmt.Errorf("output sandbox storage receipt missing")
		}
		inode = next
	}
	return path, nil
}

func sandboxOutputInode(result *sandboxOutputResult) string {
	for _, block := range result.Content {
		if block.Type == "text" {
			for _, line := range strings.Split(block.Text, "\n") {
				value, found := strings.CutPrefix(line, "OMA_OUTPUT_INODE=")
				if found {
					if inode, err := strconv.ParseUint(value, 10, 64); err == nil && inode > 0 {
						return value
					}
				}
			}
		}
	}
	return ""
}

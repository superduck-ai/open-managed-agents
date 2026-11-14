package sessions

import (
	"strings"
	"testing"
)

func TestSessionAttachmentRequiresNativeSessionAuthorization(t *testing.T) {
	handler := &Handler{}
	file, err := handler.PutAttachment(t.Context(), "session", "/uploads/message_attachments/input/0.txt", "text/plain", strings.NewReader("private"))
	if err == nil || file.ExternalID != "" {
		t.Fatal("未授权请求不得访问附件存储")
	}
}

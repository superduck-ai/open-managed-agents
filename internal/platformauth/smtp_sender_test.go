package platformauth

import (
	"strings"
	"testing"
)

func TestSMTPSenderMessage(t *testing.T) {
	sender := &smtpSender{username: "sender@example.com"}
	message := string(sender.message("recipient@example.com", "123456"))
	for _, expected := range []string{
		"From: sender@example.com\r\n",
		"To: recipient@example.com\r\n",
		"Content-Type: text/plain; charset=UTF-8\r\n",
		"123456",
		"验证码将在 10 分钟内失效",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message does not contain %q: %q", expected, message)
		}
	}
}

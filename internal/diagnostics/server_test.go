package diagnostics

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestRejectsPublicDiagnostics(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", ":0", "localhost:0", "192.0.2.1:0", "invalid"} {
		if close, err := Start(addr, "", nil); err == nil {
			_ = close()
			t.Errorf("accepted unsafe address %s", addr)
		}
	}
}
func TestDiagnosticsLifecycle(t *testing.T) {
	close, err := Start("", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := close(); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	close, err = Start(addr, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := close(); err != nil {
		t.Fatal(err)
	}
	listener, err = net.Listen("tcp", addr)
	if err != nil {
		t.Fatal("diagnostics listener leaked", err)
	}
	_ = listener.Close()
}

func TestUnexpectedServeErrorIsLogged(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	serve(&http.Server{ReadHeaderTimeout: time.Second}, listener, logger)
	var record struct {
		Level   string `json:"level"`
		Message string `json:"msg"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Level != "ERROR" || record.Message != "diagnostics server failed" || record.Error == "" {
		t.Fatalf("invalid error record %+v", record)
	}
	output.Reset()
	server := &http.Server{ReadHeaderTimeout: time.Second}
	server.Close()
	serve(server, listener, logger)
	if output.Len() != 0 {
		t.Fatal("normal shutdown logged an error")
	}
}

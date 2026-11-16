package porttunnel

import (
	"bytes"
	"testing"
)

func TestDecodeResponseRejectsMalformedFrames(t *testing.T) {
	for name, frame := range map[string]string{
		"not json":     `{"httpChunk":`,
		"bad base64":   `{"httpChunk":{"requestId":"r","data":"***"}}`,
		"wrong number": `{"httpHeaders":{"statusCode":"abc"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeResponse([]byte(frame)); err == nil {
				t.Fatalf("decoded %s", frame)
			}
		})
	}
}

func TestEncodeRequestMatchesRustProtoJSONContract(t *testing.T) {
	data, err := encodeRequest(tunnelRequest{
		SessionID: "session-1",
		HTTPRequest: &httpRequestMsg{
			RequestID: "req-1", Port: 8080, Method: "POST", Path: "/upload",
			Headers: []Header{{Name: "X-Test", Value: "ok"}}, Body: Bytes{0xfb, 0xff},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"sessionId":"session-1","httpRequest":{"requestId":"req-1","port":8080,"method":"POST","path":"/upload","headers":[{"name":"X-Test","value":"ok"}],"body":"+/8="}}`
	if string(data) != want {
		t.Fatalf("encoded = %s\nwant      %s", data, want)
	}
	keepalive, _ := encodeRequest(tunnelRequest{SessionID: "session-1"})
	if string(keepalive) != `{"sessionId":"session-1"}` {
		t.Fatalf("keepalive = %s", keepalive)
	}
}

func TestDecodeResponseAcceptsBothProtoJSONSpellings(t *testing.T) {
	camel, err := decodeResponse([]byte(`{"httpChunk":{"requestId":"req-1","data":"b2s=","isFinal":true,"sequenceNumber":3}}`))
	if err != nil {
		t.Fatal(err)
	}
	snake, err := decodeResponse([]byte(`{"http_chunk":{"request_id":"req-1","data":"b2s","is_final":true,"sequence_number":3}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, response := range []tunnelResponse{camel, snake} {
		chunk := response.HTTPChunk
		if chunk == nil || chunk.RequestID != "req-1" || !bytes.Equal(chunk.Data, []byte("ok")) || !chunk.IsFinal || chunk.SequenceNumber != 3 {
			t.Fatalf("chunk = %+v", chunk)
		}
	}
	headers, err := decodeResponse([]byte(`{"http_headers":{"request_id":"r","status_code":201,"headers":[{"name":"x_y","value":"a_b"}]}}`))
	if err != nil || headers.HTTPHeaders.StatusCode != 201 || headers.HTTPHeaders.Headers[0] != (Header{Name: "x_y", Value: "a_b"}) {
		t.Fatalf("headers = %+v, %v", headers.HTTPHeaders, err)
	}
	ws, err := decodeResponse([]byte(`{"wsClose":{"tunnelId":"t","code":1000,"reason":"bye"}}`))
	if err != nil || ws.WSClose.Code != 1000 || ws.WSClose.Reason != "bye" {
		t.Fatalf("ws close = %+v, %v", ws.WSClose, err)
	}
}

func TestWindowUpdateMatchesRustProtoJSONContract(t *testing.T) {
	data, err := encodeRequest(tunnelRequest{SessionID: "session-1", WindowUpdate: &windowUpdateMsg{StreamID: "req-1", Bytes: 524288}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"sessionId":"session-1","windowUpdate":{"streamId":"req-1","bytes":524288}}`
	if string(data) != want {
		t.Fatalf("encoded = %s\nwant      %s", data, want)
	}
	for _, frame := range []string{
		`{"windowUpdate":{"streamId":"ws-1","bytes":524288}}`,
		`{"window_update":{"stream_id":"ws-1","bytes":524288}}`,
	} {
		response, err := decodeResponse([]byte(frame))
		if err != nil || response.WindowUpdate == nil || *response.WindowUpdate != (windowUpdateMsg{StreamID: "ws-1", Bytes: 524288}) {
			t.Fatalf("decode %s = %+v, %v", frame, response.WindowUpdate, err)
		}
	}
}

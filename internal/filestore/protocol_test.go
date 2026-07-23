package filestore

import (
	"encoding/json"
	"testing"
)

func TestProtoInt64UsesProtoJSONStringEncoding(t *testing.T) {
	value, err := json.Marshal(struct {
		Size protoInt64 `json:"size"`
	}{Size: 42})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(value) != `{"size":"42"}` {
		t.Fatalf("value = %s", value)
	}

	var request readFileRequest
	if err := json.Unmarshal([]byte(`{"filesystemId":"fs_test","path":"/a","range":{"offset":"3","length":"-1"}}`), &request); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if request.Range == nil || request.Range.Offset != 3 || request.Range.Length != -1 {
		t.Fatalf("range = %#v", request.Range)
	}
}

func TestValidateFilestorePath(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		allowRoot bool
		wantError bool
	}{
		{name: "relative", value: "reports/a.txt", wantError: true},
		{name: "empty segment", value: "/reports//a.txt", wantError: true},
		{name: "trailing slash", value: "/reports/", wantError: true},
		{name: "dot segment", value: "/reports/../a.txt", wantError: true},
		{name: "root forbidden", value: "/", wantError: true},
		{name: "file", value: "/reports/a.txt"},
		{name: "root", value: "/", allowRoot: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateFilestorePath(test.value, test.allowRoot)
			if (err != nil) != test.wantError {
				t.Fatalf("validateFilestorePath(%q) error = %v", test.value, err)
			}
		})
	}
}

func TestDecodeStrictJSONRejectsUnknownFields(t *testing.T) {
	var request pathRequest
	if err := decodeStrictJSON([]byte(`{"filesystemId":"fs_test","path":"/a","unknown":true}`), &request); err == nil {
		t.Fatal("decodeStrictJSON accepted unknown field")
	}
}

func TestAuthorizationMetadataUsesProtoScalarPresence(t *testing.T) {
	var params createFileParams
	if err := json.Unmarshal(
		[]byte(`{"filesystemId":"fs_test","path":"/a","mediaType":"text/plain","authorizationMetadata":{"intent":"assistant_output"}}`),
		&params,
	); err != nil {
		t.Fatalf("unmarshal authorization metadata: %v", err)
	}
	if params.Authorization == nil {
		t.Fatal("authorization metadata is nil")
	}
	if fileDownloadable(params.Authorization) {
		t.Fatal("omitted downloadable in a present protobuf message must retain the bool zero value")
	}

	if !fileDownloadable(nil) {
		t.Fatal("omitted authorization metadata must retain the Filestore default")
	}
}

func TestFilePayloadKeepsEntryTaggedIDWireAlias(t *testing.T) {
	value, err := json.Marshal(filePayload{EntryTaggedID: "fse_entry"})
	if err != nil {
		t.Fatalf("marshal file payload: %v", err)
	}
	if string(value) != `{"workspaceTaggedId":"fse_entry"}` {
		t.Fatalf("file payload = %s", value)
	}
}

package codesessions

import (
	"reflect"
	"testing"
)

func TestModelRequestPublicTools(t *testing.T) {
	tools := []ModelRequestToolUse{
		{ID: "valid", Name: "Bash"},
		{ID: "invalid", Name: "Write", InputRejected: true},
		{ID: "invalid-second", Name: "Read", InputRejected: true},
	}
	for _, test := range []struct {
		name      string
		errorType string
		want      []ModelRequestToolUse
	}{
		{name: "invalid response selects rejected tools", errorType: "invalid_response", want: tools[1:]},
		{name: "other errors publish no tools", errorType: "upstream_error"},
		{name: "successful response keeps all tools", want: tools},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := ModelRequestResult{ToolUses: tools, ErrorType: test.errorType}
			if got := modelRequestPublicTools(result); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("public tools = %#v, want %#v", got, test.want)
			}
			if tools[0].InputRejected || tools[0].ID != "valid" {
				t.Fatal("selection changed the source tools")
			}
		})
	}
}

package devinit

import (
	"strings"
	"testing"
)

func TestRenderE2BConfigTemplate(t *testing.T) {
	template := `
server:
  addr: "0.0.0.0:3099"
docker:
  container_name_prefix: "e2b-envd-"
  envd_binary: "__ENVD_BIN_DIR__/envd-linux-amd64"
`
	rendered := renderE2BConfigTemplate(template, "feature-x", 43123, "/work/envd-bin")
	for _, want := range []string{
		`addr: "0.0.0.0:43123"`,
		`container_name_prefix: "e2b-envd-feature-x-"`,
		`envd_binary: "/work/envd-bin/envd-linux-amd64"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered config missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "__ENVD_BIN_DIR__") || strings.Contains(rendered, ":3099") {
		t.Errorf("unrendered placeholder left behind:\n%s", rendered)
	}
}

package tunnels

import "testing"

func TestServerInfoRejectsUnsupportedSchema(t *testing.T) {
	for _, input := range []string{
		`{"version":3,"channels":[{"name":"main"}]}`,
		`{"version":1,"channels":[{"name":"main","stateless":false}]}`,
		`{"version":1,"channels":[{"name":"main","stateless":null}]}`,
		`{"version":2,"channels":[{"name":"main","stateless":null}]}`,
		`{"version":2,"channels":[{"name":"main","stateless":"true"}]}`,
		`{"version":2,"channels":[{"name":"main"},{"name":"main"}]}`,
		`{"version":2,"channels":[{"name":"main","url":"https://example.com"}]}`,
	} {
		if _, err := ParseMCPServerInfo(input); err == nil {
			t.Errorf("accepted %s", input)
		}
	}
}

func TestServerInfoAcceptsIndependentVersionTwoCapabilities(t *testing.T) {
	channels, err := ParseMCPServerInfo(`{"version":2,"channels":[{"name":"main"},{"name":"stdio","proc_affinity":true},{"name":"stateless","stateless":true},{"name":"harpoon","stateless":true,"proc_affinity":true}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 4 || channels[0].Stateless || !channels[1].ProcessAffinity || channels[1].Stateless || !channels[2].Stateless || channels[2].ProcessAffinity || !channels[3].Stateless || !channels[3].ProcessAffinity {
		t.Fatalf("capabilities = %+v", channels)
	}
	if legacy, err := ParseMCPServerInfo(`{"version":1,"channels":[{"name":"main","proc_affinity":true}]}`); err != nil || len(legacy) != 1 || !legacy[0].ProcessAffinity {
		t.Fatalf("v1 = %+v, %v", legacy, err)
	}
}

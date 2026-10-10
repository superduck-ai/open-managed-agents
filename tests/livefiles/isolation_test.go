package livefiles

import (
	"bytes"
	"testing"
)

func TestFilesIsolation(t *testing.T) {
	e := newFilesEnv(t)
	content := []byte("tenant private bytes")
	uploaded := e.upload(t, "private.bin", content, 200)
	record := e.record(t, uploaded.ID)
	output := e.downloadable(t, content)
	for _, otherOrganization := range []bool{false, true} {
		token := e.tenantKey(t, otherOrganization)
		for _, file := range []string{uploaded.ID, output.ExternalID} {
			for _, request := range []struct{ method, suffix string }{{"GET", ""}, {"GET", "/content"}, {"DELETE", ""}} {
				e.request(t, request.method, "/v1/files/"+file+request.suffix, token, "", nil, true, 404)
			}
			for _, query := range []string{"", "?after_id=" + file, "?before_id=" + file} {
				page := e.page(t, token, query)
				if len(page.Data) != 0 || page.HasMore {
					t.Fatal("foreign file leaked through list or cursor")
				}
			}
		}
	}
	e.proof(t, "tenant_access_denied")
	e.assertObject(t, record.S3Key, content)
	e.assertObject(t, output.S3Key, content)
	data, _ := e.request(t, "GET", "/v1/files/"+output.ExternalID+"/content", e.token, "", nil, true, 200)
	if !bytes.Equal(data, content) || len(e.page(t, e.token, "").Data) != 2 {
		t.Fatal("unauthorized requests changed owner data")
	}
	e.proof(t, "owner_data_unchanged")
	e.delete(t, record)
	e.delete(t, output)
}

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStorageFaultIsolationAndRecovery(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "test-signature" || r.URL.RawQuery != "versionId=fixture" {
			t.Error("proxy changed signed request")
		}
		_, _ = io.Copy(w, r.Body)
	}))
	defer upstream.Close()
	fault, err := newStorageFault(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer fault.close()
	request := func(method, address, body string, status int) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, address, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "test-signature")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("status=%d want=%d", response.StatusCode, status)
		}
	}
	request("PUT", fault.control.URL, `{"method":"DELETE","prefix":"/shared/","status":403}`, 400)
	request("PUT", fault.control.URL, `{"method":"DELETE","prefix":"/verify-be/owned","status":403}`, 204)
	request("DELETE", fault.proxy.URL+"/verify-be/owned?versionId=fixture", "", 403)
	request("DELETE", fault.proxy.URL+"/verify-be/other?versionId=fixture", "", 200)
	request("PUT", fault.control.URL, `{}`, 204)
	request("DELETE", fault.proxy.URL+"/verify-be/owned?versionId=fixture", "", 200)
	response, err := http.Get(fault.control.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var attempts []storageAttempt
	if err := json.NewDecoder(response.Body).Decode(&attempts); err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 3 || !attempts[0].Denied || attempts[1].Denied || attempts[2].Denied {
		t.Fatalf("attempts=%+v", attempts)
	}
}

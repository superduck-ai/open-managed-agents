package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

type storageFaultRule struct {
	Method     string `json:"method"`
	Prefix     string `json:"prefix"`
	Status     int    `json:"status"`
	Disconnect bool   `json:"disconnect"`
}

type storageAttempt struct {
	Method string    `json:"method"`
	Path   string    `json:"path"`
	At     time.Time `json:"at"`
	Denied bool      `json:"denied"`
}

type storageFault struct {
	mu             sync.Mutex
	rule           storageFaultRule
	attempts       []storageAttempt
	proxy, control *httptest.Server
}

func newStorageFault(endpoint string) (*storageFault, error) {
	target, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	f := &storageFault{}
	upstream := httputil.NewSingleHostReverseProxy(target)
	upstream.Director = func(r *http.Request) {
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
	}
	f.proxy = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		rule := f.rule
		denied := (rule.Status != 0 || rule.Disconnect) && r.Method == rule.Method && strings.HasPrefix(r.URL.Path, rule.Prefix)
		if len(f.attempts) < 4096 {
			f.attempts = append(f.attempts, storageAttempt{Method: r.Method, Path: r.URL.Path, At: time.Now().UTC(), Denied: denied})
		}
		f.mu.Unlock()
		if denied && rule.Disconnect {
			conn, _, err := http.NewResponseController(w).Hijack()
			if err != nil {
				http.Error(w, "disconnect unavailable", 500)
				return
			}
			_ = conn.Close()
			return
		}
		if denied {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(rule.Status)
			_, _ = w.Write([]byte(`<Error><Code>AccessDenied</Code><Message>Verification fault</Message></Error>`))
			return
		}
		upstream.ServeHTTP(w, r)
	}))
	f.control = httptest.NewServer(http.HandlerFunc(f.controlRequest))
	return f, nil
}

func (f *storageFault) controlRequest(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		var rule storageFaultRule
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&rule); err != nil {
			http.Error(w, "invalid rule", 400)
			return
		}
		if (rule.Status != 0 && rule.Status != 403) || ((rule.Status != 0 || rule.Disconnect) && (rule.Method == "" || !strings.HasPrefix(rule.Prefix, "/verify-be/"))) {
			http.Error(w, "invalid rule", 400)
			return
		}
		f.rule = rule
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.attempts)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *storageFault) close() {
	f.control.Close()
	f.proxy.Close()
}

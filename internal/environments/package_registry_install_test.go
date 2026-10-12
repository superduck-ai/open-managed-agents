package environments

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestRegistryProvisioningInstallsFromLocalRegistry(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("real npm installation requires npm")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("real npm installation requires node")
	}
	for _, mode := range []string{"unconfigured", "default", "scoped"} {
		t.Run(mode, func(t *testing.T) {
			proxy, certificate, requests, authenticated := localPackageRegistry(t)
			dir := t.TempDir()
			original := filepath.Join(dir, "original-npmrc")
			originalText := "registry=https://example.com/public/\n//example.com/npm/:_authToken=fixture-token\n"
			if err := os.WriteFile(original, []byte(originalText), 0600); err != nil {
				t.Fatal(err)
			}
			manager := filepath.Join(dir, "manager")
			managerScript := `#!/usr/bin/env node
const fs = require('fs');
const { spawnSync } = require('child_process');
const manifest = JSON.parse(fs.readFileSync(0, 'utf8'));
const result = spawnSync('npm', ['install', '--global', '--', ...manifest.packages.npm], { encoding: 'utf8' });
process.stderr.write(result.stdout || '');
process.stderr.write(result.stderr || '');
const output = { version: 1, status: result.status === 0 ? 'succeeded' : 'failed', package_count: manifest.packages.npm.length, duration_ms: 0 };
if (result.status !== 0) Object.assign(output, { category: 'package_manager', manager: 'npm', stage: 'install', exit_code: result.status || 1 });
process.stdout.write(JSON.stringify(output));
process.exit(result.status === 0 ? 0 : 10);
`
			if err := os.WriteFile(manager, []byte(managerScript), 0700); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{EnvironmentRunner: config.EnvironmentRunnerConfig{ManagerPath: manager}}
			if mode == "default" {
				cfg.EnvironmentPackages.NPM.Registry = "https://example.com/npm/"
			} else if mode == "scoped" {
				cfg.EnvironmentPackages.NPM.ScopedRegistries = map[string]string{"@example": "https://example.com/npm/"}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "sh", "-c", buildPackageProvisionCommand(cfg))
			for _, value := range os.Environ() {
				key, _, _ := strings.Cut(value, "=")
				lower := strings.ToLower(key)
				if lower != "home" && !strings.HasPrefix(lower, "npm_config_") && !strings.HasSuffix(lower, "proxy") {
					command.Env = append(command.Env, value)
				}
			}
			command.Env = append(command.Env, "HOME="+dir, "NPM_CONFIG_USERCONFIG="+original, "NPM_CONFIG_PREFIX="+dir, "NPM_CONFIG_CACHE="+filepath.Join(dir, "cache"), "NPM_CONFIG_CAFILE="+certificate, "NPM_CONFIG_STRICT_SSL=true", "NPM_CONFIG_HTTPS_PROXY="+proxy, "NPM_CONFIG_FETCH_RETRIES=0", "NPM_CONFIG_FETCH_TIMEOUT=5000", "NPM_CONFIG_AUDIT=false", "NPM_CONFIG_FUND=false", "NO_PROXY=", "no_proxy=")
			command.Stdin = strings.NewReader(`{"version":1,"packages":{"type":"packages","npm":["@example/demo-cli"]}}`)
			var diagnostics bytes.Buffer
			command.Stderr = &diagnostics
			output, err := command.Output()
			wantSuccess := mode != "unconfigured"
			if (err == nil) != wantSuccess || ctx.Err() != nil {
				t.Fatalf("installation success=%t, error=%v, diagnostics=%s", wantSuccess, err, diagnostics.String())
			}
			result, decodeErr := decodePackageProvisioningResult(output)
			if decodeErr != nil || result.Status == nil || (*result.Status == "succeeded") != wantSuccess {
				t.Fatalf("manager result=%s, error=%v", output, decodeErr)
			}
			if got := readPackageSourceFile(t, original); string(got) != originalText {
				t.Fatal("installation changed the original userconfig")
			}
			if !wantSuccess {
				if !strings.Contains(diagnostics.String(), "E404") {
					t.Fatalf("unconfigured source did not return package not found: %s", diagnostics.String())
				}
				return
			}
			installed := filepath.Join(dir, "lib", "node_modules", "@example", "demo-cli", "bin.js")
			version, err := exec.CommandContext(ctx, "node", installed).CombinedOutput()
			if err != nil || string(version) != "demo-cli 1.0.0\n" || requests.Load() < 2 || authenticated.Load() < 2 {
				t.Fatalf("installed CLI=%q, error=%v, registry requests=%d, authenticated=%d", version, err, requests.Load(), authenticated.Load())
			}
		})
	}
}

func localPackageRegistry(t *testing.T) (string, string, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	archive := localPackageArchive(t)
	digest := sha512.Sum512(archive)
	metadata := fmt.Sprintf(`{"name":"@example/demo-cli","dist-tags":{"latest":"1.0.0"},"versions":{"1.0.0":{"name":"@example/demo-cli","version":"1.0.0","bin":{"demo-cli":"bin.js"},"dist":{"tarball":"https://example.com/npm/demo-cli-1.0.0.tgz","integrity":"sha512-%s"}}}}`, base64.StdEncoding.EncodeToString(digest[:]))
	requests, authenticated := new(atomic.Int32), new(atomic.Int32)
	registry := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "example.com" {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		if r.Header.Get("Authorization") == "Bearer fixture-token" {
			authenticated.Add(1)
		}
		if r.URL.Path == "/npm/@example/demo-cli" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, metadata)
			return
		}
		if r.URL.Path == "/npm/demo-cli-1.0.0.tgz" {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archive)
			return
		}
		http.NotFound(w, r)
	}))
	path := filepath.Join(t.TempDir(), "registry-ca.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: registry.Certificate().Raw})
	if err := os.WriteFile(path, certificate, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(registry.Close)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "example.com:443" {
			http.Error(w, "fixture proxy denies other targets", http.StatusForbidden)
			return
		}
		upstream, err := net.DialTimeout("tcp", registry.Listener.Addr().String(), 5*time.Second)
		if err != nil {
			http.Error(w, "fixture unavailable", http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		client, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer client.Close()
		_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		if err := buffer.Flush(); err != nil {
			return
		}
		var copies sync.WaitGroup
		copies.Go(func() { _, _ = io.Copy(upstream, buffer); _ = upstream.Close() })
		_, _ = io.Copy(client, upstream)
		_ = client.Close()
		copies.Wait()
	}))
	t.Cleanup(proxy.Close)
	return proxy.URL, path, requests, authenticated
}

func localPackageArchive(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	compressed := gzip.NewWriter(&output)
	archive := tar.NewWriter(compressed)
	for name, contents := range map[string]string{
		"package/package.json": `{"name":"@example/demo-cli","version":"1.0.0","bin":{"demo-cli":"bin.js"}}`,
		"package/bin.js":       "#!/usr/bin/env node\nprocess.stdout.write('demo-cli 1.0.0\\n');\n",
	} {
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(contents))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(archive, contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

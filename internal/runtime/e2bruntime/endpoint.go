package e2bruntime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
)

var errInvalidServiceEndpoint = errors.New("sandbox service endpoint is unavailable or invalid")

type publishedPort struct {
	ContainerPort int    `json:"containerPort"`
	HostPort      int    `json:"hostPort"`
	URL           string `json:"url"`
}

func (p *E2BProvider) ServiceEndpoint(ctx context.Context, sandboxID string, port int, path string) (endpoint string, err error) {
	ctx, finish := observeProviderCall(ctx, "service_endpoint", sandboxID)
	defer func() { finish(err) }()
	if p.cfg.LocalPortLookup {
		if _, err := p.connect(ctx, sandboxID); err != nil {
			return "", classifySandboxError(err)
		}
		return p.publishedServiceEndpoint(ctx, sandboxID, port, path)
	}
	key := sandboxID + ":" + strconv.Itoa(port)
	host, ok := p.serviceHosts.get(key, time.Now())
	if !ok {
		sandbox, err := p.connect(ctx, sandboxID)
		if err != nil {
			return "", classifySandboxError(err)
		}
		host = sandbox.GetHost(port)
		p.serviceHosts.put(key, host, time.Now())
	}
	scheme := "https://"
	if p.cfg.Debug {
		scheme = "http://"
	}
	return scheme + host + path, nil
}

const serviceHostCacheTTL = 5 * time.Minute

type cachedServiceHost struct {
	host     string
	cachedAt time.Time
}

type serviceHostCache struct {
	mu    sync.Mutex
	hosts map[string]cachedServiceHost
}

func (c *serviceHostCache) get(key string, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.hosts[key]
	if !ok || now.Sub(entry.cachedAt) >= serviceHostCacheTTL {
		return "", false
	}
	return entry.host, true
}

func (c *serviceHostCache) put(key, host string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hosts == nil {
		c.hosts = make(map[string]cachedServiceHost)
	}
	for cachedKey, entry := range c.hosts {
		if now.Sub(entry.cachedAt) >= serviceHostCacheTTL {
			delete(c.hosts, cachedKey)
		}
	}
	c.hosts[key] = cachedServiceHost{host: host, cachedAt: now}
}

func (p *E2BProvider) publishedServiceEndpoint(ctx context.Context, sandboxID string, port int, path string) (string, error) {
	endpoint := strings.TrimRight(p.cfg.APIURL, "/") + "/sandboxes/" + url.PathEscape(sandboxID) + "/ports/" + strconv.Itoa(port)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", errInvalidServiceEndpoint
	}
	request.Header.Set("X-API-Key", p.cfg.APIKey)
	if p.cfg.AccessToken != "" {
		request.Header.Set("Authorization", "Bearer "+p.cfg.AccessToken)
	}
	timeout := p.cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := &http.Client{Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return "", errInvalidServiceEndpoint
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return "", sandboxruntime.ErrSandboxNotFound
	}
	if response.StatusCode != http.StatusOK {
		return "", errInvalidServiceEndpoint
	}
	var mapping publishedPort
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<10)).Decode(&mapping); err != nil || mapping.ContainerPort != port || mapping.HostPort < 1 || mapping.HostPort > 65535 {
		return "", errInvalidServiceEndpoint
	}
	location, err := url.Parse(mapping.URL)
	if err != nil || location.Hostname() == "" || location.User != nil || location.RawQuery != "" || location.Fragment != "" || location.Path != "" && location.Path != "/" || location.Scheme != "http" && location.Scheme != "https" || location.Port() != strconv.Itoa(mapping.HostPort) {
		return "", errInvalidServiceEndpoint
	}
	if p.cfg.LocalServiceHost != "" {
		location.Host = net.JoinHostPort(p.cfg.LocalServiceHost, strconv.Itoa(mapping.HostPort))
	}
	location.Path = path
	return location.String(), nil
}

package codesessions

import (
	"bytes"
	"crypto"
	"crypto/tls"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testUpstreamProxySignerStartTimeout      = 2 * time.Second
	testUpstreamProxyDuplicateSignWindow     = 500 * time.Millisecond
	testUpstreamProxyConcurrentSignerWorkers = 16
)

func TestUpstreamProxyLeafCacheEvictsLeastRecentlyUsed(t *testing.T) {
	t.Parallel()

	authority := newTestUpstreamProxyCertificateAuthority(t)
	now := time.Now().UTC()
	cached := upstreamProxyCachedLeaf{refreshAt: now.Add(time.Hour)}
	for index := range maxUpstreamProxyLeafCacheSize {
		authority.leafCache.Add(testUpstreamProxyLeafCacheHost(index), cached)
	}
	if got := authority.leafCache.Len(); got != maxUpstreamProxyLeafCacheSize {
		t.Fatalf("leaf cache size = %d, want %d", got, maxUpstreamProxyLeafCacheSize)
	}

	// 第一个域名原本最老；通过正式读取路径命中一次后，它应成为最近使用项，第二个域名转为淘汰候选。
	mostRecentlyUsedHost := testUpstreamProxyLeafCacheHost(0)
	if _, err := authority.certificateForHost(mostRecentlyUsedHost, now); err != nil {
		t.Fatalf("touch cached leaf: %v", err)
	}
	overflowHost := "overflow.example.com"
	if _, err := authority.certificateForHost(overflowHost, now); err != nil {
		t.Fatalf("add overflow leaf: %v", err)
	}

	if got := authority.leafCache.Len(); got != maxUpstreamProxyLeafCacheSize {
		t.Fatalf("leaf cache size after overflow = %d, want %d", got, maxUpstreamProxyLeafCacheSize)
	}
	assertUpstreamProxyLeafCacheContains(t, authority, mostRecentlyUsedHost, true)
	assertUpstreamProxyLeafCacheContains(t, authority, testUpstreamProxyLeafCacheHost(1), false)
	assertUpstreamProxyLeafCacheContains(t, authority, overflowHost, true)
}

func TestUpstreamProxyCertificateForHostCoalescesConcurrentSigning(t *testing.T) {
	t.Parallel()

	authority := newTestUpstreamProxyCertificateAuthority(t)
	signer := newTestBlockingCountingSigner(authority.signer, testUpstreamProxyConcurrentSignerWorkers)
	authority.signer = signer

	start := make(chan struct{})
	entered := make(chan struct{})
	results := make(chan testUpstreamProxyLeafResult, testUpstreamProxyConcurrentSignerWorkers)
	var workers sync.WaitGroup
	for index := range testUpstreamProxyConcurrentSignerWorkers {
		host := "api.example.com"
		if index%2 == 1 {
			host = "API.Example.COM."
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			// 无缓冲信号保证测试线程观察到每个 worker 已越过启动栅栏，缩小调度造成的假阴性窗口。
			entered <- struct{}{}
			certificate, err := authority.certificateForHost(host, time.Now().UTC())
			results <- testUpstreamProxyLeafResult{certificate: certificate, err: err}
		}()
	}
	registerTestSignerCleanup(t, signer, &workers)

	close(start)
	for range testUpstreamProxyConcurrentSignerWorkers {
		<-entered
	}
	waitForTestSignerStart(t, signer.started)
	assertNoAdditionalTestSignerStart(t, signer.started)

	signer.releaseAll()
	workers.Wait()
	close(results)
	assertTestLeafResultsShareCertificate(t, results, testUpstreamProxyConcurrentSignerWorkers)
	if got := signer.calls.Load(); got != 1 {
		t.Fatalf("leaf signing calls = %d, want 1", got)
	}
}

func TestUpstreamProxyCertificateForHostSignsDifferentHostsConcurrently(t *testing.T) {
	t.Parallel()

	authority := newTestUpstreamProxyCertificateAuthority(t)
	signer := newTestBlockingCountingSigner(authority.signer, 2)
	authority.signer = signer
	results := make(chan testUpstreamProxyLeafResult, 2)
	var workers sync.WaitGroup
	registerTestSignerCleanup(t, signer, &workers)

	startWorker := func(host string) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			certificate, err := authority.certificateForHost(host, time.Now().UTC())
			results <- testUpstreamProxyLeafResult{certificate: certificate, err: err}
		}()
	}
	startWorker("first.example.com")
	waitForTestSignerStart(t, signer.started)

	// 第一个域名仍阻塞在根证书签名时，第二个域名也必须进入 Sign；若存在全局 leaf 锁，这里会超时。
	startWorker("second.example.com")
	waitForTestSignerStart(t, signer.started)

	signer.releaseAll()
	workers.Wait()
	close(results)
	for result := range results {
		if result.err != nil {
			t.Fatalf("certificateForHost() error = %v", result.err)
		}
		if result.certificate == nil || result.certificate.Leaf == nil {
			t.Fatal("certificateForHost() returned a nil leaf certificate")
		}
	}
	if got := signer.calls.Load(); got != 2 {
		t.Fatalf("leaf signing calls = %d, want 2", got)
	}
}

func TestUpstreamProxyLeafCacheRefreshBoundary(t *testing.T) {
	t.Parallel()

	authority := newTestUpstreamProxyCertificateAuthority(t)
	signer := newTestBlockingCountingSigner(authority.signer, 2)
	signer.releaseAll()
	authority.signer = signer
	now := time.Now().UTC()

	first, err := authority.certificateForHost("refresh.example.com", now)
	if err != nil {
		t.Fatalf("issue first leaf: %v", err)
	}
	beforeRefresh, err := authority.certificateForHost(
		"refresh.example.com",
		now.Add(upstreamProxyLeafRefreshAfter-time.Nanosecond),
	)
	if err != nil {
		t.Fatalf("read leaf before refresh boundary: %v", err)
	}
	if !bytes.Equal(first.Leaf.Raw, beforeRefresh.Leaf.Raw) {
		t.Fatal("leaf changed before refreshAt")
	}
	if got := signer.calls.Load(); got != 1 {
		t.Fatalf("leaf signing calls before refreshAt = %d, want 1", got)
	}

	atRefresh, err := authority.certificateForHost("refresh.example.com", now.Add(upstreamProxyLeafRefreshAfter))
	if err != nil {
		t.Fatalf("refresh leaf at boundary: %v", err)
	}
	if bytes.Equal(first.Leaf.Raw, atRefresh.Leaf.Raw) {
		t.Fatal("leaf was reused at refreshAt boundary")
	}
	if got := signer.calls.Load(); got != 2 {
		t.Fatalf("leaf signing calls at refreshAt = %d, want 2", got)
	}
}

type testUpstreamProxyLeafResult struct {
	certificate *tls.Certificate
	err         error
}

type testBlockingCountingSigner struct {
	crypto.Signer
	started     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	calls       atomic.Int64
}

func newTestBlockingCountingSigner(signer crypto.Signer, startBuffer int) *testBlockingCountingSigner {
	return &testBlockingCountingSigner{
		Signer:  signer,
		started: make(chan struct{}, startBuffer),
		release: make(chan struct{}),
	}
}

func (s *testBlockingCountingSigner) Sign(random io.Reader, digest []byte, options crypto.SignerOpts) ([]byte, error) {
	s.calls.Add(1)
	s.started <- struct{}{}
	<-s.release
	return s.Signer.Sign(random, digest, options)
}

func (s *testBlockingCountingSigner) releaseAll() {
	s.releaseOnce.Do(func() {
		close(s.release)
	})
}

func newTestUpstreamProxyCertificateAuthority(t *testing.T) *upstreamProxyCertificateAuthority {
	t.Helper()
	authority, err := generateUpstreamProxyCertificateAuthority()
	if err != nil {
		t.Fatalf("generate test certificate authority: %v", err)
	}
	return authority
}

func testUpstreamProxyLeafCacheHost(index int) string {
	return fmt.Sprintf("host-%04d.example.com", index)
}

func assertUpstreamProxyLeafCacheContains(
	t *testing.T,
	authority *upstreamProxyCertificateAuthority,
	host string,
	want bool,
) {
	t.Helper()
	_, got := authority.leafCache.Peek(host)
	if got != want {
		t.Fatalf("leaf cache contains %q = %t, want %t", host, got, want)
	}
}

func registerTestSignerCleanup(t *testing.T, signer *testBlockingCountingSigner, workers *sync.WaitGroup) {
	t.Helper()
	t.Cleanup(func() {
		signer.releaseAll()
		workers.Wait()
	})
}

func waitForTestSignerStart(t *testing.T, started <-chan struct{}) {
	t.Helper()
	timer := time.NewTimer(testUpstreamProxySignerStartTimeout)
	defer timer.Stop()
	select {
	case <-started:
	case <-timer.C:
		t.Fatal("timed out waiting for leaf certificate signing to start")
	}
}

func assertNoAdditionalTestSignerStart(t *testing.T, started <-chan struct{}) {
	t.Helper()
	timer := time.NewTimer(testUpstreamProxyDuplicateSignWindow)
	defer timer.Stop()
	select {
	case <-started:
		t.Fatal("same hostname started more than one leaf certificate signing operation")
	case <-timer.C:
	}
}

func assertTestLeafResultsShareCertificate(
	t *testing.T,
	results <-chan testUpstreamProxyLeafResult,
	wantCount int,
) {
	t.Helper()
	var raw []byte
	count := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("certificateForHost() error = %v", result.err)
		}
		if result.certificate == nil || result.certificate.Leaf == nil {
			t.Fatal("certificateForHost() returned a nil leaf certificate")
		}
		if raw == nil {
			raw = result.certificate.Leaf.Raw
		} else if !bytes.Equal(raw, result.certificate.Leaf.Raw) {
			t.Fatal("concurrent callers received different leaf certificates")
		}
		count++
	}
	if count != wantCount {
		t.Fatalf("leaf result count = %d, want %d", count, wantCount)
	}
}

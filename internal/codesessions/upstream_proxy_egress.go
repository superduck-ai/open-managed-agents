package codesessions

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// dialUpstreamProxyEgress keeps DNS/SSRF validation in OMA: even through a
// configured HTTP proxy, CONNECT carries the previously validated IP address.
// Proxy credentials belong to the server's environment, never to the sandbox.
func dialUpstreamProxyEgress(ctx context.Context, resolvedTarget string, proxyURL *url.URL) (net.Conn, error) {
	if proxyURL == nil {
		return dialUpstreamProxyTarget(ctx, resolvedTarget)
	}
	if proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
		return nil, errors.New("upstream HTTPS proxy must use http or https")
	}
	port := proxyURL.Port()
	if port == "" {
		port = "80"
		if proxyURL.Scheme == "https" {
			port = "443"
		}
	}
	ctx, cancel := context.WithTimeout(ctx, upstreamProxyDialTimeout)
	defer cancel()
	dialer := &net.Dialer{KeepAlive: 30 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(proxyURL.Hostname(), port))
	if err != nil {
		return nil, err
	}
	rawConnection := connection
	stopCancellation := context.AfterFunc(ctx, func() { _ = rawConnection.Close() })
	defer stopCancellation()
	ready := false
	defer func() {
		if !ready {
			_ = connection.Close()
		}
	}()
	if proxyURL.Scheme == "https" {
		tlsConnection := tls.Client(connection, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: proxyURL.Hostname()})
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		connection = tlsConnection
	}
	request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: resolvedTarget}, Host: resolvedTarget, Header: make(http.Header)}
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		credentials := base64.StdEncoding.EncodeToString([]byte(proxyURL.User.Username() + ":" + password))
		request.Header.Set("Proxy-Authorization", "Basic "+credentials)
	}
	if err := request.Write(connection); err != nil {
		return nil, err
	}
	response, err := http.ReadResponse(bufio.NewReader(io.LimitReader(connection, maxUpstreamProxyConnectHeadBytes)), request)
	if err != nil {
		return nil, errors.New("read upstream HTTPS proxy CONNECT response failed")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream HTTPS proxy CONNECT returned status %d", response.StatusCode)
	}
	if !stopCancellation() || ctx.Err() != nil {
		return nil, ctx.Err()
	}
	ready = true
	return connection, nil
}

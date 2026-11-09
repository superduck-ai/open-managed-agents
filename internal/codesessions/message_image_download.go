package codesessions

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/messagemedia"
)

func messageImageURLAllowed(target *url.URL) bool {
	return target.Hostname() != "" && target.User == nil && (target.Scheme == "http" || target.Scheme == "https")
}

func newMessageImageHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		target, err := resolveProxyTarget(ctx, address, false, false)
		if err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, network, target)
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !messageImageURLAllowed(request.URL) {
				return errMessageImageRejected
			}
			return nil
		},
	}
}

func (s *Service) downloadMessageImage(ctx context.Context, source string) (io.ReadCloser, string, error) {
	target, err := url.Parse(source)
	if err != nil || !messageImageURLAllowed(target) {
		return nil, "", errMessageImageRejected
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, "", errMessageImageRejected
	}
	request.Header.Set("Accept", "image/png, image/jpeg, image/webp, image/gif")
	response, err := s.attachmentHTTP.Do(request)
	if err != nil {
		if failure, ok := errors.AsType[*url.Error](err); ok {
			err = failure.Err
		}
		if failure, ok := errors.AsType[*upstreamProxyTargetError](err); ok && failure.status < 500 {
			return nil, "", errMessageImageRejected
		}
		return nil, "", fmt.Errorf("download message image: %w", err)
	}
	accepted := false
	defer func() {
		if !accepted {
			response.Body.Close()
		}
	}()
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return nil, "", fmt.Errorf("download message image: HTTP %d", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK || (s.attachmentMaxBytes > 0 && response.ContentLength > s.attachmentMaxBytes) {
		return nil, "", errMessageImageRejected
	}
	var body io.Reader = response.Body
	if s.attachmentMaxBytes > 0 {
		body = http.MaxBytesReader(nil, response.Body, s.attachmentMaxBytes)
	}
	reader := bufio.NewReader(body)
	head, err := reader.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return nil, "", errMessageImageRejected
		}
		return nil, "", fmt.Errorf("read message image: %w", err)
	}
	mediaType := http.DetectContentType(head)
	if !messagemedia.SupportedMediaType(messagemedia.BlockImage, mediaType) {
		return nil, "", errMessageImageRejected
	}
	if declared := response.Header.Get("Content-Type"); declared != "" {
		kind, _, parseErr := mime.ParseMediaType(declared)
		if parseErr != nil || (kind != "application/octet-stream" && kind != mediaType) {
			return nil, "", errMessageImageRejected
		}
	}
	accepted = true
	return struct {
		io.Reader
		io.Closer
	}{reader, response.Body}, mediaType, nil
}

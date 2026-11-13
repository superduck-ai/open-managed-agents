package pricing

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

type priceSource struct {
	name  string
	url   string
	parse func([]byte) ([]CatalogPrice, error)
}

func (s *Store) Start(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	if s.config.ReadOnly {
		workers.Go(func() { s.watch(ctx) })
	} else {
		for _, source := range []priceSource{{"openrouter", endpoint, Parse}, {"narrows", narrowsEndpoint, ParseNarrows}} {
			workers.Go(func() { s.refreshLoop(ctx, source) })
		}
	}
	return func() { cancel(); workers.Wait() }
}

func (s *Store) refreshLoop(ctx context.Context, source priceSource) {
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	fetched := s.current.Load().file.Sources[source.name].FetchedAt
	delay := max(time.Duration(0), time.Until(fetched.Add(s.config.RefreshInterval)))
	failures := 0
	for wait(ctx, delay) {
		err := s.refresh(ctx, client, source)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			failures++
			delay = retryDelay(failures)
			s.logger.WarnContext(ctx, "refresh model prices failed", "source", source.name, "failures", failures, "error", err)
			continue
		}
		failures = 0
		catalog := s.current.Load().file.Sources[source.name]
		s.logger.InfoContext(ctx, "model prices refreshed", "source", source.name, "revision", catalog.Revision, "model_count", len(catalog.Models), "fetched_at", catalog.FetchedAt)
		delay = time.Duration(float64(s.config.RefreshInterval) * (0.9 + rand.Float64()*0.2))
	}
}

func (s *Store) refresh(ctx context.Context, client *http.Client, source priceSource) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.url, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("模型价格源返回非成功状态")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(body) > 8<<20 {
		return errors.New("模型价格目录超过大小限制")
	}
	prices, err := source.parse(body)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Replace(source.name, prices)
}

func (s *Store) watch(ctx context.Context) {
	for wait(ctx, s.config.ReloadInterval) {
		if err := s.Reload(); err != nil {
			s.logger.WarnContext(ctx, "reload model prices failed", "error", err)
		}
	}
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}

func retryDelay(failures int) time.Duration {
	delays := [...]time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, time.Hour}
	return delays[min(max(failures-1, 0), len(delays)-1)]
}

package pricing

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(config.PricingConfig{SnapshotFile: filepath.Join(t.TempDir(), "prices.json"), RefreshInterval: time.Hour, ReloadInterval: time.Millisecond}, nil)
}

func testPrices(t *testing.T, rate string) []CatalogPrice {
	t.Helper()
	prices, err := Parse([]byte(`{"data":[{"id":"vendor/model","pricing":{"prompt":"` + rate + `","completion":"0"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return prices
}

func TestStoreRejectsInvalidUpdates(t *testing.T) {
	s := testStore(t)
	prices := testPrices(t, "0.000001")
	if err := s.Replace("openrouter", prices); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.config.SnapshotFile)
	invalid := testPrices(t, "0.000002")
	invalid[0].Price.Rates.Input = new("-1")
	for _, entries := range [][]CatalogPrice{nil, {prices[0], prices[0]}, invalid} {
		if err := s.Replace("openrouter", entries); err == nil {
			t.Fatal("无效更新成功")
		}
		after, _ := os.ReadFile(s.config.SnapshotFile)
		if string(before) != string(after) {
			t.Fatal("无效更新覆盖文件")
		}
		if *s.Lookup("openrouter", "model").Rates.Input != "1.000000000000" {
			t.Fatal("无效更新覆盖内存")
		}
	}
	if err := os.WriteFile(s.config.SnapshotFile, []byte(`{"schema_version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err == nil {
		t.Fatal("损坏文件加载成功")
	}
	if s.Lookup("openrouter", "model") == nil {
		t.Fatal("损坏文件清空旧快照")
	}
}

func TestStoreWriteFailureKeepsSnapshot(t *testing.T) {
	s := testStore(t)
	if err := s.Replace("openrouter", testPrices(t, "0.000001")); err != nil {
		t.Fatal(err)
	}
	s.config.SnapshotFile = t.TempDir()
	if err := s.Replace("openrouter", testPrices(t, "0.000002")); err == nil {
		t.Fatal("目录被替换为文件")
	}
	if *s.Lookup("openrouter", "model").Rates.Input != "1.000000000000" {
		t.Fatal("写入失败发布了新快照")
	}
}

func TestStoreAtomicSourcesAndSnapshots(t *testing.T) {
	s := testStore(t)
	prices := testPrices(t, "0.000001")
	narrows := testPrices(t, "0.0000072")
	narrows[0].Price.Source = "narrows"
	narrows[0].Price.SourceCurrency = "CNY"
	narrows[0].Price.ExchangeRate = "7.2"
	var writers sync.WaitGroup
	for source, entries := range map[string][]CatalogPrice{"openrouter": prices, "narrows": narrows} {
		writers.Go(func() {
			if err := s.Replace(source, entries); err != nil {
				t.Error(err)
			}
		})
	}
	writers.Wait()
	old := s.Lookup("openrouter", "model")
	if old == nil || s.Lookup("narrows", "vendor/model") == nil || s.Lookup("narrows", "model") != nil {
		t.Fatal("来源或别名错误")
	}
	reader := NewStore(s.config, nil)
	if err := reader.Reload(); err != nil {
		t.Fatal(err)
	}
	if reader.Lookup("narrows", "vendor/model") == nil {
		t.Fatal("重启丢失另一来源")
	}
	if err := s.Replace("openrouter", testPrices(t, "0.000002")); err != nil {
		t.Fatal(err)
	}
	if *old.Rates.Input != "1.000000000000" {
		t.Fatal("在途快照变化")
	}
	*old.Rates.Input = "999"
	if *s.Lookup("openrouter", "model").Rates.Input != "2.000000000000" {
		t.Fatal("外部修改污染快照")
	}
	if err := reader.Reload(); err != nil {
		t.Fatal(err)
	}
	if *reader.Lookup("openrouter", "model").Rates.Input != "2.000000000000" {
		t.Fatal("读者未热加载")
	}
}

func TestRefreshFailureAndCancellation(t *testing.T) {
	s := testStore(t)
	if err := s.Replace("openrouter", testPrices(t, "0.000001")); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	source := priceSource{"openrouter", server.URL, Parse}
	if err := s.refresh(t.Context(), server.Client(), source); err == nil {
		t.Fatal("失败响应被接受")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.refresh(ctx, server.Client(), source); err == nil {
		t.Fatal("取消未生效")
	}
	if s.Lookup("openrouter", "model") == nil {
		t.Fatal("刷新失败清空数据")
	}
	if wait(ctx, time.Hour) {
		t.Fatal("取消继续循环")
	}
}

func TestRefreshSuccessfulAndReadOnlyLifecycle(t *testing.T) {
	s := testStore(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/model","pricing":{"prompt":"0.000003","completion":"0"}}]}`))
	}))
	defer server.Close()
	if err := s.refresh(t.Context(), server.Client(), priceSource{"openrouter", server.URL, Parse}); err != nil {
		t.Fatal(err)
	}
	readerCfg := s.config
	readerCfg.ReadOnly = true
	reader := NewStore(readerCfg, nil)
	stop := reader.Start(t.Context())
	defer stop()
	deadline := time.After(time.Second)
	for reader.Lookup("openrouter", "model") == nil {
		select {
		case <-deadline:
			t.Fatal("只读实例未加载文件")
		case <-time.After(time.Millisecond):
		}
	}
	if *reader.Lookup("openrouter", "model").Rates.Input != "3.000000000000" {
		t.Fatal("刷新价格错误")
	}
}

func TestSnapshotTamperingRejected(t *testing.T) {
	s := testStore(t)
	if err := s.Replace("openrouter", testPrices(t, "0.000001")); err != nil {
		t.Fatal(err)
	}
	file := s.current.Load().file
	entry := file.Sources["openrouter"].Models["vendor/model"]
	entry.Price.Rates = nil
	file.Sources["openrouter"].Models["vendor/model"] = entry
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeSnapshot(data); err == nil {
		t.Fatal("摘要未校验")
	}
}

func TestReadOnlyRejectsWrite(t *testing.T) {
	s := testStore(t)
	s.config.ReadOnly = true
	if err := s.Replace("openrouter", testPrices(t, "0.000001")); err == nil {
		t.Fatal("只读实例写入成功")
	}
	if s.Lookup("openrouter", "model") != nil {
		t.Fatal("只读实例发布了更新")
	}
}

func TestRefreshRejectsOversizeAndTimeout(t *testing.T) {
	for _, mode := range []string{"oversize", "timeout", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "oversize":
					_, _ = w.Write(make([]byte, (8<<20)+1))
				case "timeout":
					<-r.Context().Done()
				default:
					_, _ = w.Write([]byte(`{"data":null}`))
				}
			}))
			defer server.Close()
			client := server.Client()
			client.Timeout = 100 * time.Millisecond
			if err := s.refresh(t.Context(), client, priceSource{"openrouter", server.URL, Parse}); err == nil {
				t.Fatal("无效响应覆盖目录")
			}
			if s.Lookup("openrouter", "model") != nil {
				t.Fatal("失败刷新发布了目录")
			}
		})
	}
}

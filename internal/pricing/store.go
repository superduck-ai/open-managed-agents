package pricing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

type CatalogPrice struct {
	Model    string             `json:"model"`
	Price    billing.ModelPrice `json:"price"`
	Original jsontext.Value     `json:"original"`
}

type sourceSnapshot struct {
	FetchedAt time.Time               `json:"fetched_at"`
	Revision  string                  `json:"revision"`
	Models    map[string]CatalogPrice `json:"models"`
}

type fileSnapshot struct {
	SchemaVersion int                       `json:"schema_version"`
	Sources       map[string]sourceSnapshot `json:"sources"`
}

type snapshot struct {
	file    fileSnapshot
	indexes map[string]map[string]billing.ModelPrice
}

type Store struct {
	config  config.PricingConfig
	logger  *slog.Logger
	current atomic.Pointer[snapshot]
	writeMu sync.Mutex
}

func NewStore(cfg config.PricingConfig, logger *slog.Logger) *Store {
	s := &Store{config: cfg, logger: logging.LoggerOrDefault(logger)}
	s.current.Store(&snapshot{file: fileSnapshot{SchemaVersion: 1, Sources: map[string]sourceSnapshot{}}, indexes: map[string]map[string]billing.ModelPrice{}})
	return s
}

func (s *Store) Lookup(source, model string) *billing.ModelPrice {
	if s == nil {
		return nil
	}
	p, ok := s.current.Load().indexes[source][model]
	if !ok {
		return nil
	}
	return p.Clone()
}

func (s *Store) Replace(source string, entries []CatalogPrice) error {
	if s.config.ReadOnly {
		return errors.New("只读实例不能更新价格文件")
	}
	if source != "openrouter" && source != "narrows" {
		return errors.New("未知价格来源")
	}
	if len(entries) == 0 {
		return errors.New("价格目录为空")
	}
	models := make(map[string]CatalogPrice, len(entries))
	for _, entry := range entries {
		if _, exists := models[entry.Model]; exists {
			return errors.New("价格目录模型重复")
		}
		models[entry.Model] = entry
	}
	next := sourceSnapshot{FetchedAt: time.Now().UTC(), Models: models}
	data, err := json.Marshal(models, json.Deterministic(true))
	if err != nil {
		return err
	}
	next.Revision = digest(data)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	file := s.current.Load().file
	file.Sources = maps.Clone(file.Sources)
	file.Sources[source] = next
	data, err = json.Marshal(file, json.Deterministic(true))
	if err != nil {
		return err
	}
	prepared, err := decodeSnapshot(data)
	if err != nil {
		return err
	}
	if err := writeSnapshot(s.config.SnapshotFile, data); err != nil {
		return err
	}
	s.current.Store(prepared)
	return nil
}

func digest(data []byte) string {
	value := sha256.Sum256(data)
	return hex.EncodeToString(value[:])
}

func decodeSnapshot(data []byte) (*snapshot, error) {
	var file fileSnapshot
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if file.SchemaVersion != 1 || len(file.Sources) == 0 {
		return nil, errors.New("价格文件版本或来源无效")
	}
	result := &snapshot{file: file, indexes: make(map[string]map[string]billing.ModelPrice)}
	for source, catalog := range file.Sources {
		index, err := validateSource(source, catalog)
		if err != nil {
			return nil, err
		}
		result.indexes[source] = index
	}
	return result, nil
}

func validateSource(source string, catalog sourceSnapshot) (map[string]billing.ModelPrice, error) {
	if source != "openrouter" && source != "narrows" || catalog.FetchedAt.IsZero() || len(catalog.Models) == 0 {
		return nil, errors.New("价格来源快照无效")
	}
	data, err := json.Marshal(catalog.Models, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	if digest(data) != catalog.Revision {
		return nil, errors.New("价格内容摘要不匹配")
	}
	index := make(map[string]billing.ModelPrice, len(catalog.Models))
	for model, entry := range catalog.Models {
		if err := validatePrice(source, model, entry); err != nil {
			return nil, err
		}
		price := entry.Price
		price.FetchedAt = catalog.FetchedAt
		index[model] = price
	}
	if source == "openrouter" {
		index = billing.PriceAliases(index)
	}
	return index, nil
}

func validatePrice(source, model string, entry CatalogPrice) error {
	p := entry.Price
	if model == "" || entry.Model != model || p.SourceModel != model || p.Source != source || p.Rates == nil || p.Version == "" {
		return errors.New("价格条目身份无效")
	}
	if p.ProviderUUID != "" || !p.PricedAt.IsZero() || !p.FetchedAt.IsZero() {
		return errors.New("价格目录不能包含请求身份或时间")
	}
	if source == "narrows" && (p.SourceCurrency != "CNY" || p.ExchangeRate != NarrowsCNYPerUSD) || source == "openrouter" && (p.SourceCurrency != "" && p.SourceCurrency != "USD" || p.ExchangeRate != "") {
		return errors.New("价格币种或汇率无效")
	}
	if err := p.Rates.Validate(); err != nil {
		return err
	}
	for _, rule := range p.Overrides {
		if err := rule.Rates.Validate(); err != nil {
			return err
		}
		if err := validateOverride(override{MinPromptTokens: rule.MinPromptTokens, UTCStart: rule.UTCStart, UTCEnd: rule.UTCEnd, UTCDays: rule.UTCDays}); err != nil {
			return err
		}
		if rule.MaxPromptTokens != nil && (*rule.MaxPromptTokens < 0 || rule.MinPromptTokens != nil && *rule.MaxPromptTokens <= *rule.MinPromptTokens) {
			return errors.New("价格分档上界无效")
		}
	}
	return nil
}

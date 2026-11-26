package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Cost is estimated from a public price catalog, the way the original does it:
// models.dev publishes per-model rates, and a session's billed buckets are
// multiplied by them. CPA has no prices of its own, so the catalog is fetched
// once, cached on disk, and refreshed on a long interval.
//
// Nothing here is on the request path. A missing or stale catalog only means the
// dashboard shows no cost, never a delay on traffic.

const (
	priceCatalogURL   = "https://models.dev/api.json"
	priceCatalogTTL   = 6 * time.Hour
	priceCatalogRetry = 10 * time.Minute
	priceCatalogFile  = "prices.json"
	priceFetchTimeout = 20 * time.Second
)

// priceTriple is one model's per-million-token rates in USD.
type priceTriple struct {
	Hit   float64 `json:"hit"`
	Miss  float64 `json:"miss"`
	Write float64 `json:"write"`
	Out   float64 `json:"out"`
}

// priceBook maps provider to model to rates, plus the canonical id each entry
// declares so an alias can be resolved to the entry it mirrors.
type priceBook struct {
	Prices    map[string]map[string]priceTriple `json:"prices"`
	Canonical map[string]map[string]string      `json:"canonical"`
}

type priceCatalog struct {
	mu       sync.RWMutex
	book     *priceBook
	path     string
	loadedAt time.Time
	failedAt time.Time
	client   *http.Client
}

func newPriceCatalog(dir string) *priceCatalog {
	path := ""
	if dir != "" {
		path = filepath.Join(dir, priceCatalogFile)
	}
	return &priceCatalog{
		path:   path,
		client: &http.Client{Timeout: priceFetchTimeout},
	}
}

// startLoads the cached book and refreshes it when it is old. It returns at
// once: the dashboard renders without costs until the book is ready.
func (c *priceCatalog) start() {
	c.loadFromDisk()
	go func() {
		if c.book == nil || time.Since(c.loadedAt) > priceCatalogTTL {
			c.fetch()
		}
	}()
}

func (c *priceCatalog) loadFromDisk() {
	if c.path == "" {
		return
	}
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	var book priceBook
	if err := json.Unmarshal(raw, &book); err != nil || len(book.Prices) == 0 {
		return
	}
	c.mu.Lock()
	c.book = &book
	if info, errStat := os.Stat(c.path); errStat == nil {
		c.loadedAt = info.ModTime()
	}
	c.mu.Unlock()
}

// fetch reads the catalog and replaces the book. A failure is remembered so a
// broken network is not retried on every render.
func (c *priceCatalog) fetch() {
	c.mu.RLock()
	recent := c.failedAt != (time.Time{}) && time.Since(c.failedAt) < priceCatalogRetry
	c.mu.RUnlock()
	if recent {
		return
	}
	resp, err := c.client.Get(priceCatalogURL)
	if err != nil {
		c.markFailed()
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.markFailed()
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		c.markFailed()
		return
	}
	book := parsePriceBook(body)
	if book == nil || len(book.Prices) == 0 {
		c.markFailed()
		return
	}
	c.mu.Lock()
	c.book = book
	c.loadedAt = time.Now()
	c.failedAt = time.Time{}
	c.mu.Unlock()
	c.saveToDisk(book)
}

func (c *priceCatalog) markFailed() {
	c.mu.Lock()
	c.failedAt = time.Now()
	c.mu.Unlock()
}

func (c *priceCatalog) saveToDisk(book *priceBook) {
	if c.path == "" {
		return
	}
	raw, err := json.Marshal(book)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return
	}
	// A partial write would be read back as a corrupt catalog, so the file is
	// replaced atomically.
	tmp, err := os.CreateTemp(filepath.Dir(c.path), "prices-*.json")
	if err != nil {
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(name)
		return
	}
	tmp.Close()
	if err := os.Rename(name, c.path); err != nil {
		os.Remove(name)
	}
}

// parsePriceBook reads the models.dev document: provider -> models -> cost.
func parsePriceBook(raw []byte) *priceBook {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil
	}
	book := &priceBook{
		Prices:    map[string]map[string]priceTriple{},
		Canonical: map[string]map[string]string{},
	}
	for provider, providerRaw := range root {
		var providerDoc struct {
			Models map[string]struct {
				Cost *struct {
					Input      *float64 `json:"input"`
					Output     *float64 `json:"output"`
					CacheRead  *float64 `json:"cache_read"`
					CacheWrite *float64 `json:"cache_write"`
				} `json:"cost"`
				CanonicalID string `json:"canonical_model_id"`
			} `json:"models"`
		}
		if err := json.Unmarshal(providerRaw, &providerDoc); err != nil {
			continue
		}
		for model, entry := range providerDoc.Models {
			if entry.Cost == nil || entry.Cost.Input == nil || entry.Cost.Output == nil {
				continue
			}
			miss := *entry.Cost.Input
			triple := priceTriple{Miss: miss, Out: *entry.Cost.Output}
			triple.Hit = miss
			if entry.Cost.CacheRead != nil {
				triple.Hit = *entry.Cost.CacheRead
			}
			triple.Write = miss
			if entry.Cost.CacheWrite != nil {
				triple.Write = *entry.Cost.CacheWrite
			}
			branch, ok := book.Prices[provider]
			if !ok {
				branch = map[string]priceTriple{}
				book.Prices[provider] = branch
			}
			branch[model] = triple
			if entry.CanonicalID != "" {
				canonical, ok := book.Canonical[provider]
				if !ok {
					canonical = map[string]string{}
					book.Canonical[provider] = canonical
				}
				canonical[model] = entry.CanonicalID
			}
		}
	}
	return book
}

// costOf computes the estimated USD cost of one session, or nil when the model
// is not priced. The rates are per million tokens, which is how the catalog
// publishes them.
func (c *priceCatalog) costOf(provider, model string, uncached, cacheRead, cacheWrite, output int64) *float64 {
	c.mu.RLock()
	book := c.book
	c.mu.RUnlock()
	if book == nil {
		return nil
	}
	triple, ok := lookupPrice(book, provider, model)
	if !ok {
		return nil
	}
	total := (float64(uncached)*triple.Miss +
		float64(cacheRead)*triple.Hit +
		float64(cacheWrite)*triple.Write +
		float64(output)*triple.Out) / 1_000_000
	return &total
}

// lookupPrice finds a rate, trying the provider the request went through and
// then every branch that prices that model, because a model id is often served
// by a compatibility provider under a different name.
func lookupPrice(book *priceBook, provider, model string) (priceTriple, bool) {
	candidates := []string{model, bareModel(model)}
	if branch, ok := book.Prices[strings.ToLower(provider)]; ok {
		for _, name := range candidates {
			if triple, ok := branch[name]; ok {
				return triple, true
			}
		}
	}
	for providerName, branch := range book.Prices {
		for _, name := range candidates {
			if triple, ok := branch[name]; ok {
				_ = providerName
				return triple, true
			}
		}
	}
	return priceTriple{}, false
}

// bareModel drops a vendor prefix, because catalogs often list the model
// without the routing prefix a provider adds.
func bareModel(model string) string {
	if index := strings.LastIndex(model, "/"); index >= 0 {
		return model[index+1:]
	}
	return model
}

// applyCost adds the estimated price of one session to its projection.
//
// The shape is the one the client reads: cost buckets per provider and model,
// and a costRead face that says whether the figure is ready and where it came
// from. A model that is not in the catalog is reported as a miss rather than
// silently priced at zero.
func applyCost(value *projection, catalog *priceCatalog, snap *snapshot) {
	if value == nil || value.ContextTimeline == nil || snap == nil {
		return
	}
	provider := snap.Provider
	if provider == "" {
		provider = "unknown"
	}
	model := snap.Model
	if model == "" {
		return
	}
	outputTokens := snap.OutputTokens + snap.ReasoningTokens
	total := catalog.costOf(provider, model, snap.UncachedTokens, snap.CacheReadTokens, snap.CacheWriteTokens, outputTokens)

	value.ContextTimeline.Cost = map[string]any{
		provider: map[string]any{
			model: map[string]any{
				"off": map[string]any{
					"uncached":   snap.UncachedTokens,
					"cacheRead":  snap.CacheReadTokens,
					"cacheWrite": snap.CacheWriteTokens,
					"output":     outputTokens,
				},
			},
		},
	}
	if total == nil {
		value.ContextTimeline.CostRead = map[string]any{
			"usd":   nil,
			"ready": true,
			"misses": []map[string]any{{
				"provider": provider,
				"model":    model,
				"reason":   "model-unknown",
			}},
		}
		return
	}
	if triple, ok := lookupPriceBook(catalog, provider, model); ok {
		value.ContextTimeline.CostRead = map[string]any{
			"usd":   *total,
			"ready": true,
			"faces": []map[string]any{{
				"provider":   provider,
				"model":      model,
				"rate":       map[string]any{"hit": triple.Hit, "miss": triple.Miss, "write": triple.Write, "out": triple.Out},
				"via":        "direct",
				"confidence": "medium",
			}},
		}
		return
	}
	value.ContextTimeline.CostRead = map[string]any{"usd": *total, "ready": true}
}

// lookupPriceBook exposes the resolved rate to the cost face.
func lookupPriceBook(catalog *priceCatalog, provider, model string) (priceTriple, bool) {
	catalog.mu.RLock()
	book := catalog.book
	catalog.mu.RUnlock()
	if book == nil {
		return priceTriple{}, false
	}
	return lookupPrice(book, provider, model)
}

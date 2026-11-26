package main

import (
	"encoding/json"
	"testing"
)

// The cost cards are only honest if the arithmetic and the lookup both work, so
// both are asserted against a catalog shaped like the published one.
func TestPriceBookParsesCatalog(t *testing.T) {
	raw := []byte(`{
		"anthropic": {
			"models": {
				"claude-sonnet-4-5": {
					"cost": {"input": 3, "output": 15, "cache_read": 0.3, "cache_write": 3.75},
					"canonical_model_id": "claude-sonnet-4-5-20250929"
				},
				"unpriced-model": {"cost": null}
			}
		},
		"opencode": {
			"models": {
				"anthropic/claude-sonnet-4-5": {
					"cost": {"input": 3, "output": 15}
				}
			}
		}
	}`)
	book := parsePriceBook(raw)
	if book == nil {
		t.Fatal("catalog did not parse")
	}
	rate, ok := book.Prices["anthropic"]["claude-sonnet-4-5"]
	if !ok {
		t.Fatal("the sonnet rate is missing")
	}
	if rate.Miss != 3 || rate.Out != 15 || rate.Hit != 0.3 || rate.Write != 3.75 {
		t.Errorf("rate = %+v, want the published four", rate)
	}
	if _, present := book.Prices["anthropic"]["unpriced-model"]; present {
		t.Error("a model without a cost was priced")
	}
	if got := book.Canonical["anthropic"]["claude-sonnet-4-5"]; got != "claude-sonnet-4-5-20250929" {
		t.Errorf("canonical id = %q", got)
	}
	// A cache rate that the catalog omits falls back to the input rate rather
	// than billing cache reads at zero.
	fallback := book.Prices["opencode"]["anthropic/claude-sonnet-4-5"]
	if fallback.Hit != 3 || fallback.Write != 3 {
		t.Errorf("missing cache rates = %+v, want the input rate", fallback)
	}
}

func TestCostOfOneSession(t *testing.T) {
	catalog := newPriceCatalog("")
	catalog.book = parsePriceBook([]byte(`{
		"anthropic": {"models": {"claude-sonnet-4-5": {
			"cost": {"input": 3, "output": 15, "cache_read": 0.3, "cache_write": 3.75}
		}}}
	}`))
	if catalog.book == nil {
		t.Fatal("catalog did not parse")
	}

	// One million of each bucket, so the result is the sum of the four rates.
	cost := catalog.costOf("anthropic", "claude-sonnet-4-5", 1_000_000, 1_000_000, 1_000_000, 1_000_000)
	if cost == nil {
		t.Fatal("a priced model produced no cost")
	}
	want := 3.0 + 0.3 + 3.75 + 15.0
	if diff := *cost - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cost = %v, want %v", *cost, want)
	}

	// Through a compatibility provider the model is still priced, because a
	// catalog entry anywhere is better than none.
	if other := catalog.costOf("openai-compatibility", "anthropic/claude-sonnet-4-5", 1_000_000, 0, 0, 0); other == nil {
		t.Error("a model served through a compatibility provider was not priced")
	}

	// An unknown model must say so, not cost zero.
	if none := catalog.costOf("anthropic", "not-a-model", 1_000_000, 0, 0, 0); none != nil {
		t.Errorf("an unknown model was priced at %v", *none)
	}
}

// The projection must carry the price in the shape the card reads.
func TestProjectionCarriesCost(t *testing.T) {
	p := newPlugin(nil)
	p.cfg.Enabled = true
	catalog := newPriceCatalog("")
	catalog.book = parsePriceBook([]byte(`{
		"anthropic": {"models": {"claude-sonnet-4-5": {
			"cost": {"input": 3, "output": 15, "cache_read": 0.3}
		}}}
	}`))
	p.store.prices = catalog

	body, _ := json.Marshal(map[string]any{
		"model": "claude-sonnet-4-5",
		"messages": []map[string]any{
			{"role": "user", "content": "price this"},
		},
	})
	intercept, _ := json.Marshal(map[string]any{
		"RequestID": "r1", "TraceID": "t1", "SourceFormat": "openai",
		"Model": "claude-sonnet-4-5", "Body": body,
	})
	p.handle("request.intercept_before", intercept)
	usageRaw, _ := json.Marshal(map[string]any{
		"TraceID": "t1", "Model": "claude-sonnet-4-5", "SessionID": "s-cost",
		"Provider": "anthropic",
		"Detail": map[string]any{
			"InputTokens": 2000, "OutputTokens": 100, "TotalTokens": 2100,
			"CacheReadTokens": 1000,
		},
	})
	p.handle("usage.handle", usageRaw)

	built, ok := p.store.projectionFor("s-cost")
	if !ok {
		t.Fatal("projection is missing")
	}
	read := built.ContextTimeline.CostRead
	if read == nil {
		t.Fatal("the projection carries no cost face")
	}
	usd, present := read["usd"]
	if !present || usd == nil {
		t.Fatalf("costRead.usd = %v, want a number", usd)
	}
	amount, isFloat := usd.(float64)
	if !isFloat || amount <= 0 {
		t.Errorf("costRead.usd = %v, want a positive number", usd)
	}
	if read["ready"] != true {
		t.Error("costRead is not marked ready")
	}
}

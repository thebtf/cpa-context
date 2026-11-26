package main

import "testing"

// The price catalog is fetched from a third party. A payload that is truncated,
// empty, or shaped differently must leave the plugin without prices rather than
// crashing it or pricing sessions at zero.
func TestPriceBookRejectsBadPayloads(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"not json", "not json at all"},
		{"truncated", `{"anthropic":{"models":{"m":{"cost":{"input":3`},
		{"wrong shape", `[1,2,3]`},
		{"no models", `{"anthropic":{}}`},
		{"cost without numbers", `{"anthropic":{"models":{"m":{"cost":{"input":"free"}}}}}`},
		{"null cost", `{"anthropic":{"models":{"m":{"cost":null}}}}`},
	}
	for _, tc := range cases {
		book := parsePriceBook([]byte(tc.raw))
		if book == nil {
			continue // a rejected payload is a valid answer
		}
		if len(book.Prices) != 0 {
			t.Errorf("%s: produced %d priced providers, want none", tc.name, len(book.Prices))
		}
	}
}

// A catalog with no usable entry must make cost lookup answer "unknown", not
// zero: a session that looks free is worse than one that looks unpriced.
func TestUnpricedCatalogReportsUnknown(t *testing.T) {
	catalog := newPriceCatalog("")
	catalog.book = parsePriceBook([]byte(`{"p":{"models":{"m":{"cost":null}}}}`))
	if cost := catalog.costOf("p", "m", 1_000_000, 0, 0, 1_000_000); cost != nil {
		t.Errorf("an unpriced model returned %v, want nil", *cost)
	}

	// No catalog at all is the same answer.
	empty := newPriceCatalog("")
	if cost := empty.costOf("p", "m", 1000, 0, 0, 1000); cost != nil {
		t.Errorf("without a catalog, cost = %v, want nil", *cost)
	}
}

// A model served through a compatibility provider must still be priced when any
// branch of the catalog lists it. CPA routes by provider name, which rarely
// matches the catalog's own provider keys.
func TestPricedThroughAnotherProvider(t *testing.T) {
	catalog := newPriceCatalog("")
	catalog.book = parsePriceBook([]byte(`{
		"anthropic":{"models":{"claude-sonnet-4-5":{"cost":{"input":3,"output":15}}}}
	}`))
	if catalog.book == nil {
		t.Fatal("catalog did not parse")
	}
	for _, provider := range []string{"anthropic", "openai-compatibility", "", "unknown"} {
		if cost := catalog.costOf(provider, "claude-sonnet-4-5", 1_000_000, 0, 0, 0); cost == nil {
			t.Errorf("provider %q: the model was not priced", provider)
		}
	}
	// A vendor prefix on the model id must not defeat the lookup.
	if cost := catalog.costOf("openai-compatibility", "anthropic/claude-sonnet-4-5", 1_000_000, 0, 0, 0); cost == nil {
		t.Error("a prefixed model id was not priced")
	}
}

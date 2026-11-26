package main

import (
	"net/http"
	"sync"
	"net/http/httptest"
	"testing"
	"time"
)

// A catalog fetched once at startup quotes the prices of that day for as long as
// the proxy runs, and a model released later is never priced. The refresh has to
// keep happening.
func TestCatalogRefreshesAfterTheBookAges(t *testing.T) {
	served := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served++
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"acme":{"models":{"m":{"cost":{"input":1,"output":2}}}}}`))
	}))
	defer server.Close()

	catalog := newPriceCatalog("")
	catalog.url = server.URL

	catalog.fetch()
	if served != 1 {
		t.Fatalf("the server was asked %d times, want one", served)
	}
	if catalog.book == nil || len(catalog.book.Prices) == 0 {
		t.Fatal("the book was not stored")
	}

	// An aged book is fetched again rather than kept.
	catalog.mu.Lock()
	catalog.loadedAt = time.Now().Add(-2 * priceCatalogTTL)
	catalog.mu.Unlock()
	catalog.fetch()
	if served != 2 {
		t.Errorf("an aged book was not refreshed: server asked %d times", served)
	}

	// An empty book is retried too, not treated as a finished load.
	catalog.mu.Lock()
	catalog.book = nil
	catalog.loadedAt = time.Time{}
	catalog.mu.Unlock()
	catalog.fetch()
	if served != 3 || catalog.book == nil {
		t.Errorf("an empty book was not refetched: served=%d haveBook=%v", served, catalog.book != nil)
	}
}

// The refresh must run while the process lives, not only in the call that
// starts it. A catalog fetched once quotes the prices of the day the process
// began for as long as it runs.
func TestCatalogStartRefreshesRepeatedly(t *testing.T) {
	var mu sync.Mutex
	served := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		served++
		mu.Unlock()
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"acme":{"models":{"m":{"cost":{"input":1,"output":2}}}}}`))
	}))
	defer server.Close()

	catalog := newPriceCatalog("")
	catalog.url = server.URL
	catalog.interval = 50 * time.Millisecond
	catalog.start()

	// The first fetch happens at once; any further one can only come from the
	// repeating refresh.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := served
		mu.Unlock()
		if count >= 3 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	mu.Lock()
	count := served
	mu.Unlock()
	t.Errorf("the book was fetched %d times, want a repeating refresh", count)
}

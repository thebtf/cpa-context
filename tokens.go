package main

import (
	"strings"
	"sync"

	"github.com/tiktoken-go/tokenizer"
)

// Token estimates. CPA reports only aggregate counters per request, so the
// composition split has to be estimated. The estimate is then scaled to the
// provider-reported input total, which keeps the split honest: the bar always
// sums to the number the upstream actually billed.
//
// Two estimators exist. The exact one runs the model's real tokenizer over each
// item; it is slow and only used for the request being displayed. The cheap one
// is used for aggregates over thousands of stored requests.
type estimator struct {
	mu    sync.Mutex
	cache map[string]tokenizer.Codec
}

var tokens = &estimator{cache: make(map[string]tokenizer.Codec)}

// codecFor returns a tokenizer matching the model family, falling back to a
// general-purpose vocabulary. A nil codec means "use the character heuristic".
func (e *estimator) codecFor(model string) tokenizer.Codec {
	name := modelFamily(model)
	key := string(name)
	e.mu.Lock()
	defer e.mu.Unlock()
	if codec, ok := e.cache[key]; ok {
		return codec
	}
	codec, err := tokenizer.ForModel(name)
	if err != nil {
		codec, err = tokenizer.Get(tokenizer.O200kBase)
		if err != nil {
			e.cache[key] = nil
			return nil
		}
	}
	e.cache[key] = codec
	return codec
}

// modelFamily maps a model id onto a tiktoken model name. Unknown vendors share
// the contemporary BPE vocabulary, which is the closest offline approximation.
func modelFamily(model string) tokenizer.Model {
	sanitized := strings.ToLower(strings.TrimSpace(model))
	if family, ok := familyByName(sanitized); ok {
		return family
	}
	return tokenizer.GPT4o
}

func familyByName(sanitized string) (tokenizer.Model, bool) {
	switch {
	case strings.HasPrefix(sanitized, "gpt-5"):
		return tokenizer.GPT5, true
	case strings.HasPrefix(sanitized, "gpt-4.1"):
		return tokenizer.GPT41, true
	case strings.HasPrefix(sanitized, "gpt-4o"):
		return tokenizer.GPT4o, true
	case strings.HasPrefix(sanitized, "gpt-4"):
		return tokenizer.GPT4, true
	case strings.HasPrefix(sanitized, "gpt-3.5"), strings.HasPrefix(sanitized, "gpt-3"):
		return tokenizer.GPT35Turbo, true
	case strings.HasPrefix(sanitized, "o1"):
		return tokenizer.O1, true
	case strings.HasPrefix(sanitized, "o3"):
		return tokenizer.O3, true
	case strings.HasPrefix(sanitized, "o4"):
		return tokenizer.O4Mini, true
	}
	return "", false
}

// countItem returns the token estimate for one item. imageBytes is charged by
// area-proportional approximation because no tokenizer covers encoded images.
func (e *estimator) countItem(model string, it item) int {
	if it.Bytes > 0 && it.Chars == 0 {
		return imageTokens(it.Bytes)
	}
	if it.Chars == 0 {
		return 0
	}
	if codec := e.codecFor(model); codec != nil {
		if count, err := codec.Count(it.Text); err == nil {
			return count
		}
	}
	return charHeuristic(it.Text)
}

// charHeuristic is the fallback when no tokenizer is available. Four characters
// per token understates CJK, so each non-ASCII rune is charged whole.
func charHeuristic(text string) int {
	nonASCII, nonASCIIBytes := 0, 0
	for _, r := range text {
		if r > 127 {
			nonASCII++
			nonASCIIBytes += len(string(r))
		}
	}
	return (len(text)-nonASCIIBytes+3)/4 + nonASCII
}

// imageTokens approximates the vision token cost of an encoded image. The
// constant is chosen so a typical 1024x1024 screenshot lands near the reported
// figure for current frontier models; it is an estimate, not a promise.
func imageTokens(encodedBytes int) int {
	// Base64 inflates by ~4/3, so the decoded payload is about 3/4 the length.
	decoded := encodedBytes * 3 / 4
	// Assume a JPEG near 0.15 bytes per pixel to recover pixel count.
	pixels := decoded * 100 / 15
	if pixels <= 0 {
		return 0
	}
	// 750 tokens per megapixel of tiled coverage.
	tokens := pixels * 750 / 1_000_000
	if tokens < 85 {
		return 85
	}
	return tokens
}

// scaledComposition applies a scale factor to every item so the total matches
// an authoritative number. Rounding drift is absorbed by the largest item.
func scaledComposition(base map[string]int, target int) map[string]int {
	out := make(map[string]int, len(base))
	total := 0
	for _, value := range base {
		total += value
	}
	if total <= 0 || target <= 0 {
		for key, value := range base {
			out[key] = value
		}
		return out
	}
	largest, largestValue := "", -1
	sum := 0
	for _, category := range categoryOrder {
		value, ok := base[category]
		if !ok {
			continue
		}
		scaled := value * target / total
		out[category] = scaled
		sum += scaled
		if value > largestValue {
			largest, largestValue = category, value
		}
	}
	if remainder := target - sum; remainder != 0 && largest != "" {
		out[largest] += remainder
	}
	return out
}

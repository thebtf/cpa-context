// Benchmarks that isolate the two costs the request path pays: the exact
// tokenizer and the cheaper character heuristic. They exist to justify which one
// runs on traffic, not to guard behaviour.
package main

import "testing"

func BenchmarkTokenizerPerItem(b *testing.B) {
	body := bigPrompt(40)
	parsed := parseRequestBody("openai", body)
	for b.Loop() {
		counts := make(map[string]int, len(categoryOrder))
		for _, it := range parsed.Items {
			counts[it.Category] += tokens.countItem("claude-sonnet-4-5", it)
		}
	}
}

func BenchmarkTokenizerHeuristic(b *testing.B) {
	body := bigPrompt(40)
	parsed := parseRequestBody("openai", body)
	for b.Loop() {
		counts := make(map[string]int, len(categoryOrder))
		for _, it := range parsed.Items {
			counts[it.Category] += charHeuristic(it.Text)
		}
	}
}

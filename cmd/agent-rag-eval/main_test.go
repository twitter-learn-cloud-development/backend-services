package main

import "testing"

func TestParseStrategiesUsesStableOrderAndDeduplicates(t *testing.T) {
	got, err := parseStrategies("rrf_rerank, bm25, rrf, bm25")
	if err != nil {
		t.Fatalf("parse strategies: %v", err)
	}
	want := []string{"bm25", "rrf", "rrf_rerank"}
	if len(got) != len(want) {
		t.Fatalf("unexpected strategy count: got %v want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("unexpected strategy order: got %v want %v", got, want)
		}
	}
}

func TestParseStrategiesRejectsUnknownAndEmptyInput(t *testing.T) {
	for _, input := range []string{"", "bm25,unknown"} {
		if _, err := parseStrategies(input); err == nil {
			t.Fatalf("expected invalid strategy input %q to fail", input)
		}
	}
}

func TestStrategyRequirementsOnlyInitializesNeededDependencies(t *testing.T) {
	vectorOnly := strategyRequirements([]string{"vector"})
	if vectorOnly.needBM25 || !vectorOnly.needVector || vectorOnly.needRRF || vectorOnly.needReranker {
		t.Fatalf("unexpected vector-only requirements: %#v", vectorOnly)
	}
	rerank := strategyRequirements([]string{"rrf_rerank"})
	if !rerank.needBM25 || !rerank.needVector || !rerank.needRRF || !rerank.needReranker {
		t.Fatalf("RRF rerank dependencies were not closed: %#v", rerank)
	}
}

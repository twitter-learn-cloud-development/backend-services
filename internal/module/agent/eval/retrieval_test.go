package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluateRetrievalCalculatesStableMetrics(t *testing.T) {
	metrics := EvaluateRetrieval([]RetrievalCase{
		{RelevantIDs: []string{"a"}, RetrievedIDs: []string{"noise", "a"}},
		{RelevantIDs: []string{"b", "c"}, RetrievedIDs: []string{"b", "noise"}},
		{RelevantIDs: []string{"missing"}, RetrievedIDs: nil},
	}, 2)

	if metrics.Cases != 3 {
		t.Fatalf("unexpected case count: %#v", metrics)
	}
	if metrics.RecallAtK < 0.49 || metrics.RecallAtK > 0.51 {
		t.Fatalf("unexpected recall: %#v", metrics)
	}
	if metrics.MRR < 0.49 || metrics.MRR > 0.51 {
		t.Fatalf("unexpected mrr: %#v", metrics)
	}
	if metrics.EmptyRate < 0.32 || metrics.EmptyRate > 0.34 {
		t.Fatalf("unexpected empty rate: %#v", metrics)
	}
	if metrics.NoiseRate < 0.49 || metrics.NoiseRate > 0.51 {
		t.Fatalf("unexpected noise rate: %#v", metrics)
	}
}

func TestEvaluateRetrievalHandlesInvalidInputs(t *testing.T) {
	metrics := EvaluateRetrieval(nil, 5)
	if metrics.Cases != 0 || metrics.RecallAtK != 0 {
		t.Fatalf("expected zero metrics for empty input: %#v", metrics)
	}
	metrics = EvaluateRetrieval([]RetrievalCase{{RelevantIDs: []string{"a"}, RetrievedIDs: []string{"a"}}}, 0)
	if metrics.MRR != 0 || metrics.NDCGAtK != 0 {
		t.Fatalf("expected zero metrics for invalid k: %#v", metrics)
	}
}

func TestRAGDatasetHasBaselineCoverage(t *testing.T) {
	path := filepath.Join("testdata", "rag_cases.json")
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open RAG dataset: %v", err)
	}
	defer file.Close()
	dataset, err := LoadDataset(file)
	if err != nil {
		t.Fatalf("load RAG dataset: %v", err)
	}
	if len(dataset) < 50 {
		t.Fatalf("expected at least 50 cases, got %d", len(dataset))
	}
}

package eval

import (
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

// RetrievalCase is an offline, model-agnostic retrieval evaluation sample.
// IDs are opaque document identifiers, so the same evaluator works for
// BM25, vector, RRF and reranked outputs.
type RetrievalCase struct {
	ID           string `json:"id,omitempty"`
	Query        string `json:"query,omitempty"`
	Category     string `json:"category,omitempty"`
	RelevantIDs  []string
	RetrievedIDs []string
}

type DatasetCase struct {
	ID          string   `json:"id"`
	Query       string   `json:"query"`
	Category    string   `json:"category"`
	RelevantIDs []string `json:"relevant_ids"`
}

func LoadDataset(reader io.Reader) ([]DatasetCase, error) {
	if reader == nil {
		return nil, fmt.Errorf("evaluation dataset reader is nil")
	}
	var dataset []DatasetCase
	if _, err := decodeBoundedEvaluationJSON(reader, &dataset, "retrieval evaluation dataset"); err != nil {
		return nil, err
	}
	if err := validateEvaluationCaseCount(len(dataset), "retrieval evaluation dataset"); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(dataset))
	for index := range dataset {
		sample := &dataset[index]
		sample.ID = strings.TrimSpace(sample.ID)
		sample.Query = strings.TrimSpace(sample.Query)
		sample.Category = strings.TrimSpace(sample.Category)
		if sample.ID == "" || sample.Query == "" {
			return nil, fmt.Errorf("evaluation dataset case %d is missing id or query", index)
		}
		if utf8.RuneCountInString(sample.ID) > maxEvaluationIdentifierRunes ||
			utf8.RuneCountInString(sample.Category) > maxEvaluationIdentifierRunes ||
			utf8.RuneCountInString(sample.Query) > maxEvaluationTextRunes {
			return nil, fmt.Errorf("evaluation dataset case %d exceeds string limits", index)
		}
		if _, exists := seen[sample.ID]; exists {
			return nil, fmt.Errorf("evaluation dataset contains duplicate id %q", sample.ID)
		}
		seen[sample.ID] = struct{}{}
		if len(sample.RelevantIDs) > maxEvaluationListItems {
			return nil, fmt.Errorf("evaluation dataset case %q has too many relevant IDs", sample.ID)
		}
		relevant := make(map[string]struct{}, len(sample.RelevantIDs))
		for relevantIndex := range sample.RelevantIDs {
			id := strings.TrimSpace(sample.RelevantIDs[relevantIndex])
			if id == "" || utf8.RuneCountInString(id) > maxEvaluationIdentifierRunes {
				return nil, fmt.Errorf("evaluation dataset case %q has invalid relevant ID", sample.ID)
			}
			if _, exists := relevant[id]; exists {
				return nil, fmt.Errorf("evaluation dataset case %q has duplicate relevant ID %q", sample.ID, id)
			}
			relevant[id] = struct{}{}
			sample.RelevantIDs[relevantIndex] = id
		}
	}
	return dataset, nil
}

type RetrievalMetrics struct {
	Cases     int
	RecallAtK float64
	MRR       float64
	NDCGAtK   float64
	EmptyRate float64
	NoiseRate float64
}

// EvaluateRetrieval calculates binary-relevance metrics over the first k
// results. It intentionally has no model or storage dependency so CI can run
// a deterministic baseline with fixed retrieval outputs.
func EvaluateRetrieval(cases []RetrievalCase, k int) RetrievalMetrics {
	metrics := RetrievalMetrics{Cases: len(cases)}
	if len(cases) == 0 || k <= 0 {
		return metrics
	}

	noiseResults := 0
	returnedResults := 0
	for _, sample := range cases {
		relevant := make(map[string]struct{}, len(sample.RelevantIDs))
		for _, id := range sample.RelevantIDs {
			if id != "" {
				relevant[id] = struct{}{}
			}
		}
		results := sample.RetrievedIDs
		if len(results) > k {
			results = results[:k]
		}
		if len(results) == 0 {
			metrics.EmptyRate++
		}

		hits := 0
		firstRelevantRank := 0
		seenResults := make(map[string]struct{}, len(results))
		for rank, id := range results {
			if _, duplicate := seenResults[id]; duplicate {
				continue
			}
			seenResults[id] = struct{}{}
			returnedResults++
			if _, ok := relevant[id]; !ok {
				noiseResults++
				continue
			}
			hits++
			if firstRelevantRank == 0 {
				firstRelevantRank = rank + 1
			}
		}
		if len(relevant) > 0 {
			metrics.RecallAtK += float64(hits) / float64(len(relevant))
		}
		if firstRelevantRank > 0 {
			metrics.MRR += 1.0 / float64(firstRelevantRank)
		}

		if len(relevant) > 0 {
			actualDCG := 0.0
			for rank, id := range results {
				if _, ok := relevant[id]; ok {
					actualDCG += 1.0 / log2(float64(rank+2))
				}
			}
			idealDCG := 0.0
			idealHits := len(relevant)
			if idealHits > k {
				idealHits = k
			}
			for rank := 0; rank < idealHits; rank++ {
				idealDCG += 1.0 / log2(float64(rank+2))
			}
			if idealDCG > 0 {
				metrics.NDCGAtK += actualDCG / idealDCG
			}
		}
	}

	divisor := float64(len(cases))
	metrics.RecallAtK /= divisor
	metrics.MRR /= divisor
	metrics.NDCGAtK /= divisor
	metrics.EmptyRate /= divisor
	if returnedResults > 0 {
		metrics.NoiseRate = float64(noiseResults) / float64(returnedResults)
	}
	return metrics
}

func log2(value float64) float64 {
	if value <= 1 {
		return 1
	}
	return math.Log2(value)
}

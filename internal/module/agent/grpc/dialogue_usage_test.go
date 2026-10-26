package grpc

import "testing"

func TestDialogueMessageRuntimeUsageMetadata(t *testing.T) {
	for _, test := range []struct {
		name      string
		metadata  map[string]any
		available bool
		tokens    int64
		estimated bool
	}{
		{name: "legacy", metadata: nil},
		{name: "exact", metadata: map[string]any{"runtime_tokens": int64(123), "runtime_tokens_estimated": false}, available: true, tokens: 123},
		{name: "estimated", metadata: map[string]any{"runtime_tokens": int32(42), "runtime_tokens_estimated": true}, available: true, tokens: 42, estimated: true},
		{name: "zero", metadata: map[string]any{"runtime_tokens": 0}, available: true},
		{name: "invalid", metadata: map[string]any{"runtime_tokens": "unknown"}},
		{name: "negative", metadata: map[string]any{"runtime_tokens": -1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := dialogueMessageHasRuntimeUsage(test.metadata); got != test.available {
				t.Fatalf("availability = %t, want %t", got, test.available)
			}
			if got := dialogueMessageRuntimeTokens(test.metadata); got != test.tokens {
				t.Fatalf("tokens = %d, want %d", got, test.tokens)
			}
			if got := dialogueMessageMetadataBool(test.metadata, "runtime_tokens_estimated"); got != test.estimated {
				t.Fatalf("estimated = %t, want %t", got, test.estimated)
			}
		})
	}
}

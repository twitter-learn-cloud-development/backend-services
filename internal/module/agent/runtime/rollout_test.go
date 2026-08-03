package runtime

import (
	"reflect"
	"testing"
)

func TestParseRollout(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []Mode
		wantErr bool
	}{
		{name: "empty uses legacy", raw: "", want: []Mode{}},
		{name: "none uses legacy", raw: "none", want: []Mode{}},
		{name: "single mode", raw: "consult", want: []Mode{ModeConsult}},
		{
			name: "normalizes whitespace case and duplicates",
			raw:  " Consult,assist,CONSULT ",
			want: []Mode{ModeConsult, ModeAssist},
		},
		{
			name: "all modes",
			raw:  "all",
			want: []Mode{ModeChat, ModeConsult, ModeAssist, ModeMulti, ModeWorkflow},
		},
		{name: "unknown mode", raw: "chat,unknown", wantErr: true},
		{name: "empty segment", raw: "chat,,assist", wantErr: true},
		{name: "ambiguous none", raw: "none,chat", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRollout(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ParseRollout() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRollout() error = %v", err)
			}
			if !reflect.DeepEqual(got.Modes(), tt.want) {
				t.Fatalf("ParseRollout().Modes() = %v, want %v", got.Modes(), tt.want)
			}
		})
	}
}

func TestRolloutDefaultAndUnknownModesAreDisabled(t *testing.T) {
	var rollout Rollout
	for _, mode := range orderedModes {
		if rollout.Enabled(mode) {
			t.Fatalf("zero-value rollout unexpectedly enables %q", mode)
		}
	}
	if rollout.Enabled(Mode("invalid")) {
		t.Fatal("unknown mode must never be enabled")
	}
	if got := rollout.String(); got != "none" {
		t.Fatalf("zero-value rollout String() = %q, want none", got)
	}
}

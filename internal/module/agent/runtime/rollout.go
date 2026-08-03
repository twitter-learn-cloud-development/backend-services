package runtime

import (
	"fmt"
	"strings"
)

const RuntimeV2ModesEnv = "AGENT_RUNTIME_V2_MODES"

// Mode identifies one externally visible Agent entry point.
type Mode string

const (
	ModeChat     Mode = "chat"
	ModeConsult  Mode = "consult"
	ModeAssist   Mode = "assist"
	ModeMulti    Mode = "multi"
	ModeWorkflow Mode = "workflow"
)

var orderedModes = [...]Mode{
	ModeChat,
	ModeConsult,
	ModeAssist,
	ModeMulti,
	ModeWorkflow,
}

var modeBits = map[Mode]uint8{
	ModeChat:     1 << 0,
	ModeConsult:  1 << 1,
	ModeAssist:   1 << 2,
	ModeMulti:    1 << 3,
	ModeWorkflow: 1 << 4,
}

// Rollout is an immutable per-process snapshot of the Runtime v2 rollout.
// Its zero value keeps every mode on the legacy execution path.
type Rollout struct {
	mask uint8
}

// ParseRollout parses a comma-separated mode list. Empty and "none" select
// the legacy path for every mode; "all" selects Runtime v2 for every mode.
func ParseRollout(raw string) (Rollout, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "none") {
		return Rollout{}, nil
	}

	parts := strings.Split(raw, ",")
	var rollout Rollout
	for _, part := range parts {
		token := strings.ToLower(strings.TrimSpace(part))
		if token == "" {
			return Rollout{}, fmt.Errorf("empty mode in %s", RuntimeV2ModesEnv)
		}
		if token == "none" {
			return Rollout{}, fmt.Errorf("mode %q cannot be combined with other modes", token)
		}
		if token == "all" {
			for _, mode := range orderedModes {
				rollout.mask |= modeBits[mode]
			}
			continue
		}

		mode := Mode(token)
		bit, ok := modeBits[mode]
		if !ok {
			return Rollout{}, fmt.Errorf(
				"unknown mode %q in %s (valid: %s)",
				token,
				RuntimeV2ModesEnv,
				strings.Join(modeNames(), ","),
			)
		}
		rollout.mask |= bit
	}

	return rollout, nil
}

// Enabled reports whether a mode should use Runtime v2.
func (r Rollout) Enabled(mode Mode) bool {
	bit, ok := modeBits[mode]
	return ok && r.mask&bit != 0
}

// Modes returns enabled modes in stable order.
func (r Rollout) Modes() []Mode {
	modes := make([]Mode, 0, len(orderedModes))
	for _, mode := range orderedModes {
		if r.Enabled(mode) {
			modes = append(modes, mode)
		}
	}
	return modes
}

func (r Rollout) String() string {
	if r.mask == 0 {
		return "none"
	}

	names := make([]string, 0, len(orderedModes))
	for _, mode := range r.Modes() {
		names = append(names, string(mode))
	}
	return strings.Join(names, ",")
}

func modeNames() []string {
	names := make([]string, 0, len(orderedModes))
	for _, mode := range orderedModes {
		names = append(names, string(mode))
	}
	return names
}

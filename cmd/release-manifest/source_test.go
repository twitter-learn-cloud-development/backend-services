package main

import (
	"errors"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestReleaseSourceRejectsStaleDirtyAndUntrackedCheckout(t *testing.T) {
	const source = "1234567890123456789012345678901234567890"
	for _, test := range []struct {
		name             string
		head             string
		dirty, untracked bool
		want             bool
	}{
		{"clean", source, false, false, true}, {"wrong", strings.Repeat("a", 40), false, false, false}, {"tracked", source, true, false, false}, {"untracked", source, false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := func(args ...string) (string, error) {
				switch args[0] {
				case "rev-parse":
					return test.head, nil
				case "diff":
					if test.dirty {
						return "", errors.New("private diff failure")
					}
				case "ls-files":
					if test.untracked {
						return "private-file-name", nil
					}
				}
				return "", nil
			}
			err := validateSourceResult(source, run)
			if test.want {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private")
			}
		})
	}
	output := &boundedGitOutput{}
	_, err := output.Write([]byte(strings.Repeat("x", 65537)))
	require.Error(t, err)
	require.Zero(t, output.Len())
}

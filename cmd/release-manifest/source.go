package main

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

type boundedGitOutput struct{ bytes.Buffer }

func (b *boundedGitOutput) Write(value []byte) (int, error) {
	if b.Len()+len(value) > 65536 {
		return 0, errors.New("source metadata exceeds bound")
	}
	return b.Buffer.Write(value)
}

// Receipt commands require the actual clean checkout; ignored CI output and
// vendored build inputs do not change the source commit being attested.
func verifySource(root, source string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run := func(args ...string) (string, error) {
		command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
		output := &boundedGitOutput{}
		command.Stdout = output
		command.Stderr = &boundedGitOutput{}
		if err := command.Run(); err != nil {
			return "", errors.New("source checkout verification failed")
		}
		return strings.TrimSpace(output.String()), nil
	}
	return validateSourceResult(source, run)
}
func validateSourceResult(source string, run func(...string) (string, error)) error {
	head, err := run("rev-parse", "HEAD")
	if err != nil || head != source {
		return errors.New("source SHA does not match checkout")
	}
	if _, err := run("diff", "--quiet", "HEAD", "--"); err != nil {
		return errors.New("source checkout contains tracked changes")
	}
	untracked, err := run("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	if untracked != "" {
		return errors.New("source checkout contains untracked files")
	}
	return nil
}

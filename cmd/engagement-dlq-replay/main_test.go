package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"twitter-clone/internal/mq/consumer"
)

type runnerFake struct{ called bool }

func (f *runnerFake) Run(ctx context.Context, o consumer.EngagementReplayOptions) (consumer.TimelineEventReplayReport, error) {
	f.called = true
	if _, ok := ctx.Deadline(); !ok {
		return consumer.TimelineEventReplayReport{}, errors.New("missing deadline")
	}
	return consumer.TimelineEventReplayReport{Mode: "inspect", Event: consumer.TimelineEventKind(o.Event)}, nil
}
func TestReplayCLIIsOfflineUnlessExplicit(t *testing.T) {
	var out bytes.Buffer
	called := false
	factory := func(context.Context) (replayRunner, func(), error) {
		called = true
		return nil, nil, errors.New("credentials must not be read")
	}
	if err := run(nil, &out, factory); err != nil {
		t.Fatal(err)
	}
	if called || !bytes.Contains(out.Bytes(), []byte(`"mode":"offline"`)) {
		t.Fatal("default connected")
	}
	for _, args := range [][]string{{"--execute", "--operator", "op", "--reason", "repair"}, {"--allow-live", "--execute"}, {"--limit", "101"}, {"--timeout", "61s"}, {"--event", "created"}, {"extra"}} {
		if run(args, &out, factory) == nil {
			t.Fatalf("invalid options accepted: %v", args)
		}
	}
	if called {
		t.Fatal("invalid request connected")
	}
}
func TestReplayCLIExplicitInspectionHasDeadlineAndClose(t *testing.T) {
	var out bytes.Buffer
	f := &runnerFake{}
	closed := false
	err := run([]string{"--allow-live", "--event", "comment"}, &out, func(ctx context.Context) (replayRunner, func(), error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("setup unbounded")
		}
		return f, func() { closed = true }, nil
	})
	if err != nil || !f.called || !closed {
		t.Fatalf("err=%v called=%v closed=%v", err, f.called, closed)
	}
}

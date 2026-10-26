package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"twitter-clone/internal/infrastructure/schema"
)

func TestMigrationCLIPlanIsOfflineAndExecutionRequiresReview(t *testing.T) {
	for _, owner := range schema.OwnerNames() {
		var out bytes.Buffer
		connected := false
		factory := func(context.Context) (schema.BootstrapStore, func(), error) {
			connected = true
			return nil, nil, errors.New("credentials must not be read")
		}
		if err := run([]string{"--owner", owner}, &out, factory); err != nil {
			t.Fatal(owner, err)
		}
		if connected || !bytes.Contains(out.Bytes(), []byte(`"sha256"`)) {
			t.Fatal("offline plan connected")
		}
	}
	var out bytes.Buffer
	called := false
	factory := func(context.Context) (schema.BootstrapStore, func(), error) {
		called = true
		return nil, nil, errors.New("connected")
	}
	for _, args := range [][]string{{"--owner", "unknown"}, {"--owner", "user", "--execute"}, {"--owner", "user", "--allow-live", "--execute", "--approved-plan-sha256", "bad", "--operator", "op", "--reason", "change", "--operation", "op1"}, {"--owner", "user", "--timeout", "61s"}} {
		if run(args, &out, factory) == nil {
			t.Fatal("unsafe request accepted", args)
		}
	}
	if called {
		t.Fatal("invalid request connected")
	}
}

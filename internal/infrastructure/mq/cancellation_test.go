package mq

import (
	"context"
	"github.com/stretchr/testify/require"
	"net"
	"testing"
	"time"
)

func TestBoundedConnectionCancellationInterruptsTransport(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()
	broker := &RabbitMQ{socket: local}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, err := broker.CloseOnCancellation(ctx)
	require.NoError(t, err)
	defer stop()
	done := make(chan error, 1)
	go func() { _, err := remote.Read(make([]byte, 1)); done <- err }()
	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("protocol wait did not exit")
	}
}
func TestBoundedConnectionCancellationWatcherCanBeReplaced(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()
	broker := &RabbitMQ{socket: local}
	setup, cancel := context.WithCancel(context.Background())
	stop, err := broker.CloseOnCancellation(setup)
	require.NoError(t, err)
	stop()
	cancel()
	require.NoError(t, remote.SetReadDeadline(time.Now().Add(time.Second)))
	done := make(chan error, 1)
	go func() { _, err := local.Write([]byte("x")); done <- err }()
	body := make([]byte, 1)
	_, err = remote.Read(body)
	require.NoError(t, err)
	require.NoError(t, <-done)
	_, err = (&RabbitMQ{}).CloseOnCancellation(context.Background())
	require.Error(t, err)
}

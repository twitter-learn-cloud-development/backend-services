package mq

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBoundedBrokerConnectionRejectsCancelledOperationAndInvalidTargetBeforeDial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client, err := NewRabbitMQWithContext(ctx, &RabbitMQConfig{Host: "unreachable.invalid", Port: 5672})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, client)
	for _, config := range []*RabbitMQConfig{nil, {Host: "", Port: 5672}, {Host: "unreachable.invalid", Port: 70000}} {
		client, err = NewRabbitMQWithContext(context.Background(), config)
		require.ErrorContains(t, err, "invalid broker target")
		require.Nil(t, client)
	}
}

func TestBoundedBrokerHandshakeCannotOutliveOperationDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	release, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-release // Deliberately never sends the AMQP handshake response.
	}()
	t.Cleanup(func() { _ = listener.Close(); close(release); <-done })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	client, err := NewRabbitMQWithContext(ctx, &RabbitMQConfig{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, User: "fixture", Password: "fixture", Vhost: "/"})
	require.Error(t, err)
	require.Nil(t, client)
	require.Less(t, time.Since(start), 2*time.Second)
}

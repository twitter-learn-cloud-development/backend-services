package mq

import (
	"context"
	"errors"
	"net"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// NewRabbitMQWithContext bounds connection, protocol handshake and channel
// initialization for a maintenance operation. Long-running service callers keep
// their existing constructor. No exchange or queue is declared here.
func NewRabbitMQWithContext(ctx context.Context, config *RabbitMQConfig) (*RabbitMQ, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config == nil || config.Host == "" || config.Port < 1 || config.Port > 65535 {
		return nil, errors.New("invalid broker target")
	}
	deadline := time.Now().Add(5 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	var socket net.Conn
	uri := amqp.URI{Scheme: "amqp", Host: config.Host, Port: config.Port, Username: config.User, Password: config.Password, Vhost: config.Vhost}
	conn, err := amqp.DialConfig(uri.String(), amqp.Config{Dial: func(network, address string) (net.Conn, error) {
		connection, err := (&net.Dialer{Deadline: deadline}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if err = connection.SetDeadline(deadline); err != nil {
			_ = connection.Close()
			return nil, err
		}
		socket = connection
		return connection, nil
	}})
	if err != nil {
		return nil, errors.New("bounded broker connection unavailable")
	}
	channel, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, errors.New("bounded broker channel unavailable")
	}
	if err = ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if socket == nil || socket.SetDeadline(time.Time{}) != nil {
		_ = conn.Close()
		return nil, errors.New("broker deadline release unavailable")
	}
	return &RabbitMQ{conn: conn, channel: channel, config: config, socket: socket}, nil
}

// CloseOnCancellation interrupts protocol waits on a bounded, owned connection.
// Stop the watcher before switching from setup timeout to session lifetime.
func (r *RabbitMQ) CloseOnCancellation(ctx context.Context) (func(), error) {
	if r == nil || r.socket == nil {
		return nil, errors.New("bounded broker transport is required")
	}
	stop := context.AfterFunc(ctx, func() { _ = r.socket.Close() })
	return func() { stop() }, nil
}

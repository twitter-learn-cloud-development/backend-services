package projection

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/prometheus/client_golang/prometheus"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
	"testing"
)

type ackFixture struct {
	acks, nacks int
	ackError    error
}

func (a *ackFixture) Ack(uint64, bool) error        { a.acks++; return a.ackError }
func (a *ackFixture) Nack(uint64, bool, bool) error { a.nacks++; return nil }
func (a *ackFixture) Reject(uint64, bool) error     { return nil }

type publisherFixture struct {
	messages  []amqp.Publishing
	exchanges []string
	fail      bool
	before    func()
}

func (*publisherFixture) EnablePublisherConfirms() error { return nil }
func (p *publisherFixture) PublishMessageConfirmed(_ context.Context, exchange, key string, message amqp.Publishing) error {
	if p.before != nil {
		p.before()
	}
	if p.fail {
		return errors.New("confirm failed")
	}
	p.messages = append(p.messages, message)
	p.exchanges = append(p.exchanges, exchange)
	return nil
}
func TestFollowIngressAckOccursOnlyAfterDurableRegistrationAndConfirmedRetry(t *testing.T) {
	s := &storeFake{}
	publisher := &publisherFixture{}
	r := Runner{Engine: Engine{Store: s}, Publisher: publisher}
	body, _ := json.Marshal(eventFixture())
	a := &ackFixture{}
	msg := amqp.Delivery{Acknowledger: a, Body: body, RoutingKey: "user.unfollowed"}
	r.Handle(context.Background(), msg)
	require.Equal(t, 1, a.acks)
	require.Equal(t, 1, s.registered)
	require.Empty(t, publisher.messages)
	s.failRegister = true
	a.acks = 0
	publisher.before = func() { require.Zero(t, a.acks) }
	r.Handle(context.Background(), msg)
	require.Equal(t, 1, a.acks)
	require.Equal(t, RetryExchange, publisher.exchanges[0])
	require.Equal(t, "1000", publisher.messages[0].Expiration)
	publisher.fail = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.acks = 0
	r.Handle(ctx, msg)
	require.Zero(t, a.acks)
}
func TestFollowIngressMalformedAndExhaustedMessagesOnlyAckAfterDLQConfirm(t *testing.T) {
	for _, kind := range []string{"malformed", "exhausted", "wrong_route", "wrong_header"} {
		s := &storeFake{failRegister: true}
		p := &publisherFixture{}
		r := Runner{Engine: Engine{Store: s}, Publisher: p}
		a := &ackFixture{}
		body, _ := json.Marshal(eventFixture())
		msg := amqp.Delivery{Acknowledger: a, Body: body, RoutingKey: "user.unfollowed", Headers: amqp.Table{}}
		switch kind {
		case "malformed":
			msg.Body = []byte(`{`)
		case "exhausted":
			msg.Headers["x-follow-retry"] = int32(3)
		case "wrong_route":
			msg.RoutingKey = "user.followed"
		case "wrong_header":
			msg.Headers["x-follow-retry"] = "private poison"
		}
		r.Handle(context.Background(), msg)
		require.Equal(t, 1, a.acks)
		require.Equal(t, DLX, p.exchanges[0])
		require.Empty(t, p.messages[0].Expiration)
	}
}
func TestFollowMetricsClampLabelsAndExposeExhaustedBacklog(t *testing.T) {
	registry := prometheus.NewRegistry()
	m, err := NewMetrics(registry)
	require.NoError(t, err)
	m.Observe("private user id", "private error")
	m.SetBacklog([]BacklogRow{{TaskType: PairTask, State: "exhausted", Count: 3, OldestAt: 1000}}, 2000)
	families, err := registry.Gather()
	require.NoError(t, err)
	found := false
	for _, family := range families {
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				require.NotContains(t, label.GetValue(), "private")
			}
			if family.GetName() == "follow_projection_backlog" && metric.GetGauge().GetValue() == 3 {
				found = true
			}
		}
	}
	require.True(t, found)
}

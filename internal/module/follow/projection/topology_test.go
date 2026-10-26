package projection

import (
	"errors"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"os"
	"testing"
)

type brokerFixture struct {
	binds []string
	args  map[string]amqp.Table
	fail  string
}

func (b *brokerFixture) DeclareExchange(name, kind string, durable bool) error {
	if name == b.fail {
		return errors.New("exchange unavailable")
	}
	return nil
}
func (b *brokerFixture) DeclareQueue(name string, durable bool) (amqp.Queue, error) {
	if name == b.fail {
		return amqp.Queue{}, errors.New("queue unavailable")
	}
	return amqp.Queue{}, nil
}
func (b *brokerFixture) DeclareQueueWithArgs(name string, durable bool, args amqp.Table) (amqp.Queue, error) {
	if b.args == nil {
		b.args = map[string]amqp.Table{}
	}
	b.args[name] = args
	return b.DeclareQueue(name, durable)
}
func (b *brokerFixture) BindQueue(queue, key, exchange string) error {
	b.binds = append(b.binds, queue+"|"+key+"|"+exchange)
	if key == b.fail {
		return errors.New("binding unavailable")
	}
	return nil
}
func (*brokerFixture) Consume(string, string) (<-chan amqp.Delivery, error) {
	return nil, errors.New("not used")
}
func TestFollowRecoveryTopologyReturnsOnlyToDedicatedIngressAndPropagatesFailures(t *testing.T) {
	b := &brokerFixture{}
	r := &Runner{Broker: b, Publisher: &publisherFixture{}}
	require.NoError(t, r.Declare())
	require.Equal(t, IngressExchange, b.args[RetryQueue]["x-dead-letter-exchange"])
	require.Equal(t, "reconcile", b.args[RetryQueue]["x-dead-letter-routing-key"])
	require.Contains(t, b.binds, "follow.relations.v1|user.followed|twitter.events")
	require.Contains(t, b.binds, "follow.relations.v1|reconcile|"+IngressExchange)
	for _, fail := range []string{RetryExchange, RetryQueue, "dead"} {
		r.Broker = &brokerFixture{fail: fail}
		require.Error(t, r.Declare())
	}
}
func TestFollowAlertRulesYAMLParsesAndContainsBacklogFailure(t *testing.T) {
	body, err := os.ReadFile("../../../../deploy/prometheus/rules/twitter-clone-production.yml")
	require.NoError(t, err)
	var config struct {
		Groups []struct {
			Name  string `yaml:"name"`
			Rules []struct {
				Alert string `yaml:"alert"`
				Expr  string `yaml:"expr"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	require.NoError(t, yaml.Unmarshal(body, &config))
	found := map[string]bool{}
	for _, group := range config.Groups {
		if group.Name == "twitter-clone-follow-projection" {
			for _, rule := range group.Rules {
				found[rule.Alert] = true
				require.NotEmpty(t, rule.Expr)
			}
		}
	}
	for _, alert := range []string{"FollowProjectionTasksExhausted", "FollowProjectionBacklogOld", "FollowProjectionTelemetryUnavailable"} {
		require.True(t, found[alert], alert)
	}
}

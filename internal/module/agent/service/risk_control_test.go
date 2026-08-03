package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"twitter-clone/internal/events"
)

type riskControlBrokerFake struct {
	exchanges    []string
	queues       []string
	queueArgs    map[string]amqp.Table
	bindings     [][3]string
	qos          int
	confirmCalls int
	confirmErr   error
	publishErr   error
	published    []riskControlPublished
	operations   *[]string
}

type riskControlPublished struct {
	exchange   string
	routingKey string
	message    amqp.Publishing
}

func (b *riskControlBrokerFake) DeclareExchange(name, _ string, _ bool) error {
	b.exchanges = append(b.exchanges, name)
	return nil
}

func (b *riskControlBrokerFake) DeclareQueue(name string, _ bool) (amqp.Queue, error) {
	b.queues = append(b.queues, name)
	return amqp.Queue{Name: name}, nil
}

func (b *riskControlBrokerFake) DeclareQueueWithArgs(name string, _ bool, args amqp.Table) (amqp.Queue, error) {
	if b.queueArgs == nil {
		b.queueArgs = make(map[string]amqp.Table)
	}
	b.queueArgs[name] = args
	return amqp.Queue{Name: name}, nil
}

func (b *riskControlBrokerFake) BindQueue(queueName, routingKey, exchangeName string) error {
	b.bindings = append(b.bindings, [3]string{queueName, routingKey, exchangeName})
	return nil
}

func (b *riskControlBrokerFake) SetQoS(prefetchCount int) error {
	b.qos = prefetchCount
	return nil
}

func (b *riskControlBrokerFake) EnablePublisherConfirms() error {
	b.confirmCalls++
	if b.operations != nil {
		*b.operations = append(*b.operations, "confirm")
	}
	return b.confirmErr
}

func (b *riskControlBrokerFake) Consume(string, string) (<-chan amqp.Delivery, error) {
	return nil, errors.New("not used")
}

func (b *riskControlBrokerFake) PublishMessageConfirmed(
	_ context.Context,
	exchange string,
	routingKey string,
	message amqp.Publishing,
) error {
	if b.operations != nil {
		*b.operations = append(*b.operations, "publish")
	}
	if b.publishErr != nil {
		return b.publishErr
	}
	b.published = append(b.published, riskControlPublished{
		exchange: exchange, routingKey: routingKey, message: message,
	})
	return nil
}

type riskWorkflowClientFake struct {
	err     error
	options client.StartWorkflowOptions
	event   events.TweetCreatedEvent
	calls   int
}

func (c *riskWorkflowClientFake) ExecuteWorkflow(
	_ context.Context,
	options client.StartWorkflowOptions,
	_ interface{},
	args ...interface{},
) (client.WorkflowRun, error) {
	c.calls++
	c.options = options
	if len(args) == 1 {
		c.event, _ = args[0].(events.TweetCreatedEvent)
	}
	return nil, c.err
}

type riskControlObserverFake struct {
	results map[string]int
}

func (o *riskControlObserverFake) Observe(result string) {
	if o.results == nil {
		o.results = make(map[string]int)
	}
	o.results[result]++
}

type riskControlAcknowledgerFake struct {
	acked      int
	nacked     int
	requeue    bool
	ackErr     error
	nackErr    error
	operations *[]string
}

func (a *riskControlAcknowledgerFake) Ack(uint64, bool) error {
	a.acked++
	if a.operations != nil {
		*a.operations = append(*a.operations, "ack")
	}
	return a.ackErr
}

func (a *riskControlAcknowledgerFake) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacked++
	a.requeue = requeue
	if a.operations != nil {
		*a.operations = append(*a.operations, "nack")
	}
	return a.nackErr
}

func (a *riskControlAcknowledgerFake) Reject(_ uint64, requeue bool) error {
	return a.Nack(0, false, requeue)
}

func TestDeclareRiskControlTopologyUsesDedicatedRetryIngress(t *testing.T) {
	broker := &riskControlBrokerFake{}
	if err := DeclareRiskControlTopology(broker); err != nil {
		t.Fatal(err)
	}
	args := broker.queueArgs[riskControlRetryQueue]
	if args["x-dead-letter-exchange"] != riskControlIngressExchange ||
		args["x-dead-letter-routing-key"] != riskControlIngressRoutingKey {
		t.Fatalf("retry args = %+v", args)
	}
	if !containsRiskControlBinding(broker.bindings, [3]string{
		riskControlQueue, riskControlSourceRoutingKey, riskControlEventsExchange,
	}) || !containsRiskControlBinding(broker.bindings, [3]string{
		riskControlQueue, riskControlIngressRoutingKey, riskControlIngressExchange,
	}) {
		t.Fatalf("bindings = %+v", broker.bindings)
	}
	if broker.qos != riskControlPrefetch {
		t.Fatalf("qos = %d", broker.qos)
	}
}

func TestRiskControlDispatchesWorkflowAndAcknowledges(t *testing.T) {
	operations := make([]string, 0, 2)
	broker := &riskControlBrokerFake{operations: &operations}
	workflowClient := &riskWorkflowClientFake{}
	observer := &riskControlObserverFake{}
	riskControl, err := NewRiskControl(broker, workflowClient, observer)
	if err != nil {
		t.Fatal(err)
	}
	operations = operations[:0]
	ack := &riskControlAcknowledgerFake{operations: &operations}

	riskControl.handle(context.Background(), riskControlDelivery(ack, validRiskControlBody(9001, 42), nil))
	if workflowClient.calls != 1 || workflowClient.options.ID != "RiskControl-Tweet-9001" ||
		workflowClient.options.TaskQueue != "AGENT_TASK_QUEUE" || workflowClient.event.AuthorID != 42 ||
		workflowClient.options.WorkflowIDReusePolicy != enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE ||
		!workflowClient.options.WorkflowExecutionErrorWhenAlreadyStarted {
		t.Fatalf("calls=%d options=%+v event=%+v", workflowClient.calls, workflowClient.options, workflowClient.event)
	}
	if ack.acked != 1 || ack.nacked != 0 || observer.results["dispatched"] != 1 {
		t.Fatalf("ack=%+v results=%+v", ack, observer.results)
	}
}

func TestRiskControlConfirmsRetryBeforeAcknowledgement(t *testing.T) {
	operations := make([]string, 0, 3)
	broker := &riskControlBrokerFake{operations: &operations}
	workflowClient := &riskWorkflowClientFake{err: errors.New("temporal unavailable")}
	riskControl, err := NewRiskControl(broker, workflowClient, nil)
	if err != nil {
		t.Fatal(err)
	}
	operations = operations[:0]
	ack := &riskControlAcknowledgerFake{operations: &operations}

	riskControl.handle(context.Background(), riskControlDelivery(ack, validRiskControlBody(9001, 42), nil))
	if len(broker.published) != 1 || ack.acked != 1 || ack.nacked != 0 {
		t.Fatalf("published=%+v ack=%+v", broker.published, ack)
	}
	if len(operations) != 2 || operations[0] != "publish" || operations[1] != "ack" {
		t.Fatalf("operation order = %v", operations)
	}
	published := broker.published[0]
	if published.exchange != riskControlRetryExchange || published.routingKey != riskControlRetryRoutingKey ||
		published.message.Expiration != "1000" || published.message.Headers[riskControlRetryHeader] != int32(1) {
		t.Fatalf("published retry = %+v", published)
	}
}

func TestRiskControlRoutesMalformedAndExhaustedEventsToDLQ(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		body    string
		headers amqp.Table
	}{
		{name: "malformed", body: "{"},
		{name: "exhausted", body: validRiskControlBody(9001, 42), headers: amqp.Table{riskControlRetryHeader: int32(3)}},
		{name: "invalid retry header", body: validRiskControlBody(9001, 42), headers: amqp.Table{riskControlRetryHeader: "invalid"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			broker := &riskControlBrokerFake{}
			workflowClient := &riskWorkflowClientFake{err: errors.New("temporal unavailable")}
			riskControl, err := NewRiskControl(broker, workflowClient, nil)
			if err != nil {
				t.Fatal(err)
			}
			ack := &riskControlAcknowledgerFake{}
			riskControl.handle(context.Background(), riskControlDelivery(ack, testCase.body, testCase.headers))
			if len(broker.published) != 1 || ack.acked != 1 {
				t.Fatalf("published=%+v ack=%+v", broker.published, ack)
			}
			published := broker.published[0]
			if published.exchange != riskControlDLX || published.routingKey != riskControlDLQRoutingKey ||
				published.message.Expiration != "" {
				t.Fatalf("published dlq = %+v", published)
			}
		})
	}
}

func TestRiskControlAcknowledgesAlreadyStartedWorkflow(t *testing.T) {
	broker := &riskControlBrokerFake{}
	workflowClient := &riskWorkflowClientFake{
		err: serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "workflow-id", "run-id"),
	}
	observer := &riskControlObserverFake{}
	riskControl, err := NewRiskControl(broker, workflowClient, observer)
	if err != nil {
		t.Fatal(err)
	}
	ack := &riskControlAcknowledgerFake{}

	riskControl.handle(context.Background(), riskControlDelivery(ack, validRiskControlBody(9001, 42), nil))
	if ack.acked != 1 || len(broker.published) != 0 || observer.results["duplicate"] != 1 {
		t.Fatalf("ack=%+v published=%+v results=%+v", ack, broker.published, observer.results)
	}
}

func TestRiskControlPublishFailureWaitsThenRequeues(t *testing.T) {
	broker := &riskControlBrokerFake{publishErr: errors.New("broker unavailable")}
	workflowClient := &riskWorkflowClientFake{err: errors.New("temporal unavailable")}
	riskControl, err := NewRiskControl(broker, workflowClient, nil)
	if err != nil {
		t.Fatal(err)
	}
	var waited time.Duration
	riskControl.wait = func(_ context.Context, delay time.Duration) bool {
		waited = delay
		return true
	}
	ack := &riskControlAcknowledgerFake{}

	riskControl.handle(context.Background(), riskControlDelivery(
		ack,
		validRiskControlBody(9001, 42),
		amqp.Table{riskControlRetryHeader: int32(1)},
	))
	if waited != 4*time.Second || ack.acked != 0 || ack.nacked != 1 || !ack.requeue {
		t.Fatalf("waited=%s ack=%+v", waited, ack)
	}
}

func TestNewRiskControlRequiresPublisherConfirms(t *testing.T) {
	_, err := NewRiskControl(
		&riskControlBrokerFake{confirmErr: errors.New("confirm unavailable")},
		&riskWorkflowClientFake{},
		nil,
	)
	if err == nil {
		t.Fatal("expected publisher confirm initialization failure")
	}
}

func riskControlDelivery(ack amqp.Acknowledger, body string, headers amqp.Table) amqp.Delivery {
	return amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  1,
		RoutingKey:   riskControlSourceRoutingKey,
		ContentType:  "application/json",
		Headers:      headers,
		Body:         []byte(body),
		Timestamp:    time.UnixMilli(1_700_000_000_000),
	}
}

func validRiskControlBody(tweetID, authorID uint64) string {
	body, err := json.Marshal(events.TweetCreatedEvent{TweetID: tweetID, AuthorID: authorID, Content: "content"})
	if err != nil {
		panic(err)
	}
	return string(body)
}

func containsRiskControlBinding(bindings [][3]string, expected [3]string) bool {
	for _, binding := range bindings {
		if binding == expected {
			return true
		}
	}
	return false
}

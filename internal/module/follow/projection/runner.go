package projection

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	amqp "github.com/rabbitmq/amqp091-go"
	"log"
	"strconv"
	"sync"
	"time"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/internal/module/follow"
)

const (
	IngressExchange = "follow.ingress.v1"
	RetryExchange   = "follow.retry.v1"
	DLX             = "follow.dlx.v1"
	RetryQueue      = "follow.relations.retry.v1"
	DLQ             = "follow.relations.dlq.v1"
)

type Broker interface {
	DeclareExchange(string, string, bool) error
	DeclareQueue(string, bool) (amqp.Queue, error)
	DeclareQueueWithArgs(string, bool, amqp.Table) (amqp.Queue, error)
	BindQueue(string, string, string) error
	Consume(string, string) (<-chan amqp.Delivery, error)
}
type Publisher interface {
	EnablePublisherConfirms() error
	PublishMessageConfirmed(context.Context, string, string, amqp.Publishing) error
}
type Runner struct {
	Broker      Broker
	Publisher   Publisher
	Tasks       domain.OutboxRepository
	Engine      Engine
	Owner       string
	Backlog     func(context.Context) ([]BacklogRow, error)
	lastBacklog time.Time
}

func token() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func (r *Runner) Declare() error {
	for _, name := range []string{"twitter.events", IngressExchange, RetryExchange, DLX} {
		if err := r.Broker.DeclareExchange(name, "topic", true); err != nil {
			return err
		}
	}
	if _, err := r.Broker.DeclareQueue(follow.RelationQueue, true); err != nil {
		return err
	}
	for _, key := range []string{"user.followed", "user.unfollowed"} {
		if err := r.Broker.BindQueue(follow.RelationQueue, key, "twitter.events"); err != nil {
			return err
		}
	}
	if err := r.Broker.BindQueue(follow.RelationQueue, "reconcile", IngressExchange); err != nil {
		return err
	}
	if _, err := r.Broker.DeclareQueueWithArgs(RetryQueue, true, amqp.Table{"x-dead-letter-exchange": IngressExchange, "x-dead-letter-routing-key": "reconcile"}); err != nil {
		return err
	}
	if err := r.Broker.BindQueue(RetryQueue, "retry", RetryExchange); err != nil {
		return err
	}
	if _, err := r.Broker.DeclareQueue(DLQ, true); err != nil {
		return err
	}
	if err := r.Broker.BindQueue(DLQ, "dead", DLX); err != nil {
		return err
	}
	return r.Publisher.EnablePublisherConfirms()
}
func (r *Runner) Run(ctx context.Context) {
	var loops sync.WaitGroup
	loops.Add(2)
	go func() { defer loops.Done(); r.ingress(ctx) }()
	go func() { defer loops.Done(); r.work(ctx) }()
	loops.Wait()
}
func wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func (r *Runner) ingress(ctx context.Context) {
	for ctx.Err() == nil {
		messages, err := r.Broker.Consume(follow.RelationQueue, r.Owner+"-follow")
		if err != nil {
			log.Print("follow consume unavailable")
			if !wait(ctx, 5*time.Second) {
				return
			}
			continue
		}
		for ctx.Err() == nil {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-messages:
				if !ok {
					if !wait(ctx, 5*time.Second) {
						return
					}
					goto reconnect
				}
				r.Handle(ctx, msg)
			}
		}
	reconnect:
	}
}
func (r *Runner) Handle(ctx context.Context, msg amqp.Delivery) {
	var event events.FollowRelationEvent
	valid := len(msg.Body) <= 64*1024 && json.Unmarshal(msg.Body, &event) == nil
	following := event.EventKey == events.FollowRelationEventKey(event.RelationID, true)
	valid = valid && event.Validate(following) == nil
	if msg.RoutingKey == "user.followed" && !following || msg.RoutingKey == "user.unfollowed" && following {
		valid = false
	}
	if !valid {
		r.routeFailure(ctx, msg, true)
		return
	}
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	_, err := r.Engine.Store.Register(call, event)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			_ = msg.Nack(false, true)
			return
		}
		r.routeFailure(ctx, msg, false)
		return
	}
	if err := msg.Ack(false); err != nil {
		log.Print("follow task registration ACK uncertain")
		r.observe("ingress", "ack_uncertain")
	} else {
		r.observe("ingress", "registered")
	}
}
func (r *Runner) routeFailure(ctx context.Context, msg amqp.Delivery, permanent bool) {
	count := int64(0)
	if value, ok := msg.Headers["x-follow-retry"]; ok {
		switch v := value.(type) {
		case int32:
			count = int64(v)
		case int64:
			count = v
		default:
			permanent = true
		}
		if count < 0 || count > 3 {
			permanent = true
		}
	}
	message := amqp.Publishing{Body: msg.Body, ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: msg.MessageId, Headers: amqp.Table{}}
	for key, value := range msg.Headers {
		if key != "x-death" {
			message.Headers[key] = value
		}
	}
	exchange, key, result := RetryExchange, "retry", "retried"
	if permanent || count >= 3 {
		exchange, key, result = DLX, "dead", "dlq"
	} else {
		count++
		message.Headers["x-follow-retry"] = int32(count)
		message.Expiration = strconv.FormatInt(1000*(1<<uint(count-1)), 10)
	}
	call, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	err := r.Publisher.PublishMessageConfirmed(call, exchange, key, message)
	cancel()
	if err != nil {
		r.observe("ingress", "confirm_failed")
		if wait(ctx, time.Second) {
			_ = msg.Nack(false, true)
		}
		return
	}
	if msg.Ack(false) != nil {
		r.observe("ingress", "ack_uncertain")
		return
	}
	r.observe("ingress", result)
}
func (r *Runner) work(ctx context.Context) {
	for ctx.Err() == nil {
		r.Tick(ctx)
		if !wait(ctx, time.Second) {
			return
		}
	}
}
func (r *Runner) Tick(ctx context.Context) {
	now := time.Now()
	if r.Backlog != nil && (r.lastBacklog.IsZero() || now.Sub(r.lastBacklog) >= time.Minute) {
		r.lastBacklog = now
		call, cancel := context.WithTimeout(ctx, 3*time.Second)
		rows, err := r.Backlog(call)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				r.observe("worker", "backlog_failed")
			}
		} else if metrics, ok := r.Engine.Observer.(*Metrics); ok {
			metrics.SetBacklog(rows, now.UnixMilli())
		}
	}
	if _, err := r.Tasks.RecoverExpiredClaims(ctx, now.UnixMilli(), 10); err != nil {
		r.observe("worker", "recover_failed")
		return
	}
	// Reserve an author slot so waiting pair jobs cannot starve promotion work.
	for _, kind := range []string{AuthorTask, PairTask} {
		now = time.Now()
		secret, err := token()
		if err != nil {
			return
		}
		tasks, err := r.Tasks.Claim(ctx, domain.OutboxClaimRequest{TaskTypes: []string{kind}, LeaseOwner: r.Owner, LeaseToken: secret, ClaimedAtUnixMilli: now.UnixMilli(), LeaseUntilUnixMilli: now.Add(90 * time.Second).UnixMilli(), Limit: 1})
		if err != nil {
			r.observe("worker", "claim_failed")
			return
		}
		for _, task := range tasks {
			if ctx.Err() != nil {
				return
			}
			if err := r.Engine.Execute(ctx, task); err != nil {
				final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
				if ctx.Err() != nil {
					accepted, saveErr := r.Engine.Store.Interrupt(final, task)
					cancel()
					if saveErr != nil || !accepted {
						r.observe("worker", "finalize_failed")
					} else {
						r.observe("worker", "interrupted")
					}
					return
				}
				accepted, saveErr := r.Tasks.FailClaim(final, domain.OutboxClaimFailure{TaskID: task.ID, LeaseOwner: task.LeaseOwner, LeaseToken: task.LeaseToken, FailedAtUnixMilli: time.Now().UnixMilli(), ErrorMsg: "follow projection page failed", Terminal: task.Retries >= task.MaxRetries})
				cancel()
				if saveErr != nil || !accepted {
					r.observe("worker", "finalize_failed")
				}
				if !errors.Is(err, context.Canceled) {
					log.Print("follow projection page failed; persistent checkpoint retained")
				}
			}
		}
	}
}
func (r *Runner) observe(kind, result string) {
	if r.Engine.Observer != nil {
		r.Engine.Observer.Observe(kind, result)
	}
}

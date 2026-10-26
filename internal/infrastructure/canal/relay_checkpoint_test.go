package canal

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goCanal "github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/schema"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
	"twitter-clone/internal/domain"
)

type checkpointStoreFake struct {
	mu       sync.Mutex
	saved    []mysql.Position
	updated  chan mysql.Position
	failures int
}

type relayReceiptFake struct {
	operations *[]string
	failure    bool
	receipts   []domain.OutboxPublication
}

func (f *relayReceiptFake) RecordPublication(_ context.Context, receipt domain.OutboxPublication) error {
	*f.operations = append(*f.operations, "receipt")
	if f.failure {
		return errors.New("fixture receipt failure")
	}
	f.receipts = append(f.receipts, receipt)
	return nil
}
func (*relayReceiptFake) ReadPublication(context.Context, uint64) (*domain.OutboxPublication, error) {
	return nil, nil
}
func (*relayReceiptFake) ReadEvents(context.Context, []uint64) ([]domain.OutboxEvent, error) {
	return nil, nil
}

type receiptPublisherFake struct{ operations *[]string }

func (p receiptPublisherFake) PublishRawJSON(context.Context, string, string, []byte) error {
	*p.operations = append(*p.operations, "confirm")
	return nil
}
func TestRelayPublicationReceiptPrecedesCheckpointAndFollowsConfirm(t *testing.T) {
	operations := []string{}
	store := &checkpointStoreFake{}
	relay := NewOutboxEventRelay(nil, receiptPublisherFake{operations: &operations}, store)
	defer relay.cancel()
	ledger := &relayReceiptFake{operations: &operations}
	relay.SetPublicationRecorder(ledger)
	require.NoError(t, relay.processRelayRecord(&OutboxRelayEvent{EventID: 17, EventType: "TWEET_DELETED", Payload: `{"tweet_id":17,"author_id":42}`}))
	require.Equal(t, []string{"confirm", "receipt"}, operations)
	require.Len(t, ledger.receipts, 1)
	pos := mysql.Position{Name: "bin.1", Pos: 100}
	require.NoError(t, relay.processRelayRecord(&OutboxRelayEvent{Position: &pos}))
	require.Len(t, store.saved, 1)
	ledger.failure = true
	done := make(chan error, 1)
	go func() {
		done <- relay.processRelayRecord(&OutboxRelayEvent{EventID: 18, EventType: "TWEET_DELETED", Payload: `{"tweet_id":18,"author_id":42}`})
	}()
	// Cancellation cannot advance the checkpoint while the receipt is absent.
	time.Sleep(20 * time.Millisecond)
	relay.cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Len(t, store.saved, 1)
	require.Len(t, ledger.receipts, 1)
}

func (s *checkpointStoreFake) SavePosition(pos mysql.Position) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failures > 0 {
		s.failures--
		return errors.New("fixture checkpoint failure")
	}
	s.saved = append(s.saved, pos)
	if s.updated != nil {
		s.updated <- pos
	}
	return nil
}
func (*checkpointStoreFake) GetPosition() (mysql.Position, error) { return mysql.Position{}, nil }

type blockingRelayPublisher struct {
	calls   atomic.Int32
	second  chan struct{}
	release chan struct{}
}

func (p *blockingRelayPublisher) PublishRawJSON(ctx context.Context, _, _ string, _ []byte) error {
	if p.calls.Add(1) == 2 {
		close(p.second)
		select {
		case <-p.release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func TestRelayCheckpointCannotPassUnconfirmedPublicationOnShutdown(t *testing.T) {
	store := &checkpointStoreFake{updated: make(chan mysql.Position, 4)}
	publisher := &blockingRelayPublisher{second: make(chan struct{}), release: make(chan struct{})}
	relay := NewOutboxEventRelay(nil, publisher, store)
	handler := &OutboxEventHandler{relay: relay}
	relay.wg.Add(1)
	go relay.runWorker()
	defer relay.Stop()
	relay.eventChan <- &OutboxRelayEvent{EventType: "TWEET_CREATED", Payload: `{"tweet_id":1}`}
	require.NoError(t, handler.OnPosSynced(nil, mysql.Position{Name: "bin.1", Pos: 100}, nil, false))
	relay.eventChan <- &OutboxRelayEvent{EventType: "TWEET_DELETED", Payload: `{"tweet_id":1}`}
	require.NoError(t, handler.OnPosSynced(nil, mysql.Position{Name: "bin.1", Pos: 200}, nil, false))
	select {
	case pos := <-store.updated:
		require.Equal(t, uint32(100), pos.Pos)
	case <-time.After(time.Second):
		t.Fatal("first confirmed boundary missing")
	}
	select {
	case <-publisher.second:
	case <-time.After(time.Second):
		t.Fatal("second publication not started")
	}
	relay.Stop()
	store.mu.Lock()
	defer store.mu.Unlock()
	require.Len(t, store.saved, 1)
	require.Equal(t, uint32(100), store.saved[0].Pos)
}

func TestRelayHandlesAllRowsAndRejectsMalformedOutboxInsteadOfSkipping(t *testing.T) {
	relay := NewOutboxEventRelay(nil, nil, nil)
	defer relay.cancel()
	handler := &OutboxEventHandler{relay: relay}
	table := &schema.Table{Schema: "twitter", Name: "outbox_events", Columns: []schema.TableColumn{{Name: "event_type"}, {Name: "payload"}}}
	rows := &goCanal.RowsEvent{Table: table, Action: goCanal.InsertAction, Rows: [][]interface{}{{"TWEET_CREATED", `{"tweet_id":1}`}, {[]byte("TWEET_DELETED"), []byte(`{"tweet_id":1}`)}}}
	require.NoError(t, handler.OnRow(rows))
	require.Len(t, relay.eventChan, 2)
	require.Equal(t, "TWEET_CREATED", (<-relay.eventChan).EventType)
	require.Equal(t, "TWEET_DELETED", (<-relay.eventChan).EventType)
	rows.Rows = [][]interface{}{{"TWEET_CREATED"}}
	require.Error(t, handler.OnRow(rows))
	require.Empty(t, relay.eventChan)
	require.Error(t, handler.OnRow(nil))
}

func TestRelayRetriesCheckpointAndWithholdsUnsupportedRoute(t *testing.T) {
	store := &checkpointStoreFake{failures: 1}
	relay := NewOutboxEventRelay(nil, nil, store)
	defer relay.cancel()
	pos := mysql.Position{Name: "bin.1", Pos: 123}
	require.NoError(t, relay.processRelayRecord(&OutboxRelayEvent{Position: &pos}))
	require.Len(t, store.saved, 1)
	require.Error(t, relay.processRelayRecord(&OutboxRelayEvent{EventType: "UNKNOWN"}))
	require.Len(t, store.saved, 1)
	relay.cancel()
	require.ErrorIs(t, relay.processRelayRecord(&OutboxRelayEvent{Position: &pos}), context.Canceled)
	require.Len(t, store.saved, 1)
}

type confirmPublisherFake struct {
	enabled  int
	failure  error
	messages []amqp.Publishing
}

func (p *confirmPublisherFake) EnablePublisherConfirms() error { p.enabled++; return p.failure }
func (p *confirmPublisherFake) PublishMessageConfirmed(_ context.Context, _, _ string, msg amqp.Publishing) error {
	p.messages = append(p.messages, msg)
	return p.failure
}
func TestRelayPublisherRequiresConfirmAndStableMessageIdentity(t *testing.T) {
	p := &confirmPublisherFake{}
	wrapper, err := NewConfirmedRelayPublisher(p)
	require.NoError(t, err)
	require.Equal(t, 1, p.enabled)
	for i := 0; i < 2; i++ {
		require.NoError(t, wrapper.PublishRawJSON(context.Background(), "twitter.events", "tweet.deleted", []byte(`{"tweet_id":17}`)))
	}
	require.Len(t, p.messages, 2)
	require.NotEmpty(t, p.messages[0].MessageId)
	require.Equal(t, p.messages[0].MessageId, p.messages[1].MessageId)
	require.Equal(t, uint8(amqp.Persistent), p.messages[0].DeliveryMode)
	p.failure = errors.New("fixture confirm failure")
	require.Error(t, wrapper.PublishRawJSON(context.Background(), "twitter.events", "tweet.deleted", nil))
	_, err = NewConfirmedRelayPublisher(p)
	require.Error(t, err)
	_, err = NewConfirmedRelayPublisher(nil)
	require.Error(t, err)
}

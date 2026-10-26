package canal

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	"twitter-clone/internal/domain"
)

// MQProducer MQ 消息生产者接口
type MQProducer interface {
	PublishRawJSON(ctx context.Context, exchange string, routingKey string, body []byte) error
}

// RouteConfig 路由配置定义
type RouteConfig struct {
	Exchange   string
	RoutingKey string
}

// 静态路由字典，彻底解耦事件类型和 MQ 路由的强关联
var routeRegistry = map[string]RouteConfig{
	"USER_FOLLOWED":   {Exchange: "twitter.events", RoutingKey: "user.followed"},
	"USER_UNFOLLOWED": {Exchange: "twitter.events", RoutingKey: "user.unfollowed"},
	"TWEET_CREATED": {
		Exchange:   "twitter.events",
		RoutingKey: "tweet.created",
	},
	"TWEET_DELETED": {
		Exchange:   "twitter.events",
		RoutingKey: "tweet.deleted",
	},
	"TWEET_MODERATED": {
		Exchange:   "twitter.events",
		RoutingKey: "tweet.moderated",
	},
	"TWEET_LIKED": {
		Exchange:   "twitter.events",
		RoutingKey: "tweet.liked",
	},
	"COMMENT_CREATED": {
		Exchange:   "twitter.events",
		RoutingKey: "comment.created",
	},
}

func OutboxRoute(eventType string) (RouteConfig, bool) {
	route, ok := routeRegistry[eventType]
	return route, ok
}

// PositionStore Binlog 消费位点存储接口
type PositionStore interface {
	SavePosition(pos mysql.Position) error
	GetPosition() (mysql.Position, error)
}

// OutboxRelayEvent 中继通道传递的事件对象
type OutboxRelayEvent struct {
	EventID   uint64
	EventType string
	Payload   string
	Position  *mysql.Position
}

// OutboxEventHandler 专门监听发件箱表的事件处理器
type OutboxEventHandler struct {
	canal.DummyEventHandler
	relay *OutboxEventRelay
}

// OutboxEventRelay 发件箱中继器（核心结构体）
type OutboxEventRelay struct {
	canalInstance *canal.Canal

	// 内存防波堤：带缓冲的 Channel
	eventChan chan *OutboxRelayEvent

	// 用于控制优雅启停
	ctx                 context.Context
	cancel              context.CancelFunc
	wg                  sync.WaitGroup
	mqProducer          MQProducer // 注入的 MQ 发送器
	posStore            PositionStore
	workerErrors        chan error
	publicationRecorder domain.OutboxPublicationRepository
}

func NewOutboxEventRelay(c *canal.Canal, mqProducer MQProducer, posStore PositionStore) *OutboxEventRelay {
	ctx, cancel := context.WithCancel(context.Background())
	return &OutboxEventRelay{
		canalInstance: c,
		eventChan:     make(chan *OutboxRelayEvent, 2000), // 设置 2000 的缓冲区，足以抵御普通的网络抖动
		ctx:           ctx,
		cancel:        cancel,
		mqProducer:    mqProducer,
		posStore:      posStore,
		workerErrors:  make(chan error, 1),
	}
}

// OnRow 核心回调：每当 MySQL 发生一行数据变更，底层就会调用这个方法
func (h *OutboxEventHandler) OnRow(e *canal.RowsEvent) error {
	if e == nil || e.Table == nil {
		return errors.New("outbox row metadata unavailable")
	}
	// ==========================================
	// 阶段一：三重过滤 (The Funnel) - 极速放行无关数据
	// ==========================================

	// 1. 动作过滤：发件箱只处理 INSERT，其他（UPDATE/DELETE）全扔掉
	if e.Action != canal.InsertAction {
		return nil
	}

	// 2. 库名过滤：确保是你推特业务所在的数据库 (根据你的实际库名修改)
	if e.Table.Schema != "twitter" {
		return nil
	}

	// 3. 表名过滤：我们的狙击目标只有 outbox_events 表
	if e.Table.Name != "outbox_events" {
		return nil
	}

	// ==========================================
	// 阶段二：动态寻址与数据提取 (Data Extraction)
	// ==========================================

	// ⚠️ 顶级避坑：绝对不用硬编码索引 (比如 rowData[2])
	// 万一 DBA 加了个字段，硬编码就全崩了。必须通过列名动态获取索引！
	payloadIdx := e.Table.FindColumn("payload")
	if payloadIdx == -1 {
		return errors.New("outbox payload column unavailable")
	}

	typeIdx := e.Table.FindColumn("event_type")
	if typeIdx == -1 {
		return errors.New("outbox event type column unavailable")
	}

	for _, rowData := range e.Rows {
		var eventID uint64
		if h.relay.publicationRecorder != nil {
			idColumn := e.Table.FindColumn("id")
			if idColumn < 0 || idColumn >= len(rowData) {
				return errors.New("publication event identity missing")
			}
			var err error
			eventID, err = strconv.ParseUint(fmt.Sprint(rowData[idColumn]), 10, 64)
			if err != nil || eventID == 0 {
				return errors.New("invalid publication event identity")
			}
		}
		if payloadIdx >= len(rowData) || typeIdx >= len(rowData) {
			return errors.New("invalid outbox row shape")
		}
		rawPayload := rowData[payloadIdx]
		rawType := rowData[typeIdx]

		// ==========================================
		// 阶段三：类型断言生死劫 (Safe Type Assertion)
		// ==========================================

		var payloadJSON string

		// MySQL 的 JSON/TEXT 字段，在 go-mysql 底层解析出来时，
		// 往往是 []byte，有时由于配置不同也可能是 string。
		// 必须严格用 switch type 保护，防止直接断言导致程序 Panic 宕机！
		switch v := rawPayload.(type) {
		case string:
			payloadJSON = v
		case []byte:
			payloadJSON = string(v)
		default:
			return errors.New("invalid outbox payload type")
		}

		var eventType string
		switch v := rawType.(type) {
		case string:
			eventType = v
		case []byte:
			eventType = string(v)
		default:
			return errors.New("invalid outbox event type")
		}

		// Payload may contain tweet content or direct identifiers. Keep it out of logs.
		log.Printf("[Canal Worker] captured outbox event type=%s", eventType)

		// ==========================================
		// 阶段四：投递与记录位点 (我们下一步要做的事)
		// ==========================================

		select {
		case h.relay.eventChan <- &OutboxRelayEvent{EventID: eventID, EventType: eventType, Payload: payloadJSON}:
		case <-h.relay.ctx.Done():
			return h.relay.ctx.Err()
		}
	}
	return nil
}

// Queue a commit boundary behind its rows. Saving the parser's current
// position could skip events still waiting for their publish confirmation.
func (h *OutboxEventHandler) OnPosSynced(_ *replication.EventHeader, pos mysql.Position, _ mysql.GTIDSet, _ bool) error {
	copyPosition := pos
	select {
	case h.relay.eventChan <- &OutboxRelayEvent{Position: &copyPosition}:
		return nil
	case <-h.relay.ctx.Done():
		return h.relay.ctx.Err()
	}
}

// Start 启动整个旁路系统
func (r *OutboxEventRelay) Start() error {
	// 1. 挂载 Handler
	r.canalInstance.SetEventHandler(&OutboxEventHandler{relay: r})

	// 2. 启动后台消费 Worker
	r.wg.Add(1)
	go r.runWorker()

	// 3. 启动 Canal (这个方法是阻塞的，通常要在 main 函数里单独跑)
	// return r.canalInstance.Run()
	return nil
}

// runWorker 核心消费逻辑
func (r *OutboxEventRelay) runWorker() {
	defer r.wg.Done()
	for {
		select {
		case event := <-r.eventChan:
			if event == nil {
				continue
			}
			if err := r.processRelayRecord(event); err != nil {
				log.Println("outbox relay stopped before unconfirmed boundary")
				if r.ctx.Err() == nil {
					select {
					case r.workerErrors <- err:
					default:
					}
				}
				r.cancel()
				return
			}
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *OutboxEventRelay) Errors() <-chan error { return r.workerErrors }

func (r *OutboxEventRelay) processRelayRecord(event *OutboxRelayEvent) error {
	if event.Position == nil {
		if err := r.publishWithRetryAndBackpressure(event); err != nil {
			return err
		}
		if r.publicationRecorder != nil {
			digest, err := domain.OutboxPayloadDigest(event.Payload)
			if err != nil {
				return err
			}
			return r.retryRelayOperation(func() error {
				ctx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
				defer cancel()
				return r.publicationRecorder.RecordPublication(ctx, domain.OutboxPublication{EventID: event.EventID, EventType: event.EventType, PayloadDigest: digest, ConfirmedAt: time.Now().UnixMilli()})
			})
		}
		return nil
	}
	return r.retryRelayOperation(func() error { return r.posStore.SavePosition(*event.Position) })
}

func (r *OutboxEventRelay) SetPublicationRecorder(recorder domain.OutboxPublicationRepository) {
	r.publicationRecorder = recorder
}

func (r *OutboxEventRelay) publishWithRetryAndBackpressure(event *OutboxRelayEvent) error {
	route, ok := routeRegistry[event.EventType]
	if !ok {
		return errors.New("unsupported outbox route; checkpoint withheld")
	}
	return r.retryRelayOperation(func() error {
		pubCtx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
		defer cancel()
		return r.mqProducer.PublishRawJSON(pubCtx, route.Exchange, route.RoutingKey, []byte(event.Payload))
	})
}

func (r *OutboxEventRelay) retryRelayOperation(operation func() error) error {
	delay := 100 * time.Millisecond
	for {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		if err := operation(); err == nil {
			return nil
		}
		log.Println("outbox relay operation failed; checkpoint withheld")
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-r.ctx.Done():
			timer.Stop()
			return r.ctx.Err()
		}
		delay *= 2
		if delay > 10*time.Second {
			delay = 10 * time.Second
		}
	}
}

// Stop 优雅关闭中继器并退出 Canal
func (r *OutboxEventRelay) Stop() {
	log.Println("🛑 正在停止 Canal 提取...")
	r.cancel() // Unblock queue callbacks before closing the parser.
	if r.canalInstance != nil {
		r.canalInstance.Close()
	}

	log.Println("🛑 正在触发中继工作协程优雅退出...")

	r.wg.Wait() // 3. 阻塞等待下游工作协程将残存事件发送完毕并退出
	log.Println("🔩 Canal relay stopped gracefully")
}

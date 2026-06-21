package canal

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-mysql-org/go-mysql/mysql"
)

// MQProducer MQ 消息生产者接口
type MQProducer interface {
	PublishRawJSON(ctx context.Context, exchange string, routingKey string, body []byte) error
}

// PositionStore Binlog 消费位点存储接口
type PositionStore interface {
	SavePosition(pos mysql.Position) error
	GetPosition() (mysql.Position, error)
}

// OutboxRelayEvent 中继通道传递的事件对象
type OutboxRelayEvent struct {
	EventType string
	Payload   string
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
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mqProducer MQProducer // 注入的 MQ 发送器
	posStore   PositionStore
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
	}
}

// OnRow 核心回调：每当 MySQL 发生一行数据变更，底层就会调用这个方法
func (h *OutboxEventHandler) OnRow(e *canal.RowsEvent) error {
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

	// 对于 INSERT 事件，新增的数据行永远在 e.Rows[0]
	rowData := e.Rows[0]

	// ⚠️ 顶级避坑：绝对不用硬编码索引 (比如 rowData[2])
	// 万一 DBA 加了个字段，硬编码就全崩了。必须通过列名动态获取索引！
	payloadIdx := e.Table.FindColumn("payload")
	if payloadIdx == -1 {
		log.Println("🔥 致命错误: outbox_events 表中未找到 payload 字段")
		return nil
	}

	typeIdx := e.Table.FindColumn("event_type")
	if typeIdx == -1 {
		log.Println("🔥 致命错误: outbox_events 表中未找到 event_type 字段")
		return nil
	}

	rawPayload := rowData[payloadIdx] // 此时它的类型是可怕的 interface{}
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
		log.Printf("⚠️ 丢弃异常事件: 预料之外的 payload 数据类型: %T\n", v)
		return nil // 遇到脏数据，记个日志放行，千万不要让整个程序挂掉
	}

	var eventType string
	switch v := rawType.(type) {
	case string:
		eventType = v
	case []byte:
		eventType = string(v)
	default:
		log.Printf("⚠️ 丢弃异常事件: 预料之外的 event_type 数据类型: %T\n", v)
		return nil
	}

	// 🎉 大功告成！你拿到了纯净的推文全景 JSON 数据！
	log.Printf("🚀 [Canal Worker] 成功拦截到发件箱事件! 类型: %s, 载荷: %s\n", eventType, payloadJSON)

	// ==========================================
	// 阶段四：投递与记录位点 (我们下一步要做的事)
	// ==========================================

	select {
	case h.relay.eventChan <- &OutboxRelayEvent{EventType: eventType, Payload: payloadJSON}:
		// 极速返回，让 Canal 继续去拉取下一条 Binlog
		return nil
	case <-h.relay.ctx.Done():
		log.Println("⚠️ 收到关闭信号，停止接收 Binlog 事件")
		return nil
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

	// 设定防抖计时器：每秒钟触发一次
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var uncommittedCount int

	for {
		select {
		case event := <-r.eventChan:
			// 引入反压与无限退避重试机制
			r.publishWithRetryAndBackpressure(event)

			// 增加未提交位点的计数
			uncommittedCount++

			// 3. 性能优化：如果堆积了 500 条还没到 1 秒，也强制提交一次，防止意外宕机丢失太多进度
			if uncommittedCount >= 500 {
				r.flushPosition()
				uncommittedCount = 0
			}

		case <-ticker.C:
			// 1 秒钟到了，如果发现有没提交的位点，统一落盘一次
			if uncommittedCount > 0 {
				r.flushPosition()
				uncommittedCount = 0
			}

		case <-r.ctx.Done():
			// 收到安全退出信号，清理并退出
			log.Println("🛑 Worker 正在安全退出...")
			r.flushPosition()
			return
		}
	}
}

// flushPosition 获取底层解析器的当前位点并持久化
func (r *OutboxEventRelay) flushPosition() {
	// 获取 Canal 当前在内存中已经成功解析到了哪一个 Binlog 位点
	// 如果你配置了 GTID 模式，这里拿到的是 GTIDSet
	pos := r.canalInstance.SyncedPosition()

	// 持久化到 Redis 或 DB，这样就算程序崩溃，下次 canal.Run() 也能传入这个位点继续跑
	if err := r.posStore.SavePosition(pos); err != nil {
		// 位点保存失败通常不致命，因为我们是“至少一次(At-Least-Once)”投递
		// 顶多重启后会有几条数据重复发送，下游业务做幂等即可
		log.Printf("⚠️ 位点持久化失败: %v", err)
	} else {
		log.Printf("💾 位点批量持久化成功: %v", pos)
	}
}

// publishWithRetryAndBackpressure 保证消息绝对送达，否则阻塞全世界
func (r *OutboxEventRelay) publishWithRetryAndBackpressure(event *OutboxRelayEvent) {
	baseDelay := 100 * time.Millisecond
	maxDelay := 10 * time.Second
	currentDelay := baseDelay

	// 根据事件类型动态映射 Exchange 和 RoutingKey
	var exchange, routingKey string
	switch event.EventType {
	case "TWEET_CREATED":
		exchange = "twitter.events"
		routingKey = "tweet.created"
	case "TWEET_DELETED":
		exchange = "twitter.events"
		routingKey = "tweet.deleted"
	default:
		// 丢弃未知事件，继续处理
		log.Printf("⚠️ 忽略未知事件类型: %s", event.EventType)
		return
	}

	for {
		// 1. 尝试发送到 RabbitMQ (使用动态映射的 Exchange 和 RoutingKey，带 5 秒超时保护)
		pubCtx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
		err := r.mqProducer.PublishRawJSON(pubCtx, exchange, routingKey, []byte(event.Payload))
		cancel()

		if err == nil {
			// 发送成功，跳出重试循环，恢复正常流动
			return
		}

		// 2. 发送失败，触发降级保护
		log.Printf("⚠️ MQ 投递失败，进入退避重试 (延迟: %v). 错误: %v", currentDelay, err)

		// 监听是否在重试期间收到了系统的退出信号
		select {
		case <-time.After(currentDelay):
			// 延迟结束，准备下一次循环重试
		case <-r.ctx.Done():
			// 系统正在关闭，直接退出，这条消息会因为没有 flushPosition 而在下次重启时重新被拉取
			return
		}

		// 3. 指数退避：每次失败延迟翻倍，但最大不超过 maxDelay
		currentDelay *= 2
		if currentDelay > maxDelay {
			currentDelay = maxDelay
		}
	}
}

// Stop 优雅关闭中继器并退出 Canal
func (r *OutboxEventRelay) Stop() {
	r.cancel()
	r.canalInstance.Close()
	r.wg.Wait()
	log.Println("✅ Canal relay stopped gracefully")
}

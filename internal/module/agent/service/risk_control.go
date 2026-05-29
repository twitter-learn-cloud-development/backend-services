package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"

	"twitter-clone/internal/events"
	"twitter-clone/internal/infrastructure/mq"
)

type RiskControl struct {
	mqClient       *mq.RabbitMQ
	temporalClient client.Client
}

func NewRiskControl(
	mqClient *mq.RabbitMQ,
	temporalClient client.Client,
) *RiskControl {
	return &RiskControl{
		mqClient:       mqClient,
		temporalClient: temporalClient,
	}
}

// Start 启动监听风控队列
func (r *RiskControl) Start(ctx context.Context) {
	log.Println("📥 Risk control listener starting...")
	messages, err := r.mqClient.Consume("queue.tweet.risk", "agent-risk-control")
	if err != nil {
		log.Printf("❌ Failed to consume risk queue: %v", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("⏹️  Risk control listener stopped")
			return
		case msg, ok := <-messages:
			if !ok {
				log.Println("⚠️ Risk control MQ channel closed, attempting reconnect in 5s...")
				time.Sleep(5 * time.Second)
				newMsgs, err := r.mqClient.Consume("queue.tweet.risk", "agent-risk-control")
				if err == nil {
					messages = newMsgs
				}
				continue
			}
			go r.handleRiskMessage(msg)
		}
	}
}

// handleRiskMessage 处理单条风控消息
func (r *RiskControl) handleRiskMessage(msg amqp.Delivery) {
	var acked bool
	defer func() {
		if !acked {
			// 异常退出或没有触发成功，退回重试
			msg.Nack(false, true)
		}
	}()

	var event events.TweetCreatedEvent
	if err := json.Unmarshal(msg.Body, &event); err != nil {
		log.Printf("❌ Failed to unmarshal risk event: %v", err)
		msg.Nack(false, false) // 格式错误直接丢弃
		acked = true
		return
	}

	log.Printf("🛡️ Dispatching risk check to Temporal for tweet_id=%d, author_id=%d", event.TweetID, event.AuthorID)

	// 1. 设置唯一 Workflow ID 实现去重幂等 (防 MQ 重复消费)
	workflowOptions := client.StartWorkflowOptions{
		ID:        fmt.Sprintf("RiskControl-Tweet-%d", event.TweetID),
		TaskQueue: "AGENT_TASK_QUEUE",
	}

	// 2. 触发风控自愈工作流
	we, err := r.temporalClient.ExecuteWorkflow(context.Background(), workflowOptions, TweetRiskControlWorkflow, event)
	if err != nil {
		// 3. 判断是否为重复启动（已被其他 consumer 触发）
		if temporal.IsWorkflowExecutionAlreadyStartedError(err) {
			log.Printf("ℹ️ Workflow RiskControl-Tweet-%d already started, ack duplicate MQ message", event.TweetID)
			msg.Ack(false)
			acked = true
			return
		}

		log.Printf("❌ Failed to trigger Temporal workflow for tweet_id=%d: %v", event.TweetID, err)
		// 不 ACK，退回 MQ 重试
		return
	}

	log.Printf("🚀 Successfully dispatched risk control workflow: ID=%s, RunID=%s", we.GetID(), we.GetRunID())

	// 4. 【核心生死线】确保只有在 Temporal 成功持久化工作流后，才 Ack MQ 消息
	msg.Ack(false)
	acked = true
}

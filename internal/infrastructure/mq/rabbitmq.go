package mq

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQConfig RabbitMQ 配置
type RabbitMQConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Vhost    string
}

// DefaultRabbitMQConfig 默认配置
func DefaultRabbitMQConfig() *RabbitMQConfig {
	return &RabbitMQConfig{
		Host:     getEnv("MQ_HOST", "127.0.0.1"),
		Port:     getEnvInt("MQ_PORT", 5672),
		User:     getEnv("MQ_USER", "guest"),
		Password: getEnv("MQ_PASSWORD", "guest"),
		Vhost:    getEnv("MQ_VHOST", "/"),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}

// RabbitMQ RabbitMQ 客户端
type RabbitMQ struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	config  *RabbitMQConfig
}

// NewRabbitMQ 创建 RabbitMQ 客户端
func NewRabbitMQ(config *RabbitMQConfig) (*RabbitMQ, error) {
	//构建连接字符串
	url := fmt.Sprintf("amqp://%s:%s@%s:%d%s", config.User, config.Password, config.Host, config.Port, config.Vhost)

	//连接 RabbitMQ
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to rabbitmq: %w", err)
	}

	//创建 Channel
	channel, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	return &RabbitMQ{
		conn:    conn,
		channel: channel,
		config:  config,
	}, nil
}

// Close 关闭连接
func (r *RabbitMQ) Close() error {
	if r.channel != nil {
		r.channel.Close()
	}

	if r.conn != nil {
		r.conn.Close()
	}
	return nil
}

// DeclareExchange 声明 Exchange
func (r *RabbitMQ) DeclareExchange(name, kind string, durable bool) error {
	return r.channel.ExchangeDeclare(
		name,    // name
		kind,    // type: direct, fanout, topic, headers
		durable, // durable
		false,   // auto-deleted
		false,   // internal
		false,   // no-wait
		nil,     // arguments
	)
}

// DeclareQueue 声明 Queue
func (r *RabbitMQ) DeclareQueue(name string, durable bool) (amqp.Queue, error) {
	return r.channel.QueueDeclare(
		name,    // name
		durable, // durable
		false,   // delete when unused
		false,   // exclusive
		false,   // no-wait
		nil,     // arguments
	)
}

// BindQueue 绑定 Queue 到 Exchange
func (r *RabbitMQ) BindQueue(queueName, routingKey, exchangeName string) error {
	return r.channel.QueueBind(
		queueName,    // queue name
		routingKey,   // routing key
		exchangeName, // exchange
		false,
		nil,
	)
}

// Publish 发布消息
func (r *RabbitMQ) Publish(ctx context.Context, exchange, routingKey string, body []byte) error {
	return r.channel.PublishWithContext(
		ctx,
		exchange,   // exchange
		routingKey, // routing key
		false,      // mandatory
		false,      // immediate
		amqp.Publishing{
			ContentType:  "application/test_data",
			Body:         body,
			DeliveryMode: amqp.Persistent, // 持久化消息
			Timestamp:    time.Now(),
		},
	)
}

// Consume 消费消息
func (r *RabbitMQ) Consume(queueName, consumer string) (<-chan amqp.Delivery, error) {
	return r.channel.Consume(
		queueName,
		consumer,
		false,
		false,
		false,
		false,
		nil,
	)
}

// SetQoS 设置 QoS（限流）
func (r *RabbitMQ) SetQoS(prefetchCount int) error {
	return r.channel.Qos(
		prefetchCount, // prefetch count
		0,             // prefetch size
		false,         // global
	)
}

// GetChannel 获取 Channel (用于高级操作)
func (r *RabbitMQ) GetChannel() *amqp.Channel {
	return r.channel
}

// Reconnect 重连（在连接断开时调用）
func (r *RabbitMQ) Reconnect() error {
	log.Println("🔄 Reconnecting to RabbitMQ...")

	// 关闭旧连接
	r.Close()

	// 重新连接
	url := fmt.Sprintf(
		"amqp://%s:%s@%s:%d%s",
		r.config.User,
		r.config.Password,
		r.config.Host,
		r.config.Port,
		r.config.Vhost,
	)

	conn, err := amqp.Dial(url)
	if err != nil {
		return fmt.Errorf("failed to reconnect: %w", err)
	}

	channel, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}

	r.conn = conn
	r.channel = channel

	log.Println("✅ Reconnected to RabbitMQ")
	return nil
}

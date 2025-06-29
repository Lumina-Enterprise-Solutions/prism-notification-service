package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
)


const (
	ExchangeName           = "prism_notifications_exchange"
	QueueName              = "notification_queue"
	DLXName                = "prism_dlx"
	DLQName                = "notification_dlq"
	RoutingKey             = "email_notification"
	DLQRoutingKey          = "dlq_key"
	ReconnectDelay         = 5 * time.Second
	ReconnectMaxAttempts   = 5
	PublishTimeout         = 5 * time.Second
	ContentTypeJSON        = "application/json"
	DeliveryModePersistent = amqp091.Persistent
)

type NotificationJob struct {
	RecipientUserID string                 `json:"recipient_user_id"`
	To              string                 `json:"to"`
	Subject         string                 `json:"subject"`
	TemplateName    string                 `json:"template_name"`
	TemplateData    map[string]interface{} `json:"template_data"`
}

// Interface baru untuk Channel RabbitMQ agar bisa di-mock
type AMQPChannel interface {
	PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp091.Publishing) error
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp091.Table) (<-chan amqp091.Delivery, error)
	Close() error
}

// Interface baru untuk Connection RabbitMQ agar bisa di-mock
type AMQPConnection interface {
	Channel() (AMQPChannel, error)
	Close() error
}

type Queue interface {
	Enqueue(ctx context.Context, job NotificationJob) error
	Consume(ctx context.Context, handler func(job NotificationJob) error) error
	Close() error
}

type RabbitMQQueueService struct {
	conn    AMQPConnection // Menggunakan interface
	channel AMQPChannel    // Menggunakan interface
}

var _ Queue = (*RabbitMQQueueService)(nil)

// wrapper agar amqp091.Connection memenuhi interface AMQPConnection
type amqpConnectionWrapper struct {
	*amqp091.Connection
}

func (w *amqpConnectionWrapper) Channel() (AMQPChannel, error) {
	return w.Connection.Channel()
}

func NewRabbitMQQueueService(amqpURL string) (Queue, error) {
	var conn *amqp091.Connection
	var err error

	for i := 0; i < ReconnectMaxAttempts; i++ {
		cfg := amqp091.Config{Properties: amqp091.NewConnectionProperties()}
		cfg.Properties.SetClientConnectionName("prism-notification-service")
		conn, err = amqp091.DialConfig(amqpURL, cfg)
		if err == nil {
			break
		}
		log.Warn().Err(err).Int("attempt", i+1).Msg("Gagal terhubung ke RabbitMQ, mencoba lagi...")
		time.Sleep(ReconnectDelay)
	}
	if err != nil {
		return nil, fmt.Errorf("gagal terhubung ke RabbitMQ setelah %d percobaan: %w", ReconnectMaxAttempts, err)
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("gagal membuka channel RabbitMQ: %w", err)
	}

	log.Info().Msg("Koneksi RabbitMQ berhasil dibuat.")
	return &RabbitMQQueueService{
		conn:    &amqpConnectionWrapper{conn},
		channel: ch,
	}, nil
}

func (s *RabbitMQQueueService) Close() error {
	var errs []error
	if s.channel != nil {
		if err := s.channel.Close(); err != nil {
			errs = append(errs, fmt.Errorf("gagal menutup channel RabbitMQ: %w", err))
		}
	}
	if s.conn != nil {
		if err := s.conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("gagal menutup koneksi RabbitMQ: %w", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("terjadi beberapa error saat menutup RabbitMQ: %v", errs)
	}
	return nil
}

func (s *RabbitMQQueueService) Enqueue(ctx context.Context, job NotificationJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("gagal marshal job ke JSON: %w", err)
	}

	publishCtx, cancel := context.WithTimeout(ctx, PublishTimeout)
	defer cancel()

	return s.channel.PublishWithContext(
		publishCtx,
		ExchangeName,
		RoutingKey,
		false,
		false,
		amqp091.Publishing{
			ContentType:  ContentTypeJSON,
			DeliveryMode: DeliveryModePersistent,
			Body:         payload,
		},
	)
}

func (s *RabbitMQQueueService) Consume(ctx context.Context, handler func(job NotificationJob) error) error {
	msgs, err := s.channel.Consume(
		QueueName,
		"notification-worker",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("gagal register consumer: %w", err)
	}

	log.Info().Str("queue", QueueName).Msg("Worker consumer RabbitMQ dimulai, menunggu pesan...")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Context dibatalkan, menghentikan consumer RabbitMQ.")
			return ctx.Err()
		case d, ok := <-msgs:
			if !ok {
				log.Warn().Msg("Channel konsumsi RabbitMQ ditutup.")
				return nil
			}

			var job NotificationJob
			if err := json.Unmarshal(d.Body, &job); err != nil {
				log.Error().Err(err).Msg("Gagal unmarshal job. Mengirim Nack dan drop pesan.")
				d.Nack(false, false)
				continue
			}

			err := handler(job)
			if err != nil {
				log.Error().Err(err).Str("user_id", job.RecipientUserID).Msg("Handler gagal memproses job. Mengirim Nack agar pesan di-DLQ.")
				d.Nack(false, false)
			} else {
				log.Info().Str("user_id", job.RecipientUserID).Msg("Job berhasil diproses. Mengirim Ack.")
				d.Ack(false)
			}
		}
	}
}

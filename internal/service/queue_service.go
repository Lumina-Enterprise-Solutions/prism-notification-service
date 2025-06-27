package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const (
	NotificationQueueKey = "notification_queue"
	NotificationDLQKey   = "notification_dlq" // <-- KEY BARU
)

// PERBAIKAN: Tambahkan field RecipientUserID
type NotificationJob struct {
	RecipientUserID string                 `json:"recipient_user_id"`
	To              string                 `json:"to"`
	Subject         string                 `json:"subject"`
	TemplateName    string                 `json:"template_name"` // <-- Ganti 'Body' dengan 'TemplateName'
	TemplateData    map[string]interface{} `json:"template_data"` // <-- Data dinamis untuk template
}

type Queue interface {
	Enqueue(ctx context.Context, job NotificationJob) error
	Dequeue(ctx context.Context) (*NotificationJob, error)
	EnqueueToDLQ(ctx context.Context, job NotificationJob) error
}

type QueueService struct {
	redisClient *redis.Client
}

var _ Queue = (*QueueService)(nil)

func NewQueueService(redisClient *redis.Client) Queue {
	return &QueueService{redisClient: redisClient}
}

func (s *QueueService) Enqueue(ctx context.Context, job NotificationJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return s.redisClient.LPush(ctx, NotificationQueueKey, payload).Err()
}

func (s *QueueService) Dequeue(ctx context.Context) (*NotificationJob, error) {
	result, err := s.redisClient.BRPop(ctx, 5*time.Second, NotificationQueueKey).Result()
	if err != nil {
		return nil, err // Error akan ditangani oleh worker (redis.Nil jika timeout)
	}

	// BRPop mengembalikan slice [key, value]
	if len(result) < 2 {
		return nil, fmt.Errorf("hasil BRPop tidak valid")
	}

	var job NotificationJob
	err = json.Unmarshal([]byte(result[1]), &job)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *QueueService) EnqueueToDLQ(ctx context.Context, job NotificationJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job for DLQ: %w", err)
	}
	log.Warn().Str("recipient", job.To).Msg("Moving job to Dead-Letter Queue")
	return s.redisClient.LPush(ctx, NotificationDLQKey, payload).Err()
}

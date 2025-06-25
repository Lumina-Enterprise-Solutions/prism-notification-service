package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const (
	NotificationQueueKey = "notification_queue"
	NotificationDLQKey   = "notification_dlq" // <-- KEY BARU
)

type NotificationJob struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type QueueService struct {
	redisClient *redis.Client
}

// Perubahan di sini: Menerima alamat redis dari config
func NewQueueService(redisAddr string) *QueueService {
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	return &QueueService{redisClient: client}
}

func (s *QueueService) Enqueue(ctx context.Context, job NotificationJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return s.redisClient.LPush(ctx, NotificationQueueKey, payload).Err()
}

func (s *QueueService) Dequeue(ctx context.Context) (*NotificationJob, error) {
	result, err := s.redisClient.BRPop(ctx, 0, NotificationQueueKey).Result()
	if err != nil {
		return nil, err
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
		// Error ini seharusnya tidak terjadi jika job-nya valid
		return fmt.Errorf("failed to marshal job for DLQ: %w", err)
	}
	log.Warn().Str("recipient", job.To).Msg("Moving job to Dead-Letter Queue")
	// Gunakan LPush ke key DLQ
	return s.redisClient.LPush(ctx, NotificationDLQKey, payload).Err()
}

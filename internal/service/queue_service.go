package service

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"
)

const NotificationQueueKey = "notification_queue"

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

package service

import (
	"context"
	"encoding/json"
	"os"

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

func NewQueueService() *QueueService {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "cache-redis:6379"
	}
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

// Dequeue menggunakan BRPOP untuk block sampai ada job baru, sangat efisien.
func (s *QueueService) Dequeue(ctx context.Context) (*NotificationJob, error) {
	// BRPOP akan menunggu (blocking) selama 0 detik (selamanya) sampai ada item di list.
	result, err := s.redisClient.BRPop(ctx, 0, NotificationQueueKey).Result()
	if err != nil {
		return nil, err
	}

	// result adalah slice [nama_key, nilai]
	var job NotificationJob
	err = json.Unmarshal([]byte(result[1]), &job)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

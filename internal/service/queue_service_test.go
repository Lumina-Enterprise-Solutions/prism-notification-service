package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time" // <-- Impor paket time

	"github.com/go-redis/redismock/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnqueue_Success tidak berubah dan sudah benar.
func TestEnqueue_Success(t *testing.T) {
	db, mock := redismock.NewClientMock()
	queueService := NewQueueService(db) // Menggunakan konstruktor agar konsisten
	job := NotificationJob{
		RecipientUserID: "user-123",
		To:              "test@example.com",
		Subject:         "Test Subject",
		TemplateName:    "welcome.html",
	}
	payload, err := json.Marshal(job)
	require.NoError(t, err)
	mock.ExpectLPush(NotificationQueueKey, payload).SetVal(1)
	err = queueService.Enqueue(context.Background(), job)
	assert.NoError(t, err, "Enqueue seharusnya tidak menghasilkan error")
	assert.NoError(t, mock.ExpectationsWereMet(), "Ekspektasi mock tidak terpenuhi")
}

// TestDequeue_Success diperbaiki.
func TestDequeue_Success(t *testing.T) {
	db, mock := redismock.NewClientMock()
	queueService := NewQueueService(db)

	job := NotificationJob{
		RecipientUserID: "user-456",
		To:              "another@example.com",
		Subject:         "Another Subject",
		TemplateName:    "password_reset.html",
	}
	payload, err := json.Marshal(job)
	require.NoError(t, err)

	// FIX: Sesuaikan timeout BRPop agar cocok dengan implementasi (5 detik).
	mock.ExpectBRPop(5*time.Second, NotificationQueueKey).SetVal([]string{NotificationQueueKey, string(payload)})

	dequeuedJob, err := queueService.Dequeue(context.Background())
	assert.NoError(t, err, "Dequeue seharusnya tidak menghasilkan error")
	require.NotNil(t, dequeuedJob, "Job yang di-dequeue tidak boleh nil")
	assert.Equal(t, job.RecipientUserID, dequeuedJob.RecipientUserID, "UserID tidak cocok")
	assert.Equal(t, job.To, dequeuedJob.To, "Penerima email tidak cocok")
	assert.NoError(t, mock.ExpectationsWereMet(), "Ekspektasi mock tidak terpenuhi")
}

// TestDequeue_RedisError diperbaiki.
func TestDequeue_RedisError(t *testing.T) {
	db, mock := redismock.NewClientMock()
	queueService := NewQueueService(db)

	expectedError := errors.New("koneksi redis putus")
	// FIX: Sesuaikan timeout BRPop agar cocok dengan implementasi (5 detik).
	mock.ExpectBRPop(5*time.Second, NotificationQueueKey).SetErr(expectedError)

	dequeuedJob, err := queueService.Dequeue(context.Background())

	assert.Error(t, err, "Dequeue seharusnya mengembalikan error")
	assert.Equal(t, expectedError, err, "Error yang dikembalikan tidak sesuai harapan")
	assert.Nil(t, dequeuedJob, "Job seharusnya nil saat terjadi error")
	assert.NoError(t, mock.ExpectationsWereMet(), "Ekspektasi mock tidak terpenuhi")
}

// TestEnqueueToDLQ_Success tidak berubah.
func TestEnqueueToDLQ_Success(t *testing.T) {
	db, mock := redismock.NewClientMock()
	queueService := NewQueueService(db)
	job := NotificationJob{
		RecipientUserID: "user-789",
		To:              "failed@example.com",
		Subject:         "Failed Job",
	}
	payload, err := json.Marshal(job)
	require.NoError(t, err)
	mock.ExpectLPush(NotificationDLQKey, payload).SetVal(1)
	err = queueService.EnqueueToDLQ(context.Background(), job)
	assert.NoError(t, err, "EnqueueToDLQ seharusnya tidak menghasilkan error")
	assert.NoError(t, mock.ExpectationsWereMet(), "Ekspektasi mock tidak terpenuhi")
}

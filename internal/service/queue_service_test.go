// File: notification-service/internal/service/queue_service_test.go
package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- Mocks untuk dependensi AMQP ---

// mockAcknowledger untuk mensimulasikan Ack/Nack
type mockAcknowledger struct{ mock.Mock }

func (m *mockAcknowledger) Ack(tag uint64, multiple bool) error {
	args := m.Called(tag, multiple)
	return args.Error(0)
}
func (m *mockAcknowledger) Nack(tag uint64, multiple bool, requeue bool) error {
	args := m.Called(tag, multiple, requeue)
	return args.Error(0)
}
func (m *mockAcknowledger) Reject(tag uint64, requeue bool) error {
	args := m.Called(tag, requeue)
	return args.Error(0)
}

// mockAMQPChannel untuk mensimulasikan channel RabbitMQ
type mockAMQPChannel struct{ mock.Mock }

func (m *mockAMQPChannel) PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp091.Publishing) error {
	args := m.Called(ctx, exchange, key, mandatory, immediate, msg)
	return args.Error(0)
}
func (m *mockAMQPChannel) Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp091.Table) (<-chan amqp091.Delivery, error) {
	calledArgs := m.Called(queue, consumer, autoAck, exclusive, noLocal, noWait, args)
	if calledArgs.Get(0) == nil {
		return nil, calledArgs.Error(1)
	}
	return calledArgs.Get(0).(<-chan amqp091.Delivery), calledArgs.Error(1)
}
func (m *mockAMQPChannel) Close() error {
	args := m.Called()
	return args.Error(0)
}

// Test untuk Enqueue (sudah ada, hanya untuk kelengkapan)
func TestRabbitMQ_Enqueue(t *testing.T) {
	mockChannel := new(mockAMQPChannel)
	service := &RabbitMQQueueService{channel: mockChannel}
	ctx := context.Background()

	job := NotificationJob{To: "test@example.com", Subject: "Test"}
	payload, _ := json.Marshal(job)

	mockChannel.On("PublishWithContext",
		mock.Anything, ExchangeName, RoutingKey, false, false,
		amqp091.Publishing{ContentType: ContentTypeJSON, DeliveryMode: DeliveryModePersistent, Body: payload},
	).Return(nil).Once()

	err := service.Enqueue(ctx, job)
	require.NoError(t, err)
	mockChannel.AssertExpectations(t)
}

// --- Tes BARU untuk Consumer ---

func TestRabbitMQ_Consume_Success(t *testing.T) {
	mockChannel := new(mockAMQPChannel)
	service := &RabbitMQQueueService{channel: mockChannel}

	// Buat channel untuk mengirim pesan tiruan
	deliveryChan := make(chan amqp091.Delivery, 1)
	job := NotificationJob{To: "test@example.com", Subject: "Consume Success"}
	payload, _ := json.Marshal(job)

	// Mock Acknowledger
	mockAck := new(mockAcknowledger)
	mockAck.On("Ack", uint64(1), false).Return(nil).Once() // Ekspektasi: Ack akan dipanggil

	// Kirim pesan ke channel
	deliveryChan <- amqp091.Delivery{
		Acknowledger: mockAck,
		DeliveryTag:  1,
		Body:         payload,
	}

	// Tutup channel setelah 1 pesan untuk menghentikan loop consumer
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(deliveryChan)
	}()

	mockChannel.On("Consume", QueueName, mock.Anything, false, false, false, false, mock.Anything).Return((<-chan amqp091.Delivery)(deliveryChan), nil).Once()

	// Handler tiruan yang selalu berhasil
	successfulHandler := func(j NotificationJob) error {
		assert.Equal(t, job.To, j.To)
		return nil
	}

	// Jalankan consumer. Ini akan berhenti setelah deliveryChan ditutup.
	err := service.Consume(context.Background(), successfulHandler)

	require.NoError(t, err)
	mockChannel.AssertExpectations(t)
	mockAck.AssertExpectations(t)
}

func TestRabbitMQ_Consume_HandlerFailure(t *testing.T) {
	mockChannel := new(mockAMQPChannel)
	service := &RabbitMQQueueService{channel: mockChannel}
	deliveryChan := make(chan amqp091.Delivery, 1)

	// Mock Acknowledger
	mockAck := new(mockAcknowledger)
	// Ekspektasi: Nack akan dipanggil dengan requeue=false
	mockAck.On("Nack", uint64(1), false, false).Return(nil).Once()

	deliveryChan <- amqp091.Delivery{
		Acknowledger: mockAck,
		DeliveryTag:  1,
		Body:         []byte(`{"to": "fail@example.com"}`),
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(deliveryChan)
	}()

	mockChannel.On("Consume", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return((<-chan amqp091.Delivery)(deliveryChan), nil).Once()

	// Handler tiruan yang selalu gagal
	failingHandler := func(j NotificationJob) error {
		return errors.New("processing failed")
	}

	err := service.Consume(context.Background(), failingHandler)

	require.NoError(t, err)
	mockChannel.AssertExpectations(t)
	mockAck.AssertExpectations(t)
}

func TestRabbitMQ_Consume_UnmarshalFailure(t *testing.T) {
	mockChannel := new(mockAMQPChannel)
	service := &RabbitMQQueueService{channel: mockChannel}
	deliveryChan := make(chan amqp091.Delivery, 1)

	mockAck := new(mockAcknowledger)
	// Ekspektasi: Nack akan dipanggil karena pesan korup
	mockAck.On("Nack", uint64(1), false, false).Return(nil).Once()

	deliveryChan <- amqp091.Delivery{
		Acknowledger: mockAck,
		DeliveryTag:  1,
		Body:         []byte(`this is not valid json`),
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(deliveryChan)
	}()

	mockChannel.On("Consume", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return((<-chan amqp091.Delivery)(deliveryChan), nil).Once()

	// Handler tidak akan pernah dipanggil
	var handlerCalled bool
	handler := func(j NotificationJob) error {
		handlerCalled = true
		return nil
	}

	err := service.Consume(context.Background(), handler)

	require.NoError(t, err)
	assert.False(t, handlerCalled, "Handler seharusnya tidak dipanggil untuk pesan yang korup")
	mockChannel.AssertExpectations(t)
	mockAck.AssertExpectations(t)
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Mock untuk AMQPChannel (tidak ada perubahan)
type mockAMQPChannel struct {
	mock.Mock
}

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

// Mock untuk AMQPConnection (tidak ada perubahan)
type mockAMQPConnection struct {
	mock.Mock
}

func (m *mockAMQPConnection) Channel() (AMQPChannel, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(AMQPChannel), args.Error(1)
}

func (m *mockAMQPConnection) Close() error {
	args := m.Called()
	return args.Error(0)
}

// Mock Acknowledger (tidak ada perubahan)
type mockAcknowledger struct{ mock.Mock }

func (m *mockAcknowledger) Ack(tag uint64, multiple bool) error                { return nil }
func (m *mockAcknowledger) Nack(tag uint64, multiple bool, requeue bool) error { return nil }
func (m *mockAcknowledger) Reject(tag uint64, requeue bool) error              { return nil }

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

func TestRabbitMQ_EnqueueFails(t *testing.T) {
	mockChannel := new(mockAMQPChannel)
	service := &RabbitMQQueueService{channel: mockChannel}
	ctx := context.Background()
	expectedError := errors.New("publish failed")
	job := NotificationJob{To: "test@example.com", Subject: "Test"}

	mockChannel.On("PublishWithContext", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(expectedError).Once()

	err := service.Enqueue(ctx, job)

	require.Error(t, err)
	assert.Equal(t, expectedError, err)
	mockChannel.AssertExpectations(t)
}

func TestRabbitMQ_Consume(t *testing.T) {
	mockChannel := new(mockAMQPChannel)
	service := &RabbitMQQueueService{channel: mockChannel}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deliveryChan := make(chan amqp091.Delivery, 1)
	job := NotificationJob{To: "test@example.com", Subject: "Consume Test"}
	payload, _ := json.Marshal(job)

	deliveryChan <- amqp091.Delivery{
		Acknowledger: &mockAcknowledger{},
		Body:         payload,
	}
	close(deliveryChan)

	// --- INI ADALAH PERBAIKAN UTAMA ---
	mockChannel.On(
		"Consume",
		QueueName,             // queue
		"notification-worker", // consumer
		false,                 // autoAck
		false,                 // exclusive
		false,                 // noLocal
		false,                 // noWait
		(amqp091.Table)(nil),  // args dengan tipe yang benar
	).Return((<-chan amqp091.Delivery)(deliveryChan), nil).Once()

	err := service.Consume(ctx, func(j NotificationJob) error {
		assert.Equal(t, job, j)
		return nil
	})

	assert.NoError(t, err)
	mockChannel.AssertExpectations(t)
}

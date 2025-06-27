package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings" // Impor strings
	"testing"
	"time"

	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/service"
	ws "github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/websocket"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak" // Impor goleak
)

// ... MockQueueService dan setupRouter tetap sama ...
type MockQueueService struct {
	EnqueueFunc    func(ctx context.Context, job service.NotificationJob) error
	DequeueFunc    func(ctx context.Context) (*service.NotificationJob, error)
	EnqueueDLQFunc func(ctx context.Context, job service.NotificationJob) error
}

func (m *MockQueueService) Enqueue(ctx context.Context, job service.NotificationJob) error {
	if m.EnqueueFunc != nil {
		return m.EnqueueFunc(ctx, job)
	}
	return nil
}
func (m *MockQueueService) Dequeue(ctx context.Context) (*service.NotificationJob, error) {
	if m.DequeueFunc != nil {
		return m.DequeueFunc(ctx)
	}
	return nil, nil
}
func (m *MockQueueService) EnqueueToDLQ(ctx context.Context, job service.NotificationJob) error {
	if m.EnqueueDLQFunc != nil {
		return m.EnqueueDLQFunc(ctx, job)
	}
	return nil
}

var _ service.Queue = (*MockQueueService)(nil)

func setupRouter(q service.Queue, h *ws.Hub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	handler := NewNotificationHandler(q, h)
	router.POST("/notifications/send", handler.SendNotification)
	return router
}

// FIX: Gabungkan semua test handler ke dalam satu TestMain untuk kontrol lifecycle
func TestMain(m *testing.M) {
	// Opsi untuk mengabaikan goroutine internal dari go-redis/redismock
	// yang mungkin muncul sebagai false positive.
	goleak.VerifyTestMain(m, goleak.IgnoreCurrent())
}

func TestSendNotification_Success(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()
	// ... (sisa test ini tetap sama)
	var enqueuedJob service.NotificationJob
	mockQueue := &MockQueueService{
		EnqueueFunc: func(ctx context.Context, job service.NotificationJob) error {
			enqueuedJob = job
			return nil
		},
	}
	router := setupRouter(mockQueue, hub)

	reqBody := SendNotificationRequest{RecipientID: "u1", Recipient: "t@e.com", Subject: "s", TemplateName: "tn"}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest(http.MethodPost, "/notifications/send", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusAccepted, rr.Code)
	assert.Equal(t, "u1", enqueuedJob.RecipientUserID)
}

func TestHandleWebSocket(t *testing.T) {
	defer goleak.VerifyNone(t)

	// 1. Buat Hub
	hub := ws.NewHub()
	go hub.Run()
	// Hub harus dihentikan terakhir
	defer hub.Stop()

	// 2. Buat handler
	handler := NewNotificationHandler(&MockQueueService{}, hub)

	// 3. Buat router dan server
	router := gin.New()
	router.GET("/ws", func(c *gin.Context) {
		c.Set("userID", "user-test")
	}, handler.HandleWebSocket) // Perhatikan, tidak ada c.Next() karena ini adalah handler terakhir

	server := httptest.NewServer(router)
	// Server harus ditutup sebelum hub
	defer server.Close()

	// 4. Buat koneksi
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	// Koneksi harus ditutup pertama kali
	defer conn.Close()

	time.Sleep(100 * time.Millisecond)
}

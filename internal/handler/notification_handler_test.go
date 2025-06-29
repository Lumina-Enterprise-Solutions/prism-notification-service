package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/auth"
	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/service"
	ws "github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/websocket"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redismock/v9"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// MockQueueService sekarang memiliki semua method yang dibutuhkan oleh service.Queue.
type MockQueueService struct {
	EnqueueFunc func(ctx context.Context, job service.NotificationJob) error
	ConsumeFunc func(ctx context.Context, handler func(job service.NotificationJob) error) error
	CloseFunc   func() error
}

func (m *MockQueueService) Enqueue(ctx context.Context, job service.NotificationJob) error {
	if m.EnqueueFunc != nil {
		return m.EnqueueFunc(ctx, job)
	}
	return nil
}

func (m *MockQueueService) Consume(ctx context.Context, handler func(job service.NotificationJob) error) error {
	if m.ConsumeFunc != nil {
		return m.ConsumeFunc(ctx, handler)
	}
	return nil
}

func (m *MockQueueService) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
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

func setupRouterWithRealMiddleware(q service.Queue, h *ws.Hub, redisClient *redis.Client) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	handler := NewNotificationHandler(q, h)
	jwtAuthMiddleware := auth.JWTMiddleware(redisClient)
	router.GET("/ws", jwtAuthMiddleware, handler.HandleWebSocket)
	return router
}

func TestSendNotification_Success(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

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
	t.Setenv("JWT_SECRET_KEY", "test-secret-for-ws")

	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	redisClient, redisMock := redismock.NewClientMock()
	defer func() {
		if err := redisClient.Close(); err != nil {
			t.Logf("failed to close redis mock client: %v", err)
		}
	}()

	router := setupRouterWithRealMiddleware(&MockQueueService{}, hub, redisClient)
	server := httptest.NewServer(router)
	defer server.Close()

	userID := "user-ws-test"
	jti := "unique-jwt-id-for-test"
	tokenString, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": userID,
		"jti": jti,
		"exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte("test-secret-for-ws"))
	require.NoError(t, err)

	redisMock.ExpectGet(jti).RedisNil()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	header := http.Header{}
	header.Add("Authorization", "Bearer "+tokenString)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	require.NoError(t, err, "Handshake WebSocket seharusnya berhasil")

	if resp != nil {
		assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("failed to close response body: %v", err)
			}
		}()
	}
	require.NotNil(t, conn)
	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("failed to close websocket connection: %v", err)
		}
	}()

	time.Sleep(50 * time.Millisecond)
	assert.True(t, hub.IsClientRegistered(userID), "Klien harus terdaftar setelah handshake")
	require.NoError(t, redisMock.ExpectationsWereMet())
}

// func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
// 	c := make(chan struct{})
// 	go func() {
// 		defer close(c)
// 		wg.Wait()
// 	}()
// 	select {
// 	case <-c:
// 		return false // completed normally
// 	case <-time.After(timeout):
// 		return true // timed out
// 	}
// }

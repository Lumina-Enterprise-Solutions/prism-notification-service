package handler

import (
	"log"
	"net/http"

	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/service"
	ws "github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/websocket"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type NotificationHandler struct {
	queueService service.Queue
	hub          *ws.Hub
}

func NewNotificationHandler(queueService service.Queue, hub *ws.Hub) *NotificationHandler {
	return &NotificationHandler{
		queueService: queueService,
		hub:          hub,
	}
}

type SendNotificationRequest struct {
	RecipientID  string                 `json:"recipient_id" binding:"required"`
	Recipient    string                 `json:"recipient" binding:"required,email"`
	Subject      string                 `json:"subject" binding:"required"`
	TemplateName string                 `json:"template_name" binding:"required"`
	TemplateData map[string]interface{} `json:"template_data"`
}

func (h *NotificationHandler) SendNotification(c *gin.Context) {
	var req SendNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	job := service.NotificationJob{
		RecipientUserID: req.RecipientID,
		To:              req.Recipient,
		Subject:         req.Subject,
		TemplateName:    req.TemplateName,
		TemplateData:    req.TemplateData,
	}
	err := h.queueService.Enqueue(c.Request.Context(), job)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enqueue notification"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"message": "Notification accepted for processing"})
}

func (h *NotificationHandler) HandleWebSocket(c *gin.Context) {
	userIDValue, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}

	userID, ok := userIDValue.(string)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user ID in context"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection for user %s: %v", userID, err)
		return
	}

	// FIX: Buat client dengan koneksi asli (*websocket.Conn).
	// Ini valid karena *websocket.Conn secara implisit memenuhi interface ws.WSConnection.
	client := &ws.Client{UserID: userID, Conn: conn}
	h.hub.Register(client)

	defer func() {
		h.hub.Unregister(client)
		conn.Close() // Pastikan koneksi juga ditutup saat handler selesai.
	}()

	// Loop ini diperlukan untuk menjaga koneksi tetap hidup dan mendeteksi penutupan.
	// Tanpa ini, defer akan langsung dieksekusi dan koneksi ditutup.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Unexpected WebSocket close error for user %s: %v", userID, err)
			}
			break
		}
	}
}

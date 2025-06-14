// file: services/prism-notification-service/internal/handler/notification_handler.go
package handler

import (
	"net/http"

	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/service"
	"github.com/gin-gonic/gin"
)

type NotificationHandler struct {
	queueService *service.QueueService // <-- Ganti dari emailService ke queueService
}

func NewNotificationHandler(queueService *service.QueueService) *NotificationHandler { // <-- UBAH
	return &NotificationHandler{queueService: queueService}
}

type SendNotificationRequest struct {
	Recipient string `json:"recipient" binding:"required,email"`
	Subject   string `json:"subject" binding:"required"`
	Message   string `json:"message" binding:"required"`
}

func (h *NotificationHandler) SendNotification(c *gin.Context) {
	var req SendNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	job := service.NotificationJob{
		To:      req.Recipient,
		Subject: req.Subject,
		Body:    req.Message,
	}
	err := h.queueService.Enqueue(c.Request.Context(), job)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enqueue notification"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Notification accepted for processing"})
}

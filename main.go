// services/prism-notification-service/main.go (FIXED)
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/auth"
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/client"
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/enhanced_logger"
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/telemetry"
	notifconfig "github.com/Lumina-Enterprise-Solutions/prism-notification-service/config"
	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/handler"
	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/service"
	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/websocket"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	ginprometheus "github.com/zsais/go-gin-prometheus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// setupDependencies hanya fokus pada loading rahasia yang dibutuhkan.
func setupDependencies(cfg *notifconfig.Config, logger zerolog.Logger) error {
	vaultClient, err := client.NewVaultClient(cfg.VaultAddr, cfg.VaultToken)
	if err != nil {
		return fmt.Errorf("gagal membuat klien Vault: %w", err)
	}
	secretPath := "secret/data/prism"
	requiredSecrets := []string{
		"mailtrap_host", "mailtrap_port", "mailtrap_user", "mailtrap_pass",
	}
	if err := vaultClient.LoadSecretsToEnv(secretPath, requiredSecrets...); err != nil {
		return fmt.Errorf("gagal memuat kredensial Mailtrap dari Vault: %w", err)
	}
	logger.Info().Msg("Kredensial Mailtrap berhasil dimuat dari Vault.")
	return nil
}

// createJobHandler adalah closure yang membuat handler fungsi untuk memproses job RabbitMQ.
// Ini memungkinkan injeksi dependensi (EmailService, Hub) ke dalam logic worker.
func createJobHandler(es *service.EmailService, hub *websocket.Hub, logger zerolog.Logger) func(job service.NotificationJob) error {
	const maxRetries = 3
	const retryDelay = 20 * time.Second

	return func(job service.NotificationJob) error {
		log := logger.With().Str("user_id", job.RecipientUserID).Str("subject", job.Subject).Logger()
		log.Info().Msg("Memproses job notifikasi")

		if hub.SendToUser(job.RecipientUserID, map[string]string{"type": "new_notification", "subject": job.Subject}) {
			log.Info().Msg("Notifikasi terkirim via WebSocket")
		}

		var sendErr error
		for i := 0; i < maxRetries; i++ {
			sendErr = es.Send(job.To, job.Subject, job.TemplateName, job.TemplateData)
			if sendErr == nil {
				log.Info().Msg("Email berhasil terkirim")
				return nil // Sukses, pesan akan di-ack.
			}
			log.Warn().Err(sendErr).Int("attempt", i+1).Msg("Gagal mengirim email, mencoba lagi...")
			if i < maxRetries-1 {
				time.Sleep(retryDelay)
			}
		}

		// Jika semua percobaan gagal, kembalikan error. Pesan akan di-nack dan masuk DLQ.
		return fmt.Errorf("job gagal setelah %d percobaan: %w", maxRetries, sendErr)
	}
}

func main() {
	// === Inisialisasi & Konfigurasi ===
	enhanced_logger.Init()
	serviceLogger := enhanced_logger.WithService("prism-notification-service")
	cfg := notifconfig.Load()

	enhanced_logger.LogStartup(cfg.ServiceName, cfg.Port, map[string]interface{}{
		"jaeger_endpoint": cfg.JaegerEndpoint,
		"redis_addr":      cfg.RedisAddr,
		"rabbitmq_url":    cfg.RabbitMQURL,
	})

	tp, err := telemetry.InitTracerProvider(cfg.ServiceName, cfg.JaegerEndpoint)
	if err != nil {
		serviceLogger.Fatal().Err(err).Msg("Gagal menginisialisasi OTel tracer provider")
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			serviceLogger.Error().Err(err).Msg("Error saat mematikan tracer provider")
		}
	}()

	if err := setupDependencies(cfg, serviceLogger); err != nil {
		serviceLogger.Fatal().Err(err).Msg("Gagal menginisialisasi dependensi")
	}

	// === Setup Komponen Inti ===
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer func() {
		if err := redisClient.Close(); err != nil {
			serviceLogger.Error().Err(err).Msg("Gagal menutup koneksi Redis dengan benar")
		}
	}()

	hub := websocket.NewHub()
	go hub.Run()
	defer hub.Stop()

	emailService := service.NewEmailService()

	// Inisialisasi RabbitMQ Queue Service
	queueService, err := service.NewRabbitMQQueueService(cfg.RabbitMQURL)
	if err != nil {
		serviceLogger.Fatal().Err(err).Msg("Gagal menginisialisasi Queue Service (RabbitMQ)")
	}
	defer func() {
		if err := queueService.Close(); err != nil {
			serviceLogger.Error().Err(err).Msg("Gagal menutup koneksi RabbitMQ dengan benar")
		}
	}()

	notificationHandler := handler.NewNotificationHandler(queueService, hub)

	// === Jalankan Worker Background (Consumer) ===
	workerCtx, workerCancel := context.WithCancel(context.Background())
	jobHandler := createJobHandler(emailService, hub, serviceLogger)

	go func() {
		if err := queueService.Consume(workerCtx, jobHandler); err != nil && !errors.Is(err, context.Canceled) {
			serviceLogger.Fatal().Err(err).Msg("RabbitMQ consumer berhenti karena error tak terduga")
		}
	}()

	// === Setup Server HTTP ===
	portStr := strconv.Itoa(cfg.Port)
	router := gin.Default()
	router.Use(otelgin.Middleware(cfg.ServiceName))

	p := ginprometheus.NewPrometheus("gin")
	p.Use(router)

	// ## PERBAIKAN: Suntikkan redisClient ke dalam JWTMiddleware ##
	jwtAuthMiddleware := auth.JWTMiddleware(redisClient)

	notificationRoutes := router.Group("/notifications")
	{
		notificationRoutes.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "healthy"}) })
		notificationRoutes.POST("/send", notificationHandler.SendNotification)
		notificationRoutes.GET("/ws", jwtAuthMiddleware, notificationHandler.HandleWebSocket)
	}

	srv := &http.Server{
		Addr:    ":" + portStr,
		Handler: router,
	}

	go func() {
		serviceLogger.Info().Msgf("Memulai server HTTP di port %s", portStr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serviceLogger.Fatal().Err(err).Msg("Server HTTP gagal berjalan")
		}
	}()

	// === Graceful Shutdown ===
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	serviceLogger.Info().Msg("Sinyal shutdown diterima, memulai graceful shutdown...")

	// 1. Batalkan konteks worker agar loop Consume berhenti.
	workerCancel()

	// 2. Beri waktu sedikit agar consumer bisa menyelesaikan ack/nack pesan terakhir.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// 3. Matikan server HTTP.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		serviceLogger.Fatal().Err(err).Msg("Server terpaksa dimatikan")
	}

	enhanced_logger.LogShutdown(cfg.ServiceName)
}

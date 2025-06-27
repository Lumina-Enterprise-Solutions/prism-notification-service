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
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/enhanced_logger" // Ganti nama impor agar lebih pendek
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/telemetry"
	notifconfig "github.com/Lumina-Enterprise-Solutions/prism-notification-service/config"
	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/handler"
	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/service"
	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/websocket"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog" // Impor zerolog untuk menggunakan tipenya
	ginprometheus "github.com/zsais/go-gin-prometheus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// FIX: Ganti enhanced_logger.Logger menjadi zerolog.Logger
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

func main() {
	// === Inisialisasi ===
	enhanced_logger.Init() // Tetap panggil Init() untuk setup global
	serviceLogger := enhanced_logger.WithService("prism-notification-service")
	cfg := notifconfig.Load()

	enhanced_logger.LogStartup(cfg.ServiceName, cfg.Port, map[string]interface{}{
		"jaeger_endpoint": cfg.JaegerEndpoint,
		"redis_addr":      cfg.RedisAddr,
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
			serviceLogger.Error().Err(err).Msg("Error saat menutup klien Redis")
		}
	}()

	hub := websocket.NewHub()
	go hub.Run()

	emailService := service.NewEmailService()
	queueService := service.NewQueueService(redisClient) // FIX: Pass Redis client yang sudah ada
	notificationHandler := handler.NewNotificationHandler(queueService, hub)

	// === Jalankan Worker Background ===
	workerCtx, workerCancel := context.WithCancel(context.Background())
	go runWorker(workerCtx, queueService, emailService, hub, serviceLogger)

	// === Setup Server HTTP ===
	portStr := strconv.Itoa(cfg.Port)
	router := gin.Default()
	router.Use(otelgin.Middleware(cfg.ServiceName))
	p := ginprometheus.NewPrometheus("gin")
	p.Use(router)

	// FIX: Gunakan redisClient yang sama untuk JWT middleware
	jwtAuthMiddleware := auth.JWTMiddleware(redisClient)

	// --- Rute API ---
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

	workerCancel()
	hub.Stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		serviceLogger.Fatal().Err(err).Msg("Server terpaksa dimatikan")
	}

	enhanced_logger.LogShutdown(cfg.ServiceName)
}

// FIX: Ubah tipe EmailSender ke tipe konkret *service.EmailService dan Logger ke zerolog.Logger
func runWorker(ctx context.Context, qs service.Queue, es *service.EmailService, hub *websocket.Hub, logger zerolog.Logger) {
	logger.Info().Msg("Worker antrian notifikasi dimulai...")
	const maxRetries = 3
	const retryDelay = 20 * time.Second

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("Worker antrian notifikasi berhenti.")
			return
		default:
			job, err := qs.Dequeue(ctx)
			if err != nil {
				if !errors.Is(err, redis.Nil) && !errors.Is(err, context.Canceled) {
					logger.Error().Err(err).Msg("Gagal mengambil job dari antrian, mencoba lagi...")
					time.Sleep(5 * time.Second)
				}
				continue
			}

			logger.Info().Str("recipient_id", job.RecipientUserID).Str("subject", job.Subject).Msg("Memproses job notifikasi")

			if hub.SendToUser(job.RecipientUserID, map[string]string{"type": "new_notification", "subject": job.Subject}) {
				logger.Info().Str("user_id", job.RecipientUserID).Msg("Notifikasi terkirim via WebSocket")
			}

			var sendErr error
			for i := 0; i < maxRetries; i++ {
				sendErr = es.Send(job.To, job.Subject, job.TemplateName, job.TemplateData)
				if sendErr == nil {
					break
				}
				logger.Warn().Err(sendErr).Int("attempt", i+1).Msg("Gagal mengirim email, mencoba lagi...")
				if i < maxRetries-1 {
					time.Sleep(retryDelay)
				}
			}

			if sendErr != nil {
				logger.Error().Err(sendErr).Msg("Job gagal setelah semua percobaan, dipindahkan ke DLQ")
				_ = qs.EnqueueToDLQ(context.Background(), *job)
			}
		}
	}
}
